Here are some name suggestions that better capture the project's purpose:

**Safety-focused names:**
- `dynamo-guardian` — emphasizes protection and oversight
- `dynamo-sentinel` — implies monitoring and alerting
- `dynamo-shield` — conveys protection from harmful ops
- `dynamo-sage` — wise advisor that warns before acting
- `dynamo-gate` — a gate that controls what passes through

**AI/MCP-focused names:**
- `dynamo-copilot` — AI assistant that guides DynamoDB ops
- `dynamo-pilot` — you're in control, it navigates safely
- `dynamo-counsel` — gives advice before executing

My top pick: `dynamo-sage` — it's short, memorable, and captures both the AI advisory role and the safety-first philosophy.

---

## How to approach this project

The core idea is an MCP server that wraps DynamoDB operations with a cost/risk analysis layer before execution.

**Architecture:**

```
MCP Client (Claude/Cursor/etc)
        ↓
  dynamo-sage (MCP Server)
        ↓
  [Risk Analyzer] ← cost estimator + harm detector
        ↓ (if safe or user confirms)
  AWS DynamoDB
```

**Key components to build:**

1. MCP Server layer — expose DynamoDB operations as MCP tools (query, put, delete, scan, etc.)

2. Risk Analyzer — intercept every operation and evaluate:
   - Table size check before scan (describe-table → item count × avg item size)
   - Production table detection (by name pattern like `prod-*`, tags, or config)
   - Bulk delete/update detection (BatchWriteItem with large payloads)
   - Expensive filter expressions on large tables

3. Cost Estimator — rough WCU/RCU calculation before execution:
   - Scan: full table read = table size / 4KB RCUs
   - Query: estimated based on index + filter selectivity
   - Write ops: item size / 1KB WCUs

4. Guardrails — define rules to prevent dangerous operations:
   - `config/environments.go` — dev/staging/prod environment detection
   - `config/policies.go` — fine-grained permission policies per table
   - `config/budgets.go` — daily/monthly cost budgets with alerts
   - `config/approval_flows.go` — multi-step approval for high-risk operations

5. Confirmation flow — return a warning tool response asking user to confirm before proceeding with risky ops

6. Audit Log — SQLite-backed audit log exposed as an MCP tool (done):
   - `internal/audit/` — logger, entry model, SQLite queries
   - MCP tool `read_audit_logs` with time range + limit filters

7. Multi-tenant isolation — per-team API keys, separate audit logs, table namespacing:
   - `internal/auth/apikeys.go` — API key generation and validation
   - `internal/auth/tenant.go` — tenant context extraction from request
   - `config/provisioning.go` — per-tenant guardrails and table prefix

8. Admin dashboard — simple web UI for configuration:
   - View audit logs with filters
   - Manage API keys per tenant
   - Configure guardrails (throttles, budgets, allowlists)
   - Usage stats (calls, capacity, costs per tenant)

9. Docker Compose one-liner — `docker compose up` for self-hosting:
   - `docker-compose.yml` with server + optional dashboard
   - Health checks, volume mounts for persistence

10. Usage-based billing hooks — track per-tenant consumption for monetization:
    - `internal/billing/meter.go` — count tool calls, RCU/WCU, tokens per tenant
    - Stripe or AWS Marketplace metering API integration
    - Exportable usage reports for invoice generation

11. REST API wrapper — expose MCP tools as REST endpoints for non-MCP clients:
    - `POST /tools/{toolName}` — call a tool programmatically
    - `GET /health` — already done
    - `GET /tools` — list available tools

11. Testing:
    - `testing/integration_test.go` — real AWS integration tests
    - `testing/mocks.go` — unit test mocks

12. Schema Advisor — analyze table schemas and recommend improvements:
    - Evaluate partition key cardinality (detect hot keys / low-cardinality PKs)
    - Suggest sort keys for common access patterns (e.g., time-based queries)
    - Recommend GSIs/LSIs based on observed or described query patterns
    - Detect missing/bad attribute types (e.g., storing numbers as strings)
    - Warn on over-provisioned GSIs or unused indexes
    - Exposed as an MCP tool `suggest_table_schema` (describe-table → analysis → recommendations)
    - Fits the "sage/advisor" theme — not just preventing bad ops, but proactively giving design advice

