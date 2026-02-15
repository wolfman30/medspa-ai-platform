variable "environment" {
  description = "Environment name"
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for VPC"
  type        = string
}

variable "availability_zones" {
  description = "List of availability zones"
  type        = list(string)
}

variable "single_nat_gateway" {
  description = "Use a single NAT gateway for all private subnets (cheaper, less HA)"
  type        = bool
  default     = false
}

variable "enable_nat_gateway" {
  description = "Create NAT gateway(s) for private subnet internet access. Disable when ECS uses public subnets."
  type        = bool
  default     = true
}
