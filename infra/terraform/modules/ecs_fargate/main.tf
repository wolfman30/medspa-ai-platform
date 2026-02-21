locals {
  name_prefix = "medspa-${var.environment}"
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/ecs/${local.name_prefix}-api"
  retention_in_days = 14

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-api-logs"
  })
}

resource "aws_cloudwatch_log_group" "migrate" {
  name              = "/ecs/${local.name_prefix}-migrate"
  retention_in_days = 14

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-migrate-logs"
  })
}

resource "aws_cloudwatch_log_group" "browser_sidecar" {
  count             = var.enable_browser_sidecar ? 1 : 0
  name              = "/ecs/${local.name_prefix}-browser-sidecar"
  retention_in_days = 14

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-browser-sidecar-logs"
  })
}

resource "aws_ecr_repository" "api" {
  name                 = "${local.name_prefix}-api"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-api-ecr"
  })
}

resource "aws_ecr_repository" "browser_sidecar" {
  count                = var.enable_browser_sidecar ? 1 : 0
  name                 = "${local.name_prefix}-browser-sidecar"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-browser-sidecar-ecr"
  })
}

resource "aws_ecs_cluster" "main" {
  name = "${local.name_prefix}-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-cluster"
  })
}

resource "aws_ecs_cluster_capacity_providers" "main" {
  cluster_name = aws_ecs_cluster.main.name

  capacity_providers = var.enable_fargate_fallback ? ["FARGATE", "FARGATE_SPOT"] : ["FARGATE_SPOT"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE_SPOT"
    weight            = var.spot_weight
    base              = 0
  }

  dynamic "default_capacity_provider_strategy" {
    for_each = var.enable_fargate_fallback ? [1] : []
    content {
      capacity_provider = "FARGATE"
      weight            = var.fargate_weight
      base              = var.fargate_base
    }
  }
}

resource "aws_security_group" "alb" {
  name        = "${local.name_prefix}-alb-sg"
  description = "ALB security group"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from internet"
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTPS from internet"
  }

  dynamic "ingress" {
    for_each = var.enable_blue_green ? [1] : []
    content {
      from_port   = var.test_listener_port
      to_port     = var.test_listener_port
      protocol    = "tcp"
      cidr_blocks = [var.vpc_cidr]
      description = "CodeDeploy test listener from VPC"
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-alb-sg"
  })
}

resource "aws_security_group" "tasks" {
  name        = "${local.name_prefix}-tasks-sg"
  description = "ECS tasks security group"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = var.container_port
    to_port         = var.container_port
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
    description     = "Container port from ALB"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-tasks-sg"
  })
}

resource "aws_lb" "api" {
  name               = "${local.name_prefix}-alb"
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = var.public_subnet_ids

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-alb"
  })
}

resource "aws_lb_target_group" "api" {
  name        = "${local.name_prefix}-tg"
  port        = var.container_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  health_check {
    path                = var.health_check_path
    matcher             = "200-399"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-tg"
  })
}

resource "aws_lb_target_group" "api_green" {
  count = var.enable_blue_green ? 1 : 0

  name        = "${local.name_prefix}-tg-green"
  port        = var.container_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  health_check {
    path                = var.health_check_path
    matcher             = "200-399"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-tg-green"
  })
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.api.arn
  port              = 80
  protocol          = "HTTP"

  dynamic "default_action" {
    for_each = var.certificate_arn != "" ? [1] : []
    content {
      type = "redirect"

      redirect {
        port        = "443"
        protocol    = "HTTPS"
        status_code = "HTTP_301"
      }
    }
  }

  dynamic "default_action" {
    for_each = var.certificate_arn == "" ? [1] : []
    content {
      type             = "forward"
      target_group_arn = aws_lb_target_group.api.arn
    }
  }

  lifecycle {
    ignore_changes = [default_action]
  }
}

resource "aws_lb_listener" "https" {
  count             = var.certificate_arn != "" ? 1 : 0
  load_balancer_arn = aws_lb.api.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  lifecycle {
    ignore_changes = [default_action]
  }
}

