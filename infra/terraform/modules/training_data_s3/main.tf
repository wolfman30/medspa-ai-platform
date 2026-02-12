resource "aws_s3_bucket" "training_data" {
  bucket = "${var.bucket_name}"

  tags = {
    Environment = var.environment
    Purpose     = "llm-training-data"
    ManagedBy   = "terraform"
  }
}

resource "aws_s3_bucket_versioning" "training_data" {
  bucket = aws_s3_bucket.training_data.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "training_data" {
  bucket = aws_s3_bucket.training_data.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "training_data" {
  bucket = aws_s3_bucket.training_data.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "training_data" {
  bucket = aws_s3_bucket.training_data.id

  rule {
    id     = "glacier-after-365-days"
    status = "Enabled"

    transition {
      days          = 365
      storage_class = "GLACIER"
    }
  }
}

# IAM policy for ECS task role to access training data bucket
resource "aws_iam_policy" "training_data_access" {
  name        = "${var.environment}-training-data-s3-access"
  description = "Allow ECS tasks to read/write training data S3 bucket"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
          "s3:ListBucket"
        ]
        Resource = [
          aws_s3_bucket.training_data.arn,
          "${aws_s3_bucket.training_data.arn}/*"
        ]
      }
    ]
  })
}

output "bucket_name" {
  value = aws_s3_bucket.training_data.id
}

output "bucket_arn" {
  value = aws_s3_bucket.training_data.arn
}

output "policy_arn" {
  value = aws_iam_policy.training_data_access.arn
}
