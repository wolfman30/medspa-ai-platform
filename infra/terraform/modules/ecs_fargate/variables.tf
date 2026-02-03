variable "environment" {
  description = "Environment name (dev, prod, etc.)"
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

variable "vpc_cidr" {
  description = "VPC CIDR (used to restrict internal listeners)"
  type        = string
}

variable "public_subnet_ids" {
  description = "Public subnet IDs (for the ALB)"
  type        = list(string)
}

variable "private_subnet_ids" {
  description = "Private subnet IDs (for ECS tasks)"
  type        = list(string)
}

variable "container_port" {
  description = "Container port the API listens on"
  type        = number
  default     = 8080
}

variable "task_cpu" {
  description = "Fargate task CPU units (256, 512, 1024, ...)"
  type        = number
  default     = 512
}

variable "task_memory" {
  description = "Fargate task memory (MiB)"
  type        = number
  default     = 1024
}

variable "migrate_task_cpu" {
  description = "Fargate task CPU units for the one-off DB migrator task"
  type        = number
  default     = 256
}

variable "migrate_task_memory" {
  description = "Fargate task memory (MiB) for the one-off DB migrator task"
  type        = number
  default     = 512
}

variable "desired_count" {
  description = "Desired number of API tasks"
  type        = number
  default     = 1
}

variable "deployment_minimum_healthy_percent" {
  description = "Minimum healthy percent for ECS rolling deployments (lower in non-prod to reduce capacity requirements)"
  type        = number
  default     = 100
}

variable "deployment_maximum_percent" {
  description = "Maximum percent for ECS rolling deployments"
  type        = number
  default     = 200
}

variable "assign_public_ip" {
  description = "Assign public IPs to tasks (generally false when using private subnets + NAT)"
  type        = bool
  default     = false
}

variable "image_tag" {
  description = "Container image tag to deploy (used when api_image_uri is empty)"
  type        = string
  default     = "latest"
}

variable "api_image_uri" {
  description = "Full container image URI for the API. When empty, uses the module-managed ECR repo + image_tag."
  type        = string
  default     = ""
}

variable "certificate_arn" {
  description = "Optional ACM certificate ARN to enable HTTPS on the ALB"
  type        = string
  default     = ""
}

variable "enable_blue_green" {
  description = "Enable ECS blue/green deployments via CodeDeploy"
  type        = bool
  default     = true
}

variable "test_listener_port" {
  description = "ALB listener port used by CodeDeploy for test traffic"
  type        = number
  default     = 9000
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

variable "health_check_path" {
  description = "ALB target group health check path"
  type        = string
  default     = "/health"
}

variable "environment_variables" {
  description = "Plaintext environment variables to inject into the container"
  type        = map(string)
  default     = {}
}

variable "secret_environment_variables" {
  description = "Secret environment variables (name -> Secrets Manager/SSM valueFrom string)"
  type        = map(string)
  default     = {}
}

variable "secret_arns" {
  description = "Base ARNs of Secrets Manager secrets used by the task (used to scope IAM permissions)"
  type        = list(string)
  default     = []
}

variable "enable_execute_command" {
  description = "Enable ECS Exec for the service"
  type        = bool
  default     = true
}

variable "enable_fargate_fallback" {
  description = "Enable on-demand Fargate fallback capacity provider"
  type        = bool
  default     = true
}

variable "spot_weight" {
  description = "Capacity provider weight for FARGATE_SPOT"
  type        = number
  default     = 2
}

variable "fargate_weight" {
  description = "Capacity provider weight for FARGATE (on-demand)"
  type        = number
  default     = 1
}

variable "fargate_base" {
  description = "Base on-demand tasks to run before using Spot"
  type        = number
  default     = 0
}

variable "tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

# Browser Sidecar Configuration
variable "enable_browser_sidecar" {
  description = "Enable the browser sidecar container for booking availability scraping"
  type        = bool
  default     = false
}

variable "browser_sidecar_image_uri" {
  description = "Full container image URI for the browser sidecar. When empty, uses the module-managed ECR repo."
  type        = string
  default     = ""
}

variable "browser_sidecar_port" {
  description = "Port the browser sidecar listens on"
  type        = number
  default     = 3000
}

variable "browser_sidecar_cpu" {
  description = "CPU units allocated to the browser sidecar container"
  type        = number
  default     = 512
}

variable "browser_sidecar_memory" {
  description = "Memory (MiB) allocated to the browser sidecar container"
  type        = number
  default     = 2048
}
