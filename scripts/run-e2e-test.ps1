param(
  [switch]$Quick,
  [switch]$NoDb,
  [string]$ApiUrl = $env:API_URL
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ApiUrl)) {
  $ApiUrl = "http://localhost:8082"
}

$env:API_URL = $ApiUrl
if ($Quick) {
  $env:AI_RESPONSE_WAIT = "3"
  $env:STEP_DELAY = "1"
} else {
  if ([string]::IsNullOrWhiteSpace($env:AI_RESPONSE_WAIT)) {
    $env:AI_RESPONSE_WAIT = "8"
  }
  if ([string]::IsNullOrWhiteSpace($env:STEP_DELAY)) {
    $env:STEP_DELAY = "2"
  }
}
if ($NoDb) {
  $env:SKIP_DB_CHECK = "1"
}

Write-Host ""
Write-Host "=============================================="
Write-Host "  MedSpa AI Platform - E2E Test Runner (PS)"
Write-Host "=============================================="
Write-Host ""

function Get-PythonCommand {
  $python = Get-Command python -ErrorAction SilentlyContinue
  if ($python) {
    return @($python.Source)
  }
  $python3 = Get-Command python3 -ErrorAction SilentlyContinue
  if ($python3) {
    return @($python3.Source)
  }
  return $null
}

$pythonCmd = Get-PythonCommand
if (-not $pythonCmd) {
  Write-Error "Python is required but was not found on PATH. Install Python 3 and try again."
}

& $pythonCmd -c "import requests" *> $null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Installing 'requests' module..."
  & $pythonCmd -m pip install requests --quiet
  if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to install 'requests'."
  }
}

Write-Host -NoNewline "Checking API availability... "
$deadline = (Get-Date).AddSeconds(60)
$healthy = $false
while ((Get-Date) -lt $deadline) {
  try {
    $resp = Invoke-WebRequest -UseBasicParsing -Uri "$ApiUrl/health" -TimeoutSec 5
    if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) {
      $healthy = $true
      break
    }
  } catch {
    # ignore and retry
  }
  Start-Sleep -Seconds 2
}
if ($healthy) {
  Write-Host "OK"
} else {
  Write-Host "FAILED"
  Write-Host ""
  Write-Host "API is not reachable at $ApiUrl"
  Write-Host ""
  Write-Host "Start it with one of:"
  Write-Host "  docker compose up -d --build"
  Write-Host "  go run cmd/api/main.go"
  exit 1
}

$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
  & $pythonCmd scripts/e2e_full_flow.py
  exit $LASTEXITCODE
} finally {
  Pop-Location
}
