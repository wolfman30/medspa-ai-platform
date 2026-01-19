# Deploy the onboarding portal to S3/CloudFront
# Usage: .\scripts\deploy-portal.ps1

param(
    [Parameter(Mandatory=$false)]
    [string]$Environment = "dev",

    [Parameter(Mandatory=$false)]
    [string]$S3Bucket = "",

    [Parameter(Mandatory=$false)]
    [string]$DistributionId = ""
)

$ErrorActionPreference = "Stop"

# Set defaults based on environment
if ($Environment -eq "dev") {
    if (-not $S3Bucket) { $S3Bucket = "medspa-dev-portal-339713028352" }
    if (-not $DistributionId) { $DistributionId = "E3QG58BCZCOXRP" }
} elseif ($Environment -eq "prod") {
    if (-not $S3Bucket) { $S3Bucket = "medspa-prod-portal-339713028352" }
    # Add prod distribution ID when available
}

Write-Host "Deploying portal to $Environment environment" -ForegroundColor Cyan
Write-Host "S3 Bucket: $S3Bucket" -ForegroundColor Gray
Write-Host "CloudFront Distribution: $DistributionId" -ForegroundColor Gray

# Build the frontend
Write-Host "`nBuilding frontend..." -ForegroundColor Yellow
Push-Location web/onboarding
try {
    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed"
    }
} finally {
    Pop-Location
}

# Sync to S3
Write-Host "`nSyncing to S3..." -ForegroundColor Yellow
aws s3 sync web/onboarding/dist "s3://$S3Bucket" --delete

if ($LASTEXITCODE -ne 0) {
    throw "S3 sync failed"
}

# Invalidate CloudFront cache
if ($DistributionId) {
    Write-Host "`nInvalidating CloudFront cache..." -ForegroundColor Yellow
    aws cloudfront create-invalidation --distribution-id $DistributionId --paths "/*"

    if ($LASTEXITCODE -ne 0) {
        Write-Host "CloudFront invalidation failed (non-critical)" -ForegroundColor Yellow
    }
}

Write-Host "`nDeployment complete!" -ForegroundColor Green
Write-Host "Site URL: https://portal-dev.aiwolfsolutions.com" -ForegroundColor Cyan
