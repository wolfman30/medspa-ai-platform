# Purge phone data from dev environment
param(
    [Parameter(Mandatory=$true)]
    [string]$OrgId,

    [Parameter(Mandatory=$true)]
    [string]$Phone,

    [string]$ApiUrl = "https://api-dev.aiwolfsolutions.com"
)

# Get the secret from AWS
$secretJson = aws secretsmanager get-secret-value --secret-id medspa-development-app-secrets --query SecretString --output text
$secrets = $secretJson | ConvertFrom-Json
$adminSecret = $secrets.ADMIN_JWT_SECRET

if (-not $adminSecret) {
    Write-Error "Could not retrieve ADMIN_JWT_SECRET"
    exit 1
}

# Generate HS256 JWT manually
function New-Jwt {
    param([string]$Secret)

    $header = @{ alg = "HS256"; typ = "JWT" } | ConvertTo-Json -Compress
    $now = [int][double]::Parse((Get-Date -UFormat %s))
    $payload = @{ sub = "admin"; iat = $now; exp = $now + 3600 } | ConvertTo-Json -Compress

    $headerB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($header)).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    $payloadB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($payload)).TrimEnd('=').Replace('+', '-').Replace('/', '_')

    $signingInput = "$headerB64.$payloadB64"
    $hmac = New-Object System.Security.Cryptography.HMACSHA256
    $hmac.Key = [Text.Encoding]::UTF8.GetBytes($Secret)
    $sigBytes = $hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($signingInput))
    $sigB64 = [Convert]::ToBase64String($sigBytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')

    return "$signingInput.$sigB64"
}

$token = New-Jwt -Secret $adminSecret
$headers = @{
    "Authorization" = "Bearer $token"
    "Content-Type" = "application/json"
}

$uri = "$ApiUrl/admin/clinics/$OrgId/phones/$Phone"
Write-Host "Purging data for phone $Phone in org $OrgId..."
Write-Host "URI: $uri"

try {
    $response = Invoke-RestMethod -Method DELETE -Uri $uri -Headers $headers
    Write-Host "Success!" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 5
} catch {
    Write-Error "Failed to purge: $_"
    Write-Host "Response: $($_.Exception.Response)"
}
