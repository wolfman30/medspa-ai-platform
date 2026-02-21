# Dev environment overrides
environment = "development"
aws_region  = "us-east-1"

# ECS task sizing (current production values)
api_task_cpu    = 1024
api_task_memory = 3072
api_desired_count = 1

# Blue/green deployment via CodeDeploy
enable_blue_green                        = true
codedeploy_deployment_config_name        = "CodeDeployDefault.ECSAllAtOnce"
codedeploy_termination_wait_time_minutes = 5

# HTTPS
api_certificate_arn = "arn:aws:acm:us-east-1:422017356225:certificate/65cd7cfe-4f83-48a6-8d3f-70db25dbf9d1"

# Public base URL
api_public_base_url = "https://api-dev.aiwolfsolutions.com"

# No NAT Gateway — ECS on public subnets (saves ~$32/mo)
enable_nat_gateway = false
assign_public_ip   = true

# No browser sidecar (replaced by Moxie API)
enable_browser_sidecar = false

# Portal UI
ui_domain_name    = "portal-dev.aiwolfsolutions.com"
ui_certificate_arn = "arn:aws:acm:us-east-1:422017356225:certificate/65cd7cfe-4f83-48a6-8d3f-70db25dbf9d1"

# Cognito auth
cognito_user_pool_id = ""
cognito_client_id    = ""
cognito_region       = ""
