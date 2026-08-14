# Deployment

## Option A: AWS Lightsail (Recommended)

A single Lightsail instance runs the full stack. nginx + Let's Encrypt provide HTTPS.

**First-time setup:**

```bash
cd terraform/lightsail
terraform init && terraform apply
```

This creates: Lightsail instance, static IP, SSH key, IAM user, SSM parameter for the API key, and firewall rules.

**Deploy:**

```bash
./scripts/deploy.sh dynamodb-sage.yourdomain.com
```

The script builds locally, uploads via SCP, and starts everything with Docker Compose.

**Set the LLM API key:**

```bash
aws ssm put-parameter \
  --name "/dynamodb-sage/claude/api-key" \
  --value "sk-ant-your-key" \
  --type "SecureString" \
  --overwrite
```

**Redeploy after code changes:**

```bash
./scripts/deploy.sh dynamodb-sage.yourdomain.com
```

**Verify:**

```bash
curl https://dynamodb-sage.yourdomain.com/health
# → ok
```

### Instance name & Terraform state

The Lightsail instance uses the `instance_name` variable (default `Ubuntu-1`). Instance names are immutable — changing the variable forces a destroy/recreate. To adopt an existing instance with a different name:

```bash
cd terraform/lightsail
terraform state rm aws_lightsail_instance.app
terraform import aws_lightsail_instance.app Ubuntu-2
# Set instance_name = "Ubuntu-2" in terraform.tfvars
terraform plan   # should report "No changes"
```

> `scripts/deploy.sh` resolves the instance name from `terraform output -raw instance_name`. Override at runtime with `INSTANCE_NAME=... ./scripts/deploy.sh dynamodb-sage.yourdomain.com`.

### Versioning

The binary embeds a version from `git describe --tags --always`. Tag before deploying:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

No tags → falls back to commit hash → `"dev"`. Set `VERSION=...` to override.

## Option B: ECS + ALB + CloudFront (Reference)

The original high-availability deployment using ECS Fargate, ALB, CloudFront, and ECR. Infrastructure code preserved at `terraform/ecs-cloudfront/` for reference.

## Environment Variables

See [`.env.example`](../.env.example) for the full reference. Key auth variables:

| Variable | Purpose |
|----------|---------|
| `GUEST_KEY` | Read-only guest bearer token (shared) |
| `ADMIN_KEY` | Full-access admin bearer token |
| `SKIP_TOKEN_CHECK` | Set to `true` to disable auth entirely (dev only) |
| `CORS_ORIGIN` | Allowed browser origin for the dashboard |
