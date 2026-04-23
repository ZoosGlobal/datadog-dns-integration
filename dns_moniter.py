# checks.d/dns_monitor.py
# ============================================================
# Zoos Global — Microsoft DNS Monitor for Datadog
# https://www.zoosglobal.com
#
# Datadog Agent custom check — runs every 60s via checks.d
# No binary, no Go, no Task Scheduler, no Defender issues.
#
# Collection strategy:
#   Primary  : pywin32 (WMI + win32pdh + win32service)
#   Fallback : subprocess PowerShell / sc.exe / raw socket
#   Always   : Python stdlib socket (forwarder + resolution probes)
#
# Metric categories:
#   dns.service.*       — service health
#   dns.performance.*   — PDH perfmon counters
#   dns.queries.*       — query type breakdown (Get-DnsServerStatistics)
#   dns.errors.*        — NXDOMAIN, SERVFAIL, refused, etc.
#   dns.recursion.*     — recursive resolution stats
#   dns.cache.*         — cache hit ratio, size, memory
#   dns.updates.*       — dynamic update stats
#   dns.security.*      — TSIG, TKEY, context queue
#   dns.dnssec.*        — DNSSEC validation stats
#   dns.forwarders.*    — forwarder availability + latency
#   dns.resolution.*    — resolution response time
#   dns.zones.*         — zone health
#   dns.process.*       — dns.exe process metrics
#   dns.events.*        — Windows Event Log signals
#   dns.scavenging.*    — scavenging health
#   dns.monitor.*       — self-monitoring
# ============================================================

from __future__ import annotations

import json
import os
import random
import socket
import string
import struct
import subprocess
import time
from typing import Any, Dict, List, Optional, Tuple

from datadog_checks.base import AgentCheck

# ── pywin32 availability flag ─────────────────────────────────────────────────
try:
    import win32service
    import win32serviceutil
    import win32con
    import win32pdh
    import win32pdhutil
    import wmi as _wmi
    _WMI_CLIENT = None
    PYWIN32_OK = True
except ImportError:
    PYWIN32_OK = False


def _get_wmi():
    """Lazy WMI client — reused across calls within one check cycle."""
    global _WMI_CLIENT
    if PYWIN32_OK and _WMI_CLIENT is None:
        try:
            _WMI_CLIENT = _wmi.WMI()
        except Exception:
            pass
    return _WMI_CLIENT


# ── Safe PowerShell runner ────────────────────────────────────────────────────
def _ps(script: str, timeout: int = 10) -> str:
    """Run a PowerShell one-liner and return stdout. Returns '' on failure."""
    try:
        result = subprocess.run(
            ['powershell.exe', '-NonInteractive', '-NoProfile',
             '-ExecutionPolicy', 'Bypass', '-Command', script],
            capture_output=True, text=True, timeout=timeout
        )
        return result.stdout.strip()
    except Exception:
        return ''


# ── Raw DNS UDP probe ─────────────────────────────────────────────────────────
def _random_label(n: int = 12) -> str:
    return ''.join(random.choices(string.ascii_lowercase + string.digits, k=n))


def _build_dns_query(domain: str, tx_id: int) -> bytes:
    """Build a minimal DNS A-record query packet (RFC 1035)."""
    buf = struct.pack('>HHHHHH', tx_id, 0x0100, 1, 0, 0, 0)
    for label in domain.rstrip('.').split('.'):
        buf += struct.pack('B', len(label)) + label.encode()
    buf += b'\x00\x00\x01\x00\x01'
    return buf


def _is_valid_dns_response(data: bytes, tx_id: int) -> bool:
    """Return True if data is a valid DNS response (NXDOMAIN counts as UP)."""
    if len(data) < 12:
        return False
    resp_id, flags = struct.unpack_from('>HH', data, 0)
    qr    = (flags >> 15) & 0x1
    rcode = flags & 0x000F
    return qr == 1 and resp_id == tx_id and rcode != 2  # 2=SERVFAIL


def _probe_dns(server: str, base_domain: str, timeout: float = 5.0) -> Tuple[bool, float]:
    """
    Send a cache-busting DNS query to server:53 and measure RTT.
    Returns (success, latency_ms).
    NXDOMAIN = server UP. Timeout/SERVFAIL = server DOWN.
    """
    tx_id  = random.randint(1, 0xFFFE)
    target = f'{_random_label()}.{base_domain}'
    query  = _build_dns_query(target, tx_id)

    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.settimeout(timeout)
        t0 = time.monotonic()
        sock.sendto(query, (server, 53))
        data, _ = sock.recvfrom(512)
        elapsed = (time.monotonic() - t0) * 1000
        sock.close()
        return _is_valid_dns_response(data, tx_id), round(elapsed, 2)
    except Exception:
        return False, 0.0
    finally:
        try:
            sock.close()
        except Exception:
            pass


