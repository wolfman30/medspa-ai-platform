locals {
  name_prefix = "medspa-${var.environment}"
}

resource "aws_elasticache_subnet_group" "main" {
  name       = "${local.name_prefix}-redis-subnet"
  subnet_ids = var.private_subnet_ids

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-redis-subnet"
  })
}

resource "aws_security_group" "redis" {
  name        = "${local.name_prefix}-redis-sg"
  description = "Security group for ElastiCache Redis"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = var.port
    to_port     = var.port
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
    description = "Redis from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-redis-sg"
  })
}

resource "random_password" "auth_token" {
  length  = 40
  special = false
}

resource "aws_secretsmanager_secret" "auth_token" {
  name        = "${local.name_prefix}-redis-auth-token"
  description = "ElastiCache Redis AUTH token for ${var.environment}"

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-redis-auth-token"
  })
}

resource "aws_secretsmanager_secret_version" "auth_token" {
  secret_id     = aws_secretsmanager_secret.auth_token.id
  secret_string = random_password.auth_token.result
}

resource "aws_elasticache_replication_group" "main" {
  replication_group_id = "${local.name_prefix}-redis"
  description          = "Redis for ${local.name_prefix}"

  engine         = "redis"
  engine_version = "7.0"
  node_type      = var.node_type
  port           = var.port

  num_cache_clusters = var.num_cache_clusters

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token                 = random_password.auth_token.result
  automatic_failover_enabled = var.num_cache_clusters > 1
  multi_az_enabled           = var.num_cache_clusters > 1

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-redis"
  })
}