resource "aws_lb_listener" "test" {
  count = var.enable_blue_green ? 1 : 0

  load_balancer_arn = aws_lb.api.arn
  port              = var.test_listener_port
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  lifecycle {
    precondition {
      condition     = var.certificate_arn != ""
      error_message = "certificate_arn must be set when enable_blue_green is true (required for the CodeDeploy HTTPS test listener)."
    }
  }

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api_green[0].arn
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-test-listener"
  })
}

data "aws_iam_policy_document" "task_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "execution" {
  name               = "${local.name_prefix}-ecs-exec-role"
  assume_role_policy = data.aws_iam_policy_document.task_assume.json

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-ecs-exec-role"
  })
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "execution_secrets" {
  count = length(var.secret_arns) > 0 ? 1 : 0
  name  = "${local.name_prefix}-ecs-exec-secrets"
  role  = aws_iam_role.execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [for arn in var.secret_arns : "${arn}*"]
      },
      {
        Effect   = "Allow"
        Action   = ["kms:Decrypt"]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role" "task" {
  name               = "${local.name_prefix}-ecs-task-role"
  assume_role_policy = data.aws_iam_policy_document.task_assume.json

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-ecs-task-role"
  })
}

data "aws_caller_identity" "current" {}

resource "aws_iam_role_policy" "bedrock_runtime" {
  name = "${local.name_prefix}-bedrock-runtime"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "BedrockInvokeClaudeModels"
        Effect = "Allow"
        Action = [
          "bedrock:Converse",
          "bedrock:ConverseStream",
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithResponseStream"
        ]
        Resource = [
          # Sonnet 4.6 (primary SMS + Voice LLM)
          "arn:aws:bedrock:${var.aws_region}:${data.aws_caller_identity.current.account_id}:inference-profile/us.anthropic.claude-sonnet-4-6",
          "arn:aws:bedrock:*::foundation-model/anthropic.claude-sonnet-4-6",

          # Haiku 4.5 (classifier + fallback)
          "arn:aws:bedrock:${var.aws_region}:${data.aws_caller_identity.current.account_id}:application-inference-profile/0llkmqbvb1gw",
          "arn:aws:bedrock:${var.aws_region}:${data.aws_caller_identity.current.account_id}:inference-profile/us.anthropic.claude-haiku-4-5-20251001-v1:0",
          "arn:aws:bedrock:${var.aws_region}:${data.aws_caller_identity.current.account_id}:inference-profile/global.anthropic.claude-haiku-4-5-20251001-v1:0",

          "arn:aws:bedrock:*::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0",
          "arn:aws:bedrock:*::foundation-model/amazon.titan-embed-text-v1",
          "arn:aws:bedrock:*::foundation-model/amazon.titan-embed-text-v1:2:8k",
          "arn:aws:bedrock:*::foundation-model/amazon.titan-embed-text-v2:0",
          "arn:aws:bedrock:*::foundation-model/amazon.titan-embed-text-v2:0:8k"
        ]
      }
    ]
  })
}

