<#
.SYNOPSIS
    Run the 3-scenario video demo with split-screen recording.

.DESCRIPTION
    Records a video demonstration showing:
    - LEFT: Patient phone emulator (iPhone-style SMS view)
    - RIGHT: Dashboard with live metrics updates

    Scenarios:
    1. HAPPY PATH: Missed call -> SMS conversation -> Deposit payment
    2. PCI GUARDRAIL: Customer sends credit card -> Blocked
    3. NO CONVERSION: Customer inquires but declines

.PARAMETER ApiUrl
    The API base URL. Default: http://localhost:8082

.PARAMETER Headed
    Run Playwright in headed mode (visible browser) for debugging.

.EXAMPLE
    .\scripts\run-video-demo.ps1

.EXAMPLE
    .\scripts\run-video-demo.ps1 -ApiUrl https://api.example.com -Headed
#>

param(
    [string]$ApiUrl = "http://localhost:8082",
    [switch]$Headed
)

$ErrorActionPreference = "Stop"

# Navigate to project root
$projectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $projectRoot

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  MedSpa AI - Video Demo Recorder" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "API URL:  $ApiUrl" -ForegroundColor Yellow
Write-Host "Mode:     $(if ($Headed) { 'Headed (visible browser)' } else { 'Headless (background)' })" -ForegroundColor Yellow
Write-Host ""

# Check Python
$pythonCmd = $null
foreach ($cmd in @("python", "python3", "py")) {
    try {
        $version = & $cmd --version 2>&1
        if ($LASTEXITCODE -eq 0) {
            $pythonCmd = $cmd
            Write-Host "Python:   $version" -ForegroundColor Green
            break
        }
    } catch {}
}

if (-not $pythonCmd) {
    Write-Host "ERROR: Python not found in PATH" -ForegroundColor Red
    exit 1
}

# Check required packages
Write-Host ""
Write-Host "Checking dependencies..." -ForegroundColor Yellow

$packages = @("requests", "playwright")
foreach ($pkg in $packages) {
    $check = & $pythonCmd -c "import $pkg" 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Installing $pkg..." -ForegroundColor Yellow
        & $pythonCmd -m pip install $pkg --quiet
    }
}

# Ensure Playwright browsers are installed
Write-Host "Ensuring Playwright browsers..." -ForegroundColor Yellow
& $pythonCmd -m playwright install chromium --quiet 2>&1 | Out-Null

# Build args
$args = @("scripts/e2e_video_demo.py", "--api-url", $ApiUrl)
if ($Headed) {
    $args += "--headed"
}

Write-Host ""
Write-Host "Starting demo recording..." -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Run the demo
& $pythonCmd @args
$exitCode = $LASTEXITCODE

if ($exitCode -eq 0) {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "  Demo completed successfully!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Videos saved to: tmp/demo_videos/" -ForegroundColor Yellow
    Write-Host ""
} else {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Red
    Write-Host "  Demo failed with exit code: $exitCode" -ForegroundColor Red
    Write-Host "========================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "Check tmp/demo_artifacts/ for debug info" -ForegroundColor Yellow
}

exit $exitCode
