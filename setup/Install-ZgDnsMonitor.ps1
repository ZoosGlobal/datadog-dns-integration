# ==============================================================================
#
#   ███████╗ ██████╗  ██████╗███████╗     ██████╗ ██╗      ██████╗ ██████╗  █████╗ ██╗
#   ╚══███╔╝██╔═══██╗██╔═══██╗██╔════╝    ██╔════╝ ██║     ██╔═══██╗██╔══██╗██╔══██╗██║
#     ███╔╝ ██║   ██║██║   ██║███████╗    ██║  ███╗██║     ██║   ██║██████╔╝███████║██║
#    ███╔╝  ██║   ██║██║   ██║╚════██║    ██║   ██║██║     ██║   ██║██╔══██╗██╔══██║██║
#   ███████╗╚██████╔╝╚██████╔╝███████║     ╚██████╔╝███████╗╚██████╔╝██████╔╝██║  ██║███████╗
#   ╚══════╝ ╚═════╝  ╚═════╝ ╚══════╝      ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝
#
# ==============================================================================
#  Script    : Install-ZgDnsMonitor.ps1
#  Version   : 2.2.0
#  Purpose   : Deploys the Zoos Global DNS Monitor integration for Datadog.
#              Copies files from the local repository (no internet required),
#              backs up any existing files, restarts the Agent, and creates a
#              success marker file only when installation completes successfully.
#              All configuration (domains, env, thresholds) is managed directly
#              in dns_monitor.d\conf.yaml in the repository -- no patching here.
#
#  Repo layout (paths relative to this script at setup\Install-ZgDnsMonitor.ps1):
#    ..\dns_moniter.py          ->  C:\ProgramData\Datadog\checks.d\dns_monitor.py
#    ..\dns_monitor.d\conf.yaml ->  C:\ProgramData\Datadog\conf.d\dns_monitor.d\conf.yaml
#
#  Usage (run as Administrator):
#    PowerShell.exe -ExecutionPolicy Bypass -File .\setup\Install-ZgDnsMonitor.ps1
# ------------------------------------------------------------------------------
#  Author    : Shivam Anand
#  Title     : Sr. DevOps Engineer | Engineering
#  Org       : Zoos Global
#  Email     : shivam.anand@zoosglobal.com
#  Web       : https://www.zoosglobal.com
#  Address   : Violena, Pali Hill, Bandra West, Mumbai - 400050
# ------------------------------------------------------------------------------
#  Platform  : Windows Server 2019 / 2022 / 2025
#  Requires  : Datadog Agent v7+  (DogStatsD on 127.0.0.1:8125)
#              PowerShell 5.1+
#              No internet access required -- all files sourced from repo
# ==============================================================================

#Requires -RunAsAdministrator

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ==============================================================================
# CONFIGURATION
# ==============================================================================

$SCRIPT_VERSION = "2.2.0"

$DD_ROOT         = "C:\ProgramData\Datadog"
$DD_SERVICE      = "datadogagent"
$SUCCESS_FILE    = "C:\temp\dns_integraion.txt"
$SUCCESS_DIR     = Split-Path -Path $SUCCESS_FILE -Parent
$CHECK_NAME      = "dns_monitor"
$AGENT_EXE       = "C:\Program Files\Datadog\Datadog Agent\bin\agent.exe"

# ==============================================================================
# RESOLVE PATHS -- relative to this script's own location
# ==============================================================================
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Definition
$REPO_ROOT  = Split-Path -Parent $SCRIPT_DIR

# Source files (from repo -- no internet needed)
$PY_SRC   = Join-Path $REPO_ROOT "dns_moniter.py"
$CONF_SRC = Join-Path $REPO_ROOT "dns_monitor.d\conf.yaml"

# Destination paths inside the Datadog Agent directory tree
$CHECKS_DIR = Join-Path $DD_ROOT "checks.d"
$CONF_DIR   = Join-Path $DD_ROOT "conf.d\dns_monitor.d"
$PY_DEST    = Join-Path $CHECKS_DIR "dns_monitor.py"
$CONF_DEST  = Join-Path $CONF_DIR   "conf.yaml"

