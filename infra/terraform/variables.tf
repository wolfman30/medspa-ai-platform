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

variable "api_image_tag" {
  description = "ECR image tag for the API image"
  type        = string
  default     = "latest"
}

variable "api_task_cpu" {
  description = "ECS task CPU units for the API service"
  type        = number
  default     = 512
}

variable "api_task_memory" {
  description = "ECS task memory (MiB) for the API service"
  type        = number
  default     = 1024
}

variable "api_desired_count" {
  description = "Desired number of API tasks"
  type        = number
  default     = 1
}

variable "api_certificate_arn" {
  description = "Optional ACM certificate ARN for ALB HTTPS"
  type        = string
  default     = ""
}

variable "enable_blue_green" {
  description = "Enable ECS blue/green deployments via CodeDeploy"
  type        = bool
  default     = true
}

variable "codedeploy_deployment_config_name" {
  description = "CodeDeploy deployment config name (ECS blue/green)"
  type        = string
  default     = "CodeDeployDefault.ECSAllAtOnce"
}

variable "codedeploy_termination_wait_time_minutes" {
  description = "Minutes to keep the old task set running after a successful blue/green deployment"
  type        = number
  default     = 5
}

variable "redis_node_type" {
  description = "ElastiCache Redis node type"
  type        = string
  default     = "cache.t4g.micro"
}

variable "redis_num_cache_clusters" {
  description = "Number of cache clusters (1 = single node, 2+ enables replicas/failover)"
  type        = number
  default     = 1
}
