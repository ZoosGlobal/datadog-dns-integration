# ==============================================================================
#
#   ███████╗ ██████╗  ██████╗███████╗     ██████╗ ██╗      ██████╗ ██████╗  █████╗ ██╗
#   ╚══███╔╝██╔═══██╗██╔═══██╗██╔════╝    ██╔════╝ ██║     ██╔═══██╗██╔══██╗██╔══██╗██║
#     ███╔╝ ██║   ██║██║   ██║███████╗    ██║  ███╗██║     ██║   ██║██████╔╝███████║██║
#    ███╔╝  ██║   ██║██║   ██║╚════██║    ██║   ██║██║     ██║   ██║██╔══██╗██╔══██║██║
#   ███████╗╚██████╔╝╚██████╔╝███████║    ╚██████╔╝███████╗╚██████╔╝██████╔╝██║  ██║███████╗
#   ╚══════╝ ╚═════╝  ╚═════╝ ╚══════╝     ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝
#
# ==============================================================================
#  Script    : Install-ZgDnsMonitor.ps1
#  Version   : 2.0.0
#  Purpose   : Deploys the Zoos Global DNS Monitor integration for Datadog.
#              Copies files from the local repository (no internet required),
#              injects client-specific resolution probe domains into conf.yaml,
#              backs up any existing files, and optionally restarts the Agent.
#
#  Repo layout (paths relative to this script at setup\Install-ZgDnsMonitor.ps1):
#    ..\dns_moniter.py          →  C:\ProgramData\Datadog\checks.d\dns_monitor.py
#    ..\dns_monitor.d\conf.yaml →  C:\ProgramData\Datadog\conf.d\dns_monitor.d\conf.yaml
#
#  Usage (run as Administrator):
#    PowerShell.exe -ExecutionPolicy Bypass -File .\setup\Install-ZgDnsMonitor.ps1
# ------------------------------------------------------------------------------
#  Author    : Shivam Anand
#  Title     : Sr. DevOps Engineer | Engineering
#  Org       : Zoos Global
#  Email     : shivam.anand@zoosglobal.com
#  Web       : www.zoosglobal.com
#  Address   : Violena, Pali Hill, Bandra West, Mumbai - 400050
# ------------------------------------------------------------------------------
#  Platform  : Windows Server 2019 / 2022 / 2025
#  Requires  : Datadog Agent v7+  (DogStatsD on 127.0.0.1:8125)
#              PowerShell 5.1+
#              No internet access required — all files sourced from repo
# ==============================================================================

#Requires -RunAsAdministrator

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ==============================================================================
# CONFIGURATION — edit these values per client engagement
# ==============================================================================

# Client-specific resolution probe domains injected into conf.yaml
$RESOLUTION_DOMAINS = @(
    "planetexl.exlservice.com",
    "corp.exlservice.com"
)

# Environment tag written into conf.yaml
$ENV_TAG = "production"

# Datadog Agent root and service name
$DD_ROOT    = "C:\ProgramData\Datadog"
$DD_SERVICE = "datadogagent"

# ==============================================================================
# RESOLVE PATHS — relative to this script's own location
#
# This script lives at : <repo>\setup\Install-ZgDnsMonitor.ps1
# Repo root            : <repo>\
# Source files         : <repo>\dns_moniter.py
#                        <repo>\dns_monitor.d\conf.yaml
# ==============================================================================
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Definition
$REPO_ROOT  = Split-Path -Parent $SCRIPT_DIR

# Source files (resolved from repo root — no internet needed)
$PY_SRC     = Join-Path $REPO_ROOT "dns_moniter.py"           # typo in repo filename is intentional
$CONF_SRC   = Join-Path $REPO_ROOT "dns_monitor.d\conf.yaml"

# Destination paths inside the Datadog Agent directory tree
$CHECKS_DIR = Join-Path $DD_ROOT "checks.d"
$CONF_DIR   = Join-Path $DD_ROOT "conf.d\dns_monitor.d"
$PY_DEST    = Join-Path $CHECKS_DIR "dns_monitor.py"           # corrected filename on destination
$CONF_DEST  = Join-Path $CONF_DIR   "conf.yaml"

# ==============================================================================
# HELPERS
# ==============================================================================

function Write-Step($msg) {
    Write-Host "`n[$([char]0x25B6)] $msg" -ForegroundColor Cyan
}
function Write-OK($msg)   { Write-Host "  [OK] $msg" -ForegroundColor Green  }
function Write-Warn($msg) { Write-Host "  [!!] $msg" -ForegroundColor Yellow }
function Write-Fail($msg) { Write-Host "  [XX] $msg" -ForegroundColor Red    }

