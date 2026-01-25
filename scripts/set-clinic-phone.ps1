<#
.SYNOPSIS
    Sets the SMS "from" phone number for a clinic in clinic_square_credentials.

.DESCRIPTION
    This phone number is used as the FromNumber when sending customer confirmation
    SMS after a successful payment. Without this phone number, customer confirmation
    SMS will not be sent.

.PARAMETER OrgId
    The organization/clinic UUID.

.PARAMETER PhoneNumber
    The Telnyx phone number in E.164 format (e.g., +14407448197).

.PARAMETER Token
    The admin JWT auth token. Get this from the browser dev tools Network tab.

.PARAMETER ApiUrl
    The API base URL. Defaults to https://api.aiwolfsolutions.com.

.EXAMPLE
    .\set-clinic-phone.ps1 -OrgId "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599" -PhoneNumber "+14407448197" -Token "eyJ..."
#>

param(
    [Parameter(Mandatory=$true)]
    [string]$OrgId,

    [Parameter(Mandatory=$true)]
    [string]$PhoneNumber,

    [Parameter(Mandatory=$true)]
    [string]$Token,

    [string]$ApiUrl = "https://api.aiwolfsolutions.com"
)

$ErrorActionPreference = "Stop"

# Ensure phone number has + prefix
if (-not $PhoneNumber.StartsWith("+")) {
    $PhoneNumber = "+" + $PhoneNumber
}

Write-Host "Setting phone number for clinic $OrgId to $PhoneNumber..." -ForegroundColor Cyan

$headers = @{
    "Content-Type" = "application/json"
    "Authorization" = "Bearer $Token"
}

$body = @{
    phone_number = $PhoneNumber
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$OrgId/phone" -Method PUT -Headers $headers -Body $body
    Write-Host "SUCCESS: Phone number set successfully" -ForegroundColor Green
    Write-Host ($response | ConvertTo-Json -Depth 5)
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    Write-Host "FAILED: Status code $statusCode" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red

    if ($statusCode -eq 401) {
        Write-Host "Authentication failed. Please check your token." -ForegroundColor Yellow
    } elseif ($statusCode -eq 404) {
        Write-Host "Clinic not found or Square not connected." -ForegroundColor Yellow
    }
    exit 1
}

Write-Host ""
Write-Host "To verify the phone number is set, make a test payment and check that a confirmation SMS is sent." -ForegroundColor Cyan
