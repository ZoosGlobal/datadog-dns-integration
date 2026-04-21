#Requires -Version 5.1
#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Zoos Global — Microsoft DNS Monitor for Datadog — One-Click Setup

.DESCRIPTION
    Installs the DNS Monitor binary and configures the Datadog Agent
    to run it every 60 seconds via checks.d.

    Steps:
    [1/8]  Validate Datadog Agent is running
    [2/8]  Verify DogStatsD on UDP :8125
    [3/8]  Validate DNS Server role is running
    [4/8]  Copy binary and config to C:\ProgramData\Datadog\
    [5/8]  Install checks.d Python wrapper
    [6/8]  Install conf.d check configuration
    [7/8]  Dry-run — binary runs once, metrics printed not sent
    [8/8]  Restart Datadog Agent to pick up new check

.EXAMPLE
    PowerShell.exe -ExecutionPolicy Bypass -File .\setup.ps1

.EXAMPLE
    PowerShell.exe -ExecutionPolicy Bypass -File .\setup.ps1 -Env production -Uninstall
#>

[CmdletBinding()]
param(
    [string] $Env         = 'production',
    [string] $DDAgentPath = 'C:\ProgramData\Datadog',
    [switch] $Uninstall
)

$ErrorActionPreference = 'Stop'

$BinarySource  = Join-Path $PSScriptRoot '..\dist\dns-monitor.exe'
$BinaryDest    = Join-Path $DDAgentPath  'dns-monitor.exe'
$ConfigSource  = Join-Path $PSScriptRoot '..\dns-monitor-config.yaml.example'
$ConfigDest    = Join-Path $DDAgentPath  'dns-monitor-config.yaml'
$CheckSource   = Join-Path $PSScriptRoot '..\checks.d\dns_monitor.py'
$CheckDest     = Join-Path $DDAgentPath  'checks.d\dns_monitor.py'
$ConfSource    = Join-Path $PSScriptRoot '..\conf.d\dns_monitor.d\conf.yaml'
$ConfDest      = Join-Path $DDAgentPath  'conf.d\dns_monitor.d\conf.yaml'

function Write-Step { param([int]$N,[int]$T,[string]$M) Write-Host "  [$N/$T] $M" -ForegroundColor Cyan   }
function Write-OK   { param([string]$M) Write-Host "         OK  $M"   -ForegroundColor Green  }
function Write-Fail { param([string]$M) Write-Host "         FAIL $M"  -ForegroundColor Red; exit 1 }
function Write-Info { param([string]$M) Write-Host "         -->  $M"  -ForegroundColor Gray   }

# ── Banner ────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  ================================================================" -ForegroundColor Cyan
Write-Host "   Zoos Global — Microsoft DNS Monitor for Datadog   v1.0.0"       -ForegroundColor Cyan
Write-Host "   Datadog Premier Partner | www.zoosglobal.com"                   -ForegroundColor Cyan
Write-Host "  ================================================================" -ForegroundColor Cyan
Write-Host ""

# ── Uninstall ─────────────────────────────────────────────────────────────────
if ($Uninstall) {
    Write-Host "  [UNINSTALL] Removing DNS Monitor..." -ForegroundColor Yellow
    @($BinaryDest, $ConfigDest, $CheckDest, $ConfDest) | ForEach-Object {
        if (Test-Path $_) { Remove-Item $_ -Force; Write-Host "  Removed: $_" -ForegroundColor Gray }
    }
    $confDir = Split-Path $ConfDest -Parent
    if (Test-Path $confDir) { Remove-Item $confDir -Recurse -Force }
    Write-Host "  Restarting Datadog Agent..." -ForegroundColor Yellow
    Restart-Service -Name datadogagent -Force
    Write-Host "  [UNINSTALL] Complete." -ForegroundColor Green
    exit 0
}

# ── Step 1 — Validate Datadog Agent ──────────────────────────────────────────
Write-Step 1 8 'Validate Datadog Agent is running'
try {
    $svc = Get-Service -Name 'datadogagent' -ErrorAction Stop
    if ($svc.Status -ne 'Running') { Write-Fail "datadogagent is not running. Start it first." }
    Write-OK "datadogagent is Running"
} catch { Write-Fail "Datadog Agent not found. Install from: https://app.datadoghq.com/account/settings#agent/windows" }

# ── Step 2 — Verify DogStatsD ────────────────────────────────────────────────
Write-Step 2 8 'Verify DogStatsD on UDP :8125'
try {
    $udp = [System.Net.Sockets.UdpClient]::new()
    $udp.Connect('127.0.0.1', 8125)
    $b = [System.Text.Encoding]::UTF8.GetBytes('dns.setup.check:1|c|#env:setup')
    [void]$udp.Send($b, $b.Length)
    $udp.Dispose()
    Write-OK "DogStatsD reachable at 127.0.0.1:8125"
} catch { Write-Fail "Cannot reach DogStatsD: $($_.Exception.Message)" }