function Backup-File($path) {
    if (Test-Path $path) {
        $backup = "$path.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
        Copy-Item -Path $path -Destination $backup -Force
        Write-Warn "Existing file backed up → $backup"
    }
}

# ==============================================================================
# BANNER
# ==============================================================================
Write-Host ""
Write-Host "  Zoos Global — DNS Monitor Installer v2.0.0" -ForegroundColor White
Write-Host "  Datadog checks.d + conf.d deployment (offline)" -ForegroundColor Gray
Write-Host "  www.zoosglobal.com" -ForegroundColor Gray
Write-Host ""
Write-Host "  Repo root  : $REPO_ROOT"  -ForegroundColor Gray
Write-Host "  Script dir : $SCRIPT_DIR" -ForegroundColor Gray
Write-Host ""

# ==============================================================================
# STEP 1 : Validate source files exist in the repo
# ==============================================================================
Write-Step "Validating source files in repository"

$srcErrors = 0

if (Test-Path $PY_SRC) {
    Write-OK "Found: $PY_SRC"
} else {
    Write-Fail "NOT FOUND: $PY_SRC"
    Write-Fail "Expected dns_moniter.py at repo root. Ensure your clone is complete."
    $srcErrors++
}

if (Test-Path $CONF_SRC) {
    Write-OK "Found: $CONF_SRC"
} else {
    Write-Fail "NOT FOUND: $CONF_SRC"
    Write-Fail "Expected conf.yaml at dns_monitor.d\conf.yaml from repo root."
    $srcErrors++
}

if ($srcErrors -gt 0) {
    Write-Fail "Source file validation failed. Aborting."
    exit 1
}

# ==============================================================================
# STEP 2 : Validate Datadog Agent prerequisites
# ==============================================================================
Write-Step "Checking Datadog Agent prerequisites"

if (-not (Test-Path $DD_ROOT)) {
    Write-Fail "Datadog Agent root not found at $DD_ROOT"
    Write-Fail "Install the Datadog Agent before running this script."
    exit 1
}
Write-OK "Datadog Agent root found: $DD_ROOT"

$svc = Get-Service -Name $DD_SERVICE -ErrorAction SilentlyContinue
if (-not $svc) {
    Write-Warn "Datadog Agent service '$DD_SERVICE' not found — continuing anyway."
} else {
    Write-OK "Datadog Agent service found (status: $($svc.Status))"
}

$statsd = netstat -an | Select-String "8125"
if ($statsd) {
    Write-OK "DogStatsD is listening on port 8125"
} else {
    Write-Warn "DogStatsD not detected on port 8125 — verify dogstatsd_port: 8125 in datadog.yaml"
}

# ==============================================================================
# STEP 3 : Create Datadog directory structure
# ==============================================================================
Write-Step "Creating Datadog directory structure"

foreach ($dir in @($CHECKS_DIR, $CONF_DIR)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        Write-OK "Created : $dir"
    } else {
        Write-OK "Exists  : $dir"
    }
}

# ==============================================================================
# STEP 4 : Copy checks.d\dns_monitor.py
# ==============================================================================
Write-Step "Installing checks.d\dns_monitor.py"

Backup-File $PY_DEST
Copy-Item -Path $PY_SRC -Destination $PY_DEST -Force

if ((Get-Content $PY_DEST -Raw) -notmatch "class DnsMonitorCheck") {
    Write-Fail "Installed dns_monitor.py does not look valid (DnsMonitorCheck class not found)"
    exit 1
}
Write-OK "Installed and validated: $PY_DEST"

# ==============================================================================
# STEP 5 : Patch and install conf.d\dns_monitor.d\conf.yaml
# ==============================================================================
Write-Step "Installing conf.d\dns_monitor.d\conf.yaml"

# Read into memory for patching — source file is never modified
$confContent = Get-Content $CONF_SRC -Raw

