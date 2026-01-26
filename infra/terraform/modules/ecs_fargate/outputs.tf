output "cluster_name" {
  description = "ECS cluster name"
  value       = aws_ecs_cluster.main.name
}

output "service_name" {
  description = "ECS service name"
  value       = local.api_service_name
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = aws_lb.api.dns_name
}

output "alb_arn" {
  description = "ALB ARN"
  value       = aws_lb.api.arn
}

output "task_security_group_id" {
  description = "Security group ID for ECS tasks"
  value       = aws_security_group.tasks.id
}

output "api_ecr_repository_url" {
  description = "ECR repository URL for the API image"
  value       = aws_ecr_repository.api.repository_url
}

output "api_image_uri" {
  description = "Deployed API image URI"
  value       = local.computed_image_uri
}

output "api_task_definition_arn" {
  description = "Current API task definition ARN (latest revision created by Terraform)"
  value       = aws_ecs_task_definition.api.arn
}

output "migration_task_definition_arn" {
  description = "Task definition ARN for the one-off DB migration task"
  value       = aws_ecs_task_definition.migrate.arn
}

output "codedeploy_app_name" {
  description = "CodeDeploy application name (ECS blue/green)"
  value       = var.enable_blue_green ? aws_codedeploy_app.api[0].name : ""
}

output "codedeploy_deployment_group_name" {
  description = "CodeDeploy deployment group name (ECS blue/green)"
  value       = var.enable_blue_green ? aws_codedeploy_deployment_group.api[0].deployment_group_name : ""
}

output "browser_sidecar_ecr_repository_url" {
  description = "ECR repository URL for the browser sidecar image (empty if sidecar not enabled)"
  value       = var.enable_browser_sidecar ? aws_ecr_repository.browser_sidecar[0].repository_url : ""
}

output "browser_sidecar_enabled" {
  description = "Whether the browser sidecar is enabled"
  value       = var.enable_browser_sidecar
}
