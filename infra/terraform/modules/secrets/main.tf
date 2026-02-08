resource "random_password" "db_password" {
  length  = 32
  special = true
}

resource "aws_secretsmanager_secret" "db_password" {
  name        = "medspa-${var.environment}-db-password"
  description = "Database password for ${var.environment} environment"

  tags = {
    Name = "medspa-${var.environment}-db-password"
  }
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = random_password.db_password.result
}

resource "aws_secretsmanager_secret" "app_secrets" {
  name        = "medspa-${var.environment}-app-secrets"
  description = "Application secrets for ${var.environment} environment"

  tags = {
    Name = "medspa-${var.environment}-app-secrets"
  }
}

# IMPORTANT:
# Terraform manages the *existence* (ARN/tags) of the app secret, but not the
# secret payload. The payload is updated out-of-band (GitHub Actions refreshes
# DATABASE_URL after reading the RDS managed secret), and managing a
# `aws_secretsmanager_secret_version` here causes Terraform to overwrite/roll
# back the secret during deployments.
