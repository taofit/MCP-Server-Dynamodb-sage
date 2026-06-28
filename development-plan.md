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

7. Kafka event pipeline and async notifications — durable heavy ops, job result polling, and desktop alerts:

   **Kafka flow**

   ![Kafka flow](assets/kafka-flow.svg)

   **Readable flow**

   1. A client sends a tool request to the Go MCP server.
   2. The server runs synchronous guardrail and risk checks.
   3. If the request fails, the server returns an immediate synchronous error.
   4. If the request passes, the server evaluates `srv.isLargeOperation(req)`.
   5. **Large operation** (batch_put_items, batch_delete_items, create_optimized_table): the server generates a UUID, stores a `JobResult` in `srv.jobStorage`, and enqueues the job to Kafka topic `dynamodb-sage-heavy-ops`. It immediately returns a queued acknowledgment with the job ID.
   6. **Small operation**: the server executes the DynamoDB call synchronously and returns the result directly.
   7. The Kafka consumer (multi‑topic, single consumer group `dynamodb-sage-workers`) picks up the heavy‑op job via `ConsumeClaim`, dispatches to `processHeavyOp`, which calls `executeJobOp` to perform the actual DynamoDB operation.
   8. On completion, `processHeavyOp` publishes a notification to the `dynamodb-sage-notifications` topic with the table name, operation, severity (`"success"` or `"error"`), and a message.
   9. The same consumer group also subscribes to `dynamodb-sage-notifications`. Notifications are handled by `processNotification`, which calls `SendUINotify` (currently a macOS notification via `osascript`).
   10. The client polls `get_job_result` with the job ID to retrieve the final result from `srv.jobStorage`.
   11. If Kafka is unavailable (`initKafkaClient` fails), the server falls back to an in‑process goroutine pool (`processHeavyOpForQueue`) which skips notifications entirely.

   **How Kafka benefits the MCP server**

   The MCP server is the entry point that LLM-driven tools call to work with DynamoDB. Most operations are fast, such as list tables or get item, but heavy-weight tasks like batch writes, batch deletes, or table creation can run for seconds or minutes. Kafka moves those heavy operations out of the request path and into a durable event pipeline.

   - `dynamodb-sage-heavy-ops` is the task ingress topic (3 partitions, auto‑created).
   - `dynamodb-sage-notifications` is the egress topic for operation results (auto‑created).
   - Kafka consumer group replicas can scale worker concurrency horizontally.
   - Failed workers do not lose jobs because Kafka retains messages for replay.
   - Consumer lag and partition offsets provide operational visibility into backlog and saturation.
   - The MCP API remains fast because the client receives an immediate queued acknowledgment instead of waiting for long-running work.

   **Notification behavior**

   After a heavy op completes (success or error), a notification is published to `dynamodb-sage-notifications`. The same consumer group processes it and triggers a desktop alert. This confirms completion even if the LLM client has been restarted.

   Example macOS alert:

   ```text
   ✅ Job on table "Users" for operation "batch_put_items" has been completed successfully
   ```

   ❌ On failure:

   ```text
   ❌ batch_put_items failed: ...
   ```

   **Planned / not yet implemented**

   - **Mutation audit stream** (`dynamodb-sage-mutations`) — a dedicated topic for all DynamoDB write events, consumed by an audit sink that enriches and indexes mutation history for compliance and replay.
   - **PII / security violation detection** — an analytics consumer that inspects mutation payloads for raw PII or unencrypted secrets and emits live alerts via `notifications/message`.
   - **AI agent reaction** — surfacing security alerts to the LLM agent's context window so it can autonomously propose remediation (e.g., `delete_item` to wipe exposed records).
   - **Multi-channel notifications** — extend `SendNotification` beyond macOS to Slack, email, or webhook sinks (configurable per severity).

8. Multi-tenant isolation — per-team API keys, separate audit logs, table namespacing:
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

- [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — MCP server SDK for Go
- `aws-sdk-go-v2` — DynamoDB client
- `github.com/IBM/sarama` — Kafka producer/client for async job events, audit analytics, and proactive notifications
- Config file (YAML) to define protected tables, cost thresholds, and environment labels

**Project structure:**

```
dynamo-sage/
├── main.go
│
├── server/                        # MCP server layer
│   ├── server.go                  # server construction, HTTP/stdin handlers, shutdown
│   ├── handlers.go                # DynamoDB MCP tool handlers
│   ├── tools.go                   # MCP tool registration and schemas
│   └── types.go                   # MCP tool input argument structs
│
├── config.yaml                    # runtime guardrails and table policy configuration
│
├── internal/
│   ├── engine/                    # config loading and guardrails
│   │   ├── config.go              # load YAML config
│   │   └── guardrails.go          # protected tables, schema validation, limits
│   │
│   ├── risk/                      # risk analyzer and schema advisor
│   │   ├── analyzer.go            # main risk analysis logic
│   │   ├── advisor.go             # schema/risk recommendations
│   │   ├── mock.go                # test doubles for DynamoDB risk checks
│   │   └── analyzer_test.go       # risk analyzer unit tests
│   │
│   ├── audit/                     # SQLite audit log
│   │   └── audit.go               # audit entry model, SQLite setup, queries
│   │
│   ├── queue/                     # in-process worker pool for current async batch jobs
│   │   └── queue.go               # worker pool, job channel, retry handling
│   │
│   ├── kafka/                     # durable async job/event backbone
│   │   └── producer.go            # Sarama producer for heavy-op events
│   │
│   ├── dynamo/                    # planned DynamoDB client wrapper
│   ├── cost/                      # planned RCU/WCU cost estimation
│   ├── auth/                      # planned multi-tenant API keys and tenant context
│   ├── dashboard/                 # planned admin dashboard
│   └── rest/                      # planned REST API wrapper for non-MCP clients
│
├── docker-compose.yml             # local self-hosting
├── scripts/
│   ├── deploy.sh                  # Lightsail deploy helper
│   └── setup-lightsail.sh         # Lightsail instance setup
├── terraform/
│   └── lightsail/                 # Lightsail infrastructure reference
├── testing/
│   ├── integration_test.go        # real AWS integration tests
│   └── mocks.go                   # unit test mocks
│
└── README.md                      # project overview, setup, deployment, MCP client examples
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

