variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name (development, staging, production)"
  type        = string
  default     = "development"
}

variable "vpc_cidr" {
  description = "CIDR block for VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  description = "List of availability zones"
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b"]
}

variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.micro"
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "medspa"
}

variable "db_username" {
  description = "Database username"
  type        = string
  default     = "medspa_admin"
  sensitive   = true
}

variable "api_lambda_memory" {
  description = "Memory allocation for API Lambda function"
  type        = number
  default     = 512
}

variable "api_lambda_timeout" {
  description = "Timeout for API Lambda function in seconds"
  type        = number
  default     = 30
}
