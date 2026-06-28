# dynamodb-sage

**Security-first MCP gateway for DynamoDB** — LLMs securely interact with DynamoDB
through a two-layer protection system: a **risk analyzer** pre-assesses every operation,
then **guardrails** enforce capacity limits, validate schemas, and protect sensitive tables.
Every action is audited. Large operations (batch puts, batch deletes, table creation)
are offloaded to **Apache Kafka** for async background processing.

Key differentiators:
- **Two-layer protection**: risk analyzer pre-checks every operation for cost, size, and destructive potential; guardrails enforce hard limits on throughput, batch sizes, and schema compliance
- **Async heavy operations**: Kafka-backed job queue for large DynamoDB operations (batch writes, table creation)
- **Audit trail**: every DynamoDB operation logged with principal, timestamp, and throughput
- **No direct SQL/NoSQL injection**: structured tool calls only

[![Demo Video](https://img.youtube.com/vi/f4i8fxrdEBw/maxresdefault.jpg)](https://www.youtube.com/watch?v=f4i8fxrdEBw)

<details>
<summary><b>🗺️ View Architecture Flow Diagram</b></summary>

<img src="assets/architecture-flow.svg" width="900" alt="Architecture Flow Diagram"/>

*Full description in [project-flow.md](project-flow.md)*
</details>

## Prerequisites

- [Docker](https://www.docker.com/)
- [Go 1.25+](https://golang.org/) (for local binary development)
- [LocalStack Pro account](https://app.localstack.cloud) (for local dev)
- [Terraform 1.5+](https://www.terraform.io/) (for AWS deployment)

---

## Local Development

The project uses **Docker Compose** to run all services locally:

| Service      | Profile   | Default |
|--------------|-----------|---------|
| App (Go)     | —         | yes     |
| Zookeeper    | —         | yes     |
| Kafka        | —         | yes     |
| LocalStack   | `local`   | no      |

1. Copy the environment template and configure your LocalStack auth token:

```bash
cp .env.example .env
```

2. Edit `.env` and set your variables, e.g.:

```bash
LOCALSTACK_AUTH_TOKEN=your_token_here
```

3. Start the full development stack:

```bash
docker compose --profile local up -d --build
```

This starts the Go app, Kafka (with Zookeeper), and LocalStack. The `--build` flag ensures your latest local code changes are compiled into the Docker image before starting.

> **Troubleshooting:** If Kafka exits with `KeeperErrorCode = NodeExists`, stale broker data is left in Zookeeper. Run `docker compose --profile local down && docker compose --profile local up -d` to restart with a clean state.

4. Verify the services are healthy:

```bash
curl http://localhost:4566/_localstack/health   # LocalStack
nc -z localhost 9092 && echo "Kafka up"         # Kafka
curl http://localhost:8080/health               # Go app
```

5. Stop all containers when done:

```bash
 docker compose --profile local down -v   # stop local stack and remove volumes
```

### Run Go binary locally (outside Docker)

For faster iteration, run the Go binary directly while keeping Kafka and LocalStack in Docker:

```bash
KAFKA_BROKERS=localhost:9093 \
AWS_BASE_ENDPOINT=http://localhost:4566 \
AWS_REGION=eu-north-1 \
AWS_ACCESS_KEY_ID=your_key_id \
AWS_SECRET_ACCESS_KEY=your_secret_key \
MCP_TRANSPORT_MODE=http \
DYNAMO_SAGE_ADDR=":8081" \
go run main.go
```

> Kafka on `localhost:9093` (PLAINTEXT_HOST listener) and LocalStack on `localhost:4566` are the host-mapped ports from Docker.

**Custom HTTP address**
You can change the listen address for the Go binary by setting the `DYNAMO_SAGE_ADDR` environment variable (e.g., `DYNAMO_SAGE_ADDR=":8081"`). The server defaults to `:8080` when services are started in docker environment or if the variable is unset when running locally.
### Test with MCP Inspector

```bash
# using docker compose
npx @modelcontextprotocol/inspector --transport http http://localhost:8080
# or using local binary
npx @modelcontextprotocol/inspector --transport http http://localhost:8081
```

---

## AWS Deployment (Lightsail — Active, $5/mo)

A single Lightsail instance runs the full stack (app + Kafka + Zookeeper) in Docker via Compose.
nginx + Let's Encrypt provide HTTPS with your own domain.

### First-time infrastructure

```bash
cd terraform/lightsail
terraform init
terraform apply
```

This creates:
- Lightsail instance (`nano_3_0`, Ubuntu 22.04, Docker pre-installed)
- Static IP address
- SSH key (`keys/lightsail.pem`)
- IAM user with `AmazonDynamoDBFullAccess` (`keys/lightsail-credentials.ini`)
- Firewall rules (ports 22, 80, 443)

### Deploy the app

```bash
./scripts/deploy.sh dynamodb-sage.yourdomain.com
```

The script:
1. Gets the static IP from Terraform
2. Prompts you to add an A record at your DNS provider
3. Builds the Go binary **locally** (avoiding compilation on the small Lightsail VM)
4. Packages the project into a tarball and uploads it via SCP
5. Writes the production `.env` with IAM credentials
6. **First time only**: installs nginx + certbot for HTTPS
7. Runs `docker compose up -d --build` to start app, Kafka, and Zookeeper

### Verify

```bash
curl https://dynamodb-sage.yourdomain.com/health
# → ok
```

### Versioning

The binary embeds a version from `git describe --tags --always`. Tag before deploying:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

No tags → falls back to commit hash → `"dev"`. Set `VERSION=...` env var to override.

### Redeploy after code changes

```bash
./scripts/deploy.sh dynamodb-sage.yourdomain.com
```

The script skips nginx/certbot setup on subsequent runs.

### Architecture

| Component | Detail |
|-----------|--------|
| **Region** | `eu-north-1` |
| **Compute** | Lightsail `nano_3_0` (2 vCPU, 0.5 GiB, 20 GB SSD) |
| **App** | Go binary in Docker (pre-built locally, copied via tarball) |
| **Queue** | Apache Kafka in Docker (Confluent 7.6.0) |
| **Port** | 8080 |
| **Transport** | Streamable HTTP (POST `/`) + SSE (`GET /sse`), health at `/health` |
| **HTTPS** | Let's Encrypt via certbot + nginx reverse proxy |
| **Domain** | Your own domain (A record at DNS provider) |
| **IAM** | Dedicated IAM user with `AmazonDynamoDBFullAccess` |
| **Logs** | `sudo docker compose logs app` |

---

### Option B: ECS + ALB + CloudFront (Reference — Archived)

The original high-availability deployment using ECS Fargate, ALB, CloudFront, and ECR.
Infrastructure code is preserved at `terraform/ecs-cloudfront/` for reference.

```bash
cd terraform/ecs-cloudfront
terraform init
terraform apply
```

---

## Connecting MCP Clients

> **Public demo server** available at `https://dynamodb-sage.hzcentre.com` — try it directly with any MCP client by replacing the URL with yours in the JSON config.

> ⚠️ **Important**: The risk analyzer may return warnings for expensive or destructive operations (e.g. large scans, batch deletes, schema changes). Some MCP clients (including Claude) may auto-confirm these warnings without asking you. To prevent this, tell the LLM explicitly: *"If the server returns a risk warning, show it to me and ask for my confirmation before proceeding. Never auto-confirm."*

### opencode

```json
{
  "mcpServers": {
    "dynamo-sage-local": {
      "type": "local",
      "command": ["go", "run", "main.go"],
      "enabled": true
    },
    "dynamo-sage-aws": {
      "type": "remote",
      "url": "https://dynamodb-sage.yourdomain.com",
      "enabled": true
    }
  }
}
```

### Claude Desktop

**Local (stdio) — requires Docker stack running:**

```json
{
  "mcpServers": {
    "dynamodb-sage-local": {
      "command": "sh",
      "args": ["-c", "cd /path/to/dynamodb-sage && KAFKA_BROKERS=localhost:9093 AWS_BASE_ENDPOINT=http://localhost:4566 AWS_REGION=eu-north-1 go run main.go"]
    }
  }
}
```

**Remote AWS (Streamable HTTP):**

```json
{
  "mcpServers": {
    "dynamodb-sage-aws": {
      "command": "npx",
      "args": ["-y", "supergateway", "--streamableHttp", "https://dynamodb-sage.yourdomain.com", "--streamableHttpPath", "/"]
    }
  }
}
```

---

## Development Workflow

This project follows **GitHub Flow**:

1. Create a feature branch: `git checkout -b feature/your-feature-name`
2. Commit changes: `git commit -m "Add [feature description]"`
3. Push: `git push origin feature/your-feature-name`
4. Open a PR on GitHub
5. Merge PR and sync local main

---

## Related Documents

- [Project flow diagram](project-flow.md) — detailed architecture walkthrough
- [Kafka flow diagram](assets/kafka-flow.svg) — async job processing with Kafka
- [Development plan](development-plan.md) — original design document
