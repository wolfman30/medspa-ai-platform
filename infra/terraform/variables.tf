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

variable "enable_voice_webhooks" {
  description = "Enable voice webhook Lambda + API Gateway. Disable during image bootstrap to create ECR repos first."
  type        = bool
  default     = false
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
  default     = false
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

variable "deployment_minimum_healthy_percent" {
  description = "Minimum healthy percent for ECS rolling deployments (used when enable_blue_green=false)"
  type        = number
  default     = 100
}

variable "deployment_maximum_percent" {
  description = "Maximum percent for ECS rolling deployments (used when enable_blue_green=false)"
  type        = number
  default     = 200
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

variable "api_public_base_url" {
  description = "Public base URL for the API (used for demo payment links and voice webhook proxy upstream)"
  type        = string
  default     = ""
}

variable "voice_upstream_base_url" {
  description = "Optional override for voice-lambda upstream base URL (default uses the ALB DNS over HTTP)"
  type        = string
  default     = ""
}

variable "ui_domain_name" {
  description = "Domain name for the onboarding UI (e.g., portal-dev.aiwolfsolutions.com)"
  type        = string
  default     = ""
}

variable "ui_certificate_arn" {
  description = "ACM certificate ARN for the onboarding UI CloudFront distribution"
  type        = string
  default     = ""
}

variable "ui_price_class" {
  description = "CloudFront price class for the onboarding UI distribution"
  type        = string
  default     = "PriceClass_100"
}

variable "cognito_user_pool_id" {
  description = "Cognito User Pool ID for admin dashboard auth"
  type        = string
  default     = ""
}

variable "cognito_client_id" {
  description = "Cognito app client ID for admin dashboard auth"
  type        = string
  default     = ""
}

variable "cognito_region" {
  description = "Cognito region for admin dashboard auth"
  type        = string
  default     = ""
}

variable "enable_browser_sidecar" {
  description = "Enable the browser sidecar container for booking availability scraping (Moxie integration)"
  type        = bool
  default     = false
}

variable "enable_nat_gateway" {
  description = "Create NAT gateway for private subnet internet access. Disable when ECS uses public subnets to save ~$32/mo."
  type        = bool
  default     = true
}

variable "assign_public_ip" {
  description = "Assign public IPs to ECS Fargate tasks. Enable when using public subnets without NAT."
  type        = bool
  default     = false
}
