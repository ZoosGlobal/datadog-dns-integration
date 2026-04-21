# Zoos Global — Microsoft DNS Monitor for Datadog

<div align="center">

<img src="https://media.licdn.com/dms/image/v2/C510BAQEaNQXhD4EVaQ/company-logo_200_200/company-logo_200_200/0/1631395395675/zoos_logo?e=2147483647&v=beta&t=OR7jdri2KV5dJZuY7I8bt0U5wOFT6-ElaMb_0Kydvj8" alt="Zoos Global" width="90" height="90"/>
&nbsp;&nbsp;&nbsp;&nbsp;
<img src="https://partners.datadoghq.com/resource/1742314164000/PRM_Assets/images/partnerlogo/datadog_partner_premier.png" alt="Datadog Premier Partner" height="90"/>

<br/>

![Version](https://img.shields.io/badge/version-1.0.0-blue?style=for-the-badge)
![Platform](https://img.shields.io/badge/platform-Windows%20Server-0078D4?style=for-the-badge&logo=windows)
![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Datadog](https://img.shields.io/badge/Datadog-DogStatsD-632CA6?style=for-the-badge&logo=datadog&logoColor=white)
![Partner](https://img.shields.io/badge/Datadog-Premier%20Partner-632CA6?style=for-the-badge&logo=datadog&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green?style=for-the-badge)
![Status](https://img.shields.io/badge/status-Production%20Ready-brightgreen?style=for-the-badge)

<br/>

**Go Binary → Windows DNS Server → DogStatsD → Datadog Metrics → Dashboards & Alerts**

*A self-contained Go binary, triggered by the Datadog Agent's `checks.d` every 60 seconds.  
Monitors DNS resolution response time, service health, forwarder availability,  
and 100+ additional metrics — with zero Task Scheduler dependency.*

<br/>

![Metrics](https://img.shields.io/badge/metrics-100%2B%20per%20run-blue?style=flat-square)
![Coverage](https://img.shields.io/badge/coverage-Service%20%2B%20Resolution%20%2B%20Forwarders%20%2B%20Zones-blue?style=flat-square)
![Scheduling](https://img.shields.io/badge/scheduling-Datadog%20Agent%20checks.d-purple?style=flat-square)
![Binary](https://img.shields.io/badge/binary-single%20.exe%20no%20runtime-green?style=flat-square)

</div>

---

## 📐 Architecture

```
Datadog Agent (every 60s)
    └── checks.d/dns_monitor.py          ← thin Python wrapper (triggers binary)
            └── dns-monitor.exe          ← Go binary (all the real work)
                    ├── WMI  → service health, perfmon, zones, process
                    ├── UDP/53 probe → forwarder availability + latency
                    ├── DNS  → resolution response time
                    └── DogStatsD UDP → pushes all metrics → Datadog
```

**No Task Scheduler. No long-running daemon. No PowerShell runtime dependency.**  
The Datadog Agent owns scheduling and supervision. The binary owns collection.

---

## 📁 Directory Structure

```text
datadog-dns-integration/
├── main.go                          # Binary entry point
├── collector/                       # All DNS metric collectors
│   ├── collector.go                 # Orchestrator — runs all collectors
│   ├── service.go                   # dns.service.* via SCM/WMI
│   ├── perfmon.go                   # dns.performance.* via WMI Perfmon
│   ├── forwarder.go                 # dns.forwarders.* via UDP/53 probe
│   ├── resolution.go                # dns.resolution.* via DNS probe
│   ├── zones.go                     # dns.zones.* via MicrosoftDNS WMI
│   └── process.go                   # dns.process.* via WMI
├── statsd/client.go                 # DogStatsD UDP client (batched)
├── config/config.go                 # YAML config loader
├── checks.d/dns_monitor.py          # Datadog Agent check wrapper
├── conf.d/dns_monitor.d/conf.yaml   # Agent check configuration
├── dns-monitor-config.yaml.example  # Binary configuration template
├── scripts/setup.ps1                # One-click installer
├── Makefile                         # make build → dns-monitor.exe
├── go.mod
└── README.md
```

**Deploy to Windows DNS Server:**
```text
C:\ProgramData\Datadog\
├── checks.d\dns_monitor.py
├── conf.d\dns_monitor.d\conf.yaml
├── dns-monitor.exe
└── dns-monitor-config.yaml
```

---

## 📊 Metrics Reference

### `dns.service.*` — Service Health

> Tags: `env`, `host`, `role:dns`, `dns_server`, `category:service`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.service.up` | gauge | DNS service running `1=up 0=down` |
| `dns.service.status` | gauge | DogStatsD service check `0=OK 2=CRITICAL` |
| `dns.service.start_auto` | gauge | StartType is Automatic `1=yes 0=no` |
| `dns.service.wid_running` | gauge | Windows Internal Database `1=up 0=down -1=N/A` |

---

### `dns.resolution.*` — Resolution Response Time ⭐

> Tags: `env`, `host`, `role:dns`, `category:resolution`, `probe_scope`, `zone`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.resolution.latency_ms` | distribution | P50/P95/P99 resolution RTT in milliseconds |
| `dns.resolution.status` | gauge | Probe result `1=success 0=failed` |
| `dns.resolution.service_check` | gauge | Threshold check `0=OK 1=WARN 2=CRIT` |

`probe_scope:baseline` — server's own hostname (always emitted, guaranteed signal)  
`probe_scope:external` — configured external probe domain

---

### `dns.forwarders.*` — Forwarder Availability ⭐

> Tags: `env`, `host`, `role:dns`, `category:forwarders`, `forwarder_ip`, `forwarder_subnet`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.forwarders.availability` | gauge | DNS resolution probe result `1=up 0=down` per forwarder |
| `dns.forwarders.availability_pct` | gauge | Fleet-level availability percentage |
| `dns.forwarders.available_count` | gauge | Number of forwarders currently up |
| `dns.forwarders.degraded_count` | gauge | Number of forwarders currently down |
| `dns.forwarders.configured_count` | gauge | Total forwarders configured |
| `dns.forwarders.probe_latency_ms` | distribution | UDP/53 round-trip time per forwarder |
| `dns.forwarders.tcp_reachable` | gauge | TCP/53 secondary signal `1=reachable` |
| `dns.forwarders.resolver_broken` | gauge | TCP up but DNS failing `1=broken resolver` |

> **Note:** Forwarder availability is tested via a real DNS query to UDP/53 with a cache-busting random subdomain. NXDOMAIN = forwarder UP. Timeout/SERVFAIL = forwarder DOWN. TCP/53 is a secondary diagnostic signal only.

---

### `dns.performance.*` — Query Volume & Rate Counters

> Tags: `env`, `host`, `role:dns`, `category:performance`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.performance.queries_received_total` | counter | Total queries received |
| `dns.performance.responses_sent_total` | counter | Total responses sent |
| `dns.performance.udp_queries_total` | counter | UDP queries |
| `dns.performance.tcp_queries_total` | counter | TCP queries (zone transfers, large responses) |
| `dns.performance.recursive_queries_total` | counter | Queries requiring upstream resolution |
| `dns.performance.recursive_query_failures_total` | counter | Recursive resolution failures |
| `dns.performance.recursive_query_timeouts_total` | counter | Recursive resolution timeouts |
| `dns.performance.dynamic_updates_total` | counter | Dynamic DNS update requests |
| `dns.performance.secure_updates_total` | counter | Secure dynamic updates |
| `dns.performance.zone_transfer_requests_total` | counter | AXFR/IXFR requests received |
| `dns.performance.zone_transfer_success_total` | counter | Successful zone transfers |
| `dns.performance.zone_transfer_failures_total` | counter | Failed zone transfers 🔴 |
| `dns.performance.notify_sent_total` | counter | NOTIFY messages sent |
| `dns.performance.notify_received_total` | counter | NOTIFY messages received |
| `dns.performance.unmatched_responses_total` | counter | Responses with no matching query |

---

### `dns.zones.*` — Zone Health

> Tags: `env`, `host`, `role:dns`, `category:zones`, `zone`, `zone_type`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.zones.total_count` | gauge | Total zones configured |
| `dns.zones.primary_count` | gauge | Primary zones |
| `dns.zones.secondary_count` | gauge | Secondary zones |
| `dns.zones.stub_count` | gauge | Stub / conditional forwarder zones |
| `dns.zones.ad_integrated_count` | gauge | AD-integrated zones |
| `dns.zones.is_paused` | gauge | Zone paused `1=yes` per zone |
| `dns.zones.is_shutdown` | gauge | Zone shutdown `1=yes` per zone |
| `dns.zones.ad_integrated` | gauge | AD-integrated flag per zone |

---

### `dns.process.*` — DNS Process (dns.exe only)

> Tags: `env`, `host`, `role:dns`, `category:process`, `process:dns`, `memory_source`

| Metric | Type | Description |
|--------|------|-------------|
| `dns.process.working_set_mb` | gauge | Working set memory (MB) |
| `dns.process.virtual_mem_mb` | gauge | Virtual memory (MB) |
| `dns.process.cpu_pct` | gauge | CPU % scoped to dns.exe process |
| `dns.process.thread_count` | gauge | Active thread count |
| `dns.process.handle_count` | gauge | Handle count |
| `dns.process.io_read_ops_total` | counter | Read I/O operations |
| `dns.process.io_write_ops_total` | counter | Write I/O operations |

> Host-level CPU and memory are already collected by the Datadog Agent. These metrics are scoped to `dns.exe` only.

---

### `dns.monitor.*` — Self-Monitoring

| Metric | Type | Description |
|--------|------|-------------|
| `dns.monitor.collection_duration_ms` | gauge | Time to complete one full cycle (ms) |
| `dns.monitor.metrics_emitted` | gauge | Total metrics pushed per cycle |

---

## ⭐ Client Requirements Coverage

| Requirement | Metric | Guaranteed every cycle |
|-------------|--------|----------------------|
| DNS Resolution Response Time | `dns.resolution.latency_ms` | ✅ Baseline probe always runs |
| DNS Server Service | `dns.service.up` | ✅ WMI query always runs |
| Forwarder Availability | `dns.forwarders.availability` | ✅ UDP/53 probe per forwarder |

---

## ⚙️ System Requirements

| Requirement | Version |
|-------------|---------|
| Windows Server | 2016 / 2019 / 2022 / 2025 |
| DNS Server Role | Installed & Running |
| Datadog Agent | v7+ (DogStatsD on `127.0.0.1:8125`) |
| Privileges | Administrator / SYSTEM |
| Go (build only) | 1.22+ |

---

## 1️⃣ Build the Binary

```bash
# Clone the repo
git clone https://github.com/ZoosGlobal/datadog-dns-integration
cd datadog-dns-integration

# Download dependencies
make deps

# Cross-compile for Windows amd64 (runs on any OS)
make build
# Output: dist/dns-monitor.exe
```

---

## 2️⃣ One-Click Setup (Recommended)

Copy `dist\dns-monitor.exe`, `scripts\setup.ps1`, and the repo to the DNS server, then run as **Administrator**:

```powershell
PowerShell.exe -ExecutionPolicy Bypass -File .\scripts\setup.ps1
```

`setup.ps1` performs 8 steps automatically:

```text
[1/8]  Validate Datadog Agent is running
[2/8]  Verify DogStatsD on UDP :8125
[3/8]  Validate DNS Server role is running
[4/8]  Copy binary and config to C:\ProgramData\Datadog\
[5/8]  Install checks.d Python wrapper
[6/8]  Install conf.d check configuration
[7/8]  Dry-run — binary executes once
[8/8]  Restart Datadog Agent to pick up new check
```

---

## 3️⃣ Manual Setup

```powershell
# 1. Copy files
Copy-Item dist\dns-monitor.exe           C:\ProgramData\Datadog\
Copy-Item dns-monitor-config.yaml.example C:\ProgramData\Datadog\dns-monitor-config.yaml
Copy-Item checks.d\dns_monitor.py        C:\ProgramData\Datadog\checks.d\
New-Item -ItemType Directory             C:\ProgramData\Datadog\conf.d\dns_monitor.d\ -Force
Copy-Item conf.d\dns_monitor.d\conf.yaml C:\ProgramData\Datadog\conf.d\dns_monitor.d\

# 2. Edit config — add your forwarder IPs
notepad C:\ProgramData\Datadog\dns-monitor-config.yaml

# 3. Test the binary directly
C:\ProgramData\Datadog\dns-monitor.exe --config C:\ProgramData\Datadog\dns-monitor-config.yaml

# 4. Verify the Agent check
& "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" check dns_monitor

# 5. Restart Agent
Restart-Service datadogagent
```

**Verify in Datadog:**  
Metrics Explorer → search `dns.service.up`

---

## 4️⃣ Configuration

Edit `C:\ProgramData\Datadog\dns-monitor-config.yaml`:

```yaml
statsd_host: "127.0.0.1"
statsd_port: 8125

env: "production"

global_tags:
  - "service:dns"
  - "team:infrastructure"

resolution_probe_domain: "www.google.com"
resolution_warn_ms: 100
resolution_crit_ms: 500

forwarder_ips:
  - "8.8.8.8"
  - "1.1.1.1"

forwarder_probe_domain: "example.com"
forwarder_timeout_sec: 5
```

---

## 5️⃣ Pre-built Datadog Monitors

### 🔴 DNS Service Down — Immediate Alert

```text
Query   : max(last_2m):max:dns.service.up{*} by {host} < 1
Alert   : < 1
Message : 🔴 DNS Server service is DOWN on {{host.name}} — name resolution will fail immediately.
```

### ⚠️ Resolution Latency — Warning / Critical

```text
Query    : avg(last_5m):avg:dns.resolution.latency_ms{probe_scope:baseline} by {host} > 100
Warning  : > 100ms
Critical : > 500ms
Message  : ⚠️ DNS resolution latency is {{value}}ms on {{host.name}} — clients may experience slow lookups.
```

### 🔴 Forwarder Down — Per-Forwarder Alert

```text
Query   : min(last_5m):min:dns.forwarders.availability{*} by {host,forwarder_ip} < 1
Alert   : < 1
Message : 🔴 Forwarder {{forwarder_ip.name}} is DOWN on {{host.name}} — external resolution degraded.
```

### ⚠️ Forwarder Fleet Degraded

```text
Query   : min(last_5m):min:dns.forwarders.availability_pct{*} by {host} < 100
Warning : < 100
Critical: < 50
Message : ⚠️ {{value}}% of forwarders available on {{host.name}}.
```

### 🔴 Zone Transfer Failures

```text
Query   : sum(last_5m):sum:dns.performance.zone_transfer_failures_total{*} by {host} > 0
Alert   : > 0
Message : 🔴 Zone transfer failure detected on {{host.name}} — secondary zones serving stale data.
```

### ⚠️ Resolver Process Broken

```text
Query   : max(last_5m):max:dns.forwarders.resolver_broken{*} by {host,forwarder_ip} > 0
Alert   : > 0
Message : ⚠️ Forwarder {{forwarder_ip.name}} TCP/53 reachable but DNS resolution failing — resolver process issue.
```

---

## 6️⃣ Datadog Dashboard Queries

| Widget | Query |
|--------|-------|
| DNS service up/down | `avg:dns.service.up{*} by {host}` |
| Resolution latency P95 | `p95:dns.resolution.latency_ms{probe_scope:baseline} by {host}` |
| Resolution latency P50 | `p50:dns.resolution.latency_ms{probe_scope:baseline} by {host}` |
| Forwarder availability % | `min:dns.forwarders.availability_pct{*} by {host}` |
| Forwarders up/down | `min:dns.forwarders.availability{*} by {host,forwarder_ip}` |
| Forwarder latency | `avg:dns.forwarders.probe_latency_ms{*} by {forwarder_ip}` |
| Queries/sec | `per_second(sum:dns.performance.queries_received_total{*} by {host})` |
| TCP vs UDP split | `per_second(sum:dns.performance.tcp_queries_total{*} by {host})` |
| Zone transfer failures | `sum:dns.performance.zone_transfer_failures_total{*} by {host}` |
| Recursive queries | `per_second(sum:dns.performance.recursive_queries_total{*} by {host})` |
| DNS process CPU % | `avg:dns.process.cpu_pct{*} by {host}` |
| DNS process memory | `avg:dns.process.working_set_mb{*} by {host}` |
| Total zones | `avg:dns.zones.total_count{*} by {host}` |
| Collection duration | `avg:dns.monitor.collection_duration_ms{*} by {host}` |

---

## 🛡️ Production Features

| Feature | Status |
|---------|--------|
| DNS service health (SCM/WMI) | ✅ |
| Resolution response time (baseline + external probe) | ✅ |
| Forwarder availability — DNS resolution primary signal | ✅ |
| Forwarder latency distribution (P50/P95/P99) | ✅ |
| TCP/53 secondary reachability signal | ✅ |
| Resolver-broken detection (TCP up, DNS failing) | ✅ |
| WMI Perfmon counters (15 rate counters) | ✅ |
| Zone health — primary/secondary/stub/AD-integrated | ✅ |
| DNS process metrics (CPU, memory, threads, handles) | ✅ |
| Windows Internal Database (WID) health | ✅ |
| Batched UDP DogStatsD (MTU-safe 1300-byte packets) | ✅ |
| Self-monitoring (collection duration, metrics count) | ✅ |
| No Task Scheduler dependency | ✅ |
| No PowerShell runtime dependency | ✅ |
| Single .exe — zero runtime installation | ✅ |
| Agent-managed scheduling and supervision | ✅ |
| Cross-compiled from any OS → windows/amd64 | ✅ |

---

## 🚨 Troubleshooting

| Issue | Fix |
|-------|-----|
| Metrics not in Datadog | Run `netstat -an \| findstr 8125` — verify DogStatsD is listening |
| Binary not found | Verify path in `conf.d\dns_monitor.d\conf.yaml` matches actual location |
| Check not running | Run: `& "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" check dns_monitor` |
| WMI errors in logs | Ensure running as SYSTEM or account with WMI read access |
| Forwarder showing as down | Check UDP/53 is not firewall-blocked to the forwarder IPs |
| Zero zone metrics | `MicrosoftDNS` WMI namespace requires DNS Admin rights |
| Agent check timeout | Increase `timeout` in `conf.yaml` (must be < `min_collection_interval`) |

---

## ✅ Production Checklist

- [ ] Datadog Agent installed and running
- [ ] DogStatsD listening on `127.0.0.1:8125`
- [ ] `setup.ps1` run as Administrator
- [ ] `dns-monitor-config.yaml` edited with correct forwarder IPs
- [ ] Binary test: `dns-monitor.exe --config dns-monitor-config.yaml`
- [ ] Agent check test: `agent.exe check dns_monitor`
- [ ] `dns.service.up` = `1` in Datadog Metrics Explorer
- [ ] `dns.resolution.latency_ms` appearing in Metrics Explorer
- [ ] `dns.forwarders.availability` = `1` for all forwarder IPs
- [ ] Monitor created for service down
- [ ] Monitor created for resolution latency
- [ ] Monitor created for forwarder availability

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

**Version 1.0.0 · Last Updated: April 2026**

© 2026 Zoos Global · [MIT License](LICENSE)

*Zoos Global is a Datadog Premier Partner*

</div>
