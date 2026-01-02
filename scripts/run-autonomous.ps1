#
# Autonomous MVP Build Script for Claude Code (Windows PowerShell)
#
# This script runs Claude Code in headless mode to complete the MVP.
# It will continue until all acceptance tests pass or max-turns is reached.
#
# Usage:
#   .\scripts\run-autonomous.ps1              # Default: 100 turns
#   .\scripts\run-autonomous.ps1 -MaxTurns 200    # Custom turn limit
#   .\scripts\run-autonomous.ps1 -Continue        # Resume previous session
#

param(
    [int]$MaxTurns = 100,
    [switch]$Continue
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$LogDir = Join-Path $ProjectRoot ".claude\logs"
$Timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
$LogFile = Join-Path $LogDir "autonomous_$Timestamp.log"

# Create log directory
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

Write-Host "========================================"
Write-Host "MedSpa AI Platform - Autonomous Build"
Write-Host "========================================"
Write-Host "Project: $ProjectRoot"
Write-Host "Max turns: $MaxTurns"
Write-Host "Log file: $LogFile"
Write-Host "Started: $(Get-Date)"
Write-Host "========================================"

# Change to project directory
Set-Location $ProjectRoot

# Run initial test check
Write-Host ""
Write-Host "Running initial acceptance test check..."
try {
    go test -v ./tests/... 2>&1 | Select-Object -First 50
    Write-Host ""
    Write-Host "Note: Some tests may already pass. Claude will verify and complete any gaps."
} catch {
    Write-Host "Tests check failed - Claude will investigate"
}

Write-Host ""
Write-Host "Starting autonomous Claude Code session..."
Write-Host ""

# Build the prompt
$Prompt = @"
Complete the MedSpa AI Platform MVP by following these steps:

1. First, run the acceptance tests to see current status:
   go test -v ./tests/...

2. For each failing test, fix the underlying issue.

3. Check docs/MVP_STATUS.md for remaining work items.

4. Implement missing features with proper tests.

5. Run tests after each significant change.

6. Update docs/MVP_STATUS.md when completing items.

7. Continue until ALL acceptance tests pass.

The definition of done is: ``go test -v ./tests/...`` passes with 0 failures.

Start by running the acceptance tests to understand the current state.
"@

# Run Claude Code in headless mode
$ContinueArg = if ($Continue) { "--continue" } else { "" }

$ClaudePath = Get-Command claude -ErrorAction SilentlyContinue
if ($ClaudePath) {
    $args = @(
        "-p", $Prompt,
        "--allowedTools", "Read,Write,Edit,Bash,Glob,Grep",
        "--max-turns", $MaxTurns,
        "--output-format", "stream-json"
    )
    if ($Continue) {
        $args += "--continue"
    }

    & claude @args 2>&1 | Tee-Object -FilePath $LogFile
} else {
    Write-Host "ERROR: Claude Code CLI not found."
    Write-Host ""
    Write-Host "Install it with:"
    Write-Host "  npm install -g @anthropic-ai/claude-code"
    Write-Host ""
    Write-Host "Or run manually in Claude Code with this prompt:"
    Write-Host ""
    Write-Host $Prompt
    exit 1
}

Write-Host ""
Write-Host "========================================"
Write-Host "Autonomous session completed"
Write-Host "Log saved to: $LogFile"
Write-Host "Ended: $(Get-Date)"
Write-Host "========================================"

# Run final test check
Write-Host ""
Write-Host "Final acceptance test results:"
go test -v ./tests/...
