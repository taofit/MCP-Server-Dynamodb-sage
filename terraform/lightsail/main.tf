terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ------------------------------------------------------------------------------
# SSH key pair
# ------------------------------------------------------------------------------
resource "tls_private_key" "lightsail" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "aws_lightsail_key_pair" "main" {
  name       = var.ssh_key_name
  public_key = tls_private_key.lightsail.public_key_openssh
}

resource "local_sensitive_file" "private_key" {
  content         = tls_private_key.lightsail.private_key_pem
  filename        = "${path.module}/../../keys/lightsail.pem"
  file_permission = "0600"
}

# ------------------------------------------------------------------------------
# Lightsail instance
# ------------------------------------------------------------------------------
resource "aws_lightsail_instance" "app" {
  name              = var.instance_name
  availability_zone = "${var.aws_region}a"
  blueprint_id      = "ubuntu_22_04"
  bundle_id         = var.instance_plan
  key_pair_name     = var.ssh_key_name
  depends_on        = [aws_iam_access_key.lightsail]


  # -------------------------------------------------
  # Bootstrap the VM with Docker
  # -------------------------------------------------
user_data = <<-EOF
  #!/bin/bash
  set -e

  # Install Docker
  apt-get update -y
  apt-get install -y docker.io

  # Install Docker Compose v2 plugin
  DOCKER_CONFIG=/usr/local/lib/docker
  mkdir -p $DOCKER_CONFIG/cli-plugins
  curl -sSL "https://github.com/docker/compose/releases/download/v2.27.0/docker-compose-linux-x86_64" -o $DOCKER_CONFIG/cli-plugins/docker-compose
  chmod +x $DOCKER_CONFIG/cli-plugins/docker-compose

  # Create app directory for deploy.sh to populate
  mkdir -p /opt/dynamodb-sage/data
EOF


  tags = {
    Name = var.project_name
  }

  lifecycle {
    ignore_changes = [user_data]
  }
}


# ------------------------------------------------------------------------------
# NOTE: The static IP (StaticIp-1), its attachment to instance "Ubuntu-1", and
# the instance firewall (ports 22/80/443) are managed manually outside of
# Terraform. The AWS provider for this Lightsail version does not support
# importing these resources, so they are intentionally excluded from config to
# keep `terraform plan` free of destructive recreate actions.
# ------------------------------------------------------------------------------

# ------------------------------------------------------------------------------
# IAM user for Lightsail DynamoDB access
# ------------------------------------------------------------------------------
resource "aws_iam_user" "lightsail" {
  name = "${var.project_name}-lightsail"
  path = "/lightsail/"
}

resource "aws_iam_user_policy_attachment" "dynamodb_read_write" {
  user       = aws_iam_user.lightsail.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess"
}

resource "aws_iam_user_policy_attachment" "ssm_read" {
  user       = aws_iam_user.lightsail.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMReadOnlyAccess"
}

# ------------------------------------------------------------------------------
# SSM parameter for LLM API key (fill value via AWS Console after deploy)
# ------------------------------------------------------------------------------
resource "aws_ssm_parameter" "llm_api_key" {
  name        = "/dynamodb-sage/claude/api-key"
  description = "API key for LLM provider (Anthropic Claude)"
  type        = "SecureString"
  value       = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name = "${var.project_name}-llm-api-key"
  }
}

resource "aws_ssm_parameter" "openai_api_key" {
  name        = "/dynamodb-sage/openai/api-key"
  description = "API key for OpenAI (embeddings)"
  type        = "SecureString"
  value       = "PLACEHOLDER"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name = "${var.project_name}-openai-api-key"
  }
}

resource "aws_iam_access_key" "lightsail" {
  user = aws_iam_user.lightsail.name
}

resource "local_sensitive_file" "aws_credentials" {
  content  = <<-EOF
[default]
aws_access_key_id = ${aws_iam_access_key.lightsail.id}
aws_secret_access_key = ${aws_iam_access_key.lightsail.secret}
region = eu-north-1
EOF
  filename = "${path.module}/../../keys/lightsail-credentials.ini"
  file_permission = "0600"
}
