output "bucket_name" {
  description = "S3 bucket name for the onboarding UI"
  value       = aws_s3_bucket.site.bucket
}

output "distribution_id" {
  description = "CloudFront distribution ID"
  value       = aws_cloudfront_distribution.site.id
}

output "distribution_domain_name" {
  description = "CloudFront distribution domain name"
  value       = aws_cloudfront_distribution.site.domain_name
}

output "distribution_arn" {
  description = "CloudFront distribution ARN"
  value       = aws_cloudfront_distribution.site.arn
}

output "site_url" {
  description = "Custom domain URL for the onboarding UI"
  value       = "https://${trimspace(var.domain_name)}"
}
