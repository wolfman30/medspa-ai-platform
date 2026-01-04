variable "environment" {
  description = "Environment name (development, staging, production)"
  type        = string
}

variable "domain_name" {
  description = "Domain name to serve the UI (e.g., portal-dev.aiwolfsolutions.com)"
  type        = string
  validation {
    condition     = length(trimspace(var.domain_name)) >= 3
    error_message = "domain_name must be at least 3 characters."
  }
}

variable "certificate_arn" {
  description = "ACM certificate ARN for the CloudFront distribution"
  type        = string
}

variable "price_class" {
  description = "CloudFront price class"
  type        = string
  default     = "PriceClass_100"
}

variable "tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}
