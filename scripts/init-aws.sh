#!/bin/bash

# ============================================================================
# SETUP TABLE WITH GSI
# ============================================================================

echo "⚙️  Creating DynamoDB table 'Users' with Global Secondary Index..."

awslocal dynamodb create-table \
    --table-name Users \
    --attribute-definitions \
        AttributeName=user_id,AttributeType=S \
        AttributeName=email,AttributeType=S \
    --key-schema \
        AttributeName=user_id,KeyType=HASH \
    --global-secondary-indexes \
        "[{\"IndexName\": \"emailIndex\", \"KeySchema\": [{\"AttributeName\":\"email\",\"KeyType\":\"HASH\"}], \"Projection\": {\"ProjectionType\":\"ALL\"}}]" \
    --billing-mode PAY_PER_REQUEST

echo "✓ Table 'Users' created successfully!"

# ============================================================================
# DATA INSERTIONS
# ============================================================================

echo ""
echo "📝 Inserting test data..."

# Insert Alice
awslocal dynamodb put-item \
    --table-name Users \
    --item '{"user_id":{"S":"1"},"user_name":{"S":"alice"},"email":{"S":"alice@example.com"},"age":{"N":"25"},"is_active":{"BOOL":true}}'

# Insert Bob
awslocal dynamodb put-item \
    --table-name Users \
    --item '{"user_id":{"S":"2"},"user_name":{"S":"bob"},"email":{"S":"bob@example.com"},"age":{"N":"30"},"is_active":{"BOOL":true}}'

# Insert Charlie
awslocal dynamodb put-item \
    --table-name Users \
    --item '{"user_id":{"S":"3"},"user_name":{"S":"charlie"},"email":{"S":"charlie@example.com"},"age":{"N":"22"},"is_active":{"BOOL":true}}'

echo "✓ Test data inserted!"

# ============================================================================
# SCAN TO TEST GSI FUNCTIONALITY
# ============================================================================

echo ""
echo "🔍 Scanning table by email (testing GSI)..."

awslocal dynamodb scan \
    --table-name Users \
    --filter-expression "email = :email" \
    --expression-attribute-values '{ ":email": {"S":"alice@example.com"} }'

echo "✓ Scan complete!"