# ── PDH counter reader ────────────────────────────────────────────────────────
# Maps metric name → PDH English counter path
_PDH_COUNTERS = {
    'dns.performance.queries_received_total':          r'\DNS\Total Query Received',
    'dns.performance.responses_sent_total':            r'\DNS\Total Response Sent',
    'dns.performance.udp_queries_total':               r'\DNS\UDP Query Received',
    'dns.performance.tcp_queries_total':               r'\DNS\TCP Query Received',
    'dns.performance.recursive_queries_total':         r'\DNS\Recursive Queries',
    'dns.performance.recursive_query_failures_total':  r'\DNS\Recursive Query Failure',
    'dns.performance.recursive_query_timeouts_total':  r'\DNS\Recursive TimeOut',
    'dns.performance.dynamic_updates_total':           r'\DNS\Dynamic Update Received',
    'dns.performance.secure_updates_total':            r'\DNS\Secure Update Received',
    'dns.performance.zone_transfer_requests_total':    r'\DNS\Zone Transfer Request Received',
    'dns.performance.zone_transfer_success_total':     r'\DNS\Zone Transfer Success',
    'dns.performance.zone_transfer_failures_total':    r'\DNS\Zone Transfer Failure',
    'dns.performance.notify_sent_total':               r'\DNS\Notify Sent',
    'dns.performance.notify_received_total':           r'\DNS\Notify Received',
    'dns.performance.unmatched_responses_total':       r'\DNS\Unmatched Responses Received',
}


def _read_pdh_counters(log) -> Dict[str, float]:
    """
    Read DNS PDH counters via typeperf.exe subprocess.
    typeperf works under all user contexts including the Datadog Agent
    service account — unlike win32pdh which fails with restricted permissions.
    """
    results = {}
    paths = list(_PDH_COUNTERS.values())

    try:
        cmd = ['typeperf.exe'] + paths + ['-sc', '1']
        proc = subprocess.run(
            cmd, capture_output=True, text=True, timeout=20,
            creationflags=0x08000000  # CREATE_NO_WINDOW — suppress console
        )
        out = proc.stdout

        # typeperf output format:
        #   "Timestamp","\DNS\Total Query Received",...
        #   "04/22/2026 15:00:00.000","12345",...
        data_lines = [
            l for l in out.splitlines()
            if l.strip().startswith('"') and ',' in l
            and not l.strip().startswith('"Timestamp"')
        ]

        if not data_lines:
            log.warning('[dns_monitor] typeperf: no data lines in output')
            return results

        vals  = [v.strip().strip('"') for v in data_lines[-1].split(',')[1:]]
        names = list(_PDH_COUNTERS.keys())

        for i, name in enumerate(names):
            if i < len(vals) and vals[i]:
                try:
                    v = float(vals[i])
                    if v >= 0:  # typeperf returns -1 for unavailable counters
                        results[name] = v
                except ValueError:
                    pass

        log.debug(f'[dns_monitor] typeperf: {len(results)}/{len(names)} counters collected')

    except subprocess.TimeoutExpired:
        log.warning('[dns_monitor] typeperf timed out')
    except Exception as e:
        log.warning(f'[dns_monitor] typeperf failed: {e}')

    return results

