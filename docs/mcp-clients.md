# Connecting MCP Clients

> **Public demo server** available at `https://dynamodb-sage.hzcentre.com` — try it directly with any MCP client by replacing the URL with yours in the JSON config below.

> ⚠️ **Important:** The risk analyzer returns warnings for expensive or destructive operations. Some MCP clients may auto-confirm these without asking. Tell the LLM explicitly: *"If the server returns a risk warning, show it to me and ask for my confirmation before proceeding."*

## Authentication

MCP clients have no browser, so the demo button and `?key=` login links don't apply — the token is sent as a static `Authorization` header on every request. Use `GUEST_KEY` for read-only access or `ADMIN_KEY` for full access from your `.env`.

## opencode

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

## Claude Desktop

**Remote (Streamable HTTP):**

Replace `<YOUR_KEY>` with your `GUEST_KEY` (read-only) or `ADMIN_KEY` (full access) from `.env`.

```json
{
  "mcpServers": {
    "dynamodb-sage": {
      "command": "npx",
      "args": [
        "-y", "supergateway",
        "--streamableHttp", "https://dynamodb-sage.yourdomain.com",
        "--streamableHttpPath", "/",
        "--streamableHttpHeaders", "Authorization: Bearer <YOUR_KEY>"
      ]
    }
  }
}
```

**Local (stdio — requires Docker stack running):**

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

## Dashboard

Open `https://dynamodb-sage.yourdomain.com/` in a browser. A Next.js SPA embedded directly in the Go binary — no separate deployment.

For local development, visit `http://localhost:8080/` (Docker) or `http://localhost:8081/` (Go binary on host).

| Tab | Description |
|-----|-------------|
| Chat | **Default landing page.** AI-powered natural language interface with streaming responses, markdown tables, JSON rendering, copy button on messages, and suggested prompts |
| Overview | Summary dashboard with stats, quick actions, recent activity feed, and system health |
| Activity | Grouped audit feed with success rate %, time filters (Today/This Week/All), and search |
| Monitoring | Prometheus metrics dashboard with trend charts, metric cards, and color-coded health status |
| Tools | Interactive DynamoDB tool playground (hidden by default, accessible via `?tools=true`) |

**Built-in features:** Dark/light mode toggle, Sonner toast notifications, loading skeletons, responsive mobile layout
