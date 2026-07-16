#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:?Usage: $0 yourdomain.com}"
APP_NAME="dynamodb-sage"
APP_DIR="/opt/$APP_NAME"

echo "=== Installing system dependencies ==="
apt-get update -qq
apt-get install -y -qq nginx certbot python3-certbot-nginx

echo "=== Creating app directory if needed ==="
mkdir -p "$APP_DIR/data"

echo "=== Configuring nginx reverse proxy to Docker app ==="
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
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
NGINXEOF

rm -f "/etc/nginx/sites-enabled/default"
ln -sf "/etc/nginx/sites-available/$APP_NAME" "/etc/nginx/sites-enabled/$APP_NAME"

echo "=== Obtaining HTTPS certificate ==="
certbot --nginx --non-interactive --agree-tos --email "admin@$DOMAIN" -d "$DOMAIN" --redirect || echo "certbot failed — run manually later: certbot --nginx -d $DOMAIN"

echo "=== Restarting nginx ==="
systemctl restart nginx

echo "=== Setup complete ==="
echo ""
echo "  URL:  https://$DOMAIN"
echo ""
echo "To redeploy after code changes:"
echo "  $0 $DOMAIN"