locals {
  computed_image_uri = var.api_image_uri != "" ? var.api_image_uri : "${aws_ecr_repository.api.repository_url}:${var.image_tag}"
  migrate_image_uri  = "${aws_ecr_repository.api.repository_url}:migrate-${var.image_tag}"
  prod_listener_arns = var.certificate_arn != "" ? [aws_lb_listener.https[0].arn] : [aws_lb_listener.http.arn]

  # Browser sidecar image URI (only computed when enabled)
  browser_sidecar_image_uri = var.enable_browser_sidecar ? (
    var.browser_sidecar_image_uri != "" ? var.browser_sidecar_image_uri : "${aws_ecr_repository.browser_sidecar[0].repository_url}:${var.image_tag}"
  ) : ""

  # Browser sidecar container definition (only when enabled)
  browser_sidecar_container = var.enable_browser_sidecar ? [{
    name      = "browser-sidecar"
    image     = local.browser_sidecar_image_uri
    essential = false
    portMappings = [
      {
        containerPort = var.browser_sidecar_port
        hostPort      = var.browser_sidecar_port
        protocol      = "tcp"
      }
    ]
    environment = [
      { name = "PORT", value = tostring(var.browser_sidecar_port) },
      { name = "HEADLESS", value = "true" },
      { name = "NODE_ENV", value = "production" },
      { name = "PLAYWRIGHT_BROWSERS_PATH", value = "/ms-playwright" }
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.browser_sidecar[0].name
        awslogs-region        = var.aws_region
        awslogs-stream-prefix = "browser"
      }
    }
    healthCheck = {
      command     = ["CMD-SHELL", "curl -f http://localhost:${var.browser_sidecar_port}/health || exit 1"]
      interval    = 30
      timeout     = 10
      retries     = 3
      startPeriod = 60
    }
  }] : []

  base_env = merge({
    PORT = tostring(var.container_port)
    ENV  = var.environment
    }, var.environment_variables, var.enable_browser_sidecar ? {
    BROWSER_SIDECAR_URL = "http://localhost:${var.browser_sidecar_port}"
  } : {})

  env_list = [
    for k, v in local.base_env : {
      name  = k
      value = v
    }
  ]

  secrets_list = [
    for k, v in var.secret_environment_variables : {
      name      = k
      valueFrom = v
    }
  ]

  migrate_secrets_list = [
    for k, v in var.secret_environment_variables : {
      name      = k
      valueFrom = v
    } if k == "DATABASE_URL"
  ]
}

resource "aws_ecs_task_definition" "api" {
  family                   = "${local.name_prefix}-api"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  # Increase CPU/memory when browser sidecar is enabled
  cpu                = var.enable_browser_sidecar ? var.task_cpu + var.browser_sidecar_cpu : var.task_cpu
  memory             = var.enable_browser_sidecar ? var.task_memory + var.browser_sidecar_memory : var.task_memory
  execution_role_arn = aws_iam_role.execution.arn
  task_role_arn      = aws_iam_role.task.arn

  container_definitions = jsonencode(concat([
    {
      name      = "api"
      image     = local.computed_image_uri
      essential = true
      portMappings = [
        {
          containerPort = var.container_port
          hostPort      = var.container_port
          protocol      = "tcp"
        }
      ]
      environment = local.env_list
      secrets     = local.secrets_list
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.api.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "api"
        }
      }
    }
  ], local.browser_sidecar_container))

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-taskdef"
  })
}

resource "aws_ecs_task_definition" "migrate" {
  family                   = "${local.name_prefix}-migrate"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.migrate_task_cpu
  memory                   = var.migrate_task_memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  container_definitions = jsonencode([
    {
      name      = "migrate"
      image     = local.migrate_image_uri
      essential = true
      secrets   = local.migrate_secrets_list
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.migrate.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "migrate"
        }
      }
    }
  ])

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-migrate-taskdef"
  })
}

resource "aws_ecs_service" "api" {
  count = var.enable_blue_green ? 1 : 0

  name                   = "${local.name_prefix}-api"
  cluster                = aws_ecs_cluster.main.id
  task_definition        = aws_ecs_task_definition.api.arn
  desired_count          = var.desired_count
  enable_execute_command = var.enable_execute_command

  deployment_controller {
    type = "CODE_DEPLOY"
  }

  network_configuration {
    subnets          = var.assign_public_ip ? var.public_subnet_ids : var.private_subnet_ids
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = var.assign_public_ip
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = var.container_port
  }

  lifecycle {
    ignore_changes = [
      task_definition,
      load_balancer,
      network_configuration,
    ]
  }

  capacity_provider_strategy {
    capacity_provider = "FARGATE_SPOT"
    weight            = var.spot_weight
    base              = 0
  }

  dynamic "capacity_provider_strategy" {
    for_each = var.enable_fargate_fallback ? [1] : []
    content {
      capacity_provider = "FARGATE"
      weight            = var.fargate_weight
      base              = var.fargate_base
    }
  }

  health_check_grace_period_seconds = 30

  depends_on = [
    aws_lb_listener.http,
    aws_ecs_cluster_capacity_providers.main
  ]

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-api-service"
  })
}

