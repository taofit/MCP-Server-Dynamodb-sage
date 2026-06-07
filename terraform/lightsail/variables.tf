variable "project_name" {
  description = "Project name"
  type        = string
  default     = "dynamodb-sage"
}

variable "aws_region" {
  description = "AWS region for Lightsail"
  type        = string
  default     = "eu-north-1"
}

variable "instance_plan" {
  description = "Lightsail plan (bundle ID)"
  type        = string
  default     = "nano_3_0"
}

variable "ssh_key_name" {
  description = "Name for the Lightsail SSH key pair"
  type        = string
  default     = "dynamodb-sage-key"
}
