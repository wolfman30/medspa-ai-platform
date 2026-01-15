# Update clinic config name for demo purposes
# Usage: .\scripts\update-clinic-name.ps1 -OrgId "your-org-id" -Name "Wolf Aesthetics"

param(
    [Parameter(Mandatory=$true)]
    [string]$OrgId,

    [Parameter(Mandatory=$false)]
    [string]$Name = "Wolf Aesthetics",

    [Parameter(Mandatory=$false)]
    [string]$ApiUrl = "https://api-dev.aiwolfsolutions.com",

    [Parameter(Mandatory=$false)]
    [string]$AdminToken = $env:ADMIN_JWT_SECRET
)

if (-not $AdminToken) {
    Write-Error "ADMIN_JWT_SECRET environment variable not set. Please set it or pass -AdminToken"
    exit 1
}

$headers = @{
    "Authorization" = "Bearer $AdminToken"
    "Content-Type" = "application/json"
}

$body = @{
    name = $Name
} | ConvertTo-Json

Write-Host "Updating clinic config for org: $OrgId"
Write-Host "New name: $Name"
Write-Host "API URL: $ApiUrl"

try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/admin/clinics/$OrgId/config" `
        -Method PUT `
        -Headers $headers `
        -Body $body

    Write-Host "`nSuccess! Clinic config updated:" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 5
} catch {
    Write-Error "Failed to update clinic config: $_"
    Write-Host "Response: $($_.Exception.Response)"
    exit 1
}
