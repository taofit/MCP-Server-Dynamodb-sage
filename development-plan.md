# DynamoDB-Sage Development Plan

## Architecture

```
MCP Client (Claude/Cursor/etc)
        ↓
  dynamo-sage (MCP Server)
        ↓
  [Risk Analyzer] ← cost estimator + harm detector
        ↓ (if safe or user confirms)
  AWS DynamoDB
```

## Key components

1. **MCP Server layer** — expose DynamoDB operations as MCP tools (query, put, delete, scan, etc.)

2. **Risk Analyzer** — intercept every operation and evaluate:
   - Table size check before scan (describe-table → item count × avg item size)
   - Production table detection (by name pattern like `prod-*`, tags, or config)
   - Bulk delete/update detection (BatchWriteItem with large payloads)
   - Expensive filter expressions on large tables

3. **Cost Estimator** — rough WCU/RCU calculation before execution:
   - Scan: full table read = table size / 4KB RCUs
   - Query: estimated based on index + filter selectivity
   - Write ops: item size / 1KB WCUs

4. **Guardrails** — define rules to prevent dangerous operations:
   - `config/environments.go` — dev/staging/prod environment detection
   - `config/policies.go` — fine-grained permission policies per table
   - `config/budgets.go` — daily/monthly cost budgets with alerts
   - `config/approval_flows.go` — multi-step approval for high-risk operations

5. **Confirmation flow** — return a warning tool response asking user to confirm before proceeding with risky ops

6. **Audit Log** — SQLite-backed audit log exposed as an MCP tool (done):
   - `internal/audit/` — logger, entry model, SQLite queries
   - MCP tool `read_audit_logs` with time range + limit filters

