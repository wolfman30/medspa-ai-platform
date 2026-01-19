#
# onboard-clinic.ps1 - Orchestrates the complete clinic onboarding process
#
# Usage:
#   .\scripts\onboard-clinic.ps1 -Name "Clinic Name" [-Timezone "America/New_York"] [-ApiUrl "http://localhost:8080"]
#
# Parameters:
#   -Name        Clinic name (required)
#   -Timezone    Timezone (default: America/New_York)
#   -ApiUrl      API base URL (default: http://localhost:8080)
#   -Token       Admin JWT token (or set ADMIN_JWT_TOKEN env var)
#   -SkipSquare  Skip Square OAuth step
#

param(
    [Parameter(Mandatory=$true)]
    [string]$Name,

    [string]$Timezone = "America/New_York",

    [string]$ApiUrl = $env:API_URL,

    [string]$Token = $env:ADMIN_JWT_TOKEN,

    [switch]$SkipSquare,

    [string]$OnboardingToken = $env:ONBOARDING_TOKEN
)

# Set default API URL if not provided
if ([string]::IsNullOrEmpty($ApiUrl)) {
    $ApiUrl = "http://localhost:8080"
}

# Validate token
if ([string]::IsNullOrEmpty($Token)) {
    Write-Host "Error: Admin token required. Set ADMIN_JWT_TOKEN or use -Token" -ForegroundColor Red
    exit 1
}

Write-Host "========================================" -ForegroundColor Blue
Write-Host "  MedSpa AI Platform - Clinic Onboarding" -ForegroundColor Blue
Write-Host "========================================" -ForegroundColor Blue
Write-Host ""
Write-Host "Clinic Name: " -NoNewline; Write-Host $Name -ForegroundColor Green
Write-Host "Timezone:    " -NoNewline; Write-Host $Timezone -ForegroundColor Green
Write-Host "API URL:     " -NoNewline; Write-Host $ApiUrl -ForegroundColor Green
Write-Host ""

# Step 1: Create the clinic
Write-Host "Step 1: Creating clinic..." -ForegroundColor Yellow

$headers = @{
    "Authorization" = "Bearer $Token"
    "Content-Type" = "application/json"
}

$body = @{
    name = $Name
    timezone = $Timezone
} | ConvertTo-Json

try {
    $createResponse = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics" -Method POST -Headers $headers -Body $body
    $orgId = $createResponse.org_id

    if ([string]::IsNullOrEmpty($orgId)) {
        Write-Host "Failed to create clinic - no org_id returned" -ForegroundColor Red
        exit 1
    }

    Write-Host "Created clinic with org_id: $orgId" -ForegroundColor Green
} catch {
    Write-Host "Failed to create clinic:" -ForegroundColor Red
    Write-Host $_.Exception.Message
    exit 1
}

Write-Host ""

# Step 2: Check onboarding status
Write-Host "Step 2: Checking onboarding status..." -ForegroundColor Yellow

try {
    $statusResponse = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$orgId/onboarding-status" -Method GET -Headers $headers

    foreach ($step in $statusResponse.steps) {
        $status = if ($step.completed) { "[X]" } else { "[ ]" }
        $color = if ($step.completed) { "Green" } else { "Yellow" }
        Write-Host "$status $($step.name)" -ForegroundColor $color
    }
} catch {
    Write-Host "Failed to get onboarding status:" -ForegroundColor Red
    Write-Host $_.Exception.Message
}

Write-Host ""

# Step 3: Square OAuth
if (-not $SkipSquare) {
    Write-Host "Step 3: Square OAuth" -ForegroundColor Yellow
    Write-Host "Open the following URL in your browser to connect Square:"
    Write-Host ""
    Write-Host "$ApiUrl/admin/clinics/$orgId/square/connect" -ForegroundColor Green
    Write-Host ""
    Read-Host "Press Enter after completing Square OAuth..."
} else {
    Write-Host "Step 3: Square OAuth (skipped)" -ForegroundColor Yellow
    Write-Host "Run this later: $ApiUrl/admin/clinics/$orgId/square/connect" -ForegroundColor Green
}

Write-Host ""