# ── DNS Server Statistics (Get-DnsServerStatistics) ──────────────────────────
def _read_dns_statistics(log) -> Dict[str, Any]:
    """
    Read all DNS server statistics via PowerShell Get-DnsServerStatistics.
    Returns a flat dict of property_name → value.
    Uses ConvertTo-Json for reliable parsing.
    """
    script = r"""
$s = Get-DnsServerStatistics -ErrorAction SilentlyContinue
if ($null -eq $s) { Write-Output '{}'; exit }
$out = @{}

# Query2Statistics
$q = $s.Query2Statistics
if ($q) {
    $out['q_TotalQueries']  = [int64]$q.TotalQueries
    $out['q_TypeA']         = [int64]$q.TypeA
    $out['q_TypeAaaa']      = try { [int64]$q.TypeAaaa }    catch { 0 }
    $out['q_TypePtr']       = try { [int64]$q.TypePtr }     catch { 0 }
    $out['q_TypeMx']        = try { [int64]$q.TypeMx }      catch { 0 }
    $out['q_TypeSrv']       = try { [int64]$q.TypeSrv }     catch { 0 }
    $out['q_TypeNs']        = try { [int64]$q.TypeNs }      catch { 0 }
    $out['q_TypeTxt']       = try { [int64]$q.TypeTxt }     catch { 0 }
    $out['q_TypeSoa']       = try { [int64]$q.TypeSoa }     catch { 0 }
    $out['q_TypeOther']     = try { [int64]$q.TypeOther }   catch { 0 }
    $out['q_Update']        = try { [int64]$q.Update }      catch { 0 }
}

# ErrorStatistics
$e = $s.ErrorStatistics
if ($e) {
    $out['e_NxDomain']   = try { [int64]$e.NxDomain }   catch { 0 }
    $out['e_ServFail']   = try { [int64]$e.ServFail }    catch { 0 }
    $out['e_Refused']    = try { [int64]$e.Refused }     catch { 0 }
    $out['e_FormError']  = try { [int64]$e.FormError }   catch { 0 }
    $out['e_NotImpl']    = try { [int64]$e.NotImpl }     catch { 0 }
    $out['e_NxRRSet']    = try { [int64]$e.NxRRSet }     catch { 0 }
}

# RecursionStatistics
$r = $s.RecursionStatistics
if ($r) {
    $out['r_QueriesRecursed']          = try { [int64]$r.QueriesRecursed }          catch { 0 }
    $out['r_Forwards']                 = try { [int64]$r.Forwards }                 catch { 0 }
    $out['r_RecursionFailure']         = try { [int64]$r.RecursionFailure }         catch { 0 }
    $out['r_TimedoutQueries']          = try { [int64]$r.TimedoutQueries }          catch { 0 }
    $out['r_Responses']                = try { [int64]$r.Responses }                catch { 0 }
    $out['r_Retries']                  = try { [int64]$r.Retries }                  catch { 0 }
    $out['r_DuplicateCoalesedQueries'] = try { [int64]$r.DuplicateCoalesedQueries } catch { 0 }
    $out['r_ServerNotAuthNoData']      = try { [int64]$r.ServerNotAuthNoData }      catch { 0 }
}

# RecordStatistics (cache)
$c = $s.RecordStatistics
if ($c) {
    $out['c_CacheCurrent']  = try { [int64]$c.CacheCurrent }  catch { 0 }
    $out['c_InUse']         = try { [int64]$c.InUse }         catch { 0 }
    $out['c_Memory']        = try { [int64]$c.Memory }        catch { 0 }
    $out['c_Return']        = try { [int64]$c.Return }        catch { 0 }
    $out['c_CacheTimeouts'] = try { [int64]$c.CacheTimeouts } catch { 0 }
}

# UpdateStatistics
$u = $s.UpdateStatistics
if ($u) {
    $out['u_Received']      = try { [int64]$u.Received }      catch { 0 }
    $out['u_Completed']     = try { [int64]$u.Completed }     catch { 0 }
    $out['u_Rejected']      = try { [int64]$u.Rejected }      catch { 0 }
    $out['u_Refused']       = try { [int64]$u.Refused }       catch { 0 }
    $out['u_Timeout']       = try { [int64]$u.Timeout }       catch { 0 }
    $out['u_DsWriteFailure']= try { [int64]$u.DsWriteFailure } catch { 0 }
    $out['u_SecureSuccess'] = try { [int64]$u.SecureSuccess } catch { 0 }
    $out['u_SecureFailure'] = try { [int64]$u.SecureFailure } catch { 0 }
    $out['u_Queued']        = try { [int64]$u.Queued }        catch { 0 }
}

# SecurityStatistics
$sec = $s.SecurityStatistics
if ($sec) {
    $out['s_TSigVerifySuccess']  = try { [int64]$sec.SecurityTSigVerifySuccess }  catch { 0 }
    $out['s_TSigVerifyFailed']   = try { [int64]$sec.SecurityTSigVerifyFailed }   catch { 0 }
    $out['s_TKeyInvalid']        = try { [int64]$sec.SecurityTKeyInvalid }        catch { 0 }
    $out['s_ContextTimeout']     = try { [int64]$sec.SecurityContextTimeout }     catch { 0 }
    $out['s_ContextQueueLength'] = try { [int64]$sec.SecurityContextQueueLength } catch { 0 }
}

# DnssecStatistics
$d = $s.DnssecStatistics
if ($d) {
    $out['d_ValidationSuccess']      = try { [int64]$d.DnssecValidationSuccess }      catch { 0 }
    $out['d_ValidationFailure']      = try { [int64]$d.DnssecValidationFailure }      catch { 0 }
    $out['d_BadSignature']           = try { [int64]$d.DnssecBadSignature }           catch { 0 }
    $out['d_NoSignature']            = try { [int64]$d.DnssecNoSignature }            catch { 0 }
    $out['d_BogusTotal']             = try { [int64]$d.DnssecBogusTotal }             catch { 0 }
    $out['d_TimestampOutOfRange']    = try { [int64]$d.DnssecTimestampOutOfRange }    catch { 0 }
    $out['d_AlgorithmUnsupported']   = try { [int64]$d.DnssecAlgorithmUnsupported }   catch { 0 }
}

# MemoryStatistics
$m = $s.MemoryStatistics
if ($m) { $out['m_Memory'] = try { [int64]$m.Memory } catch { 0 } }

$out | ConvertTo-Json -Compress
"""
    raw = _ps(script, timeout=20)
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except Exception as e:
        log.warning(f'[dns_monitor] statistics JSON parse failed: {e}')
        return {}


