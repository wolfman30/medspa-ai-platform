terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    # Configure backend in terraform init:
    # terraform init -backend-config="bucket=my-terraform-state" \
    #                -backend-config="key=medspa-ai-platform/terraform.tfstate" \
    #                -backend-config="region=us-east-1"
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "medspa-ai-platform"
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}
