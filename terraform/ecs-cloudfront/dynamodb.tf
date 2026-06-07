# ============================================================================
# DynamoDB Tables
# ============================================================================

# ----------------------------------------------------------------------------
# Users Table
# ----------------------------------------------------------------------------
resource "aws_dynamodb_table" "users" {
  name         = "Users"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"

  attribute {
    name = "user_id"
    type = "S"
  }

  attribute {
    name = "email"
    type = "S"
  }

  global_secondary_index {
    name            = "emailIndex"
    hash_key        = "email"
    projection_type = "ALL"
  }

  tags = {
    TablePurpose = "UserManagement"
  }
}

# ----------------------------------------------------------------------------
# Orders Table
# ----------------------------------------------------------------------------
resource "aws_dynamodb_table" "orders" {
  name         = "Orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "customer_id"

  attribute {
    name = "customer_id"
    type = "S"
  }

  tags = {
    TablePurpose = "OrderManagement"
  }
}