# ── 5a. Set env tag ───────────────────────────────────────────────────────────
if ($confContent -match 'env\s*:') {
    $confContent = $confContent -replace '(?m)^\s*env\s*:.*$', "  env: `"$ENV_TAG`""
    Write-OK "Set env tag: $ENV_TAG"
} else {
    Write-Warn "Could not find 'env:' key in conf.yaml — skipping env injection"
}

# ── 5b. Build resolution_probe_domains YAML block ────────────────────────────
$domainLines = $RESOLUTION_DOMAINS | ForEach-Object { "      - `"$_`"" }
$domainBlock = "      resolution_probe_domains:`n" + ($domainLines -join "`n")

if ($confContent -match '(?ms)\s*resolution_probe_domains\s*:') {
    # Replace existing multi-domain block
    $confContent = $confContent -replace '(?ms)([ \t]*)resolution_probe_domains\s*:.*?(?=\n\s{0,6}\S|\z)', $domainBlock
    Write-OK "Replaced existing resolution_probe_domains block"
} elseif ($confContent -match '(?m)^\s*resolution_probe_domain\s*:') {
    # Replace legacy single-domain key
    $confContent = $confContent -replace '(?m)^\s*resolution_probe_domain\s*:.*$', $domainBlock
    Write-OK "Replaced legacy resolution_probe_domain with resolution_probe_domains block"
} else {
    # Key absent — append
    $confContent += "`n      # Injected by Install-ZgDnsMonitor.ps1`n$domainBlock`n"
    Write-OK "Appended resolution_probe_domains (key was absent in template)"
}

Write-OK "Resolution probe domains configured:"
foreach ($d in $RESOLUTION_DOMAINS) {
    Write-Host "           → $d" -ForegroundColor Gray
}

# ── 5c. Write patched conf.yaml to destination ───────────────────────────────
Backup-File $CONF_DEST
[System.IO.File]::WriteAllText($CONF_DEST, $confContent, [System.Text.Encoding]::UTF8)
Write-OK "Installed: $CONF_DEST"

# ==============================================================================
# STEP 6 : Verify installed files
# ==============================================================================
Write-Step "Verifying installed files"

$errors = 0

if (Test-Path $PY_DEST) {
    Write-OK "dns_monitor.py   $((Get-Item $PY_DEST).Length) bytes  →  $PY_DEST"
} else {
    Write-Fail "dns_monitor.py   MISSING at $PY_DEST"
    $errors++
}

if (Test-Path $CONF_DEST) {
    Write-OK "conf.yaml        $((Get-Item $CONF_DEST).Length) bytes  →  $CONF_DEST"
    $confVerify = Get-Content $CONF_DEST -Raw
    foreach ($d in $RESOLUTION_DOMAINS) {
        if ($confVerify -match [regex]::Escape($d)) {
            Write-OK "  Domain verified: $d"
        } else {
            Write-Warn "  Domain NOT found in conf.yaml: $d — inspect file manually"
            $errors++
        }
    }
} else {
    Write-Fail "conf.yaml        MISSING at $CONF_DEST"
    $errors++
}

if ($errors -gt 0) {
    Write-Fail "$errors verification error(s) — review output above."
    exit 1
}

# ==============================================================================
# STEP 7 : Restart Datadog Agent
# ==============================================================================
Write-Step "Restart Datadog Agent"

$restart = Read-Host "  Restart the Datadog Agent now to activate the check? [Y/N]"
if ($restart -match '^[Yy]') {
    try {
        Restart-Service -Name $DD_SERVICE -Force
        Start-Sleep -Seconds 8
        Write-OK "Agent restarted — status: $((Get-Service -Name $DD_SERVICE).Status)"
    } catch {
        Write-Warn "Could not restart Agent automatically: $_"
        Write-Warn "Run manually: Restart-Service $DD_SERVICE"
    }
} else {
    Write-Warn "Agent NOT restarted. Run manually to activate the check:"
    Write-Host "    Restart-Service $DD_SERVICE" -ForegroundColor White
}

# ==============================================================================
# STEP 8 : Post-install validation hints
# ==============================================================================
Write-Step "Post-install validation"

Write-Host ""
Write-Host "  Run these on the DNS server to confirm the check is working:" -ForegroundColor White
Write-Host ""
Write-Host "  # Test the check directly" -ForegroundColor Gray
Write-Host '  & "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" check dns_monitor' -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Confirm the check appears in Agent status" -ForegroundColor Gray
Write-Host '  & "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe" status | findstr dns_monitor' -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Confirm DogStatsD is receiving data" -ForegroundColor Gray
Write-Host "  netstat -an | findstr 8125" -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Metrics to verify in Datadog Metrics Explorer:" -ForegroundColor Gray
Write-Host "    zg.dns.service.up              → should be 1" -ForegroundColor Gray
Write-Host "    zg.dns.resolution.up           → should be 1  (probe_scope:baseline)" -ForegroundColor Gray
Write-Host "    zg.dns.forwarders.availability → should be 1  per forwarder_ip" -ForegroundColor Gray
Write-Host ""

# ==============================================================================
Write-Host ""
Write-Host "  Installation complete." -ForegroundColor Green
Write-Host "  © Zoos Global | www.zoosglobal.com | shivam.anand@zoosglobal.com" -ForegroundColor Gray
Write-Host ""
# ==============================================================================