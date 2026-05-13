# dynamodb-sage

A Go project using DynamoDB via LocalStack for local development.

## Prerequisites

- [Docker](https://www.docker.com/)
- [Go 1.25+](https://golang.org/)
- [LocalStack Pro account](https://app.localstack.cloud) (for auth token)

## Setup

1. Copy the env file and add your LocalStack auth token:

```bash
cp .env.example .env
```

Edit `.env`:
```
LOCALSTACK_AUTH_TOKEN=your_token_here
```

## Start LocalStack

```bash
docker compose up
```

This will:
- Start a LocalStack container on port `4566`
- Automatically create the `Users` table with a GSI on `Email`
- Insert test data (alice, bob, charlie)

To run in the background:
```bash
docker compose up -d
```

To stop:
```bash
docker compose down
```

## Run Go Code

```bash
go run main.go
```

Make sure LocalStack is running before executing Go code. The DynamoDB endpoint is:
```
http://localhost:4566
```

Configure your AWS SDK client to point to LocalStack:
```go
cfg, _ := config.LoadDefaultConfig(context.TODO(),
    config.WithRegion("eu-north-1"),
    config.WithEndpointResolverWithOptions(
        aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
            return aws.Endpoint{URL: "http://localhost:4566"}, nil
        }),
    ),
    config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
)
```

## Verify Table

Scan the `Users` table directly via CLI:
```bash
awslocal dynamodb scan --table-name Users
```

Or using the standard AWS CLI:
```bash
aws dynamodb scan --table-name Users --endpoint-url http://localhost:4566
```
## check localstack
```
curl http://localhost:4566/_localstack/health
```

or
```
http://localhost:4566/_localstack/health

## Testing the MCP Server

This project uses the **Model Context Protocol (MCP)** with an SSE transport. You can test it using the official MCP Inspector.

### 1. Run the MCP Server
In one terminal, start the Go server:
```bash
go run main.go
```
The server will start listening on port `3001` (or the port configured in `main.go`).

### 2. Run the MCP Inspector
In another terminal, run the inspector pointing to your server's SSE endpoint:
```bash
npx @modelcontextprotocol/inspector http://localhost:3001/sse
```

### 3. Using the Inspector
1. Open the URL provided by the inspector in your terminal (e.g., `http://localhost:6274/...`) in your browser.
2. Click **"List Tools"** to verify that `list_tables` is registered.
3. Click **"Call Tool"** for `list_tables` to see the results from your LocalStack DynamoDB.

## Configuration for AI Clients (Claude Desktop)

To use this server with Claude Desktop, you need a bridge because Claude primarily supports stdio. Use `supergateway` to connect to your running Go server:

```json
{
  "mcpServers": {
    "dynamodb-sage": {
      "command": "npx",
      "args": [
        "-y",
        "supergateway",
        "--sse",
        "http://localhost:3001/sse"
      ]
    }
  }
}
```

> **Note**: Your Go server (`go run main.go`) must be running for Claude to connect.