#!/bin/bash

# ============================================================================
# SETUP TABLE WITH GSI
# ============================================================================

echo "⚙️  Creating DynamoDB table 'Users' with Global Secondary Index..."

awslocal dynamodb create-table \
    --table-name Users \
    --attribute-definitions \
        AttributeName=Username,AttributeType=S \
        AttributeName=Email,AttributeType=S \
    --key-schema \
        AttributeName=Username,KeyType=HASH \
    --global-secondary-indexes \
        "[{\"IndexName\": \"EmailIndex\", \"KeySchema\": [{\"AttributeName\":\"Email\",\"KeyType\":\"HASH\"}], \"Projection\": {\"ProjectionType\":\"ALL\"}}]" \
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
    --item '{"Username":{"S":"alice"},"Email":{"S":"[EMAIL_ADDRESS]"},"Age":{"N":"25"},"City":{"S":"New York"}}'

# Insert Bob
awslocal dynamodb put-item \
    --table-name Users \
    --item '{"Username":{"S":"bob"},"Email":{"S":"[EMAIL_ADDRESS]"},"Age":{"N":"30"},"City":{"S":"London"}}'

# Insert Charlie
awslocal dynamodb put-item \
    --table-name Users \
    --item '{"Username":{"S":"charlie"},"Email":{"S":"[EMAIL_ADDRESS]"},"Age":{"N":"22"},"City":{"S":"Paris"}}'

echo "✓ Test data inserted!"

# ============================================================================
# SCAN TO TEST GSI FUNCTIONALITY
# ============================================================================

echo ""
echo "🔍 Scanning table by email (testing GSI)..."

awslocal dynamodb scan \
    --table-name Users \
    --filter-expression "Email = :email" \
    --expression-attribute-values '{ ":email": {"S":"[EMAIL_ADDRESS]"} }'

echo "✓ Scan complete!"