# Generate an admin JWT token for API access
# Usage: .\scripts\generate-admin-token.ps1

param(
    [Parameter(Mandatory=$false)]
    [string]$Secret = $env:ADMIN_JWT_SECRET,

    [Parameter(Mandatory=$false)]
    [int]$ExpiresInHours = 24
)

if (-not $Secret) {
    Write-Error "ADMIN_JWT_SECRET environment variable not set"
    exit 1
}

# Create JWT header and payload
$header = @{
    alg = "HS256"
    typ = "JWT"
} | ConvertTo-Json -Compress

$now = [int][double]::Parse((Get-Date -UFormat %s))
$exp = $now + ($ExpiresInHours * 3600)

$payload = @{
    sub = "admin"
    iat = $now
    exp = $exp
} | ConvertTo-Json -Compress

# Base64Url encode
function ConvertTo-Base64Url($bytes) {
    $base64 = [Convert]::ToBase64String($bytes)
    return $base64.Replace('+', '-').Replace('/', '_').TrimEnd('=')
}

$headerBytes = [System.Text.Encoding]::UTF8.GetBytes($header)
$payloadBytes = [System.Text.Encoding]::UTF8.GetBytes($payload)

$headerB64 = ConvertTo-Base64Url $headerBytes
$payloadB64 = ConvertTo-Base64Url $payloadBytes

# Create signature
$message = "$headerB64.$payloadB64"
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [System.Text.Encoding]::UTF8.GetBytes($Secret)
$signatureBytes = $hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($message))
$signatureB64 = ConvertTo-Base64Url $signatureBytes

$token = "$headerB64.$payloadB64.$signatureB64"

Write-Host "Admin JWT Token (expires in $ExpiresInHours hours):"
Write-Host ""
Write-Host $token
Write-Host ""
Write-Host "Use with: -H 'Authorization: Bearer $token'"
