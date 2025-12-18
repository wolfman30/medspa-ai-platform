locals {
  name_prefix = "medspa-${var.environment}"
}

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${local.name_prefix}-voice"
  retention_in_days = 14

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-voice-logs"
  })
}

resource "aws_ecr_repository" "voice" {
  name                 = "${local.name_prefix}-voice-lambda"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-voice-ecr"
  })
}

resource "aws_security_group" "lambda" {
  name        = "${local.name_prefix}-voice-lambda-sg"
  description = "Security group for voice webhook Lambda"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-voice-lambda-sg"
  })
}

resource "aws_iam_role" "lambda" {
  name = "${local.name_prefix}-voice-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-voice-lambda-role"
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_vpc" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_lambda_function" "voice" {
  count = var.create_function ? 1 : 0

  function_name = "${local.name_prefix}-voice"
  role          = aws_iam_role.lambda.arn
  package_type  = "Image"
  image_uri     = "${aws_ecr_repository.voice.repository_url}:${var.image_tag}"

  memory_size = var.memory_size
  timeout     = var.timeout

  vpc_config {
    subnet_ids         = var.subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      ENV               = var.environment
      UPSTREAM_BASE_URL = var.upstream_base_url
      UPSTREAM_TIMEOUT  = "${var.timeout}s"
    }
  }

  tags = merge(var.tags, {
    Name = "${local.name_prefix}-voice"
  })

  depends_on = [aws_cloudwatch_log_group.lambda]
}
