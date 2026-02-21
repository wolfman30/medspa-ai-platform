data "aws_caller_identity" "current" {}

module "vpc" {
  source = "./modules/vpc"

  environment        = var.environment
  vpc_cidr           = var.vpc_cidr
  availability_zones = var.availability_zones
  single_nat_gateway = var.environment != "production"
  enable_nat_gateway = var.enable_nat_gateway
}

module "rds" {
  source = "./modules/rds"

  environment            = var.environment
  vpc_id                 = module.vpc.vpc_id
  private_subnet_ids     = module.vpc.private_subnet_ids
  db_instance_class      = var.db_instance_class
  db_name                = var.db_name
  db_username            = var.db_username
  db_password_secret_arn = module.secrets.db_password_secret_arn
}

module "secrets" {
  source = "./modules/secrets"

  environment = var.environment
  db_username = var.db_username
}

module "redis" {
  source = "./modules/elasticache_redis"

  environment        = var.environment
  vpc_id             = module.vpc.vpc_id
  vpc_cidr           = var.vpc_cidr
  private_subnet_ids = module.vpc.private_subnet_ids
  node_type          = var.redis_node_type
  num_cache_clusters = var.redis_num_cache_clusters
}

module "ecs_fargate" {
  source = "./modules/ecs_fargate"

  environment        = var.environment
  aws_region         = var.aws_region
  vpc_id             = module.vpc.vpc_id
  vpc_cidr           = module.vpc.vpc_cidr
  public_subnet_ids  = module.vpc.public_subnet_ids
  private_subnet_ids = module.vpc.private_subnet_ids

  image_tag       = var.api_image_tag
  desired_count   = var.api_desired_count
  task_cpu        = var.api_task_cpu
  task_memory     = var.api_task_memory
  certificate_arn = var.api_certificate_arn

  enable_blue_green                        = var.enable_blue_green
  codedeploy_deployment_config_name        = var.codedeploy_deployment_config_name
  codedeploy_termination_wait_time_minutes = var.codedeploy_termination_wait_time_minutes
  deployment_minimum_healthy_percent       = var.deployment_minimum_healthy_percent
  deployment_maximum_percent               = var.deployment_maximum_percent

  # Browser sidecar for Moxie booking availability scraping
  enable_browser_sidecar = var.enable_browser_sidecar
  assign_public_ip       = var.assign_public_ip

  environment_variables = {
    LOG_LEVEL                    = "info"
    USE_MEMORY_QUEUE             = "true"
    WORKER_COUNT                 = var.environment == "production" ? "2" : "1"
    AWS_RETRY_MODE               = var.environment == "production" ? "standard" : "adaptive"
    AWS_MAX_ATTEMPTS             = var.environment == "production" ? "3" : "10"
    AWS_REGION                   = var.aws_region
    COGNITO_USER_POOL_ID         = var.cognito_user_pool_id
    COGNITO_CLIENT_ID            = var.cognito_client_id
    COGNITO_REGION               = var.cognito_region
    PUBLIC_BASE_URL              = var.api_public_base_url
    CORS_ALLOWED_ORIGINS         = var.environment == "production" ? "https://aiwolfsolutions.com,https://www.aiwolfsolutions.com,https://portal.aiwolfsolutions.com,https://wolfman30.github.io" : "http://localhost:8000,https://aiwolfsolutions.com,https://www.aiwolfsolutions.com,https://portal-dev.aiwolfsolutions.com,https://wolfman30.github.io"
    ALLOW_FAKE_PAYMENTS          = var.environment != "production" && var.api_public_base_url != "" ? "true" : "false"
    REDIS_ADDR                   = "${module.redis.primary_endpoint_address}:${module.redis.port}"
    REDIS_TLS                    = "true"
    BEDROCK_MODEL_ID             = "us.anthropic.claude-sonnet-4-6-20260514-v1:0"
    BEDROCK_EMBEDDING_MODEL_ID   = "amazon.titan-embed-text-v1"
    TELNYX_TRACK_JOBS            = "true"
    PERSIST_CONVERSATION_HISTORY = "true"
    S3_TRAINING_BUCKET           = "aiwolf-training-data-${var.environment}"
  }

