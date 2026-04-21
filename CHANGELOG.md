# Changelog

All notable changes to this project will be documented in this file.

## [1.0.0] — April 2026

### Added
- Initial release
- Go binary (`dns-monitor.exe`) — single self-contained executable
- Datadog Agent `checks.d` integration — Agent triggers binary every 60s
- DNS service health monitoring via WMI (`dns.service.*`)
- DNS resolution response time via active probe (`dns.resolution.*`)
- Forwarder availability via UDP/53 DNS probe (`dns.forwarders.*`)
- WMI Perfmon counters — 15 rate metrics (`dns.performance.*`)
- Zone health via MicrosoftDNS WMI namespace (`dns.zones.*`)
- DNS process metrics scoped to `dns.exe` (`dns.process.*`)
- Batched DogStatsD UDP transport (MTU-safe 1300-byte packets)
- Self-monitoring metrics (`dns.monitor.*`)
- One-click `setup.ps1` installer
- Production README with metrics reference, monitors, dashboard queries
- MIT License

### Architecture
- Agent-managed scheduling — no Task Scheduler dependency
- No PowerShell runtime dependency at collection time
- Cross-compiled from any OS to `windows/amd64`
