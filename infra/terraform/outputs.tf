output "vpc_id" {
  description = "ID of the VPC"
  value       = module.vpc.vpc_id
}

output "private_subnet_ids" {
  description = "IDs of private subnets"
  value       = module.vpc.private_subnet_ids
}

output "public_subnet_ids" {
  description = "IDs of public subnets"
  value       = module.vpc.public_subnet_ids
}

output "rds_endpoint" {
  description = "RDS instance endpoint"
  value       = module.rds.endpoint
  sensitive   = true
}

output "rds_database_name" {
  description = "Name of the RDS database"
  value       = module.rds.database_name
}

output "api_gateway_url" {
  description = "URL of the voice webhook API Gateway (HTTP API)"
  value       = try(module.api_gateway[0].api_url, "")
}

output "api_alb_dns_name" {
  description = "DNS name of the API Application Load Balancer"
  value       = module.ecs_fargate.alb_dns_name
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = module.ecs_fargate.cluster_name
}

output "ecs_service_name" {
  description = "Name of the ECS service"
  value       = module.ecs_fargate.service_name
}

output "ecs_task_security_group_id" {
  description = "Security group ID for ECS tasks"
  value       = module.ecs_fargate.task_security_group_id
}

output "migration_task_definition_arn" {
  description = "Task definition ARN for the one-off DB migration task"
  value       = module.ecs_fargate.migration_task_definition_arn
}

output "api_ecr_repository_url" {
  description = "ECR repository URL for the API image"
  value       = module.ecs_fargate.api_ecr_repository_url
}

output "api_task_definition_arn" {
  description = "Current API task definition ARN (latest revision created by Terraform)"
  value       = module.ecs_fargate.api_task_definition_arn
}

output "codedeploy_app_name" {
  description = "CodeDeploy application name (ECS blue/green)"
  value       = module.ecs_fargate.codedeploy_app_name
}

output "codedeploy_deployment_group_name" {
  description = "CodeDeploy deployment group name (ECS blue/green)"
  value       = module.ecs_fargate.codedeploy_deployment_group_name
}

output "redis_endpoint" {
  description = "Primary Redis endpoint (host:port)"
  value       = "${module.redis.primary_endpoint_address}:${module.redis.port}"
}

output "redis_auth_token_secret_arn" {
  description = "Secrets Manager ARN holding the Redis auth token"
  value       = module.redis.auth_token_secret_arn
  sensitive   = true
}

output "lambda_function_name" {
  description = "Name of the voice webhook Lambda function"
  value       = module.lambda.function_name
}

output "voice_lambda_ecr_repository_url" {
  description = "ECR repository URL for the voice webhook Lambda image"
  value       = module.lambda.ecr_repository_url
}

output "secrets_manager_arn" {
  description = "ARN of the Secrets Manager secret"
  value       = module.secrets.secret_arn
  sensitive   = true
}
