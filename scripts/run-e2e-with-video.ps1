param(
  [string]$ApiUrl = $env:API_URL,
  [switch]$Headed
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ApiUrl)) {
  $ApiUrl = "http://localhost:8082"
}
$ApiUrl = $ApiUrl.TrimEnd("/")
$env:API_URL = $ApiUrl

Write-Host ""
Write-Host "==================================================="
Write-Host "  MedSpa AI Platform - E2E Test + Video Runner (PS)"
Write-Host "==================================================="
Write-Host ""

function Get-PythonCommand {
  $projectRoot = Split-Path -Parent $PSScriptRoot
  $venvPython = Join-Path $projectRoot ".venv\\Scripts\\python.exe"
  if (Test-Path $venvPython) {
    return @($venvPython)
  }
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

function Require-EnvVar([string]$Name) {
  $value = [System.Environment]::GetEnvironmentVariable($Name)
  if ([string]::IsNullOrWhiteSpace($value)) {
    Write-Error "Missing required env var: $Name"
  }
  return $value
}

function Require-EnvValue([string]$Name, [string]$ExpectedLower) {
  $value = [System.Environment]::GetEnvironmentVariable($Name)
  if ($null -eq $value) {
    $value = ""
  }
  $value = $value.Trim()
  if ([string]::IsNullOrWhiteSpace($value)) {
    Write-Error "Missing required env var: $Name (expected '$ExpectedLower')"
  }
  if ($value.ToLowerInvariant() -ne $ExpectedLower) {
    Write-Error "Invalid env var: $Name must be '$ExpectedLower' (got '$value')"
  }
  return $value
}

$pythonCmd = Get-PythonCommand
if (-not $pythonCmd) {
  Write-Error "Python is required but was not found on PATH. Install Python 3 and try again."
}

$null = Require-EnvVar "ADMIN_JWT_SECRET"
$null = Require-EnvVar "TELNYX_WEBHOOK_SECRET"
$null = Require-EnvVar "TELNYX_API_KEY"
$null = Require-EnvVar "TELNYX_MESSAGING_PROFILE_ID"
$null = Require-EnvVar "TEST_CUSTOMER_PHONE"
$null = Require-EnvVar "TEST_CLINIC_PHONE"
$null = Require-EnvValue "SMS_PROVIDER" "telnyx"

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

& $pythonCmd -c "import requests" *> $null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Installing 'requests' module..."
  & $pythonCmd -m pip install requests --quiet
  if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to install 'requests'."
  }
}

& $pythonCmd -c "import playwright" *> $null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Installing 'playwright' module..."
  & $pythonCmd -m pip install playwright --quiet
  if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to install 'playwright'."
  }
}

Write-Host "Ensuring Playwright Chromium is installed..."
& $pythonCmd -m playwright install chromium
if ($LASTEXITCODE -ne 0) {
  Write-Error "Failed to install Playwright Chromium."
}

$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
  $scriptArgs = @("scripts/e2e_with_video.py", "--api-url", $ApiUrl)
  if ($Headed) {
    $scriptArgs += "--headed"
  }

  $output = & $pythonCmd @scriptArgs 2>&1
  $exitCode = $LASTEXITCODE
  $output | ForEach-Object { Write-Host $_ }

  $artifactsRoot = Join-Path $projectRoot "tmp\\e2e_artifacts"
  $latestArtifacts = $null
  if (Test-Path $artifactsRoot) {
    $latestArtifacts = Get-ChildItem $artifactsRoot -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
  }

  if ($exitCode -ne 0) {
    if ($latestArtifacts) {
      Write-Host ""
      Write-Host "E2E failed. Debug artifacts: $($latestArtifacts.FullName)"
    }
    exit $exitCode
  }

  $videoPath = (($output | Select-Object -Last 1) -as [string]).Trim()
  if ([string]::IsNullOrWhiteSpace($videoPath)) {
    Write-Error "E2E succeeded but did not return a video path."
  }
  if (-not (Test-Path $videoPath)) {
    Write-Host ""
    Write-Host "WARNING: Video path was returned but file was not found: $videoPath"
  }

  Write-Host ""
  Write-Host "Video saved: $videoPath"
  if ($latestArtifacts) {
    Write-Host "Run artifacts: $($latestArtifacts.FullName)"
  }
  exit 0
} finally {
  Pop-Location
}
