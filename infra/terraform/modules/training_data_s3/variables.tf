variable "bucket_name" {
  description = "Name of the S3 bucket for training data"
  type        = string
  default     = "aiwolf-training-data-dev"
}

variable "environment" {
  description = "Environment name (e.g., dev, staging, production)"
  type        = string
}
