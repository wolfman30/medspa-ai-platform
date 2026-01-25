<#
.SYNOPSIS
    Tests that the payment confirmation SMS flow is correctly configured.

.DESCRIPTION
    Verifies that the clinic has a phone number set in clinic_square_credentials,
    which is required for sending customer confirmation SMS after payment.

.PARAMETER OrgId
    The organization/clinic UUID.

.PARAMETER Token
    The admin JWT auth token.

.PARAMETER ApiUrl
    The API base URL. Defaults to https://api.aiwolfsolutions.com.

.EXAMPLE
    .\test-payment-confirmation.ps1 -OrgId "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599" -Token "eyJ..."
#>

param(
    [Parameter(Mandatory=$true)]
    [string]$OrgId,

    [Parameter(Mandatory=$true)]
    [string]$Token,

    [string]$ApiUrl = "https://api.aiwolfsolutions.com"
)

$ErrorActionPreference = "Stop"

$headers = @{
    "Content-Type" = "application/json"
    "Authorization" = "Bearer $Token"
}

Write-Host "Testing Payment Confirmation SMS Configuration" -ForegroundColor Cyan
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host ""

# Check 1: Square connection status (includes phone number)
Write-Host "1. Checking Square connection status..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$OrgId/square/status" -Method GET -Headers $headers
    Write-Host "   Square connected: $($response.connected)" -ForegroundColor $(if ($response.connected) { "Green" } else { "Red" })

    if ($response.phone_number) {
        Write-Host "   Phone number: $($response.phone_number)" -ForegroundColor Green
    } else {
        Write-Host "   Phone number: NOT SET (customer confirmation SMS will NOT be sent!)" -ForegroundColor Red
    }

    if ($response.location_id) {
        Write-Host "   Location ID: $($response.location_id)" -ForegroundColor Green
    } else {
        Write-Host "   Location ID: NOT SET" -ForegroundColor Yellow
    }
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 404) {
        Write-Host "   Square NOT connected for this clinic" -ForegroundColor Red
    } else {
        Write-Host "   Error checking Square status: $($_.Exception.Message)" -ForegroundColor Red
    }
}

Write-Host ""

# Check 2: Clinic notification settings
Write-Host "2. Checking notification settings..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$OrgId/config" -Method GET -Headers $headers
    $notif = $response.notifications

    if ($notif) {
        Write-Host "   Email notifications: $($notif.email_enabled)" -ForegroundColor $(if ($notif.email_enabled) { "Green" } else { "Yellow" })
        if ($notif.email_enabled -and $notif.email_recipients) {
            Write-Host "     Recipients: $($notif.email_recipients -join ', ')" -ForegroundColor Cyan
        }

        Write-Host "   SMS notifications: $($notif.sms_enabled)" -ForegroundColor $(if ($notif.sms_enabled) { "Green" } else { "Yellow" })
        if ($notif.sms_enabled -and $notif.sms_recipients) {
            Write-Host "     Recipients: $($notif.sms_recipients -join ', ')" -ForegroundColor Cyan
        }
    } else {
        Write-Host "   Notifications: NOT CONFIGURED" -ForegroundColor Yellow
    }
} catch {
    Write-Host "   Error checking config: $($_.Exception.Message)" -ForegroundColor Red
}

Write-Host ""
Write-Host "Summary:" -ForegroundColor Cyan
Write-Host "--------" -ForegroundColor Cyan
Write-Host "For customer confirmation SMS to be sent after payment:"
Write-Host "  - Square must be connected (check #1)"
Write-Host "  - Phone number must be set (use set-clinic-phone.ps1)"
Write-Host ""
Write-Host "For operator notifications (email/SMS to staff):"
Write-Host "  - Configure in Admin UI -> Notification Settings"
Write-Host ""
