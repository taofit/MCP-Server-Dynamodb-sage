#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:?Usage: $0 yourdomain.com}"
APP_NAME="dynamodb-sage"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
TF_DIR="$DIR/terraform/lightsail"

echo "=== Step 1: Get Lightsail IP from Terraform ==="
cd "$TF_DIR"
IP=$(terraform output -raw static_ip 2>/dev/null || true)
if [ -z "$IP" ]; then
  echo "Terraform not applied yet. Run: cd $TF_DIR && terraform apply"
  exit 1
fi
echo "  IP: $IP"

echo ""
echo "=== Step 2: IMPORTANT — Add A record at one.com ==="
echo "  Before continuing, add an A record at one.com:"
echo "    Type:  A"
echo "    Host:  @"
echo "    Value: $IP"
echo ""
read -rp "  Done adding the A record? DNS may take a few minutes. Press Enter to continue..."

echo ""
echo "=== Step 3: Build Go binary for Linux ==="
cd "$DIR"
GOOS=linux GOARCH=amd64 go build -o /tmp/$APP_NAME .
echo "  Built: /tmp/$APP_NAME"

echo ""
echo "=== Step 4: Create production .env with AWS credentials ==="
cd "$TF_DIR"
AWS_KEY=$(terraform output -raw aws_access_key_id)
AWS_SECRET=$(awk -F'= ' '/aws_secret_access_key/ {print $2}' "$DIR/keys/lightsail-credentials.ini")
cd "$DIR"
cat > /tmp/.env.lightsail <<EOF
AWS_REGION=eu-north-1
DYNAMO_SAGE_DB=data/audit.db
DYNAMO_SAGE_ADDR=:8080
MCP_TRANSPORT_MODE=http
CONFIG_PATH=config.yaml
AWS_ACCESS_KEY_ID=$AWS_KEY
AWS_SECRET_ACCESS_KEY=$AWS_SECRET
EOF
echo "  Created: /tmp/.env.lightsail (with IAM user credentials)"

echo "=== Step 5: Copy files to Lightsail ==="
SSH_KEY="$DIR/keys/lightsail.pem"
scp -i "$SSH_KEY" -o StrictHostKeyChecking=accept-new /tmp/$APP_NAME ubuntu@$IP:/tmp/$APP_NAME
scp -i "$SSH_KEY" "$DIR/config.yaml" ubuntu@$IP:/tmp/config.yaml
scp -i "$SSH_KEY" /tmp/.env.lightsail ubuntu@$IP:/tmp/.env.lightsail
scp -i "$SSH_KEY" "$DIR/scripts/setup-lightsail.sh" ubuntu@$IP:/tmp/setup-lightsail.sh

echo ""
echo "=== Step 6: Run setup on Lightsail ==="
ssh -i "$SSH_KEY" ubuntu@$IP "sudo bash /tmp/setup-lightsail.sh $DOMAIN"

echo ""
echo "=== Deploy complete ==="
echo "  https://$DOMAIN"
echo ""
echo "To restart after subsequent code changes:"
echo "  GOOS=linux GOARCH=amd64 go build -o /tmp/$APP_NAME ."
echo "  scp -i keys/lightsail.pem /tmp/$APP_NAME config.yaml ubuntu@<IP>:/tmp/"
echo "  ssh -i keys/lightsail.pem ubuntu@<IP> \"sudo cp /tmp/config.yaml /opt/$APP_NAME/config.yaml && sudo systemctl restart $APP_NAME\""
