# dynamodb-sage

**Security-first MCP gateway for DynamoDB** — LLMs securely interact with DynamoDB
through a guardrail layer that enforces capacity limits, validates operations,
and audits every action. Designed for safe multi-tenant AI access to production
data.

Key differentiators:
- **Guardrails**: capacity caps, operation whitelists, query limits
- **Audit trail**: every DynamoDB operation logged with principal, timestamp,
  and throughput
- **No direct SQL/NoSQL injection**: structured tool calls only

[![Demo Video](https://img.youtube.com/vi/zt_6hMwcw2c/maxresdefault.jpg)](https://www.youtube.com/watch?v=zt_6hMwcw2c)

## Prerequisites

- [Docker](https://www.docker.com/)
- [Go 1.25+](https://golang.org/)
- [LocalStack Pro account](https://app.localstack.cloud) (for local dev)
- [Terraform 1.5+](https://www.terraform.io/) (for AWS deployment)
- AWS CLI configured with credentials (for AWS deployment)

---

## Local Development

### Setup

1. Copy the env file and add your LocalStack auth token:
```bash
cp .env.example .env
```

Edit `.env`:
```
# update for your project
LOCALSTACK_AUTH_TOKEN=your_token_here
```

2. Update the setting in `config.yaml` according to your project's requirements. For example: set query limit, PII fields to hide, and tables schema constraints etc.

3. Start LocalStack:  
```bash
docker compose up -d
```

This will:
- Start a LocalStack container on port `4566`
- Automatically create the `Users` table with a GSI on `Email`
- Insert test data (alice, bob, charlie)

Verify it's running:
```bash
curl http://localhost:4566/_localstack/health
```

To stop:
```bash
docker compose down
```

### Run Go Code

By default the server uses **stdio transport** (reads MCP messages from stdin/stdout):

```bash
go run main.go
```

The server waits for MCP messages on stdin — make sure LocalStack is running first.

To run as an **HTTP server** (for use with the MCP Inspector or other HTTP clients):

```bash
MCP_TRANSPORT_MODE=http go run main.go
```

The server listens on port `8080` for HTTP POST requests using Streamable HTTP transport.

### Verify DynamoDB Table

```bash
aws dynamodb scan --table-name Users --endpoint-url http://localhost:4566
```

### Test MCP Server Locally with Inspector

Test with the MCP Inspector (requires Node.js/npm, opens browser):

**Option A — HTTP mode** (recommended for testing, no restart needed):

1. In one terminal, start the server in HTTP mode:
   ```bash
   MCP_TRANSPORT_MODE=http go run main.go
   ```
2. In another terminal, run the inspector with `--transport http`:
   ```bash
   npx @modelcontextprotocol/inspector --transport http http://localhost:8080
   ```
4. Click **"List Tools"** to verify tools are registered.
5. Click **"Call Tool"** for `list_tables` to see results from LocalStack.

**Option B — Stdio mode** (uses the default transport):

1. Run the inspector:
   ```bash
   npx @modelcontextprotocol/inspector
   ```
2. In the browser, set transport type to **stdio**, command to `go run main.go`.
3. Set the working directory by using `sh -c`:  
   **Command**: `sh`  
   **Args**: `-c`, `cd /path/to/project && exec go run main.go`
4. Add required environment variables in the inspector's **Environment Variables** section (or they'll be inherited from your terminal).

---

## AWS Deployment

Two deployment options are available:

### Option A: Lightsail (Active — $5/mo)

Deploy a single Lightsail instance with nginx, Let's Encrypt HTTPS, and your own domain.

**First-time setup:**

```bash
cd terraform/lightsail
terraform init
terraform apply
```

This creates:
- Lightsail instance (`nano_3_0`, Ubuntu 22.04)
- Static IP address
- SSH key (saved to `keys/lightsail.pem`)
- IAM user with `AmazonDynamoDBFullAccess` (credentials saved to `keys/lightsail-credentials.ini`)
- Firewall rules (ports 22, 80, 443)

After apply, note the static IP:

```bash
terraform output static_ip
```

**One-time domain & HTTPS setup:**

1. At your DNS provider (e.g. one.com), add an A record pointing to the static IP
2. Run the deploy script which sets up nginx, obtains certs, and deploys the app:

```bash
bash scripts/deploy.sh dynamodb-sage.yourdomain.com
```

**Deploy code changes:**

```bash
GOOS=linux GOARCH=amd64 go build -o /tmp/dynamodb-sage .
scp -i keys/lightsail.pem /tmp/dynamodb-sage ubuntu@<IP>:/tmp/dynamodb-sage
ssh -i keys/lightsail.pem ubuntu@<IP> sudo systemctl restart dynamodb-sage
```

**Verify health:**

```bash
curl https://dynamodb-sage.yourdomain.com/health
# → ok
```

---

### Option B: ECS + ALB + CloudFront (Reference — Archived)

The original high-availability deployment using ECS Fargate, ALB, CloudFront, and ECR.
Infrastructure code is preserved at `terraform/ecs-cloudfront/` for reference.

```bash
cd terraform/ecs-cloudfront
terraform init
terraform apply
```

After apply:

```bash
terraform output cloudfront_domain
# → d3xxxxxxxxxxxx.cloudfront.net
```

**Deploy code changes (ECS):**

```bash
docker buildx build --platform=linux/amd64 -t dynamodb-sage . --load && \
docker tag dynamodb-sage:latest 335360747704.dkr.ecr.eu-north-1.amazonaws.com/dynamodb-sage:latest && \
docker push 335360747704.dkr.ecr.eu-north-1.amazonaws.com/dynamodb-sage:latest && \
aws ecs update-service --cluster dynamodb-sage-cluster --service dynamodb-sage-service --force-new-deployment --region eu-north-1
```

If not logged into ECR:

```bash
aws ecr get-login-password --region eu-north-1 | docker login --username AWS --password-stdin 335360747704.dkr.ecr.eu-north-1.amazonaws.com/dynamodb-sage
```

**Check ECS status:**

```bash
aws ecs describe-services --cluster dynamodb-sage-cluster --service dynamodb-sage-service --region eu-north-1
```

---

## Connecting MCP Clients

### opencode

Add to `opencode.json` in your project root:

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

**Local (stdio):**

The server loads `.env` automatically. When using `sh -c`, the working directory is the project root so `.env` is found — no env vars needed in config.

```json
{
  "mcpServers": {
    "dynamodb-sage-local": {
      "command": "sh",
      "args": ["-c", "cd /path/to/dynamodb-sage && exec go run main.go"]
    }
  }
}
```

> Copy `.env.example` to `.env` and fill in your own credentials before starting.

**Pre-built binary** (faster startup, but must pass env vars explicitly):

```bash
cd /path/to/dynamodb-sage && go build -o /tmp/dynamodb-sage .
```

```json
{
  "mcpServers": {
    "dynamodb-sage-local": {
      "command": "/tmp/dynamodb-sage",
      "env": {
        "LOCALSTACK_AUTH_TOKEN": "your_token_here",
        "AWS_BASE_ENDPOINT": "http://localhost:4566",
        "AWS_REGION": "eu-north-1",
        "AWS_ACCESS_KEY_ID": "your_access_key",
        "AWS_SECRET_ACCESS_KEY": "your_secret_key",
        "CONFIG_PATH": "/path/to/dynamodb-sage/config.yaml",
        "DYNAMO_SAGE_DB": "/path/to/dynamodb-sage/data/audit.db"
      }
    }
  }
}
```

**Remote AWS (via supergateway, SSE — for Claude Desktop):**

```json
{
  "mcpServers": {
    "dynamodb-sage-aws": {
      "command": "npx",
      "args": ["-y", "supergateway", "--sse", "https://dynamodb-sage.yourdomain.com/sse"]
    }
  }
}
```

**Remote AWS (via supergateway, Streamable HTTP):**

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

In the chat, ask natural language questions like:

**Exploration**
   - *"List all tables"*
   - *"Describe the user table"*
   - *"Show me the schema of every table"*

**Create & Insert**
   - *"Create a table called products with id as the primary key and a GSI on category"*
   - *"Add a table orders with orderId as HASH, status as RANGE, and a GSI on status"*
   - *"Put an item in the products table with id=p1, name=Widget, price=10, category=electronics"*
   - *"Add 5 users with id, name and age fields"*
   - *"Batch insert 100 sample products with random prices and categories"*

**Query & Read**
   - *"Get me the user with id=u1 and name=alice"*
   - *"Find all products in the electronics category using the GSI"*
   - *"Query the user table using the age-lsi index for users with age >= 25"*
   - *"Check if anyone in the user table is over 80 years old"*
   - *"Find all users whose name starts with 'b'"*
   - *"Show me all orders with status=shipped"*
   - *"Scan all items in the products table and tell me the average price"*

**Update & Delete**
   - *"Update the price of product p1 to 15"*
   - *"Delete the user with id=u1 and name=alice"*
   - *"Remove all products with price over 100"*
   - *"Remove/delete a table called products"*

**Monitoring**
   - *"Show me the audit log"*
   - *"What operations have been run recently?"*
   - *"How much read capacity did my last query use?"*

### Any MCP Client

Use Streamable HTTP transport with the URL:
```
https://dynamodb-sage.yourdomain.com
```

### Glama MCP Inspector

[Glama MCP Inspector](https://glama.ai/mcp/inspector) — test tools directly in the browser with syntax highlighting.

1. Open [Glama MCP Inspector](https://glama.ai/mcp/inspector)
2. Click **"Add Server"**
3. URL: `https://dynamodb-sage.yourdomain.com`
4. Click **"Connect"**

**Tool call JSON examples (paste into the Arguments field):**

`get_item` — fetch a single item by key:
```json
{
  "tableName": "mcp_user",
  "key": {
    "id": "007",
    "name": "bond"
  }
}
```

`put_item` — insert a new item:
```json
{
  "tableName": "mcp_user",
  "item": {
    "id": "100",
    "name": "test",
    "age": 42
  }
}
```

`query_table` — query with optional index:
```json
{
  "tableName": "mcp_user",
  "keyCondition": "id = :id",
  "expressionAttributeValues": {
    ":id": "007"
  }
}
```

`query_table` with GSI:
```json
{
  "tableName": "mcp_user",
  "indexName": "age-lsi",
  "keyCondition": "age >= :age",
  "expressionAttributeValues": {
    ":age": 30
  }
}
```

`update_item` — update fields with expressions:
```json
{
  "tableName": "mcp_user",
  "key": {
    "id": "007",
    "name": "bond"
  },
  "updateExpression": "SET age = :newAge",
  "expressionAttributeValues": {
    ":newAge": 46
  }
}
```

`delete_item` — delete by key:
```json
{
  "tableName": "mcp_user",
  "key": {
    "id": "100",
    "name": "test"
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

## Architecture

Two deployment options are available:

### Lightsail (Active — $5/mo)

| Component | Detail |
|-----------|--------|
| **Region** | `eu-north-1` |
| **Compute** | Lightsail `nano_3_0` (2 vCPU, 0.5 GiB, 20 GB SSD) |
| **Port** | 8080 |
| **Transport** | Streamable HTTP (POST `/`) + SSE (`GET /sse`), health at `/health` |
| **HTTPS** | Let's Encrypt via certbot + nginx reverse proxy |
| **Domain** | Your own domain (A record at DNS provider) |
| **IAM** | Dedicated IAM user with `AmazonDynamoDBFullAccess` |
| **Logs** | `journalctl -u dynamodb-sage` |

### ECS + CloudFront (Archived — Reference)

| Component | Detail |
|-----------|--------|
| **Region** | `eu-north-1` |
| **Compute** | Fargate (0.25 vCPU, 0.5 GiB) |
| **Port** | 8080 |
| **Transport** | Streamable HTTP, health check at `/health` |
| **HTTPS** | CloudFront (`*.cloudfront.net`) with auto-provisioned SSL |
| **IAM** | DynamoDB full access + `sts:GetCallerIdentity` |
| **Logs** | CloudWatch `/ecs/dynamodb-sage` (30-day retention) |
| **Image** | `335360747704.dkr.ecr.eu-north-1.amazonaws.com/dynamodb-sage:latest` |