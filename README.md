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

```bash
go run main.go
```

The server starts on port `8080`. Make sure LocalStack is running first.

### Verify DynamoDB Table

```bash
aws dynamodb scan --table-name Users --endpoint-url http://localhost:4566
```

### Test MCP Server Locally

This project uses **SSE transport**. Test it with the MCP Inspector:

```bash
npx @modelcontextprotocol/inspector http://localhost:8080/sse
```

1. It will open a browser window with url like: `http://localhost:6274/?MCP_PROXY_AUTH_TOKEN=5fc4c8b88aba46c528c17866c4b263f78d48aed1ee329c62ddf172ddbf519890#tools`.
2. Click **"List Tools"** to verify tools are registered.
3. Click **"Call Tool"** for `list_tables` to see results from LocalStack.

---

## AWS Deployment

### First-Time Infrastructure Setup

Deploy VPC, ECS, ALB, ECR, CloudFront, IAM roles, etc.:

```bash
cd terraform
terraform init
terraform apply
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

### CloudFront (HTTPS)

After initial deployment, add CloudFront in front of the ALB for HTTPS support:

```bash
cd terraform
terraform apply
```

This creates a CloudFront distribution with a free `*.cloudfront.net` SSL certificate.
Get the domain:

```bash
terraform output cloudfront_domain
# → d3xxxxxxxxxxxx.cloudfront.net
```

Update `opencode.json` and any client configs to use `https://d2fo97f8kuq5a7.cloudfront.net/sse`.

### Verify Health

```bash
curl https://d2fo97f8kuq5a7.cloudfront.net/health
# → ok
```

### Test MCP Server on AWS

```bash
npx @modelcontextprotocol/inspector https://d2fo97f8kuq5a7.cloudfront.net/sse
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
      "command": ["./dynamo-sage"],
      "enabled": true,
      "env": {
        "DYNAMO_SAGE_CONFIG": "config.yaml",
        "DYNAMO_SAGE_DB": "data/audit.db",
        "DYNAMO_SAGE_ADDR": ":8080"
      }
    },
    "dynamo-sage-aws": {
      "type": "sse",
      "url": "https://d2fo97f8kuq5a7.cloudfront.net/sse",
      "enabled": true
    }
  }
}
```

### Claude Desktop (via supergateway)

**Local (stdin/stdout bridge to local SSE):**
```json
{
  "mcpServers": {
    "dynamodb-sage-local": {
      "command": "npx",
      "args": ["-y", "supergateway", "--sse", "http://localhost:8080/sse"]
    }
  }
}
```

**Remote AWS (HTTPS via CloudFront):**
```json
{
  "mcpServers": {
    "dynamodb-sage-aws": {
      "command": "npx",
      "args": ["-y", "supergateway", "--sse", "https://d2fo97f8kuq5a7.cloudfront.net/sse"]
    }
  }
}
```

### Any MCP Client

Use SSE transport with the URL:
```
https://d2fo97f8kuq5a7.cloudfront.net/sse
```

### Test from a Browser with AI Chat (No Install)

[MCP Playground](https://mcpsplayground.com) is a web-based MCP client that works entirely in the browser — no downloads, no local setup.

1. Open [https://mcpsplayground.com](https://mcpsplayground.com)
2. Click **"Add Server"** and choose **"Remote"**
3. Enter the SSE URL:
   ```
   https://d2fo97f8kuq5a7.cloudfront.net/sse
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
3. URL: `https://d2fo97f8kuq5a7.cloudfront.net/sse`
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
| **Transport** | SSE, health check at `/health` |
| **HTTPS** | CloudFront (`*.cloudfront.net`) with auto-provisioned SSL |
| **IAM** | DynamoDB full access + `sts:GetCallerIdentity` |
| **Logs** | CloudWatch `/ecs/dynamodb-sage` (30-day retention) |
| **Image** | `335360747704.dkr.ecr.eu-north-1.amazonaws.com/dynamodb-sage:latest` |
