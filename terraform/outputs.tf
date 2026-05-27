output "ecr_repository_url" {
  description = "ECR repository URL"
  value       = aws_ecr_repository.app.repository_url
}

output "ecs_cluster_name" {
  description = "ECS cluster name"
  value       = aws_ecs_cluster.main.name
}

output "ecs_service_name" {
  description = "ECS service name"
  value       = aws_ecs_service.app.name
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = aws_lb.app.dns_name
}

output "docker_push_commands" {
  description = "Commands to build and push the Docker image to ECR and force ECS redeploy"
  value = <<-EOT
# Authenticate Docker to ECR
aws ecr get-login-password --region ${var.aws_region} | docker login --username AWS --password-stdin ${aws_ecr_repository.app.repository_url}

# Build the image
docker build -t ${var.project_name} .

# Tag and push
docker tag ${var.project_name}:latest ${aws_ecr_repository.app.repository_url}:latest
docker push ${aws_ecr_repository.app.repository_url}:latest

# Force ECS to pull the new image
aws ecs update-service --cluster ${aws_ecs_cluster.main.name} --service ${aws_ecs_service.app.name} --force-new-deployment --region ${var.aws_region}
EOT
}
