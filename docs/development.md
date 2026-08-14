# Development

## Quick Start

```bash
# Clone and run with Docker
git clone https://github.com/taofit/MCP-Server-Dynamodb-sage.git
cd dynamodb-sage
cp .env.example .env    # edit with your AWS keys
docker compose --profile local up -d --build
```

Then connect with any MCP client:

```bash
npx @modelcontextprotocol/inspector --transport http http://localhost:8080
```

## Local Development (Full Setup)

### Services

| Service      | Profile   | Default |
|--------------|-----------|---------|
| App (Go)     | —         | yes     |
| Zookeeper    | —         | yes     |
| Kafka        | —         | yes     |
| LocalStack   | `local`   | no      |

### Steps

1. **Configure environment:**

```bash
cp .env.example .env
# Edit .env and set your variables:
# LOCALSTACK_AUTH_TOKEN=your_token_here
```

2. **Start the stack:**

```bash
docker compose --profile local up -d --build
```

3. **Verify services:**

```bash
curl http://localhost:4566/_localstack/health   # LocalStack
nc -z localhost 9092 && echo "Kafka up"         # Kafka
curl http://localhost:8080/health               # Go app
```

4. **Stop everything:**

```bash
docker compose --profile local down -v
```

### Run Go binary locally (faster iteration)

Keep Kafka and LocalStack in Docker, run the Go binary directly:

```bash
KAFKA_BROKERS=localhost:9093 \
AWS_BASE_ENDPOINT=http://localhost:4566 \
AWS_REGION=eu-north-1 \
AWS_ACCESS_KEY_ID=your_key_id \
AWS_SECRET_ACCESS_KEY=your_secret_key \
MCP_TRANSPORT_MODE=http \
DYNAMO_SAGE_ADDR=:8081 \
go run .
```

> Kafka on `localhost:9093` (PLAINTEXT_HOST) and LocalStack on `localhost:4566` are the Docker host-mapped ports.

### Test with MCP Inspector

```bash
# Docker compose
npx @modelcontextprotocol/inspector --transport http http://localhost:8080
# Local binary
npx @modelcontextprotocol/inspector --transport http http://localhost:8081
```

> **Troubleshooting:** If Kafka exits with `KeeperErrorCode = NodeExists`, run `docker compose --profile local down && docker compose --profile local up -d` for a clean restart.

## Authentication

Auth is via bearer tokens compared against env vars (`GUEST_KEY`, `ADMIN_KEY`):

| Role | Access | Env var |
|------|--------|---------|
| Guest | Read-only tools (list, describe, query, get, scan, batch_get, search) | `GUEST_KEY` |
| Admin | Full access including writes and destructive ops | `ADMIN_KEY` |

The web dashboard also exposes a public `GET /api/demo` endpoint that mints a guest session for one-click demo access.

## Chat Function

The dashboard includes a built-in **AI chat assistant** powered by Claude. Describe what you want in natural language and it calls DynamoDB tools on your behalf.

**How it works:**

1. User sends a message via the chat UI
2. Message is streamed to Claude via `POST /api/chat` (SSE)
3. Claude calls tools (`list_tables`, `query_table`, etc.) and reasons over results
4. Responses stream back token-by-token to the UI

**Example prompts:**

- *"List all my DynamoDB tables"*
- *"Show me the schema of the users table"*
- *"Query the orders table where userId = 123"*
- *"How many items are in each table?"*

> LLM settings are configured via environment variables (see `.env`). At least one of `LLM_API_KEY` or a valid SSM parameter must be available for chat to work.

## Workflow

This project follows **GitHub Flow:**

1. Create a feature branch: `git checkout -b feature/your-feature`
2. Commit changes: `git commit -m "Add [feature]"`
3. Push: `git push origin feature/your-feature`
4. Open a PR on GitHub
5. Merge and sync local main

## Related Documents

- [Development plan](../development-plan.md) — full roadmap including planned features
- [Project flow](../project-flow.md) — detailed architecture walkthrough
- [RAG development plan](../rag-development-plan.md) — planned RAG extension
- [Kafka flow](../assets/kafka-flow.svg) — async job processing diagram
- [Architecture flow](../assets/architecture-flow.svg) — full system architecture
