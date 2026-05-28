# ------------------------------------------------------------------------------
# EFS File System (persistent storage for audit log)
# ------------------------------------------------------------------------------
resource "aws_efs_file_system" "app" {
  creation_token = "${var.project_name}-audit"
  encrypted      = true

  tags = { Name = "${var.project_name}-efs" }
}

# ------------------------------------------------------------------------------
# EFS Access Point — maps to /app/data with correct ownership
# ------------------------------------------------------------------------------
resource "aws_efs_access_point" "app" {
  file_system_id = aws_efs_file_system.app.id

  posix_user {
    uid = 1000
    gid = 1000
  }

  root_directory {
    path = "/data"
    creation_info {
      owner_uid   = 1000
      owner_gid   = 1000
      permissions = 0755
    }
  }

  tags = { Name = "${var.project_name}-efs-ap" }
}

# ------------------------------------------------------------------------------
# EFS Mount Target — one per public subnet
# ------------------------------------------------------------------------------
resource "aws_efs_mount_target" "app" {
  count           = 2
  file_system_id  = aws_efs_file_system.app.id
  subnet_id       = aws_subnet.public[count.index].id
  security_groups = [aws_security_group.efs.id]
}

# ------------------------------------------------------------------------------
# Security Group for EFS — allow NFS from ECS tasks
# ------------------------------------------------------------------------------
resource "aws_security_group" "efs" {
  name        = "${var.project_name}-efs-sg"
  description = "Allow NFS from ECS tasks"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "NFS from ECS tasks"
    from_port       = 2049
    to_port         = 2049
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs_tasks.id]
  }

  tags = { Name = "${var.project_name}-efs-sg" }
}