# ==============================================================================
# HELPERS
# ==============================================================================

function Write-Step($msg) {
    Write-Host "`n[$([char]0x25B6)] $msg" -ForegroundColor Cyan
}

function Write-OK($msg) {
    Write-Host "  [OK] $msg" -ForegroundColor Green
}

function Write-Warn($msg) {
    Write-Host "  [!!] $msg" -ForegroundColor Yellow
}

function Write-Fail($msg) {
    Write-Host "  [XX] $msg" -ForegroundColor Red
}

function Backup-File($path) {
    if (Test-Path $path) {
        $backup = "$path.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
        Copy-Item -Path $path -Destination $backup -Force
        Write-Warn "Existing file backed up -> $backup"
    }
}

function Ensure-Directory($path) {
    if (-not (Test-Path $path)) {
        New-Item -ItemType Directory -Path $path -Force | Out-Null
        Write-OK "Created : $path"
    }
    else {
        Write-OK "Exists  : $path"
    }
}

# ==============================================================================
# BANNER
# ==============================================================================
Write-Host ""
Write-Host "  Zoos Global -- DNS Monitor Installer v$SCRIPT_VERSION" -ForegroundColor White
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
}
else {
    Write-Fail "NOT FOUND: $PY_SRC"
    Write-Fail "Expected dns_moniter.py at repo root. Ensure your clone is complete."
    $srcErrors++
}

if (Test-Path $CONF_SRC) {
    Write-OK "Found: $CONF_SRC"
}
else {
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
    Write-Fail "Datadog Agent service '$DD_SERVICE' not found."
    Write-Fail "Install the Datadog Agent service before running this script."
    exit 1
}
Write-OK "Datadog Agent service found (status: $($svc.Status))"

$statsd = netstat -an | Select-String "8125"
if ($statsd) {
    Write-OK "DogStatsD activity detected on port 8125"
}
else {
    Write-Warn "DogStatsD not detected on port 8125 -- verify dogstatsd_port: 8125 in datadog.yaml"
}

# ==============================================================================
# STEP 3 : Create Datadog directory structure
# ==============================================================================
Write-Step "Creating Datadog directory structure"

Ensure-Directory $CHECKS_DIR
Ensure-Directory $CONF_DIR

# ==============================================================================
# STEP 4 : Remove old success marker before install
# ==============================================================================
Write-Step "Clearing previous success marker"

if (Test-Path $SUCCESS_FILE) {
    Remove-Item -Path $SUCCESS_FILE -Force
    Write-OK "Removed old marker: $SUCCESS_FILE"
}
else {
    Write-OK "No previous marker found"
}

# ==============================================================================
# STEP 5 : Copy checks.d\dns_monitor.py
# ==============================================================================
Write-Step "Installing checks.d\dns_monitor.py"

Backup-File $PY_DEST
Copy-Item -Path $PY_SRC -Destination $PY_DEST -Force

if (-not (Test-Path $PY_DEST)) {
    Write-Fail "dns_monitor.py was not copied to $PY_DEST"
    exit 1
}

if ((Get-Item $PY_DEST).Length -le 0) {
    Write-Fail "Installed dns_monitor.py is empty"
    exit 1
}

if ((Get-Content $PY_DEST -Raw) -notmatch "class DnsMonitorCheck") {
    Write-Fail "Installed dns_monitor.py does not look valid (DnsMonitorCheck class not found)"
    exit 1
}

Write-OK "Installed and validated: $PY_DEST"

# ==============================================================================
# STEP 6 : Copy conf.d\dns_monitor.d\conf.yaml
# ==============================================================================
Write-Step "Installing conf.d\dns_monitor.d\conf.yaml"

Backup-File $CONF_DEST
Copy-Item -Path $CONF_SRC -Destination $CONF_DEST -Force

if (-not (Test-Path $CONF_DEST)) {
    Write-Fail "conf.yaml was not copied to $CONF_DEST"
    exit 1
}

if ((Get-Item $CONF_DEST).Length -le 0) {
    Write-Fail "Installed conf.yaml is empty"
    exit 1
}