# ── Step 3 — Validate DNS Server ─────────────────────────────────────────────
Write-Step 3 8 'Validate DNS Server role is running'
try {
    $dns = Get-Service -Name 'DNS' -ErrorAction Stop
    if ($dns.Status -eq 'Running') { Write-OK "DNS Server service is Running" }
    else { Write-OK "DNS Server service found (Status: $($dns.Status)) — monitor will reflect actual state" }
} catch { Write-Fail "DNS Server role not found. Install: Install-WindowsFeature DNS" }

# ── Step 4 — Copy binary and config ──────────────────────────────────────────
Write-Step 4 8 "Copy binary and config to $DDAgentPath"
if (-not (Test-Path $BinarySource)) {
    Write-Fail "dns-monitor.exe not found at $BinarySource`n         Run: make build  (from repo root on a Windows machine or via cross-compile)"
}
Copy-Item $BinarySource $BinaryDest -Force
Write-OK "Copied dns-monitor.exe → $BinaryDest"

if (-not (Test-Path $ConfigDest)) {
    Copy-Item $ConfigSource $ConfigDest -Force
    # Inject environment
    (Get-Content $ConfigDest) -replace 'env: "production"', "env: `"$Env`"" | Set-Content $ConfigDest
    Write-OK "Created config → $ConfigDest"
    Write-Info "Edit $ConfigDest to add your forwarder IPs"
} else {
    Write-OK "Config already exists at $ConfigDest (not overwritten)"
}

# ── Step 5 — Install checks.d wrapper ────────────────────────────────────────
Write-Step 5 8 'Install checks.d Python wrapper'
$checksDir = Split-Path $CheckDest -Parent
if (-not (Test-Path $checksDir)) { New-Item -ItemType Directory -Path $checksDir -Force | Out-Null }
Copy-Item $CheckSource $CheckDest -Force
Write-OK "Installed → $CheckDest"

# ── Step 6 — Install conf.d config ───────────────────────────────────────────
Write-Step 6 8 'Install conf.d check configuration'
$confDir = Split-Path $ConfDest -Parent
if (-not (Test-Path $confDir)) { New-Item -ItemType Directory -Path $confDir -Force | Out-Null }
Copy-Item $ConfSource $ConfDest -Force
# Patch binary and config paths into conf.yaml
(Get-Content $ConfDest) `
    -replace "C:\\\\ProgramData\\\\Datadog\\\\dns-monitor.exe", $BinaryDest `
    -replace "C:\\\\ProgramData\\\\Datadog\\\\dns-monitor-config.yaml", $ConfigDest |
    Set-Content $ConfDest
Write-OK "Installed → $ConfDest"

# ── Step 7 — Dry-run ─────────────────────────────────────────────────────────
Write-Step 7 8 'Dry-run — binary runs once (metrics go to DogStatsD normally)'
Write-Info "Running: dns-monitor.exe --config $ConfigDest"
Write-Host ""
try {
    & $BinaryDest --config $ConfigDest
    Write-Host ""
    Write-OK "Binary executed successfully — check Datadog Metrics Explorer: dns.service.up"
} catch {
    Write-Fail "Binary execution failed: $($_.Exception.Message)"
}

# ── Step 8 — Restart Agent ───────────────────────────────────────────────────
Write-Step 8 8 'Restart Datadog Agent to pick up new check'
Restart-Service -Name datadogagent -Force
Start-Sleep -Seconds 5
$agentStatus = (Get-Service -Name datadogagent).Status
Write-OK "datadogagent restarted — Status: $agentStatus"

# ── Summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  ================================================================" -ForegroundColor Green
Write-Host "   DNS Monitor installed successfully!" -ForegroundColor Green
Write-Host "  ================================================================" -ForegroundColor Green
Write-Host "   Binary   : $BinaryDest" -ForegroundColor White
Write-Host "   Config   : $ConfigDest" -ForegroundColor White
Write-Host "   Check    : $CheckDest" -ForegroundColor White
Write-Host "   Interval : 60s (managed by Datadog Agent)" -ForegroundColor White
Write-Host "  ================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor Cyan
Write-Host "  1. Edit config: $ConfigDest" -ForegroundColor White
Write-Host "     Add your forwarder IPs under forwarder_ips:" -ForegroundColor Gray
Write-Host "  2. Verify check: datadog-agent check dns_monitor" -ForegroundColor White
Write-Host "  3. Metrics Explorer → search: dns.service.up" -ForegroundColor White
Write-Host "  4. Uninstall: .\setup.ps1 -Uninstall" -ForegroundColor White
Write-Host ""