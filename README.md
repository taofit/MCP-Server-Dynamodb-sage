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

### First-Time Infrastructure Setup

Deploy VPC, ECS, ALB, ECR, CloudFront, IAM roles, and DynamoDB tables:

```bash
cd terraform
terraform init
terraform apply
```

This creates:
- **Users table**: Primary key `user_id`, with a GSI `emailIndex` on `email`
- **Orders table**: Primary key `customer_id`

> **Note**: If you don't want to create default tables during initial setup, delete or rename `terraform/dynamodb.tf` before running `terraform apply`. You can create tables later through the MCP server tools or AWS CLI.

To add or customize tables, edit `terraform/dynamodb.tf` before running `terraform apply`:

```hcl
resource "aws_dynamodb_table" "your_table" {
  name           = "YourTableName"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "your_primary_key"

  attribute {
    name = "your_primary_key"
    type = "S"
  }

  # Add GSI if needed
  global_secondary_index {
    name            = "your_gsi_name"
    hash_key        = "your_gsi_key"
    projection_type = "ALL"
  }

  tags = {
    TablePurpose = "YourPurpose"
  }
}
```

After apply, get the CloudFront domain:

```bash
terraform output cloudfront_domain
# → d3xxxxxxxxxxxx.cloudfront.net
```

### Deploy Code Changes

Build, push to ECR, and trigger a new Fargate deployment:

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

> **Infrastructure changes** (task size, env vars, health check, CloudFront): run `cd terraform && terraform apply` first, then the deploy command above.

### Check Deployment Status

```bash
aws ecs describe-services --cluster dynamodb-sage-cluster --service dynamodb-sage-service --region eu-north-1
```

### Verify Health

```bash
curl https://d2fo97f8kuq5a7.cloudfront.net/health
# → ok
```

### Test MCP Server on AWS

The inspector web UI only supports SSE and Stdio transports. Use the `--transport http` CLI flag:

```bash
npx @modelcontextprotocol/inspector --transport http https://d2fo97f8kuq5a7.cloudfront.net
```
or run 
```bash
npx @modelcontextprotocol/inspector --transport http
```
and add `https://d2fo97f8kuq5a7.cloudfront.net` as the server URL in the Streamable transport type.
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
      "type": "sse",
      "url": "https://d2fo97f8kuq5a7.cloudfront.net",
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

**Remote AWS (via supergateway, Streamable HTTP):**

```json
{
  "mcpServers": {
    "dynamodb-sage-aws": {
      "command": "npx",
      "args": ["-y", "supergateway", "--streamableHttp", "https://d2fo97f8kuq5a7.cloudfront.net", "--streamableHttpPath", "/"]
    }
  }
}
```

### Any MCP Client

Use Streamable HTTP transport with the URL:
```
https://d2fo97f8kuq5a7.cloudfront.net
```

### Test from a Browser with AI Chat (No Install)

[MCP Playground](https://mcpsplayground.com) is a web-based MCP client that works entirely in the browser — no downloads, no local setup.

1. Open [https://mcpsplayground.com](https://mcpsplayground.com)
2. Click **"Add Server"** and choose **"Remote"**
3. Enter the URL:
   ```
   https://d2fo97f8kuq5a7.cloudfront.net
   ```
4. Click **"Connect"** — the playground auto-discovers all registered tools
5. In the chat, ask natural language questions like:

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

The playground uses Claude / Gemini as the AI engine, so it handles tool selection and parameter filling automatically.

### Glama MCP Inspector

[Glama MCP Inspector](https://glama.ai/mcp/inspector) — test tools directly in the browser with syntax highlighting.

1. Open [Glama MCP Inspector](https://glama.ai/mcp/inspector)
2. Click **"Add Server"**
3. URL: `https://d2fo97f8kuq5a7.cloudfront.net`
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

## Architecture (AWS)

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