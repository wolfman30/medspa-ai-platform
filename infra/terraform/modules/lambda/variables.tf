variable "environment" {
  description = "Environment name"
  type        = string
}

variable "aws_region" {
  description = "AWS region (used for log configuration)"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for Lambda"
  type        = list(string)
}

variable "memory_size" {
  description = "Lambda memory size in MB"
  type        = number
}

variable "timeout" {
  description = "Lambda timeout in seconds"
  type        = number
}

variable "image_tag" {
  description = "ECR image tag for the voice lambda image"
  type        = string
  default     = "latest"
}

variable "create_function" {
  description = "Whether to create the Lambda function (set false during bootstrap before the image tag exists in ECR)"
  type        = bool
  default     = true
}

variable "upstream_base_url" {
  description = "Base URL for the upstream API (ALB) that receives forwarded webhooks"
  type        = string
}

variable "tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}