# ── Main check class ──────────────────────────────────────────────────────────
class DnsMonitorCheck(AgentCheck):

    def check(self, instance: Dict[str, Any]) -> None:
        t0 = time.monotonic()

        # Config
        env          = instance.get('env', 'production')
        forwarders   = instance.get('forwarder_ips', [])
        probe_domain = instance.get('forwarder_probe_domain', 'example.com')
        res_domain   = instance.get('resolution_probe_domain', 'www.google.com')
        warn_ms      = float(instance.get('resolution_warn_ms', 100))
        crit_ms      = float(instance.get('resolution_crit_ms', 500))
        fwd_timeout  = float(instance.get('forwarder_timeout_sec', 5))

        base_tags = [
            f'env:{env}',
            f'host:{socket.gethostname()}',
            'role:dns',
            f'dns_server:{socket.gethostname()}',
        ]
        base_tags += instance.get('tags', [])

        # Auto-detect forwarders if none configured
        if not forwarders:
            forwarders = self._detect_forwarders()

        metric_count = 0

        # Run all collectors
        metric_count += self._collect_service(base_tags)
        metric_count += self._collect_perfmon(base_tags)
        metric_count += self._collect_statistics(base_tags)
        metric_count += self._collect_forwarders(base_tags, forwarders, probe_domain, fwd_timeout)
        metric_count += self._collect_resolution(base_tags, res_domain, warn_ms, crit_ms)
        metric_count += self._collect_zones(base_tags)
        metric_count += self._collect_process(base_tags)
        metric_count += self._collect_events(base_tags, instance.get('event_lookback_minutes', 5))
        metric_count += self._collect_scavenging(base_tags)

        # Self-monitoring
        duration_ms = (time.monotonic() - t0) * 1000
        self.gauge('dns.monitor.collection_duration_ms', duration_ms, tags=base_tags + ['category:monitor'])
        self.gauge('dns.monitor.metrics_emitted', metric_count + 2,   tags=base_tags + ['category:monitor'])

        self.log.info(f'[dns_monitor] cycle complete | metrics:{metric_count} | duration:{duration_ms:.0f}ms')

    # ── 1. Service health ─────────────────────────────────────────────────────
    def _collect_service(self, tags: List[str]) -> int:
        stags = tags + ['category:service']
        n = 0

        is_up    = 0
        is_auto  = 0
        sc_status = AgentCheck.CRITICAL

        # Primary: pywin32
        if PYWIN32_OK:
            try:
                scm = win32service.OpenSCManager(None, None, win32con.GENERIC_READ)
                try:
                    svc = win32service.OpenService(scm, 'DNS', win32service.SERVICE_QUERY_STATUS | win32service.SERVICE_QUERY_CONFIG)
                    status  = win32service.QueryServiceStatus(svc)
                    config  = win32service.QueryServiceConfig(svc)
                    state   = status[1]
                    is_up   = 1 if state == win32service.SERVICE_RUNNING else 0
                    is_auto = 1 if config[1] == win32service.SERVICE_AUTO_START else 0
                    sc_status = AgentCheck.OK if is_up else AgentCheck.CRITICAL
                    win32service.CloseServiceHandle(svc)
                finally:
                    win32service.CloseServiceHandle(scm)
            except Exception as e:
                self.log.warning(f'[dns_monitor] service pywin32 failed: {e}')
                is_up, is_auto, sc_status = self._service_via_sc()
        else:
            is_up, is_auto, sc_status = self._service_via_sc()

        self.gauge('dns.service.up',         is_up,   tags=stags)
        self.gauge('dns.service.status',     sc_status, tags=stags)
        self.gauge('dns.service.start_auto', is_auto, tags=stags)
        n += 3

        # WID (Windows Internal Database)
        wid = self._check_wid()
        self.gauge('dns.service.wid_running', wid, tags=stags)
        n += 1

        self.service_check('dns.service.status',
                           AgentCheck.OK if is_up else AgentCheck.CRITICAL,
                           tags=stags,
                           message='DNS service running' if is_up else 'DNS service stopped')
        return n

    def _service_via_sc(self) -> Tuple[int, int, int]:
        """Fallback: parse sc.exe output."""
        out = subprocess.run(['sc', 'query', 'DNS'], capture_output=True, text=True, timeout=10).stdout.upper()
        is_up   = 1 if 'RUNNING' in out else 0
        is_auto = 0
        try:
            cfg = subprocess.run(['sc', 'qc', 'DNS'], capture_output=True, text=True, timeout=10).stdout.upper()
            is_auto = 1 if 'AUTO_START' in cfg else 0
        except Exception:
            pass
        sc_status = AgentCheck.OK if is_up else AgentCheck.CRITICAL
        return is_up, is_auto, sc_status

    def _check_wid(self) -> int:
        """Check Windows Internal Database service."""
        try:
            out = subprocess.run(
                ['sc', 'query', 'MSSQL$MICROSOFT##WID'],
                capture_output=True, text=True, timeout=10
            ).stdout.upper()
            if 'RUNNING' in out:
                return 1
            elif 'DOES NOT EXIST' in out or 'FAILED' in out:
                return -1
            return 0
        except Exception:
            return -1

    # ── 2. PDH Perfmon counters ────────────────────────────────────────────────
    def _collect_perfmon(self, tags: List[str]) -> int:
        ptags = tags + ['category:performance']
        results = _read_pdh_counters(self.log)
        n = 0
        for name, val in results.items():
            self.count(name, val, tags=ptags)
            n += 1
        if n == 0:
            self.gauge('dns.performance.available', 0, tags=ptags)
            n = 1
        else:
            self.gauge('dns.performance.available', 1, tags=ptags)
            n += 1
        return n

    # ── 3. DNS Server Statistics ───────────────────────────────────────────────
    def _collect_statistics(self, tags: List[str]) -> int:
        stats = _read_dns_statistics(self.log)
        if not stats:
            return 0

        n = 0
        total_queries = float(stats.get('q_TotalQueries', 0))

        # Query breakdown
        qtag = tags + ['category:queries']
        self.count('dns.queries.total',      total_queries,                tags=qtag + ['query_type:ALL'])
        self.count('dns.queries.type_a',     stats.get('q_TypeA', 0),      tags=qtag + ['query_type:A'])
        self.count('dns.queries.type_aaaa',  stats.get('q_TypeAaaa', 0),   tags=qtag + ['query_type:AAAA'])
        self.count('dns.queries.type_ptr',   stats.get('q_TypePtr', 0),    tags=qtag + ['query_type:PTR'])
        self.count('dns.queries.type_mx',    stats.get('q_TypeMx', 0),     tags=qtag + ['query_type:MX'])
        self.count('dns.queries.type_srv',   stats.get('q_TypeSrv', 0),    tags=qtag + ['query_type:SRV'])
        self.count('dns.queries.type_ns',    stats.get('q_TypeNs', 0),     tags=qtag + ['query_type:NS'])
        self.count('dns.queries.type_txt',   stats.get('q_TypeTxt', 0),    tags=qtag + ['query_type:TXT'])
        self.count('dns.queries.type_soa',   stats.get('q_TypeSoa', 0),    tags=qtag + ['query_type:SOA'])
        self.count('dns.queries.type_other', stats.get('q_TypeOther', 0),  tags=qtag + ['query_type:OTHER'])
        self.count('dns.queries.update',     stats.get('q_Update', 0),     tags=qtag + ['query_type:UPDATE'])
        n += 11

        # Error breakdown
        etag = tags + ['category:errors']
        nx  = float(stats.get('e_NxDomain', 0))
        sf  = float(stats.get('e_ServFail', 0))
        rf  = float(stats.get('e_Refused', 0))
        fe  = float(stats.get('e_FormError', 0))
        ni  = float(stats.get('e_NotImpl', 0))
        nr  = float(stats.get('e_NxRRSet', 0))
        tot = nx + sf + rf + fe + ni + nr
        self.count('dns.errors.nxdomain_total',  nx,  tags=etag + ['error_type:nxdomain'])
        self.count('dns.errors.servfail_total',  sf,  tags=etag + ['error_type:servfail'])
        self.count('dns.errors.refused_total',   rf,  tags=etag + ['error_type:refused'])
        self.count('dns.errors.formerror_total', fe,  tags=etag + ['error_type:formerror'])
        self.count('dns.errors.notimpl_total',   ni,  tags=etag + ['error_type:notimpl'])
        self.count('dns.errors.nxrrset_total',   nr,  tags=etag + ['error_type:nxrrset'])
        self.count('dns.errors.total',           tot, tags=etag)
        if total_queries > 0:
            self.gauge('dns.errors.error_rate_pct', round(tot / total_queries * 100, 4), tags=etag)
            n += 1
        n += 7

        # Recursion
        rtag = tags + ['category:recursion']
        recursed = float(stats.get('r_QueriesRecursed', 0))
        self.count('dns.recursion.queries_recursed_total',  recursed,                              tags=rtag)
        self.count('dns.recursion.forwarded_queries_total', stats.get('r_Forwards', 0),            tags=rtag)
        self.count('dns.recursion.failures_total',          stats.get('r_RecursionFailure', 0),    tags=rtag)
        self.count('dns.recursion.timeouts_total',          stats.get('r_TimedoutQueries', 0),     tags=rtag)
        self.count('dns.recursion.responses_total',         stats.get('r_Responses', 0),           tags=rtag)
        self.count('dns.recursion.retries_total',           stats.get('r_Retries', 0),             tags=rtag)
        self.count('dns.recursion.duplicate_queries_total', stats.get('r_DuplicateCoalesedQueries', 0), tags=rtag)
        self.count('dns.recursion.lookaside_nxdata_total',  stats.get('r_ServerNotAuthNoData', 0), tags=rtag)
        if total_queries > 0:
            self.gauge('dns.recursion.recursive_ratio_pct',
                       round(recursed / total_queries * 100, 4), tags=rtag)
            n += 1
        n += 8

        # Cache
        ctag = tags + ['category:cache']
        cache_current = float(stats.get('c_CacheCurrent', 0))
        self.gauge('dns.cache.records_current', cache_current,                  tags=ctag)
        self.gauge('dns.cache.records_in_use',  stats.get('c_InUse', 0),        tags=ctag)
        self.gauge('dns.cache.memory_kb',       stats.get('c_Memory', 0),       tags=ctag + ['memory_source:statistics'])
        self.count('dns.cache.returned_total',  stats.get('c_Return', 0),       tags=ctag)
        self.count('dns.cache.timeouts_total',  stats.get('c_CacheTimeouts', 0), tags=ctag)
        hits = total_queries - recursed
        if total_queries > 0 and hits >= 0:
            self.gauge('dns.cache.hit_ratio_pct', round(max(0, hits) / total_queries * 100, 4), tags=ctag)
            n += 1
        n += 5

        # Updates
        utag = tags + ['category:updates']
        rej  = float(stats.get('u_Rejected', 0))
        ref  = float(stats.get('u_Refused', 0))
        tout = float(stats.get('u_Timeout', 0))
        dsw  = float(stats.get('u_DsWriteFailure', 0))
        self.count('dns.updates.received_total',         stats.get('u_Received', 0),      tags=utag)
        self.count('dns.updates.completed_total',        stats.get('u_Completed', 0),     tags=utag)
        self.count('dns.updates.failed_total',           rej + ref + tout + dsw,          tags=utag)
        self.count('dns.updates.rejected_total',         rej,                             tags=utag)
        self.count('dns.updates.secure_success_total',   stats.get('u_SecureSuccess', 0), tags=utag)
        self.count('dns.updates.secure_failure_total',   stats.get('u_SecureFailure', 0), tags=utag)
        self.count('dns.updates.queued_total',           stats.get('u_Queued', 0),        tags=utag)
        self.count('dns.updates.ds_write_failure_total', dsw,                             tags=utag)
        n += 8

        # Security
        sectag = tags + ['category:security']
        self.count('dns.security.tsig_verify_success_total', stats.get('s_TSigVerifySuccess', 0),  tags=sectag)
        self.count('dns.security.tsig_verify_failed_total',  stats.get('s_TSigVerifyFailed', 0),   tags=sectag)
        self.count('dns.security.tkey_invalid_total',        stats.get('s_TKeyInvalid', 0),        tags=sectag)
        self.count('dns.security.context_timeout_total',     stats.get('s_ContextTimeout', 0),     tags=sectag)
        self.gauge('dns.security.context_queue_length',      stats.get('s_ContextQueueLength', 0), tags=sectag)
        n += 5

        # DNSSEC
        dtag = tags + ['category:dnssec']
        self.count('dns.dnssec.validation_success_total',     stats.get('d_ValidationSuccess', 0),    tags=dtag)
        self.count('dns.dnssec.validation_failure_total',     stats.get('d_ValidationFailure', 0),    tags=dtag)
        self.count('dns.dnssec.bad_signature_total',          stats.get('d_BadSignature', 0),         tags=dtag)
        self.count('dns.dnssec.no_signature_total',           stats.get('d_NoSignature', 0),          tags=dtag)
        self.count('dns.dnssec.bogus_total',                  stats.get('d_BogusTotal', 0),           tags=dtag)
        self.count('dns.dnssec.timestamp_out_of_range_total', stats.get('d_TimestampOutOfRange', 0),  tags=dtag)
        self.count('dns.dnssec.algorithm_unsupported_total',  stats.get('d_AlgorithmUnsupported', 0), tags=dtag)
        n += 7

        # Memory (statistics source)
        mem_bytes = float(stats.get('m_Memory', 0))
        if mem_bytes > 0:
            self.gauge('dns.process.memory_mb',
                       round(mem_bytes / 1024 / 1024, 2),
                       tags=tags + ['category:process', 'process:dns', 'memory_source:statistics'])
            n += 1

        return n

    # ── 4. Forwarder availability ──────────────────────────────────────────────
    def _detect_forwarders(self) -> List[str]:
        """Auto-detect forwarder IPs via PowerShell."""
        out = _ps(
            "(Get-DnsServerForwarder -ErrorAction SilentlyContinue).IPAddress | "
            "ForEach-Object { $_.ToString() } | Where-Object { $_ -and $_.Trim() }"
        )
        ips = [l.strip() for l in out.splitlines() if l.strip()]
        if ips:
            self.log.info(f'[dns_monitor] auto-detected forwarders: {ips}')
        return ips

    def _get_forwarder_server_info(self) -> Tuple[int, int]:
        """Returns (use_root_hint 0/1, timeout_sec)."""
        out = _ps(r"""
$f = Get-DnsServerForwarder -ErrorAction SilentlyContinue
if ($null -eq $f) { Write-Output '0|0'; exit }
$rh = if ($f.UseRootHint) { 1 } else { 0 }
$to = 0
try { $to = [int]$f.Timeout.TotalSeconds } catch { try { $to = [int]$f.Timeout } catch {} }
Write-Output "$rh|$to"
""")
        parts = out.split('|')
        if len(parts) == 2:
            try:
                return int(parts[0]), int(parts[1])
            except ValueError:
                pass
        return 0, 0

    def _collect_forwarders(self, tags: List[str], forwarders: List[str],
                             probe_domain: str, timeout: float) -> int:
        ftags = tags + ['category:forwarders']
        n = 0

        use_root_hint, timeout_sec = self._get_forwarder_server_info()
        self.gauge('dns.forwarders.configured_count', len(forwarders), tags=ftags)
        self.gauge('dns.forwarders.use_root_hint',    use_root_hint,   tags=ftags)
        self.gauge('dns.forwarders.timeout_sec',      timeout_sec,     tags=ftags)
        n += 3

        if not forwarders:
            self.gauge('dns.forwarders.available_count', 0, tags=ftags)
            self.gauge('dns.forwarders.degraded_count',  0, tags=ftags)
            return n + 2

        avail_count = 0
        best_latency: Optional[float] = None

        for ip in forwarders:
            parts  = ip.split('.')
            subnet = f'{parts[0]}.{parts[1]}.{parts[2]}.0/24' if len(parts) == 4 else ip
            itags  = ftags + [f'forwarder_ip:{ip}', f'forwarder_subnet:{subnet}']

            # DNS probe (primary)
            up, latency_ms = _probe_dns(ip, probe_domain, timeout)
            avail_val = 1 if up else 0
            if up:
                avail_count += 1
                if best_latency is None or latency_ms < best_latency:
                    best_latency = latency_ms

            self.gauge('dns.forwarders.availability',    avail_val,  tags=itags)
            self.gauge('dns.forwarders.probe_latency_ms', latency_ms, tags=itags)
            n += 2

            # TCP/53 secondary
            tcp_up = 0
            try:
                s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                s.settimeout(3)
                s.connect((ip, 53))
                s.close()
                tcp_up = 1
            except Exception:
                pass
            self.gauge('dns.forwarders.tcp_reachable', tcp_up, tags=itags)
            n += 1

            # Resolver broken: TCP up but DNS failing
            resolver_broken = 1 if (not up and tcp_up) else 0
            self.gauge('dns.forwarders.resolver_broken', resolver_broken, tags=itags)
            n += 1

        degraded = len(forwarders) - avail_count
        self.gauge('dns.forwarders.available_count', avail_count, tags=ftags)
        self.gauge('dns.forwarders.degraded_count',  degraded,    tags=ftags)
        n += 2

        if forwarders:
            pct = avail_count / len(forwarders) * 100
            self.gauge('dns.forwarders.availability_pct', pct, tags=ftags)
            n += 1

        if best_latency is not None:
            self.gauge('dns.forwarders.best_probe_latency_ms', best_latency, tags=ftags)
            n += 1

        return n

    # ── 5. Resolution response time ───────────────────────────────────────────
    def _collect_resolution(self, tags: List[str], probe_domain: str,
                             warn_ms: float, crit_ms: float) -> int:
        n = 0
        probe_count = 0

        # Baseline: raw UDP/53 probe to 127.0.0.1
        rtags = tags + ['category:resolution', 'probe_scope:baseline', 'zone:_baseline']
        up, ms = _probe_dns('127.0.0.1', probe_domain, timeout=5.0)
        probe_count += 1

        self.gauge('dns.resolution.status', 1 if up else 0, tags=rtags)
        self.gauge('dns.resolution.latency_ms', ms, tags=rtags)
        n += 2

        sc = AgentCheck.CRITICAL
        if up:
            sc = AgentCheck.OK if ms <= warn_ms else (AgentCheck.WARNING if ms <= crit_ms else AgentCheck.CRITICAL)
        self.gauge('dns.resolution.service_check', sc, tags=rtags)
        self.service_check('dns.resolution.latency',
                           sc, tags=rtags,
                           message=f'Resolution latency {ms:.1f}ms')
        n += 1

        # External probe: full recursive resolution via net resolver
        if probe_domain:
            etags = tags + ['category:resolution', 'probe_scope:external']
            probe_count += 1
            try:
                t0 = time.monotonic()
                socket.getaddrinfo(probe_domain, None)
                ext_ms = (time.monotonic() - t0) * 1000
                self.gauge('dns.resolution.status', 1, tags=etags)
                self.gauge('dns.resolution.latency_ms', round(ext_ms, 2), tags=etags)
            except Exception:
                self.gauge('dns.resolution.status', 0, tags=etags)
                self.gauge('dns.resolution.latency_ms', 0, tags=etags)
            n += 2

        self.gauge('dns.resolution.internal_probe_targets', probe_count,
                   tags=tags + ['category:resolution'])
        n += 1

        return n

    # ── 6. Zone health ─────────────────────────────────────────────────────────
    def _collect_zones(self, tags: List[str]) -> int:
        ztags = tags + ['category:zones']
        n = 0

        raw = _ps(r"""
Get-DnsServerZone -ErrorAction SilentlyContinue |
  Where-Object { -not $_.IsAutoCreated } |
  Select-Object ZoneName,ZoneType,IsReverseLookupZone,IsPaused,IsDsIntegrated,IsSigned |
  ConvertTo-Json -Compress
""", timeout=15)

        zones = []
        if raw and raw != 'null':
            try:
                parsed = json.loads(raw)
                zones = parsed if isinstance(parsed, list) else [parsed]
            except Exception as e:
                self.log.warning(f'[dns_monitor] zone JSON parse failed: {e}')

        total      = len(zones)
        forward    = sum(1 for z in zones if not z.get('IsReverseLookupZone'))
        reverse    = sum(1 for z in zones if z.get('IsReverseLookupZone'))
        primary    = sum(1 for z in zones if z.get('ZoneType') == 'Primary')
        secondary  = sum(1 for z in zones if z.get('ZoneType') == 'Secondary')
        stub       = sum(1 for z in zones if z.get('ZoneType') == 'Stub')
        ad_int     = sum(1 for z in zones if z.get('IsDsIntegrated'))
        signed     = sum(1 for z in zones if z.get('IsSigned'))

        self.gauge('dns.zones.total_count',         total,     tags=ztags)
        self.gauge('dns.zones.forward_count',       forward,   tags=ztags)
        self.gauge('dns.zones.reverse_count',       reverse,   tags=ztags)
        self.gauge('dns.zones.primary_count',       primary,   tags=ztags)
        self.gauge('dns.zones.secondary_count',     secondary, tags=ztags)
        self.gauge('dns.zones.stub_count',          stub,      tags=ztags)
        self.gauge('dns.zones.ad_integrated_count', ad_int,    tags=ztags)
        self.gauge('dns.zones.dnssec_signed_count', signed,    tags=ztags)
        n += 8

        for z in zones:
            zname = z.get('ZoneName', '')
            if not zname:
                continue
            ztype = (z.get('ZoneType') or 'unknown').lower()
            ztper = tags + ['category:zones', f'zone:{zname}', f'zone_type:{ztype}']
            self.gauge('dns.zones.is_paused',     1 if z.get('IsPaused')      else 0, tags=ztper)
            self.gauge('dns.zones.ad_integrated', 1 if z.get('IsDsIntegrated') else 0, tags=ztper)
            self.gauge('dns.zones.dnssec_signed', 1 if z.get('IsSigned')       else 0, tags=ztper)
            n += 3

        return n

    # ── 7. Process metrics ─────────────────────────────────────────────────────
    def _collect_process(self, tags: List[str]) -> int:
        """
        Collect dns.exe process metrics via PowerShell Get-Process.
        PowerShell subprocess works under all Agent user contexts.
        WMI Win32_Process is unreliable when called from restricted service accounts.
        """
        ptags = tags + ['category:process', 'process:dns']
        n = 0

        # Primary: PowerShell Get-Process (works under Agent service account)
        raw = _ps(r"""
$p = Get-Process -Name dns -ErrorAction SilentlyContinue
if ($null -eq $p) { Write-Output 'null'; exit }
$uptime = -1
try { $uptime = [math]::Round(((Get-Date) - $p.StartTime).TotalMinutes, 2) } catch {}
[PSCustomObject]@{
    WorkingSetMB  = [math]::Round($p.WorkingSet64 / 1MB, 2)
    PrivateMemMB  = [math]::Round($p.PrivateMemorySize64 / 1MB, 2)
    VirtualMemMB  = [math]::Round($p.VirtualMemorySize64 / 1MB, 2)
    ThreadCount   = $p.Threads.Count
    HandleCount   = $p.HandleCount
    IoReadOps     = $p.ReadOperationCount
    IoWriteOps    = $p.WriteOperationCount
    UptimeMinutes = $uptime
} | ConvertTo-Json -Compress
""", timeout=10)

        if raw and raw != 'null':
            try:
                import json as _json
                d = _json.loads(raw)
                self.gauge('dns.process.working_set_mb', float(d.get('WorkingSetMB', 0)), tags=ptags + ['memory_source:working_set'])
                self.gauge('dns.process.private_mem_mb', float(d.get('PrivateMemMB',  0)), tags=ptags + ['memory_source:private'])
                self.gauge('dns.process.virtual_mem_mb', float(d.get('VirtualMemMB',  0)), tags=ptags + ['memory_source:virtual'])
                self.gauge('dns.process.thread_count',   int(d.get('ThreadCount',   0)),   tags=ptags)
                self.gauge('dns.process.handle_count',   int(d.get('HandleCount',   0)),   tags=ptags)
                self.count('dns.process.io_read_ops_total',  float(d.get('IoReadOps',  0)), tags=ptags)
                self.count('dns.process.io_write_ops_total', float(d.get('IoWriteOps', 0)), tags=ptags)
                n += 7
                uptime = float(d.get('UptimeMinutes', -1))
                if uptime >= 0:
                    self.gauge('dns.process.uptime_minutes', uptime, tags=ptags)
                    n += 1
            except Exception as e:
                self.log.warning(f'[dns_monitor] process JSON parse failed: {e}')

        # CPU % via typeperf (same approach as perfmon — works under Agent context)
        if PYWIN32_OK:
            try:
                q = win32pdh.OpenQuery()
                h = win32pdh.AddEnglishCounter(q, r'\Process(dns)\% Processor Time', 0)
                win32pdh.CollectQueryData(q)
                time.sleep(0.1)
                win32pdh.CollectQueryData(q)
                _, cpu = win32pdh.GetFormattedCounterValue(h, win32pdh.PDH_FMT_DOUBLE)
                win32pdh.CloseQuery(q)
                self.gauge('dns.process.cpu_pct', float(cpu), tags=ptags)
                n += 1
            except Exception:
                pass

        return n

    # ── 8. Event log ──────────────────────────────────────────────────────────
    def _collect_events(self, tags: List[str], lookback_minutes: int) -> int:
        etags = tags + ['category:events']
        n = 0

        raw = _ps(f"""
$cutoff = (Get-Date).AddMinutes(-{lookback_minutes})
try {{
    $evts = Get-WinEvent -FilterHashtable @{{LogName='DNS Server';StartTime=$cutoff}} `
            -MaxEvents 500 -ErrorAction Stop
    $err  = @($evts | Where-Object {{ $_.LevelDisplayName -eq 'Error'   }}).Count
    $warn = @($evts | Where-Object {{ $_.LevelDisplayName -eq 'Warning' }}).Count
    $info = @($evts | Where-Object {{ $_.LevelDisplayName -eq 'Information' }}).Count
    $cap  = if ($evts.Count -ge 500) {{ 1 }} else {{ 0 }}
    Write-Output "$err|$warn|$info|$cap"
}} catch {{
    if ($_.Exception.Message -notmatch 'No events') {{
        Write-Output 'error'
    }} else {{
        Write-Output '0|0|0|0'
    }}
}}
""")

        if raw and raw != 'error':
            parts = raw.split('|')
            if len(parts) == 4:
                try:
                    self.gauge('dns.events.errors_in_window',        int(parts[0]), tags=etags + ['level:error'])
                    self.gauge('dns.events.warnings_in_window',      int(parts[1]), tags=etags + ['level:warning'])
                    self.gauge('dns.events.info_in_window',          int(parts[2]), tags=etags + ['level:info'])
                    self.gauge('dns.events.cap_reached',             int(parts[3]), tags=etags)
                    self.gauge('dns.events.lookback_window_minutes', lookback_minutes, tags=etags)
                    n += 5
                except ValueError:
                    pass
        else:
            self.gauge('dns.events.errors_in_window',   0, tags=etags + ['level:error'])
            self.gauge('dns.events.warnings_in_window', 0, tags=etags + ['level:warning'])
            n += 2

        return n

    # ── 9. Scavenging health ───────────────────────────────────────────────────
    def _collect_scavenging(self, tags: List[str]) -> int:
        stags = tags + ['category:scavenging']
        n = 0

        raw = _ps(r"""
try {
    $svr = Get-DnsServer -ErrorAction Stop -WarningAction SilentlyContinue
    $si  = $svr.ServerSetting.ScavengingInterval
    $hours = 0
    if ($null -ne $si) {
        if ($si -is [TimeSpan]) { $hours = [math]::Round($si.TotalHours,2) }
        else { $hours = [int]$si }
    }
    $enabled = if ($hours -gt 0) { 1 } else { 0 }
    Write-Output "$enabled|$hours"
} catch { Write-Output '0|0' }
""")

        parts = (raw or '0|0').split('|')
        if len(parts) == 2:
            try:
                self.gauge('dns.scavenging.enabled',        int(parts[0]),   tags=stags)
                self.gauge('dns.scavenging.interval_hours', float(parts[1]), tags=stags)
                n += 2
            except ValueError:
                pass

        # Monitor UDP send failures (always 0 in Python check — Agent handles retries)
        self.count('dns.monitor.udp_send_failures_total', 0, tags=tags + ['category:monitor'])
        n += 1

        return n