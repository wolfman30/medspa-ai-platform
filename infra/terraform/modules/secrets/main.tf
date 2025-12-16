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
    DATABASE_URL     = ""
    ADMIN_JWT_SECRET = ""

    # Twilio
    TWILIO_ACCOUNT_SID    = ""
    TWILIO_AUTH_TOKEN     = ""
    TWILIO_WEBHOOK_SECRET = ""
    TWILIO_FROM_NUMBER    = ""
    TWILIO_ORG_MAP_JSON   = "{}"

    # Telnyx
    TELNYX_API_KEY              = ""
    TELNYX_MESSAGING_PROFILE_ID = ""
    TELNYX_WEBHOOK_SECRET       = ""

    # Payments
    PAYMENT_PROVIDER_KEY         = ""
    SQUARE_ACCESS_TOKEN          = ""
    SQUARE_LOCATION_ID           = ""
    SQUARE_WEBHOOK_SIGNATURE_KEY = ""
    SQUARE_CLIENT_ID             = ""
    SQUARE_CLIENT_SECRET         = ""

    # Email
    SENDGRID_API_KEY    = ""
    SENDGRID_FROM_EMAIL = ""
    SENDGRID_FROM_NAME  = "MedSpa AI"

    # EMR (Nextech)
    NEXTECH_BASE_URL      = ""
    NEXTECH_CLIENT_ID     = ""
    NEXTECH_CLIENT_SECRET = ""
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}
