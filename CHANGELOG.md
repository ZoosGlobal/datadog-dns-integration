# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.0.0] — April 2026

### Added

**Core binary (`dns-monitor.exe`)**
- Self-contained Go binary for `windows/amd64` — zero runtime dependencies
- Run-once design: collects all metrics, pushes to DogStatsD, exits cleanly
- Triggered by Datadog Agent `checks.d` — no Task Scheduler dependency

**DNS Service Health (`dns.service.*`)**
- `dns.service.up` — service running status via WMI Win32_Service
- `dns.service.status` — DogStatsD service check `0=OK 2=CRITICAL`
- `dns.service.start_auto` — StartType is Automatic
- `dns.service.wid_running` — Windows Internal Database health (`-1` if SQL backend)

**DNS Resolution Response Time (`dns.resolution.*`)**
- `dns.resolution.status` — baseline and external probe results
- `dns.resolution.latency_ms` — distribution metric (P50/P95/P99 in Datadog)
- `dns.resolution.service_check` — threshold-based check (warn: 100ms, crit: 500ms)
- `dns.resolution.internal_probe_targets` — active probe target count
- Baseline probe uses raw UDP/53 with cache-busting random subdomain — immune to missing A records
- External probe uses `net.Resolver` targeting `127.0.0.1:53` directly

**Forwarder Availability (`dns.forwarders.*`)**
- `dns.forwarders.availability` — per-forwarder DNS resolution probe result
- `dns.forwarders.availability_pct` — fleet-level availability percentage
- `dns.forwarders.available_count` / `degraded_count` / `configured_count`
- `dns.forwarders.probe_latency_ms` — UDP/53 RTT distribution per forwarder
- `dns.forwarders.best_probe_latency_ms` — fastest forwarder RTT this cycle
- `dns.forwarders.tcp_reachable` — secondary TCP/53 diagnostic signal
- `dns.forwarders.resolver_broken` — TCP up but DNS failing (resolver process issue)
- `dns.forwarders.use_root_hint` — root hint fallback config signal
- `dns.forwarders.timeout_sec` — configured forwarder timeout
- Auto-detection of forwarder IPs via `Get-DnsServerForwarder` when not configured

**Performance Counters (`dns.performance.*`)**
- 15 PDH counters via `PdhAddEnglishCounterW` — locale-independent
- Counters missing on older Windows builds are skipped gracefully
- Covers: query totals, UDP/TCP split, recursive queries/failures/timeouts, dynamic updates, zone transfers, notify, unmatched responses

**Zone Health (`dns.zones.*`)**
- Zone enumeration via PowerShell `Get-DnsServerZone | ConvertTo-Json`
- Summary: total, forward, reverse, primary, secondary, stub, AD-integrated, DNSSEC-signed counts
- Per-zone: `is_paused`, `ad_integrated`, `dnssec_signed` with `zone:` tag

**Process Metrics (`dns.process.*`)**
- `working_set_mb`, `private_mem_mb`, `virtual_mem_mb` — tagged by `memory_source`
- `uptime_minutes` — restart detection via WMI `Win32_Process.CreationDate`
- `cpu_pct` — DNS process CPU via WMI PerfMon (not host-level)
- `thread_count`, `handle_count`, `io_read_ops_total`, `io_write_ops_total`

**Self-Monitoring (`dns.monitor.*`)**
- `dns.monitor.collection_duration_ms` — full cycle time
- `dns.monitor.metrics_emitted` — metric count per cycle

**Agent Integration**
- `checks.d/dns_monitor.py` — thin Python wrapper, triggers binary via `subprocess.run`
- `conf.d/dns_monitor.d/conf.yaml` — Agent check configuration with interval and timeout
- Service checks: `dns_monitor.binary_present`, `dns_monitor.collection`

**DogStatsD Transport**
- Batched UDP client — MTU-safe 1300-byte packet splitting
- Supports gauge, counter, distribution metric types
- Global tags applied to every metric: `env`, `host`, `role:dns`, `dns_server`

**Setup and Distribution**
- `scripts/setup.ps1` — 8-step one-click installer
- `Makefile` — `make build`, `make package`, `make tag`
- GitHub Actions CI — build and vet on every push and PR
- GitHub Actions Release — auto-publishes zip + SHA256 on version tag push

---

## Versioning Policy

- **Patch** (`v1.0.x`) — bug fixes, counter path corrections, Windows compatibility fixes
- **Minor** (`v1.x.0`) — new metric categories, new collectors, new configuration options
- **Major** (`vx.0.0`) — breaking changes to metric names, config schema, or deployment structure