resource "aws_ecs_service" "api_rolling" {
  count = var.enable_blue_green ? 0 : 1

  name                   = "${local.name_prefix}-api"
  cluster                = aws_ecs_cluster.main.id
  task_definition        = aws_ecs_task_definition.api.arn
  desired_count          = var.desired_count
  enable_execute_command = var.enable_execute_command

  deployment_controller {
    type = "ECS"
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  deployment_minimum_healthy_percent = var.deployment_minimum_healthy_percent
  deployment_maximum_percent         = var.deployment_maximum_percent

  network_configuration {
    subnets          = var.assign_public_ip ? var.public_subnet_ids : var.private_subnet_ids
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = var.assign_public_ip
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = var.container_port
  }

  capacity_provider_strategy {
    capacity_provider = "FARGATE_SPOT"
    weight            = var.spot_weight
    base              = 0
  }

  dynamic "capacity_provider_strategy" {
    for_each = var.enable_fargate_fallback ? [1] : []
    content {
      capacity_provider = "FARGATE"
      weight            = var.fargate_weight
      base              = var.fargate_base
    }
  }

  health_check_grace_period_seconds = 30

  depends_on = [
    aws_lb_listener.http,
    aws_ecs_cluster_capacity_providers.main
  ]

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-api-service"
  })
}

locals {
  api_service_name = var.enable_blue_green ? aws_ecs_service.api[0].name : aws_ecs_service.api_rolling[0].name
}

data "aws_iam_policy_document" "codedeploy_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["codedeploy.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "codedeploy" {
  count              = var.enable_blue_green ? 1 : 0
  name               = "${local.name_prefix}-codedeploy-role"
  assume_role_policy = data.aws_iam_policy_document.codedeploy_assume.json

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-codedeploy-role"
  })
}

resource "aws_iam_role_policy_attachment" "codedeploy" {
  count      = var.enable_blue_green ? 1 : 0
  role       = aws_iam_role.codedeploy[0].name
  policy_arn = "arn:aws:iam::aws:policy/AWSCodeDeployRoleForECS"
}

resource "aws_codedeploy_app" "api" {
  count            = var.enable_blue_green ? 1 : 0
  name             = "${local.name_prefix}-api"
  compute_platform = "ECS"
}

resource "aws_codedeploy_deployment_group" "api" {
  count                 = var.enable_blue_green ? 1 : 0
  app_name              = aws_codedeploy_app.api[0].name
  deployment_group_name = "${local.name_prefix}-api"
  service_role_arn      = aws_iam_role.codedeploy[0].arn

  deployment_config_name = var.codedeploy_deployment_config_name

  deployment_style {
    deployment_option = "WITH_TRAFFIC_CONTROL"
    deployment_type   = "BLUE_GREEN"
  }

  blue_green_deployment_config {
    deployment_ready_option {
      action_on_timeout    = "CONTINUE_DEPLOYMENT"
      wait_time_in_minutes = 0
    }

    terminate_blue_instances_on_deployment_success {
      action                           = "TERMINATE"
      termination_wait_time_in_minutes = var.codedeploy_termination_wait_time_minutes
    }
  }

  ecs_service {
    cluster_name = aws_ecs_cluster.main.name
    service_name = local.api_service_name
  }

  load_balancer_info {
    target_group_pair_info {
      prod_traffic_route {
        listener_arns = local.prod_listener_arns
      }

      test_traffic_route {
        listener_arns = [aws_lb_listener.test[0].arn]
      }

      target_group {
        name = aws_lb_target_group.api.name
      }

      target_group {
        name = aws_lb_target_group.api_green[0].name
      }
    }
  }

  auto_rollback_configuration {
    enabled = true
    events = [
      "DEPLOYMENT_FAILURE",
      "DEPLOYMENT_STOP_ON_REQUEST",
    ]
  }

  depends_on = [
    aws_iam_role_policy_attachment.codedeploy,
    aws_lb_listener.test,
  ]
}