7. **Kafka event pipeline and async notifications** — durable heavy ops, job result polling, and alerts.

   **Readable flow**

   1. A client sends a tool request to the Go MCP server.
   2. The server runs synchronous guardrail and risk checks.
   3. If the request fails, the server returns an immediate synchronous error.
   4. If the request passes, the server evaluates `srv.isLargeOperation(req)`.
   5. **Large operation** (batch_put_items, batch_delete_items, create_optimized_table): the server generates a UUID, stores a `JobResult` in `srv.jobStorage`, and enqueues the job to Kafka topic `dynamodb-sage-heavy-ops`. It immediately returns a queued acknowledgment with the job ID.
   6. **Small operation**: the server executes the DynamoDB call synchronously and returns the result directly.
   7. The Kafka consumer (single consumer group `dynamodb-sage`) picks up the heavy‑op job via `ConsumeClaim`, dispatches to `processHeavyOp`, which calls `executeJobOp` to perform the actual DynamoDB operation.
   8. On completion, `processHeavyOp` publishes a notification to the `dynamodb-sage-notifications` topic with the table name, operation, severity (`"success"` or `"error"`), and a message.
   9. The same consumer group subscribes to `dynamodb-sage-notifications`. `processNotification` handles them: MCP session log, macOS alert, SSE push to the dashboard (toast + activity feed), and SQLite persistence.
   10. The client polls `get_job_result` with the job ID to retrieve the final result from `srv.jobStorage`.
   11. If Kafka is not configured (`initKafkaClient` fails → `kafkaClient` is nil), the server falls back to an in‑process goroutine pool (`processHeavyOpForQueue`), which records notifications via SSE + SQLite but skips Kafka entirely.

   **Topics**

   - `dynamodb-sage-heavy-ops` — task ingress (3 partitions, auto‑created).
   - `dynamodb-sage-notifications` — egress for operation results (auto‑created).

   **Notification behavior**

   After a heavy op completes (success or error), a notification is published to `dynamodb-sage-notifications`. The same consumer group processes it: MCP session log, macOS desktop alert, SSE toast + activity feed, and SQLite persistence. The LLM client also gets the result via `get_job_result` polling.

   **Planned / not yet implemented**

   - **Mutation audit stream** (`dynamodb-sage-mutations`) — a dedicated topic for all DynamoDB write events, consumed by an audit sink that enriches and indexes mutation history for compliance and replay.
   - **PII / security violation detection** — an analytics consumer that inspects mutation payloads for raw PII or unencrypted secrets and emits live alerts.
   - **AI agent reaction** — surfacing security alerts to the LLM agent's context window so it can autonomously propose remediation (e.g., `delete_item` to wipe exposed records).
   - **Multi-channel notifications** — extend `SendNotification` beyond macOS to UI, Slack, email, or webhook sinks (configurable per severity). See `server/notification.go:24` TODO.
   - [x] **Dead Letter Queue (DLQ) & retry policy** — prevent poison pill messages from clogging the consumer pipeline. Implement exponential backoff with jitter for transient errors (DynamoDB throttling), route permanently failed messages to `dynamodb-sage-dlq` topic after max retries, and ensure the consumer group commits offsets and continues processing. Add metrics (`kafka_dlq_messages_total`) and audit logging for observability.

   **Reliability & correctness — follow-ups from Kafka article review**

   Issues called out in the published article's review comment, mapped to this codebase. The review was written against an async worker-pool sample; this repo's consumer runs handlers synchronously and marks after processing, so some points apply as-is, others need adaptation.

   - [x] **Idempotent batch mutations.** `batch_put_items` / `batch_delete_items` run with no idempotency key. Under at-least-once delivery, any redelivery (crash mid-apply, rebalance, ambiguous send timeout) duplicates writes. Fix: attach a per-job idempotency key to the mutation and enforce it with a DynamoDB conditional write (or a processed-ops ledger table) before applying; verify the outcome before marking. (Done: `processed_ops_by_job` ledger — atomic INSERT-based claim before apply, result completion afterwards, periodic + startup sweep of stale claims.)
   - [ ] **Mark offsets only after durable apply; don't drop permanent failures.** `consumer.go` calls `MarkMessage` even when the handler errors, so permanently-failed work is acknowledged and lost — no retry, no DLQ. Fix: on handler error, either leave the message unmarked (redelivery — only safe with idempotency above) or produce it to a new `dynamodb-sage-dlq` topic with a watcher/alert. (Done via the DLQ route: handler errors retry with exponential backoff, then publish to `dynamodb-sage-dlq` with audit logging + metrics; the offset is only marked after durable apply or durable DLQ routing.)
   - [ ] **Session timeout vs handler duration.** `Consumer.Group.Session.Timeout = 30s`; a large batch can exceed it, so sarama can rebalance and redeliver while the old consumer is still applying → duplicate execution. Fix: check `session.Context().Done()` inside handlers and abort early; right-size the session timeout for the processing window.
   - [x] **Unify the two execution paths behind one durable operation record.** Kafka consumer (`processHeavyOp`) and native-queue fallback (`processHeavyOpForQueue`) are independent executors of the same logical operation. A durable record keyed by job ID with status `queued → applied | failed`, claimed via conditional write, prevents double/zero execution across paths; a reconciliation pass re-drives stuck `queued` records. Note: send failures already fail closed (`tools.go:456-460`), so the immediate risk is limited to ambiguous publish timeouts and the user-visible contradiction (user told "failed", message still lands). (Done: both executors share the same claim/complete/sweep path over `processed_ops_by_job`, keyed by job ID; stale claims are swept rather than re-driven.)
   - [ ] **Durable status visible to the user, not just "accepted".** Surface `queued/applied/failed` in the UI; include the job ID in failure notifications/toasts (currently `SSEProvider` omits `jobId`) so the user can `get_job_result` the actual error; consider auto-pushing the result into chat when `jr.Done` closes instead of requiring a manual poll.
   - [ ] **Partition-aware draining on rebalance.** If processing ever moves to async workers, drain/cancel in-flight work via `Cleanup`/session context so an old owner can't finish after a new owner takes the partition. Not applicable while processing stays synchronous in `ConsumeClaim`.
   - [x] **Failure metrics on the dashboard.** Added `sage_kafka_processed_total{topic,status}` + `Kafka Failures` card on Monitoring (send + process errors). Rebuild static export after further changes.

