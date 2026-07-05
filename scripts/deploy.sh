#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:?Usage: $0 yourdomain.com}"
APP_NAME="dynamodb-sage"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
TF_DIR="$DIR/terraform/lightsail"
SSH_KEY="$DIR/keys/lightsail.pem"
TARBALL="/tmp/${APP_NAME}.tar.gz"
DEPLOY_DIR="/tmp/${APP_NAME}-deploy"

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
echo "=== Step 3: Get IAM credentials from Terraform ==="
AWS_KEY=$(terraform output -raw aws_access_key_id)
AWS_SECRET=$(awk -F'= ' '/aws_secret_access_key/ {print $2}' "$DIR/keys/lightsail-credentials.ini")
cd "$DIR"

echo ""
echo "=== Step 4: Build Go binary for linux/amd64 (locally) ==="
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
GOOS=linux GOARCH=amd64 go build -ldflags="-X dynamodb-sage/server.Version=${VERSION} -s -w" -o "/tmp/${APP_NAME}" .
echo "  Built: /tmp/${APP_NAME}"

echo ""
echo "=== Step 5: Create deployment tarball ==="
rm -rf "$DEPLOY_DIR" "$TARBALL"
mkdir -p "$DEPLOY_DIR"
# Copy project files (excluding heavy/unnecessary dirs)
rsync -a --exclude='.git' --exclude='keys' --exclude='data' --exclude='terraform' --exclude='*.tar.gz' --exclude='dynamo-sage' "$DIR/" "$DEPLOY_DIR/"
# Add pre-built binary and release Dockerfile
cp "/tmp/${APP_NAME}" "$DEPLOY_DIR/${APP_NAME}"
cp "$DIR/Dockerfile.release" "$DEPLOY_DIR/Dockerfile"
cd "$DEPLOY_DIR"
tar -czf "$TARBALL" .
echo "  Created: $TARBALL ($(du -h "$TARBALL" | cut -f1))"
rm -rf "$DEPLOY_DIR"

echo ""
echo "=== Step 6: Upload & extract on Lightsail ==="
scp -i "$SSH_KEY" -o StrictHostKeyChecking=accept-new "$TARBALL" ubuntu@$IP:/tmp/$APP_NAME.tar.gz
ssh -i "$SSH_KEY" ubuntu@$IP "sudo rm -rf /opt/$APP_NAME && sudo mkdir -p /opt/$APP_NAME && sudo tar -xzf /tmp/$APP_NAME.tar.gz -C /opt/$APP_NAME"

echo ""
echo "=== Step 7: Write production .env with IAM credentials ==="
ssh -i "$SSH_KEY" ubuntu@$IP "sudo tee /opt/$APP_NAME/.env > /dev/null <<ENVEOF
AWS_ACCESS_KEY_ID=$AWS_KEY
AWS_SECRET_ACCESS_KEY=$AWS_SECRET
AWS_REGION=eu-north-1
MCP_TRANSPORT_MODE=http
CONFIG_PATH=/app/config.yaml
KAFKA_CONFIG_PATH=/app/config/kafka.yaml
METRICS_ADDR=:2112
DYNAMO_SAGE_DB=/app/data/audit.db
DYNAMO_SAGE_ADDR=:8080
KAFKA_BROKERS=kafka:9092
ENVEOF"

echo ""
echo "=== Step 8: First-time nginx + SSL, or skip if already done ==="
if ssh -i "$SSH_KEY" ubuntu@$IP "test -f /etc/nginx/sites-enabled/$APP_NAME" 2>/dev/null; then
  echo "  nginx config found — skipping first-time setup"
else
  echo "  First-time deploy — setting up nginx + certbot"
  scp -i "$SSH_KEY" "$DIR/scripts/setup-lightsail.sh" ubuntu@$IP:/tmp/setup-lightsail.sh
  ssh -i "$SSH_KEY" ubuntu@$IP "sudo bash /tmp/setup-lightsail.sh $DOMAIN"
fi

echo ""
echo "=== Step 9: Start the full stack with Docker Compose ==="
ssh -i "$SSH_KEY" ubuntu@$IP "cd /opt/$APP_NAME && sudo docker compose up -d --build"

echo ""
echo "=== Step 10: Wait for health check ==="
sleep 8
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/health" 2>/dev/null || echo "000")
if [ "$HEALTH" = "200" ]; then
  echo "  App is healthy (HTTPS $HEALTH)"
else
  echo "  ⚠️  Health check returned HTTP $HEALTH — check logs: ssh -i $SSH_KEY ubuntu@$IP \"sudo docker compose logs app\""
fi

echo ""
echo "=== Deploy complete ==="
echo "  https://$DOMAIN"
echo ""
echo "To redeploy after code changes:"
echo "  $0 $DOMAIN"