  secret_environment_variables = {
    DATABASE_URL     = "${module.secrets.secret_arn}:DATABASE_URL::"
    ADMIN_JWT_SECRET = "${module.secrets.secret_arn}:ADMIN_JWT_SECRET::"
    REDIS_PASSWORD   = module.redis.auth_token_secret_arn

    TWILIO_ACCOUNT_SID    = "${module.secrets.secret_arn}:TWILIO_ACCOUNT_SID::"
    TWILIO_AUTH_TOKEN     = "${module.secrets.secret_arn}:TWILIO_AUTH_TOKEN::"
    TWILIO_WEBHOOK_SECRET = "${module.secrets.secret_arn}:TWILIO_WEBHOOK_SECRET::"
    TWILIO_FROM_NUMBER    = "${module.secrets.secret_arn}:TWILIO_FROM_NUMBER::"
    TWILIO_ORG_MAP_JSON   = "${module.secrets.secret_arn}:TWILIO_ORG_MAP_JSON::"

    TELNYX_API_KEY              = "${module.secrets.secret_arn}:TELNYX_API_KEY::"
    TELNYX_MESSAGING_PROFILE_ID = "${module.secrets.secret_arn}:TELNYX_MESSAGING_PROFILE_ID::"
    TELNYX_WEBHOOK_SECRET       = "${module.secrets.secret_arn}:TELNYX_WEBHOOK_SECRET::"
    TELNYX_FROM_NUMBER          = "${module.secrets.secret_arn}:TELNYX_FROM_NUMBER::"

    PAYMENT_PROVIDER_KEY         = "${module.secrets.secret_arn}:PAYMENT_PROVIDER_KEY::"
    SQUARE_ACCESS_TOKEN          = "${module.secrets.secret_arn}:SQUARE_ACCESS_TOKEN::"
    SQUARE_LOCATION_ID           = "${module.secrets.secret_arn}:SQUARE_LOCATION_ID::"
    SQUARE_BASE_URL              = "${module.secrets.secret_arn}:SQUARE_BASE_URL::"
    SQUARE_WEBHOOK_SIGNATURE_KEY = "${module.secrets.secret_arn}:SQUARE_WEBHOOK_SIGNATURE_KEY::"
    SQUARE_SUCCESS_URL           = "${module.secrets.secret_arn}:SQUARE_SUCCESS_URL::"
    SQUARE_CANCEL_URL            = "${module.secrets.secret_arn}:SQUARE_CANCEL_URL::"
    SQUARE_SANDBOX               = "${module.secrets.secret_arn}:SQUARE_SANDBOX::"
    SQUARE_CLIENT_ID             = "${module.secrets.secret_arn}:SQUARE_CLIENT_ID::"
    SQUARE_CLIENT_SECRET         = "${module.secrets.secret_arn}:SQUARE_CLIENT_SECRET::"
    SQUARE_OAUTH_REDIRECT_URI    = "${module.secrets.secret_arn}:SQUARE_OAUTH_REDIRECT_URI::"
    SQUARE_OAUTH_SUCCESS_URL     = "${module.secrets.secret_arn}:SQUARE_OAUTH_SUCCESS_URL::"

    SENDGRID_API_KEY    = "${module.secrets.secret_arn}:SENDGRID_API_KEY::"
    SENDGRID_FROM_EMAIL = "${module.secrets.secret_arn}:SENDGRID_FROM_EMAIL::"
    SENDGRID_FROM_NAME  = "${module.secrets.secret_arn}:SENDGRID_FROM_NAME::"

    SES_FROM_EMAIL = "${module.secrets.secret_arn}:SES_FROM_EMAIL::"
    SES_FROM_NAME  = "${module.secrets.secret_arn}:SES_FROM_NAME::"

    DISABLE_PAYMENT_COOLDOWN = "${module.secrets.secret_arn}:DISABLE_PAYMENT_COOLDOWN::"

    STRIPE_SECRET_KEY        = "${module.secrets.secret_arn}:STRIPE_SECRET_KEY::"
    STRIPE_WEBHOOK_SECRET    = "${module.secrets.secret_arn}:STRIPE_WEBHOOK_SECRET::"
    STRIPE_CONNECT_CLIENT_ID = "${module.secrets.secret_arn}:STRIPE_CONNECT_CLIENT_ID::"
    STRIPE_DRY_RUN           = "${module.secrets.secret_arn}:STRIPE_DRY_RUN::"
    STRIPE_SUCCESS_URL       = "${module.secrets.secret_arn}:STRIPE_SUCCESS_URL::"
    STRIPE_CANCEL_URL        = "${module.secrets.secret_arn}:STRIPE_CANCEL_URL::"

    NEXTECH_BASE_URL      = "${module.secrets.secret_arn}:NEXTECH_BASE_URL::"
    NEXTECH_CLIENT_ID     = "${module.secrets.secret_arn}:NEXTECH_CLIENT_ID::"
    NEXTECH_CLIENT_SECRET = "${module.secrets.secret_arn}:NEXTECH_CLIENT_SECRET::"
  }

  secret_arns = [module.secrets.secret_arn, module.redis.auth_token_secret_arn]
}

module "lambda" {
  source = "./modules/lambda"

  environment     = var.environment
  aws_region      = var.aws_region
  vpc_id          = module.vpc.vpc_id
  subnet_ids      = module.vpc.private_subnet_ids
  memory_size     = var.api_lambda_memory
  timeout         = var.api_lambda_timeout
  image_tag       = var.api_image_tag
  create_function = var.enable_voice_webhooks

  upstream_base_url = var.voice_upstream_base_url != "" ? var.voice_upstream_base_url : (var.api_public_base_url != "" ? var.api_public_base_url : "http://${module.ecs_fargate.alb_dns_name}")
}

module "api_gateway" {
  count  = var.enable_voice_webhooks ? 1 : 0
  source = "./modules/api_gateway"

  environment          = var.environment
  lambda_function_name = module.lambda.function_name
  lambda_invoke_arn    = module.lambda.invoke_arn
}

resource "aws_iam_role_policy_attachment" "ecs_training_data" {
  role       = module.ecs_fargate.task_role_name
  policy_arn = module.training_data_s3.policy_arn
}

module "training_data_s3" {
  source = "./modules/training_data_s3"

  bucket_name = "aiwolf-training-data-${var.environment}"
  environment = var.environment
}

module "onboarding_ui" {
  count  = trimspace(var.ui_domain_name) != "" ? 1 : 0
  source = "./modules/static_site"

  environment     = var.environment
  domain_name     = trimspace(var.ui_domain_name)
  certificate_arn = var.ui_certificate_arn
  price_class     = var.ui_price_class
}
