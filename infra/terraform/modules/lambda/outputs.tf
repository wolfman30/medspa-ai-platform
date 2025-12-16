output "function_name" {
  description = "Name of the Lambda function"
  value       = aws_lambda_function.voice.function_name
}

output "function_arn" {
  description = "ARN of the Lambda function"
  value       = aws_lambda_function.voice.arn
}

output "invoke_arn" {
  description = "Invoke ARN of the Lambda function"
  value       = aws_lambda_function.voice.invoke_arn
}

output "ecr_repository_url" {
  description = "ECR repository URL for the voice lambda image"
  value       = aws_ecr_repository.voice.repository_url
}
