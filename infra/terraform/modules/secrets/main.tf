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

resource "aws_secretsmanager_secret_version" "app_secrets" {
  secret_id = aws_secretsmanager_secret.app_secrets.id
  secret_string = jsonencode({
    twilio_account_sid    = ""
    twilio_auth_token     = ""
    twilio_webhook_secret = ""
    payment_provider_key  = ""
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}