# Step 4: Configure phone number
Write-Host "Step 4: Configure SMS phone number" -ForegroundColor Yellow
$phoneNumber = Read-Host "Enter clinic phone number (E.164 format, e.g., +15551234567)"

if (-not [string]::IsNullOrEmpty($phoneNumber)) {
    $phoneBody = @{ phone_number = $phoneNumber } | ConvertTo-Json

    try {
        $phoneResponse = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$orgId/phone" -Method PUT -Headers $headers -Body $phoneBody
        Write-Host "Phone number configured: $phoneNumber" -ForegroundColor Green
    } catch {
        Write-Host "Failed to configure phone:" -ForegroundColor Red
        Write-Host $_.Exception.Message
    }
} else {
    Write-Host "Skipped phone configuration" -ForegroundColor Yellow
}

Write-Host ""

# Step 5: Seed knowledge
Write-Host "Step 5: Seed clinic knowledge" -ForegroundColor Yellow
$seedKnowledge = Read-Host "Would you like to seed sample knowledge now? (y/n)"

if ($seedKnowledge -eq "y" -or $seedKnowledge -eq "Y") {
    $knowledgePayload = @{
        documents = @(
            "$Name offers a variety of aesthetic services including Botox, dermal fillers, laser treatments, and skincare treatments.",
            "Our Botox treatments start at `$12 per unit. Most patients need 20-40 units for common treatment areas.",
            "Dermal filler treatments range from `$600-`$1200 depending on the type and amount of filler used.",
            "We require a `$50 deposit to book your appointment. This deposit is applied to your treatment cost.",
            "Our cancellation policy requires 24 hours notice. Deposits are non-refundable for no-shows or late cancellations.",
            "New patients should arrive 15 minutes early to complete paperwork. Existing patients can check in at their appointment time."
        )
    } | ConvertTo-Json

    $knowledgeHeaders = @{
        "X-Org-Id" = $orgId
        "Content-Type" = "application/json"
    }
    if (-not [string]::IsNullOrEmpty($OnboardingToken)) {
        $knowledgeHeaders["X-Onboarding-Token"] = $OnboardingToken
    }

    try {
        Invoke-RestMethod -Uri "$ApiUrl/knowledge/$orgId" -Method POST -Headers $knowledgeHeaders -Body $knowledgePayload
        Write-Host "Knowledge seeded successfully" -ForegroundColor Green
    } catch {
        Write-Host "Failed to seed knowledge:" -ForegroundColor Red
        Write-Host $_.Exception.Message
    }
} else {
    Write-Host "Skipped knowledge seeding" -ForegroundColor Yellow
    Write-Host "Seed later with: POST $ApiUrl/knowledge/$orgId"
}

Write-Host ""

# Final status check
Write-Host "Final onboarding status:" -ForegroundColor Yellow

try {
    $finalStatus = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$orgId/onboarding-status" -Method GET -Headers $headers

    Write-Host ""
    Write-Host "========================================" -ForegroundColor Blue
    Write-Host "  Onboarding Summary" -ForegroundColor Blue
    Write-Host "========================================" -ForegroundColor Blue
    Write-Host ""
    Write-Host "Org ID:      " -NoNewline; Write-Host $orgId -ForegroundColor Green
    Write-Host "Clinic Name: " -NoNewline; Write-Host $finalStatus.clinic_name -ForegroundColor Green
    Write-Host "Progress:    " -NoNewline; Write-Host "$($finalStatus.overall_progress)%" -ForegroundColor Green

    if ($finalStatus.ready_for_launch) {
        Write-Host "Status:      " -NoNewline; Write-Host "READY FOR LAUNCH" -ForegroundColor Green
    } else {
        Write-Host "Status:      " -NoNewline; Write-Host "PENDING" -ForegroundColor Yellow
        Write-Host "Next Step:   " -NoNewline; Write-Host $finalStatus.next_action -ForegroundColor Yellow
    }
} catch {
    Write-Host "Failed to get final status:" -ForegroundColor Red
    Write-Host $_.Exception.Message
}

Write-Host ""
Write-Host "Save this org_id for future reference: " -NoNewline; Write-Host $orgId -ForegroundColor Green
