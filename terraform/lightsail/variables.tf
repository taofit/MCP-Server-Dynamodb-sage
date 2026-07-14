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
  default     = "micro_3_0"
}

variable "instance_name" {
  description = "Name of the Lightsail instance. Lightsail instance names are immutable, so this must match the remote instance. If the remote instance is created/recreated with a different name, re-adopt it with: terraform import aws_lightsail_instance.app <new-name>"
  type        = string
  default     = "Ubuntu-1"
}

variable "ssh_key_name" {
  description = "Name for the Lightsail SSH key pair"
  type        = string
  default     = "dynamodb-sage-key"
}
