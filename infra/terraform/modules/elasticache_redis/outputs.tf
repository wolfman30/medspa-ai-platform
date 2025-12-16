output "primary_endpoint_address" {
  description = "Primary endpoint address"
  value       = aws_elasticache_replication_group.main.primary_endpoint_address
}

output "port" {
  description = "Redis port"
  value       = aws_elasticache_replication_group.main.port
}

output "security_group_id" {
  description = "Security group ID for Redis"
  value       = aws_security_group.redis.id
}

output "auth_token_secret_arn" {
  description = "Secrets Manager secret ARN containing the Redis auth token"
  value       = aws_secretsmanager_secret.auth_token.arn
  sensitive   = true
}

