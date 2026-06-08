#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:?Usage: $0 yourdomain.com}"
APP_NAME="dynamodb-sage"
APP_USER="dynamodb"
APP_DIR="/opt/$APP_NAME"
BINARY="/tmp/$APP_NAME"
CONFIG_YAML="/tmp/config.yaml"
ENV_FILE="/tmp/.env.lightsail"

echo "=== Installing system dependencies ==="
apt-get update -qq
apt-get install -y -qq nginx certbot python3-certbot-nginx

echo "=== Creating app user ==="
id -u "$APP_USER" &>/dev/null || useradd --system --no-create-home --shell /usr/sbin/nologin "$APP_USER"

echo "=== Setting up app directory ==="
mkdir -p "$APP_DIR/data"
if [ -f "$BINARY" ]; then
  cp "$BINARY" "$APP_DIR/$APP_NAME"
  chmod +x "$APP_DIR/$APP_NAME"
fi
if [ -f "$CONFIG_YAML" ]; then
  cp "$CONFIG_YAML" "$APP_DIR/config.yaml"
fi
if [ -f "$ENV_FILE" ]; then
  cp "$ENV_FILE" "$APP_DIR/.env"
fi
chown -R "$APP_USER:$APP_USER" "$APP_DIR"

echo "=== Configuring systemd service ==="
cat > "/etc/systemd/system/$APP_NAME.service" <<SERVICEEOF
[Unit]
Description=$APP_NAME MCP Server
After=network.target

[Service]
Type=simple
User=$APP_USER
Group=$APP_USER
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$APP_NAME
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
Environment=AWS_REGION=eu-north-1
Environment=DYNAMO_SAGE_DB=$APP_DIR/data/audit.db

[Install]
WantedBy=multi-user.target
SERVICEEOF

systemctl daemon-reload
systemctl enable "$APP_NAME"

echo "=== Configuring nginx ==="
cat > "/etc/nginx/sites-available/$APP_NAME" <<NGINXEOF
server {
    listen 80;
    server_name $DOMAIN;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
NGINXEOF

rm -f "/etc/nginx/sites-enabled/default"
ln -sf "/etc/nginx/sites-available/$APP_NAME" "/etc/nginx/sites-enabled/$APP_NAME"

echo "=== Obtaining HTTPS certificate ==="
certbot --nginx --non-interactive --agree-tos --email "admin@$DOMAIN" -d "$DOMAIN" --redirect || echo "certbot failed — run manually later: certbot --nginx -d $DOMAIN"

echo "=== Restarting services ==="
systemctl restart nginx
systemctl start "$APP_NAME"

echo "=== Setup complete ==="
echo ""
echo "  URL:  https://$DOMAIN"
echo "  SSH:  ssh -i keys/lightsail.pem ubuntu@$(curl -s http://checkip.amazonaws.com)"
echo ""
echo "To redeploy after code changes:"
echo "  GOOS=linux GOARCH=amd64 go build -o /tmp/$APP_NAME ."
echo "  scp -i keys/lightsail.pem /tmp/$APP_NAME config.yaml ubuntu@<IP>:/tmp/"
echo "  ssh -i keys/lightsail.pem ubuntu@<IP> \"sudo cp /tmp/config.yaml /opt/$APP_NAME/config.yaml && sudo systemctl restart $APP_NAME\""
