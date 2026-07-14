output "static_ip" {
  description = "Public IP address for the Lightsail instance"
  value       = aws_lightsail_instance.app.public_ip_address
}

output "ssh_user" {
  description = "SSH user for the Lightsail instance"
  value       = "ubuntu"
}

output "ssh_private_key_file" {
  description = "Path to the SSH private key file"
  value       = abspath("${path.module}/../../keys/lightsail.pem")
}

output "instance_name" {
  description = "Lightsail instance name"
  value       = aws_lightsail_instance.app.name
}

output "connect_command" {
  description = "Command to SSH into the instance"
  value       = "ssh -i ${abspath("${path.module}/../../keys/lightsail.pem")} ubuntu@${aws_lightsail_instance.app.public_ip_address}"
}

output "aws_access_key_id" {
  description = "IAM access key ID for DynamoDB access"
  value       = aws_iam_access_key.lightsail.id
  sensitive   = true
}

output "iam_user_name" {
  description = "IAM user name for Lightsail"
  value       = aws_iam_user.lightsail.name
}
