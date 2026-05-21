# dynamodb-sage

An MCP (Model Context Protocol) server that acts as a DynamoDB gateway — enabling LLMs to perform safe, guarded operations on DynamoDB tables through natural language.

[![Demo Video](https://img.youtube.com/vi/zt_6hMwcw2c/maxresdefault.jpg)](https://www.youtube.com/watch?v=zt_6hMwcw2c)

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
npx @modelcontextprotocol/inspector http://localhost:3001/sse or http://localhost:8080(show in main.go)
```

### 3. Using the Inspector
1. Wait for the response from the above command for message like 
	```
	⚙️ Proxy server listening on localhost:6277
	...
	🚀 MCP Inspector is up and running at:
	   http://localhost:6274/?MCP_PROXY_AUTH_TOKEN=... 
	```

2. Open the URL provided by the inspector in your terminal (e.g., `http://localhost:6274/...`) in your browser.
2. Click **"List Tools"** to verify that `list_tables` is registered.
3. Click **"Call Tool"** for `list_tables` to see the results from your LocalStack DynamoDB.

## Development Workflow

This project follows the **GitHub Flow**:

1.  **Work on a Feature Branch**: Always create a new branch for features or fixes:
    ```bash
    git checkout -b feature/your-feature-name
    ```
2.  **Commit Locally**: Make your changes and commit them with descriptive messages:
    ```bash
    git add .
    git commit -m "Add [feature description]"
    ```
3.  **Push to GitHub**: Push your branch to the remote repository:
    ```bash
    git push origin feature/your-feature-name
    ```
4.  **Create a Pull Request (PR)**: Go to GitHub and open a PR to merge your feature branch into `main`.
5.  **Merge**: Once the code is verified, merge the PR on GitHub.
6.  **Sync Local Main**: After merging, pull the latest changes back to your local `main`:
    ```bash
    git checkout main
    git pull origin main
    ```

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
        "http://localhost:3001/sse" # or http://localhost:8080/sse
      ]
    }
  }
}
```

## Production
1. You set the `AUDIT_DB_PATH` environment variable in your (Terraform/Cloudformation) to /mnt/data/audit.db. Your Go code automatically switches to the high-security storage volume without you changing a single line of code.
example terraform code for mount points:
```mountPoints = [
      {
        sourceVolume  = "audit-storage"
        containerPath = "/mnt/data" # This is where Go looks!
        readOnly      = false
      }
    ]
```
2. 

> **Note**: Your Go server (`go run main.go`) must be running for Claude to connect.