Write-OK "Installed: $CONF_DEST"

# ==============================================================================
# STEP 7 : Verify installed files
# ==============================================================================
Write-Step "Verifying installed files"

$errors = 0

if (Test-Path $PY_DEST) {
    $pySize = (Get-Item $PY_DEST).Length
    if ($pySize -gt 0) {
        Write-OK "dns_monitor.py   $pySize bytes  ->  $PY_DEST"
    }
    else {
        Write-Fail "dns_monitor.py exists but is empty: $PY_DEST"
        $errors++
    }
}
else {
    Write-Fail "dns_monitor.py   MISSING at $PY_DEST"
    $errors++
}

if (Test-Path $CONF_DEST) {
    $confSize = (Get-Item $CONF_DEST).Length
    if ($confSize -gt 0) {
        Write-OK "conf.yaml        $confSize bytes  ->  $CONF_DEST"
    }
    else {
        Write-Fail "conf.yaml exists but is empty: $CONF_DEST"
        $errors++
    }
}
else {
    Write-Fail "conf.yaml        MISSING at $CONF_DEST"
    $errors++
}

if ($errors -gt 0) {
    Write-Fail "$errors verification error(s) -- review output above."
    exit 1
}

# ==============================================================================
# STEP 8 : Restart Datadog Agent
# ==============================================================================
Write-Step "Restarting Datadog Agent"

try {
    Restart-Service -Name $DD_SERVICE -Force -ErrorAction Stop
    Start-Sleep -Seconds 8

    $svcAfter = Get-Service -Name $DD_SERVICE -ErrorAction Stop
    if ($svcAfter.Status -ne 'Running') {
        Write-Fail "Agent restart did not complete successfully. Current status: $($svcAfter.Status)"
        exit 1
    }

    Write-OK "Agent restarted -- status: $($svcAfter.Status)"
}
catch {
    Write-Fail "Could not restart Agent automatically: $_"
    exit 1
}

# ==============================================================================
# STEP 9 : Create success marker file
# ==============================================================================
Write-Step "Creating success marker file"

try {
    Ensure-Directory $SUCCESS_DIR

    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $content = @"
status=success
installer_version=$SCRIPT_VERSION
check_name=$CHECK_NAME
host=$env:COMPUTERNAME
time=$timestamp
check_file=$PY_DEST
conf_file=$CONF_DEST
"@

    Set-Content -Path $SUCCESS_FILE -Value $content -Encoding UTF8
    Write-OK "Success marker created: $SUCCESS_FILE"
}
catch {
    Write-Fail "Could not create success marker file: $_"
    exit 1
}

# ==============================================================================
# STEP 10 : Post-install validation hints
# ==============================================================================
Write-Step "Post-install validation"

Write-Host ""
Write-Host "  Run these on the DNS server to confirm the check is working:" -ForegroundColor White
Write-Host ""
Write-Host "  # Test the check directly" -ForegroundColor Gray
Write-Host "  & `"$AGENT_EXE`" check $CHECK_NAME" -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Confirm the check appears in Agent status" -ForegroundColor Gray
Write-Host "  & `"$AGENT_EXE`" status | findstr $CHECK_NAME" -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Confirm DogStatsD is receiving data" -ForegroundColor Gray
Write-Host "  netstat -an | findstr 8125" -ForegroundColor Yellow
Write-Host ""
Write-Host "  # Metrics to verify in Datadog Metrics Explorer:" -ForegroundColor Gray
Write-Host "    zg.dns.service.up              -> should be 1" -ForegroundColor Gray
Write-Host "    zg.dns.resolution.up           -> should be 1  (probe_scope:baseline)" -ForegroundColor Gray
Write-Host "    zg.dns.forwarders.availability -> should be 1  per forwarder_ip" -ForegroundColor Gray
Write-Host ""

# ==============================================================================
Write-Host ""
Write-Host "  Installation complete." -ForegroundColor Green
Write-Host "  (c) Zoos Global | https://www.zoosglobal.com | shivam.anand@zoosglobal.com" -ForegroundColor Gray
Write-Host ""
# ==============================================================================