8. **Web Dashboard** — see **Section 16: Dashboard Frontend Refactor** for the full plan. Summary:
   - Served directly from the Go binary at `/` (no separate deployment)
   - React SPA with Tailwind + shadcn/ui, embedded via `//go:embed`
   - 5 tabs: Chat (main), Overview, Activity, Monitoring, Tools (hidden)
   - SSE streaming chat, grouped activity feed, beautiful metrics charts

9. **Docker Compose one-liner** — `docker compose up` for self-hosting:
   - `docker-compose.yml` with server + optional dashboard
   - Health checks, volume mounts for persistence

10. **Usage-based billing hooks** — track per-tenant consumption for monetization:
    - `internal/billing/meter.go` — count tool calls, RCU/WCU, tokens per tenant
    - Stripe or AWS Marketplace metering API integration
    - Exportable usage reports for invoice generation

11. **REST API wrapper** — expose MCP tools as REST endpoints for non-MCP clients:
    - `POST /tools/{toolName}` — call a tool programmatically
    - `GET /health` — already done
    - `GET /tools` — list available tools

12. **Testing:**
    - `testing/integration_test.go` — real AWS integration tests
    - `testing/mocks.go` — unit test mocks

13. **Schema Advisor** — analyze table schemas and recommend improvements:
    - Evaluate partition key cardinality (detect hot keys / low-cardinality PKs)
    - Suggest sort keys for common access patterns (e.g., time-based queries)
    - Recommend GSIs/LSIs based on observed or described query patterns
    - Detect missing/bad attribute types (e.g., storing numbers as strings)
    - Warn on over-provisioned GSIs or unused indexes
    - Exposed as an MCP tool `suggest_table_schema` (describe-table → analysis → recommendations)

---

## 14. MCP Server-to-Client Notifications — Real-Time Push to MCP Clients

MCP notifications dispatch via `session.Log()` to all connected sessions. Web dashboard clients receive SSE events at `/api/events` and toast popups.

---

## 15. Dashboard Persistence & UX Improvements

- SQLite-backed `Store` for notifications and chat history (`server/store.go`)
- Toast popup notifications via SSE at `/api/events`
- API endpoint `GET /api/notifications` for persisted notification history
- Metrics dashboard rendering Prometheus data from `:2112/metrics`

---

## 16. Dashboard Frontend

The frontend has been rewritten from vanilla JS to **Next.js 16 (App Router) + React + TypeScript** with static export (`output: 'export'`). The built output is embedded in the Go binary via `//go:embed`.

| Layer | Technology |
|-------|------------|
| Framework | Next.js 16 (App Router) + TypeScript |
| Styling | Tailwind CSS 4 + shadcn/ui |
| Charts | Recharts |
| Markdown | react-markdown + remark-gfm |
| State | Zustand |
| Build | `EXPORT_STATIC=true npm run build` → `out/` → copied to `server/static/` |

### Tabs

| Tab | Purpose |
|-----|---------|
| **Chat** | Main NL interface — streaming SSE chat with Claude, markdown rendering, JSON-to-table conversion |
| **Overview** | Landing page with stats, quick actions, health indicators |
| **Activity** | Grouped audit feed — operations organized by table |
| **Monitoring** | Prometheus metrics with Recharts visualizations |
| **Tools** | Manual MCP tool playground (hidden, accessible via `?tools=true`) |

### Deployment

```
Next.js out/ → server/static/ → //go:embed static/* → Go binary
```

For local development, run `cd frontend && npm run dev` (port 3000) alongside the Go backend (port 8080/8081). Next.js proxies `/api/*` to the Go server.

### Build

```bash
cd frontend && EXPORT_STATIC=true npm run build
rm -rf ../server/static && mkdir -p ../server/static
cp -r out/* ../server/static/
```

Or use the full deploy script which handles this automatically: `./scripts/deploy.sh <domain>`