**Tech stack recommendation (Go, since your workspace is Go):**

- [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) — MCP server SDK for Go
- `aws-sdk-go-v2` — DynamoDB client
- Config file (YAML) to define protected tables, cost thresholds, and environment labels

**Project structure:**

```
dynamo-sage/
├── main.go
│
├── server/                        # (1) MCP server layer
│   ├── mcp.go                     # MCP tool registration
│   └── handlers.go                # tool handler dispatch
│
├── config/                        # (4) Guardrails + configuration
│   ├── config.go                  # load YAML config
│   ├── environments.go            # dev/staging/prod detection
│   ├── policies.go                # per-table permission policies
│   ├── budgets.go                 # daily/monthly cost budgets
│   └── approval_flows.go          # multi-step approval for high-risk ops
│
├── internal/
│   ├── dynamo/                    # DynamoDB client
│   │   ├── client.go              # AWS DynamoDB client wrapper
│   │   └── describe.go            # table metadata fetching
│   │
│   ├── risk/                      # (2) Risk analyzer
│   │   ├── analyzer.go            # main risk analysis logic
│   │   ├── harm.go                # destructive op detection
│   │   └── confirm.go             # (5) confirmation flow — warning + user prompt
│   │
│   ├── cost/                      # (3) Cost estimation
│   │   └── estimator.go           # RCU/WCU cost estimation
│   │
│   ├── audit/                     # (6) Audit log (done)
│   │   ├── logger.go              # AuditLogger struct, writes after each handler
│   │   ├── entry.go               # AuditEntry struct
│   │   └── db.go                  # SQLite setup, insert and query helpers
│   │
│   ├── auth/                      # (7) Multi-tenant isolation
│   │   ├── apikeys.go             # API key generation and validation
│   │   └── tenant.go              # tenant context extraction from request
│   │
│   ├── dashboard/                 # (8) Admin dashboard
│   │   ├── handler.go             # HTTP handlers for dashboard routes
│   │   └── templates/             # HTML templates
│   │
│   └── rest/                      # (10) REST API wrapper
│       ├── handler.go             # HTTP handlers for REST endpoints
│       └── router.go              # route registration
│
├── docker-compose.yml             # (9) One-liner self-hosting
├── testing/
│   ├── integration_test.go        # real AWS integration tests
│   └── mocks.go                   # unit test mocks
│
└── README.md                      # Project overview, problem, solution, demo, setup
```

**The key insight for the risk flow:**

Every MCP tool handler should follow this pattern:
1. Parse the incoming operation
2. Run risk analysis (async describe-table if needed)
3. If risk score exceeds threshold → return a warning response with estimated cost/impact, ask for explicit confirmation
4. If confirmed (or low risk) → execute and return result

Want me to scaffold the initial project structure with the MCP server setup and risk analyzer skeleton?

---

do you think this project will impress the potential employer

---


Honestly, it depends on the employer, but here's a realistic assessment:

**What makes it stand out:**
- MCP is very new (2024) — most candidates won't have hands-on experience with it
- The safety/cost-awareness angle shows product thinking, not just coding ability
- DynamoDB expertise is genuinely valued at AWS-heavy companies
- Go + AWS SDK v2 is a solid, in-demand stack

**What could make it forgettable:**
- If it's just a thin wrapper around AWS SDK with some `if` checks, it won't impress senior engineers
- Many "AI gateway" projects exist — the differentiation needs to be obvious in the README

**What would make it truly impressive:**

- Real cost modeling — not just "this is a scan", but "this scan will cost ~$0.23 based on table size 4.2GB at $0.25/RCU per million"
- Smart production detection — use AWS resource tags, not just name patterns
- Dry-run mode — show exactly what DynamoDB would do without executing
- Audit log — every operation attempted, risk score, whether user confirmed
- A short demo video or GIF in the README showing it warning before a dangerous op

**Bottom line:**

It's a good portfolio project if you execute it well and document it clearly. The concept alone won't impress anyone — the implementation depth and the README storytelling will. Employers want to see that you thought about real-world concerns (cost, safety, production risk), not just that you wired up an MCP server.

Want to start building it? I'd suggest starting with the risk analyzer core logic first, since that's the most differentiated part.

