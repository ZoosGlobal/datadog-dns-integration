# Zoos Global — Microsoft DNS Monitor for Datadog

<div align="center">

<img src="https://media.licdn.com/dms/image/v2/C510BAQEaNQXhD4EVaQ/company-logo_200_200/company-logo_200_200/0/1631395395675/zoos_logo?e=2147483647&v=beta&t=OR7jdri2KV5dJZuY7I8bt0U5wOFT6-ElaMb_0Kydvj8" alt="Zoos Global" width="90" height="90"/>
&nbsp;&nbsp;&nbsp;&nbsp;
<img src="https://partners.datadoghq.com/resource/1742314164000/PRM_Assets/images/partnerlogo/datadog_partner_premier.png" alt="Datadog Premier Partner" height="90"/>

<br/>

![Version](https://img.shields.io/badge/version-1.0.0-blue?style=for-the-badge)
![Platform](https://img.shields.io/badge/platform-Windows%20Server-0078D4?style=for-the-badge&logo=windows)
![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Datadog](https://img.shields.io/badge/Datadog-Agent%20checks.d-632CA6?style=for-the-badge&logo=datadog&logoColor=white)
![Partner](https://img.shields.io/badge/Datadog-Premier%20Partner-632CA6?style=for-the-badge&logo=datadog&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green?style=for-the-badge)
![Status](https://img.shields.io/badge/status-Production%20Ready-brightgreen?style=for-the-badge)

<br/>

**Windows DNS Server → Go Binary → DogStatsD → Datadog**

*68 metrics across 7 categories — Service Health, Resolution Latency, Forwarder Availability,  
Performance Counters, Zone Health, Process Metrics, Self-Monitoring.  
Triggered every 60 seconds by the Datadog Agent. Zero Task Scheduler dependency.*

<br/>

![Metrics](https://img.shields.io/badge/metrics-68%20per%20cycle-blue?style=flat-square)
![Collectors](https://img.shields.io/badge/collectors-7-blue?style=flat-square)
![Scheduling](https://img.shields.io/badge/scheduling-Datadog%20Agent%20checks.d-purple?style=flat-square)
![Binary](https://img.shields.io/badge/binary-single%20.exe%20%7C%20no%20runtime-green?style=flat-square)

</div>

---

## 1. The Problem — Why This Exists

Microsoft DNS Server is a critical infrastructure component — every application, user, and server on the network depends on it for name resolution. When DNS degrades or fails, the impact is immediate and wide:

- **Users cannot reach internal applications** — login portals, SharePoint, Exchange all fail
- **Servers cannot reach each other** — service discovery, AD authentication, replication break
- **External connectivity degrades** — forwarder failures mean the internet appears unreachable
- **Incidents are misdiagnosed** — network teams chase firewall rules while DNS is the actual root cause

Despite this criticality, most organisations have **no proactive DNS monitoring**. They find out DNS is broken when the helpdesk gets flooded with tickets.

### The Three Client Requirements

After discovery, the client defined three mandatory monitoring requirements:

| # | Requirement | Why It Matters |
|---|---|---|
| 1 | **DNS Resolution Response Time** | Slow resolution = slow everything. Latency above 100ms is user-noticeable. Above 500ms causes application timeouts. |
| 2 | **DNS Server Service Health** | If the DNS service stops, all name resolution on the network fails within seconds. |
| 3 | **Forwarder Availability** | If upstream forwarders are down, external resolution fails. Internal resolution may still work, masking the issue until users try to reach the internet. |

---

## 2. The Solution — What We Built

A **self-contained Go binary** (`dns-monitor.exe`) that:

- Runs once per invocation — collects all metrics, pushes to Datadog, exits cleanly
- Is triggered every 60 seconds by the **Datadog Agent's `checks.d`** — the Agent owns scheduling, restart-on-failure, and logging
- Pushes metrics directly to **DogStatsD on `127.0.0.1:8125`** — no API key required, no external calls
- Requires **zero Task Scheduler dependency** — the Agent is the daemon

### Why a Go Binary, Not a PowerShell Script

| Concern | PowerShell Script | Go Binary |
|---|---|---|
| Task Scheduler dependency | Required — fragile, visible, overridable by admins | None — Agent owns scheduling |
| Runtime dependency | PowerShell 5.1 must be present and unrestricted | None — single `.exe`, statically linked |
| Startup overhead | 2–3 seconds per run (PS initialisation) | < 100ms |
| Distribution | Script readable by anyone with file access | Compiled — tamper-resistant |
| Windows Service conflicts | Cannot run as a proper service without NSSM | No service needed — Agent manages it |
| Customer acceptance | Often rejected by enterprise security teams | Accepted — standard binary deployment |

### Why checks.d, Not a Windows Service

The binary runs **once and exits** — it does not run as a long-running daemon. The Datadog Agent's `checks.d` provides:

- **Automatic scheduling** — calls the binary every `min_collection_interval` seconds
- **Timeout enforcement** — kills the binary if it exceeds the configured timeout
- **Failure recovery** — logs errors, continues next cycle even if a run fails
- **Unified observability** — binary output visible in `datadog-agent check dns_monitor`

This means the binary stays simple (no goroutine management, no signal handling, no service registration) and the Agent handles all operational complexity.

### How It All Fits Together

```
┌─────────────────────────────────────────────────────────┐
│  Datadog Agent  (always running Windows service)         │
│                                                          │
│  Every 60 seconds:                                       │
│  checks.d/dns_monitor.py  ←── conf.d/dns_monitor.d/     │
│         │                      conf.yaml                 │
│         │ subprocess.run()                               │
│         ▼                                                │
│  dns-monitor.exe  (runs, collects, exits in ~4-5s)      │
│         │                                                │
│         ├── WMI  → service health, process, zones        │
│         ├── PDH  → performance counters                  │
│         ├── UDP/53 probe → forwarder availability        │
│         ├── UDP/53 probe → resolution response time      │
│         └── DogStatsD UDP → 127.0.0.1:8125              │
│                                   │                      │
└───────────────────────────────────┼──────────────────────┘
                                    │
                                    ▼
                            Datadog Platform
                     (Metrics → Dashboards → Monitors)
```

---

## 3. What We Monitor — Metrics Reference

### 3.1 DNS Service Health — `dns.service.*`

**Why:** If the DNS service stops, all name resolution fails immediately. This is the most critical signal.

**When to alert:** `dns.service.up` drops to `0` — page immediately.

> Tags: `env`, `host`, `role:dns`, `dns_server`, `category:service`

| Metric | Type | Description | Alert Threshold |
|--------|------|-------------|-----------------|
| `dns.service.up` | gauge | DNS service running `1=up 0=down` | `< 1` → CRITICAL |
| `dns.service.status` | gauge | Service check status `0=OK 2=CRITICAL` | `> 0` → alert |
| `dns.service.start_auto` | gauge | StartType is Automatic `1=yes 0=no` | `< 1` → WARNING |
| `dns.service.wid_running` | gauge | Windows Internal Database `1=up 0=down -1=N/A (SQL backend)` | `0` → WARNING |

---

### 3.2 DNS Resolution Response Time — `dns.resolution.*` ⭐

**Why:** Resolution latency is the metric clients actually feel. Every application call that requires name resolution adds this latency. Above 100ms it becomes noticeable. Above 500ms applications start timing out.

**When to alert:** P95 baseline latency above 100ms for 5 minutes.

**How it's measured:** A raw UDP/53 DNS query with a cache-busting random subdomain is sent directly to `127.0.0.1:53`. NXDOMAIN response = server is UP and responding. This technique is immune to missing A records and cached responses.

> Tags: `env`, `host`, `role:dns`, `category:resolution`, `probe_scope`, `zone`

| Metric | Type | Description | Alert Threshold |
|--------|------|-------------|-----------------|
| `dns.resolution.status` | gauge | Probe result `1=success 0=failed` | `< 1` → CRITICAL |
| `dns.resolution.latency_ms` | distribution | Resolution RTT in ms (P50/P95/P99) | P95 `> 100ms` WARNING, `> 500ms` CRITICAL |
| `dns.resolution.service_check` | gauge | Threshold check `0=OK 1=WARN 2=CRIT` | `> 0` → alert |
| `dns.resolution.internal_probe_targets` | gauge | Active probe targets this cycle | `< 1` → WARNING |

`probe_scope:baseline` — UDP/53 raw probe against `127.0.0.1` (always emitted)
`probe_scope:external` — `net.Resolver` lookup of configured external domain (e.g. `www.google.com`)

---

### 3.3 Forwarder Availability — `dns.forwarders.*` ⭐

**Why:** When forwarders are down, external DNS resolution silently fails. Users can still reach internal resources, which masks the problem until they try to reach the internet or external SaaS services.

**When to alert:** Any forwarder `availability` drops to `0` — this is a P2 incident.

**How it's measured:** A raw UDP/53 DNS query with a cache-busting random subdomain is sent **directly to each forwarder IP** — bypassing the local DNS server entirely. This proves the forwarder is reachable AND resolving, not just port-open. `NXDOMAIN` = forwarder UP. Timeout or `SERVFAIL` = forwarder DOWN. TCP/53 is checked as a secondary diagnostic signal.

> Tags: `env`, `host`, `role:dns`, `category:forwarders`, `forwarder_ip`, `forwarder_subnet`

| Metric | Type | Description | Alert Threshold |
|--------|------|-------------|-----------------|
| `dns.forwarders.availability` | gauge | DNS probe result per forwarder `1=up 0=down` | `< 1` → CRITICAL |
| `dns.forwarders.availability_pct` | gauge | Fleet-level % of forwarders up | `< 100` → WARNING, `< 50` → CRITICAL |
| `dns.forwarders.available_count` | gauge | Number of forwarders currently up | monitor with `configured_count` |
| `dns.forwarders.degraded_count` | gauge | Number of forwarders currently down | `> 0` → WARNING |
| `dns.forwarders.configured_count` | gauge | Total forwarders configured | baseline reference |
| `dns.forwarders.probe_latency_ms` | distribution | UDP/53 RTT per forwarder | P95 `> 200ms` → WARNING |
| `dns.forwarders.best_probe_latency_ms` | distribution | Fastest forwarder RTT this cycle | SLA reporting |
| `dns.forwarders.tcp_reachable` | gauge | TCP/53 secondary signal `1=port open` | diagnostic only |
| `dns.forwarders.resolver_broken` | gauge | TCP up but DNS failing `1=broken resolver` | `> 0` → CRITICAL |
| `dns.forwarders.use_root_hint` | gauge | Server falls back to root hints `1=yes` | compliance check |
| `dns.forwarders.timeout_sec` | gauge | Forwarder timeout setting | configuration audit |

---

### 3.4 Performance Counters — `dns.performance.*`

**Why:** Query rate, TCP vs UDP split, zone transfer failures, and recursive query failures are the early warning signals before a DNS server becomes overloaded or misconfigured.

**When to alert:** `zone_transfer_failures_total` rate > 0 (secondary zones serving stale data).

> Tags: `env`, `host`, `role:dns`, `category:performance`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.performance.queries_received_total` | counter | Total queries received |
| `dns.performance.responses_sent_total` | counter | Total responses sent |
| `dns.performance.udp_queries_total` | counter | UDP queries (normal traffic) |
| `dns.performance.tcp_queries_total` | counter | TCP queries (zone transfers, large DNSSEC responses) |
| `dns.performance.recursive_queries_total` | counter | Queries requiring upstream resolution |
| `dns.performance.recursive_query_failures_total` | counter | Recursive resolution failures |
| `dns.performance.recursive_query_timeouts_total` | counter | Recursive timeouts (forwarder pressure signal) |
| `dns.performance.dynamic_updates_total` | counter | Dynamic DNS update requests |
| `dns.performance.secure_updates_total` | counter | Secure dynamic updates (AD-integrated) |
| `dns.performance.zone_transfer_requests_total` | counter | AXFR/IXFR requests received |
| `dns.performance.zone_transfer_success_total` | counter | Successful zone transfers |
| `dns.performance.zone_transfer_failures_total` | counter | Failed zone transfers 🔴 |
| `dns.performance.notify_sent_total` | counter | NOTIFY messages sent to secondaries |
| `dns.performance.notify_received_total` | counter | NOTIFY messages received from primary |
| `dns.performance.unmatched_responses_total` | counter | Responses with no matching query (cache poisoning signal) |

---

### 3.5 Zone Health — `dns.zones.*`

**Why:** Zone configuration changes — zones being paused, AD replication breaking — are silent failures that corrupt name resolution for specific namespaces without affecting the overall service health.

> Tags: `env`, `host`, `role:dns`, `category:zones`, `zone`, `zone_type`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.zones.total_count` | gauge | Total zones configured |
| `dns.zones.forward_count` | gauge | Forward lookup zones |
| `dns.zones.reverse_count` | gauge | Reverse lookup zones |
| `dns.zones.primary_count` | gauge | Primary zones |
| `dns.zones.secondary_count` | gauge | Secondary zones |
| `dns.zones.stub_count` | gauge | Stub / conditional forwarder zones |
| `dns.zones.ad_integrated_count` | gauge | AD-integrated zones |
| `dns.zones.dnssec_signed_count` | gauge | DNSSEC-signed zones |
| `dns.zones.is_paused` | gauge | Zone paused `1=yes` — per zone tag |
| `dns.zones.ad_integrated` | gauge | AD-integrated flag — per zone tag |
| `dns.zones.dnssec_signed` | gauge | DNSSEC signed flag — per zone tag |

---

### 3.6 Process Metrics — `dns.process.*`

**Why:** Host-level CPU and memory are already collected by the Datadog Agent. These metrics are scoped to `dns.exe` only. `private_mem_mb` trending upward (while `working_set_mb` stays flat) is the early signal of a memory leak. `uptime_minutes` resetting unexpectedly means the service restarted — often missed without this metric.

> Tags: `env`, `host`, `role:dns`, `category:process`, `process:dns`, `memory_source`

| Metric | Type | Description | Why It Matters |
|--------|------|-------------|----------------|
| `dns.process.working_set_mb` | gauge | Physical RAM in use by dns.exe | Current memory footprint |
| `dns.process.private_mem_mb` | gauge | Private committed pages | Memory leak detection |
| `dns.process.virtual_mem_mb` | gauge | Virtual address space | Normal ~2TB on 64-bit |
| `dns.process.uptime_minutes` | gauge | Minutes since dns.exe last started | Unexpected restart detection |
| `dns.process.cpu_pct` | gauge | CPU % of dns.exe process only | DNS-specific CPU load |
| `dns.process.thread_count` | gauge | Active thread count | Threading anomalies |
| `dns.process.handle_count` | gauge | Handle count | Handle leak detection |
| `dns.process.io_read_ops_total` | counter | Read I/O operations | Disk activity baseline |
| `dns.process.io_write_ops_total` | counter | Write I/O operations | Disk activity baseline |

---

### 3.7 Self-Monitoring — `dns.monitor.*`

**Why:** If the collector itself is slow or failing, you need to know. `collection_duration_ms` trending above 10 seconds means something is hanging (usually a PowerShell subprocess or WMI timeout). `metrics_emitted` dropping below expected count means a collector is failing silently.

| Metric | Type | Description | Alert Threshold |
|--------|------|-------------|-----------------|
| `dns.monitor.collection_duration_ms` | gauge | Total collection cycle time | `> 30000ms` → WARNING |
| `dns.monitor.metrics_emitted` | gauge | Metrics pushed this cycle | `< 60` → WARNING |

---

## 4. System Requirements

**Before starting, verify all requirements are met on the target DNS server.**

| Component | Requirement | How to Verify |
|-----------|-------------|---------------|
| Windows Server | 2016 / 2019 / 2022 / 2025 | `winver` |
| DNS Server Role | Installed and running | `Get-Service DNS` |
| Datadog Agent | v7+ installed and running | `Get-Service datadogagent` |
| DogStatsD | Listening on `127.0.0.1:8125` | `netstat -an \| findstr 8125` |
| Privileges | Administrator or SYSTEM | Run PowerShell as Admin |
| Go (build only) | 1.22+ (on build machine) | `go version` |
| Network | DNS server can reach `127.0.0.1:8125` | Loopback — always available |

---

## 5. Standard Operating Procedure — Deployment

### Step 1 — Build the binary

Run this on any machine with Go 1.22+ installed (Linux, macOS, or Windows). Cross-compilation produces a Windows `.exe` from any OS.

```bash
# Clone the repository
git clone https://github.com/ZoosGlobal/datadog-dns-integration
cd datadog-dns-integration

# Download Go module dependencies
make deps

# Cross-compile for Windows amd64
make build
# Output: dist/dns-monitor.exe
```

**Why build, not download?** The binary is compiled from source — customers can audit every line of code before running it on their DNS server. No pre-built binary trust required.

---

### Step 2 — Copy files to the DNS server

Transfer these four items to the target Windows DNS server:

```
dist\dns-monitor.exe          →  C:\ProgramData\Datadog\dns-monitor.exe
dns-monitor-config.yaml.example → C:\ProgramData\Datadog\dns-monitor-config.yaml
checks.d\dns_monitor.py       →  C:\ProgramData\Datadog\checks.d\dns_monitor.py
conf.d\dns_monitor.d\conf.yaml → C:\ProgramData\Datadog\conf.d\dns_monitor.d\conf.yaml
```

Or use the one-click installer (run as Administrator):

```powershell
PowerShell.exe -ExecutionPolicy Bypass -File .\scripts\setup.ps1
```

---

### Step 3 — Edit the configuration file

Open `C:\ProgramData\Datadog\dns-monitor-config.yaml` and set your environment:

```yaml
# Environment tag — used to scope all metrics in Datadog
env: "production"

# DogStatsD — leave as-is unless Agent is on a different host
statsd_host: "127.0.0.1"
statsd_port: 8125

# Resolution latency thresholds (milliseconds)
# dns.resolution.service_check will be 0=OK, 1=WARN, 2=CRIT based on these
resolution_warn_ms: 100
resolution_crit_ms: 500

# External probe domain — tests full recursive resolution through forwarders
resolution_probe_domain: "www.google.com"

# Forwarder IPs — if left empty, auto-detected from DNS server config
# Add them explicitly for environments where PowerShell is restricted
forwarder_ips: []        # leave empty for auto-detect
# forwarder_ips:
#   - "8.8.8.8"
#   - "1.1.1.1"

# Forwarder probe timeout (seconds)
forwarder_timeout_sec: 5
```

**When to set `forwarder_ips` explicitly:** If the server has PowerShell execution policy restrictions, `Get-DnsServerForwarder` may fail. In that case, list the forwarder IPs manually.

---

### Step 4 — Test the binary directly

Before involving the Agent, confirm the binary runs correctly on its own:

```powershell
# Run one collection cycle — metrics are pushed to DogStatsD
C:\ProgramData\Datadog\dns-monitor.exe --config C:\ProgramData\Datadog\dns-monitor-config.yaml
```

**Expected output:**
```
[dns-monitor] auto-detected forwarders: [1.1.1.1 8.8.8.8]
[dns-monitor] service metrics: 4
[dns-monitor] perfmon metrics: 15
[dns-monitor] forwarders metrics: 15
[dns-monitor] resolution metrics: 6
[dns-monitor] zones metrics: 17
[dns-monitor] process metrics: 9
[dns-monitor] cycle complete | metrics:68 | duration:4417ms | statsd:127.0.0.1:8125
```

**If you see fewer metrics than expected:** Check the log output for `skipped` or `failed` messages — each collector logs its own errors. The binary exits 0 on success, non-zero on fatal errors.

---

### Step 5 — Verify metrics in Datadog

Within 2 minutes of running the binary, check the Datadog Metrics Explorer:

```
dns.service.up          → should be 1
dns.resolution.status   → should be 1 (probe_scope:baseline)
dns.forwarders.availability → should be 1 per forwarder_ip
```

If metrics do not appear: verify `netstat -an | findstr 8125` shows UDP `0.0.0.0:8125` listening.

---

### Step 6 — Test via Datadog Agent check

```powershell
& "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" check dns_monitor
```

**Expected output:**
```
=== Series ===
dns.monitor.collection_duration_ms ...
dns.service.up ...
...
=== Service Checks ===
dns_monitor.binary_present   [OK]
dns_monitor.collection       [OK]

Ran 1 checks in 5.xxx s
```

This confirms the Agent can find, execute, and supervise the binary on its own schedule.

---

### Step 7 — Restart the Datadog Agent

```powershell
Restart-Service datadogagent

# Verify it picked up the new check
Start-Sleep -Seconds 10
& "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" status | findstr dns_monitor
```

From this point, the Agent calls `dns-monitor.exe` every 60 seconds automatically. No Task Scheduler required.

---

## 6. Pre-built Datadog Monitors

Copy these queries directly into Datadog → Monitors → New Monitor → Metric.

### 🔴 DNS Service Down — Page Immediately

```
Query   : max(last_2m):max:dns.service.up{*} by {host} < 1
Alert   : < 1
Recover : >= 1
Message : 🔴 DNS Server service is DOWN on {{host.name}}.
          All name resolution on this server has failed.
          Check: Get-Service DNS
          Start: Start-Service DNS
```

---

### ⚠️ Resolution Latency — SLA Breach

```
Query    : p95:dns.resolution.latency_ms{probe_scope:baseline} by {host} > 100
Warning  : > 100ms  (users starting to notice)
Critical : > 500ms  (application timeouts beginning)
Message  : ⚠️ DNS resolution latency is {{value}}ms on {{host.name}}.
           Check forwarder health and server load.
```

---

### 🔴 Forwarder Down — External Resolution Failing

```
Query   : min(last_5m):min:dns.forwarders.availability{*} by {host,forwarder_ip} < 1
Alert   : < 1
Message : 🔴 Forwarder {{forwarder_ip.name}} is DOWN on {{host.name}}.
          External DNS resolution is degraded.
          Check UDP/53 connectivity to the forwarder IP.
```

---

### ⚠️ Forwarder Fleet Partially Degraded

```
Query    : min(last_5m):min:dns.forwarders.availability_pct{*} by {host} < 100
Warning  : < 100  (at least one forwarder down)
Critical : < 50   (majority of forwarders down)
Message  : ⚠️ {{value}}% of forwarders available on {{host.name}}.
```

---

### 🔴 Resolver Process Broken

```
Query   : max(last_5m):max:dns.forwarders.resolver_broken{*} by {host,forwarder_ip} > 0
Alert   : > 0
Message : 🔴 Forwarder {{forwarder_ip.name}} TCP/53 is reachable but DNS resolution
          is failing on {{host.name}}. The forwarder process is running but broken.
          This is a forwarder-side issue, not a network issue.
```

---

### 🔴 Zone Transfer Failures

```
Query   : sum(last_5m):sum:dns.performance.zone_transfer_failures_total{*} by {host}.as_rate() > 0
Alert   : > 0
Message : 🔴 Zone transfer failures detected on {{host.name}}.
          Secondary zones are serving stale data.
          Check zone transfer permissions and network connectivity to primary.
```

---

### ⚠️ DNS Service Restarted Unexpectedly

```
Query   : min(last_10m):min:dns.process.uptime_minutes{*} by {host} < 10
Alert   : < 10
Message : ⚠️ dns.exe restarted recently on {{host.name}} (uptime: {{value}} minutes).
          Check Windows Event Log for crash or stop reason.
          Event Viewer → Windows Logs → System → Source: Service Control Manager
```

---

### ⚠️ DNS Process Memory Growing

```
Query    : avg(last_1h):avg:dns.process.private_mem_mb{*} by {host} > 500
Warning  : > 500MB
Critical : > 1000MB
Message  : ⚠️ dns.exe private memory is {{value}}MB on {{host.name}}.
           Sustained growth may indicate a memory leak.
           Baseline: ~250MB. Restart the DNS service if growth is continuous.
```

---

## 7. Dashboard Queries

| Widget | Query |
|--------|-------|
| DNS service status | `avg:dns.service.up{*} by {host}` |
| Resolution latency P95 | `p95:dns.resolution.latency_ms{probe_scope:baseline} by {host}` |
| Resolution latency P50 | `p50:dns.resolution.latency_ms{probe_scope:baseline} by {host}` |
| Forwarder availability % | `min:dns.forwarders.availability_pct{*} by {host}` |
| Forwarder status per IP | `min:dns.forwarders.availability{*} by {host,forwarder_ip}` |
| Best forwarder RTT | `avg:dns.forwarders.best_probe_latency_ms{*} by {host}` |
| Forwarder latency P95 | `p95:dns.forwarders.probe_latency_ms{*} by {forwarder_ip}` |
| Queries per second | `per_second(sum:dns.performance.queries_received_total{*} by {host})` |
| TCP vs UDP ratio | `per_second(sum:dns.performance.tcp_queries_total{*} by {host})` |
| Zone transfer failures | `sum:dns.performance.zone_transfer_failures_total{*} by {host}.as_rate()` |
| Recursive query failures | `sum:dns.performance.recursive_query_failures_total{*} by {host}.as_rate()` |
| DNS process memory | `avg:dns.process.private_mem_mb{*} by {host}` |
| DNS process uptime | `avg:dns.process.uptime_minutes{*} by {host}` |
| DNS process CPU | `avg:dns.process.cpu_pct{*} by {host}` |
| Zone count | `avg:dns.zones.total_count{*} by {host}` |
| Collection duration | `avg:dns.monitor.collection_duration_ms{*} by {host}` |

---

## 8. Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Metrics not appearing in Datadog | DogStatsD not listening | `netstat -an \| findstr 8125` — verify UDP 8125 open |
| `binary_present: CRITICAL` | Wrong binary path in conf.yaml | Check `binary_path` in `conf.d\dns_monitor.d\conf.yaml` |
| `collection: CRITICAL` | Binary runtime error | Run binary manually and check output |
| `perfmon metrics: 1` (only `available:0`) | PDH API failed | Run as SYSTEM or account in Performance Log Users group |
| `forwarders.configured_count: 0` | PowerShell auto-detect failed | Set `forwarder_ips` explicitly in `dns-monitor-config.yaml` |
| `resolution.status: 0` (baseline) | UDP/53 blocked on loopback | Verify Windows Firewall allows UDP/53 on loopback |
| `zones.total_count: 0` | PowerShell Get-DnsServerZone failed | Ensure account has DNS Admin or Read access |
| `collection_duration_ms > 10000` | PowerShell subprocess slow | Check DNS role is healthy; increase timeout in conf.yaml |
| Agent check not appearing | conf.yaml in wrong location | Verify path: `C:\ProgramData\Datadog\conf.d\dns_monitor.d\conf.yaml` |
| Agent check timeout | Binary taking > 55s | Reduce collectors or increase `timeout` in conf.yaml |

---

## 9. Production Deployment Checklist

Run through this checklist before handover to the customer.

**Pre-deployment**
- [ ] Datadog Agent v7+ installed and running (`Get-Service datadogagent`)
- [ ] DogStatsD listening on `127.0.0.1:8125` (`netstat -an | findstr 8125`)
- [ ] DNS Server role installed and running (`Get-Service DNS`)
- [ ] `dns-monitor.exe` copied to `C:\ProgramData\Datadog\`
- [ ] `dns-monitor-config.yaml` edited — `env` tag set correctly
- [ ] Forwarder IPs verified (auto-detect or manual)

**Validation**
- [ ] Binary runs manually — `metrics:68 | duration:Xs` in output
- [ ] `dns.service.up` = `1` in Datadog Metrics Explorer
- [ ] `dns.resolution.status` = `1` (probe_scope:baseline)
- [ ] `dns.forwarders.availability` = `1` for all forwarder IPs
- [ ] `datadog-agent check dns_monitor` shows `[OK]` for both service checks
- [ ] Agent restarted — check appears in `datadog-agent status`

**Monitoring**
- [ ] Monitor created: DNS service down
- [ ] Monitor created: Resolution latency P95
- [ ] Monitor created: Forwarder availability
- [ ] Monitor notification routed to correct team channel / PagerDuty

---

## 10. File Reference

```
C:\ProgramData\Datadog\
├── dns-monitor.exe                        ← Go binary (the collector)
├── dns-monitor-config.yaml                ← Binary configuration
├── checks.d\
│   └── dns_monitor.py                     ← Agent check wrapper (triggers binary)
└── conf.d\
    └── dns_monitor.d\
        └── conf.yaml                      ← Agent check configuration
```

```
Repository: ZoosGlobal/datadog-dns-integration/
├── main.go                                ← Binary entry point
├── collector/
│   ├── collector.go                       ← Orchestrator
│   ├── service.go                         ← dns.service.*
│   ├── perfmon.go                         ← dns.performance.* (PDH API)
│   ├── forwarder.go                       ← dns.forwarders.* (UDP/53 probe)
│   ├── forwarder_detect.go                ← Auto-detect forwarder IPs
│   ├── resolution.go                      ← dns.resolution.* (raw UDP probe)
│   ├── zones.go                           ← dns.zones.* (PowerShell)
│   ├── process.go                         ← dns.process.* (WMI)
│   └── wmi.go                             ← WMI struct definitions
├── statsd/client.go                       ← DogStatsD UDP client
├── config/config.go                       ← YAML config loader
├── checks.d/dns_monitor.py                ← Agent check wrapper
├── conf.d/dns_monitor.d/conf.yaml         ← Agent check config
├── dns-monitor-config.yaml.example        ← Binary config template
├── scripts/setup.ps1                      ← One-click installer
├── Makefile                               ← make build → dns-monitor.exe
├── go.mod
├── README.md
├── CHANGELOG.md
└── LICENSE
```

---

## 👤 Author

| | |
|--|--|
| **Name** | Shivam Anand |
| **Title** | Sr. DevOps Engineer \| Engineering |
| **Organisation** | Zoos Global |
| **Email** | [shivam.anand@zoosglobal.com](mailto:shivam.anand@zoosglobal.com) |
| **Web** | [www.zoosglobal.com](https://www.zoosglobal.com) |
| **Address** | Violena, Pali Hill, Bandra West, Mumbai - 400050 |

---

<div align="center">

<img src="https://media.licdn.com/dms/image/v2/C510BAQEaNQXhD4EVaQ/company-logo_200_200/company-logo_200_200/0/1631395395675/zoos_logo?e=2147483647&v=beta&t=OR7jdri2KV5dJZuY7I8bt0U5wOFT6-ElaMb_0Kydvj8" alt="Zoos Global" width="60" height="60"/>
&nbsp;&nbsp;
<img src="https://partners.datadoghq.com/resource/1742314164000/PRM_Assets/images/partnerlogo/datadog_partner_premier.png" alt="Datadog Premier Partner" height="60"/>

<br/><br/>

**Version 1.0.0 · April 2026**

© 2026 Zoos Global · [MIT License](LICENSE)

*Zoos Global is a Datadog Premier Partner*

</div>