# 🧙‍♂️ DynamoDB-Sage

### *The Zero-Trust Streaming AI Security Gateway for Amazon DynamoDB*

[![Platform: AWS Lightsail](https://img.shields.io/badge/Platform-AWS_Lightsail-orange.svg?style=flat-square)]()
[![Streaming: Apache Kafka](https://img.shields.io/badge/Streaming-Apache_Kafka-black.svg?style=flat-square)]()
[![Protocol: MCP](https://img.shields.io/badge/Protocol-MCP_SDK-blue.svg?style=flat-square)]()

DynamoDB-Sage is an enterprise-grade, security-first Model Context Protocol (MCP) gateway that bridges LLM agents (like Claude and Cursor) securely with Amazon DynamoDB. 

Autonomous AI agents are highly prone to hallucination anomalies—whether running unthrottled table scans that spike cloud bills, or performing accidental, destructive bulk mutations. DynamoDB-Sage acts as an intelligent firewall and decoupled background execution engine, ensuring that AI-driven database interactions are deterministic, cost-bounded, compliant, and real-time.

---

### 🚀 Key Differentiators

* **Two-Layer Runtime Protection:** Every single AI request passes through an automated **Risk Analyzer** to evaluate destructive blast-radius and compute costs *before* execution. A rigorous **Guardrail Engine** then acts as an inline proxy to enforce schema compliance, block PII leaks, and bound throughput constraints.
* **Dual-Pipeline Task Offloading:** Lightweight reads and writes are served synchronously for an instantaneous user experience. Multi-second, high-impact heavy operations (like massive `BatchWrites`, structural table creation, or full scans) are safely offloaded out-of-process into **Apache Kafka** worker queues.
* **Event-Driven Proactive Alerts:** Powered by a Kafka-to-MCP streaming subsystem, the server doesn't just wait to be asked questions. It actively watches database changes and streams **real-time push notifications** (`notifications/message`) directly to the client's UI console the moment compliance risks or jobs wrap up.
* **Immutable Zero-Trust Audit Trail:** Native, low-latency tracking logs that track the execution principal, time signatures, partition footprints, and real-time AWS throughput costs securely.
* **No Injection Exploits:** Complete protection against prompt-based injection attacks by exclusively enforcing structured, type-safe JSON tool calls instead of open-ended string processing.

[![DynamoDB Sage Architecture](assets/hero.svg)](https://www.youtube.com/watch?v=f4i8fxrdEBw)

<details>
<summary><b>🗺️ View Architecture Flow Diagram</b></summary>

<img src="assets/architecture-flow.svg" width="900" alt="Architecture Flow Diagram"/>

*Full description in [project-flow.md](project-flow.md)*
</details>

---

## 🛠️ Prerequisites

- [Docker](https://www.docker.com/)
- [Go 1.25+](https://golang.org/) (for local binary development)
- [LocalStack Pro account](https://app.localstack.cloud) (for local dev)
- [Terraform 1.5+](https://www.terraform.io/) (for AWS deployment)

---

## 💻 Local Development

The project uses **Docker Compose** to run all services locally:

| Service      | Profile   | Default |
|--------------|-----------|---------|
| App (Go)     | —         | yes     |
| Zookeeper    | —         | yes     |
| Kafka        | —         | yes     |
| LocalStack   | `local`   | no      |

1. Copy the environment template and configure your variables:

```bash
cp .env.example .env
```

2. Edit `.env` and set your variables:

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
go run .
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

## 🌐 AWS Deployment (Lightsail — Active, $5/mo)

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
- IAM user with `AmazonDynamoDBFullAccess` + `AmazonSSMReadOnlyAccess` (`keys/lightsail-credentials.ini`)
- SSM parameter `/dynamodb-sage/claude/api-key` (empty placeholder — fill via AWS Console after deploy)
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

### Set the LLM API key

After the first deploy, set your Anthropic API key in SSM:

```bash
# Via AWS Console: SSM → Parameter Store → /dynamodb-sage/claude/api-key → edit value
# Or via CLI:
aws ssm put-parameter \
  --name "/dynamodb-sage/claude/api-key" \
  --value "sk-ant-your-key" \
  --type "SecureString" \
  --overwrite
```

The app reads the key from SSM on startup. Restart the container after updating:

```bash
ssh -i keys/lightsail.pem ubuntu@<IP> "cd /opt/dynamodb-sage && sudo docker compose restart app"
```

### Verify

```bash
curl https://dynamodb-sage.yourdomain.com/health
# → ok
```

### Access the Dashboard

Open `https://dynamodb-sage.yourdomain.com/` in a browser. The dashboard is served from the same Go binary — no separate deployment needed.

| Page | Route | Description |
|------|-------|-------------|
| Chat interface | `/` | LLM-powered chat with tool use (see below) |
| Tools explorer | MCP protocol | List and invoke DynamoDB tools |
| Notification history | `/api/notifications` | Real-time push notifications |
| Audit log viewer | `/api/metrics` | Prometheus metrics |

### 💬 Chat Function

The dashboard includes a built-in **AI chat assistant** powered by Claude. It can call any DynamoDB tool on your behalf — just describe what you want in natural language.

**How it works:**
1. User sends a message via the chat UI
2. Message is streamed to Claude via `POST /api/chat` (SSE)
3. Claude can call tools (e.g. `list_tables`, `query_table`) and reason over results
4. Responses stream back token-by-token to the UI

**Example prompts:**
- *"List all my DynamoDB tables"*
- *"Show me the schema of the users table"*
- *"Query the orders table where userId = 123"*
- *"How many items are in each table?"*

**Configuration (env vars):**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLM_API_KEY` | No | — | Anthropic API key (`sk-ant-...`). If empty, falls back to SSM via `LLM_API_KEY_PARAM` |
| `LLM_API_KEY_PARAM` | No | `/dynamodb-sage/claude/api-key` | SSM parameter path for API key |
| `LLM_MODEL` | No | `claude-sonnet-5` | Model to use |
| `LLM_BASE_URL` | No | `https://api.anthropic.com` | API base URL (for proxies) |
| `LLM_TIMEOUT_SEC` | No | `30` | Request timeout in seconds |

> At least one of `LLM_API_KEY` or a valid `LLM_API_KEY_PARAM` SSM parameter must be available for chat to work.

The Prometheus metrics endpoint is not exposed publicly (port `:2112` is internal to the container). To scrape metrics, point your Prometheus server at `http://dynamodb-sage:2112/metrics` within the Docker network, or expose `METRICS_ADDR=:8081` to serve metrics on the same port under `/metrics`.

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

### Production Architecture Details

| Component | Detail |
|-----------|--------|
| **Region** | `eu-north-1` |
| **Compute** | Lightsail `nano_3_0` (2 vCPU, 0.5 GiB, 20 GB SSD) |
| **App** | Go binary in Docker (pre-built locally, copied via tarball) |
| **Queue** | Apache Kafka in Docker (Confluent 7.6.0) |
| **LLM** | Anthropic Claude via `LLM_API_KEY` or SSM `/dynamodb-sage/claude/api-key` |
| **Port** | 8080 |
| **Transport** | Streamable HTTP (POST `/`) + SSE (`GET /sse`), health at `/health` |
| **HTTPS** | Let's Encrypt via certbot + nginx reverse proxy |
| **Domain** | Your own domain (A record at DNS provider) |
| **IAM** | Dedicated IAM user with `AmazonDynamoDBFullAccess` + `AmazonSSMReadOnlyAccess` |
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

## 🔌 Connecting MCP Clients

> **Public demo server** available at `https://dynamodb-sage.hzcentre.com` — try it directly with any MCP client by replacing the URL with yours in the JSON config.

> ⚠️ **Important**: The risk analyzer may return warnings for expensive or destructive operations (e.g. large scans, batch deletes, schema changes). Some MCP clients (including Claude) may auto-confirm these warnings without asking you. To prevent this, tell the LLM explicitly: *"If the server returns a risk warning, show it to me and ask for my confirmation before proceeding. Never auto-confirm."*

### opencode

```json
{
  "mcpServers": {
    "dynamo-sage-local": {
      "type": "local",
      "command": ["go", "run", "."],
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
      "args": ["-c", "cd /path/to/dynamodb-sage && KAFKA_BROKERS=localhost:9093 AWS_BASE_ENDPOINT=http://localhost:4566 AWS_REGION=eu-north-1 go run ."]
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

## 📈 Development Workflow

This project follows **GitHub Flow**:

1. Create a feature branch: `git checkout -b feature/your-feature-name`
2. Commit changes: `git commit -m "Add [feature description]"`
3. Push: `git push origin feature/your-feature-name`
4. Open a PR on GitHub
5. Merge PR and sync local main

---

## 📂 Related Documents

- [Project flow diagram](project-flow.md) — detailed architecture walkthrough
- [Kafka flow diagram](assets/kafka-flow.svg) — async job processing with Kafka
- [Development plan](development-plan.md) — original design document
