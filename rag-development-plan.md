# RAG Development Plan

## Strategic Analysis: Why RAG Belongs Here

Your existing DynamoDB-Sage is an **MCP safeguard layer** — it intercepts LLM→DynamoDB calls to prevent disasters. RAG extends this to the **other direction**: LLM ← your data. The server already has:

| Existing asset | RAG leverage |
|---|---|
| Kafka pipeline + consumer group | Reuse for async document ingestion |
| Go worker pool fallback | Same pattern for embedding jobs |
| MCP tool registration (`addTools()`) | Register `search_knowledge_base` tool |
| Risk analyzer interceptor (`withRiskAnalysis`) | Same pattern for guardrailling RAG queries |
| Audit log (SQLite) | Log RAG queries + similarity scores |
| Config-driven architecture (`config.yaml`) | Add `rag:` section |
| Docker Compose stack | Add Qdrant/vector-db container |
| `internal/` package structure | Add `internal/rag/`, `internal/embedding/` |

The core insight: **RAG is not a separate application — it is a second category of MCP tools** on the same server, sharing the same lifecycle, config, and observability.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                    Go MCP Server                          │
│                                                          │
│  ┌───────────────────────┐   ┌───────────────────────┐   │
│  │   DynamoDB Tools      │   │    RAG Tools          │   │
│  │   (existing)          │   │   (new)               │   │
│  │   query_table         │   │   ingest_document      │   │
│  │   put_item            │   │   search_knowledge_base│   │
│  │   ...                 │   │   list_documents       │   │
│  └───────┬───────────────┘   └───────────┬───────────┘   │
│          │                               │               │
│          ▼                               ▼               │
│  ┌───────────────────────────────────────────────┐       │
│  │          Risk Analyzer (shared)                │       │
│  └───────┬───────────────────────────────────────┘       │
│          │                                               │
│          ▼                                               │
│  ┌───────────────────────────────────────────────┐       │
│  │  Kafka / Worker Pool (shared)                 │       │
│  └───────┬───────────────────────────────────────┘       │
│          │                                               │
└──────────┼───────────────────────────────────────────────┘
           │
     ┌─────┴─────┬──────────────┐
     ▼           ▼              ▼
  DynamoDB    Qdrant (VecDB)   Embedding API
```

---

## Phase 1: Foundation — Vector Database Integration

### 1.1 Add Qdrant to Docker Compose

Qdrant is the best fit here:
- Native Go SDK (`github.com/qdrant/go-client/qdrant`)
- Single binary, no JVM (vs OpenSearch)
- REST + gRPC
- Fits the $5 Lightsail instance

```yaml
# add to docker-compose.yml
qdrant:
  image: qdrant/qdrant:latest
  ports:
    - "6333:6333"   # REST
    - "6334:6334"   # gRPC
  volumes:
    - ./data/qdrant:/qdrant/storage
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:6333/healthz"]
    interval: 10s
    timeout: 3s
    retries: 5
```

### 1.2 New internal packages

```
internal/
  rag/
    chunker.go          # semantic + fixed-size chunking strategies
    pipeline.go         # ingestion pipeline orchestration
    retrieval.go        # search + rerank + threshold filtering
    types.go            # Document, Chunk, SearchResult structs
  embedding/
    client.go           # interface: Embedder
    openai.go           # OpenAI text-embedding-3-small adapter
    local.go            # (stretch) ONNX runtime local embedding
  vector/
    client.go           # interface: VectorDB
    qdrant.go           # Qdrant implementation
    mock.go             # test double
```

### 1.3 Embedder Interface

```go
// internal/embedding/client.go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
}
```

OpenAI adapter:

```go
// internal/embedding/openai.go
type OpenAIEmbedder struct {
    client  *openai.Client
    model   string  // "text-embedding-3-small"
    dims    int     // 1536
}
```

Design decision: `text-embedding-3-small` (1536 dims) over `text-embedding-3-large` (3072 dims) — half the storage, 1/5th the cost, within 2% accuracy on retrieval benchmarks. For a portfolio project, the pragmatic choice signals production experience better than maximalist over-engineering.

### 1.4 Vector DB Interface

```go
// internal/vector/client.go
type VectorDB interface {
    UpsertPoints(ctx context.Context, collection string, points []Point) error
    Search(ctx context.Context, collection string, vector []float32, limit int) ([]ScoredPoint, error)
    DeletePoints(ctx context.Context, collection string, ids ...uint64) error
    CreateCollection(ctx context.Context, collection string, dims int) error
}

type Point struct {
    ID      uint64
    Vector  []float32
    Payload map[string]string   // document_id, chunk_index, source, text
}

type ScoredPoint struct {
    ID      uint64
    Score   float64
    Payload map[string]string
}
```

### 1.5 Configuration

```yaml
# config.yaml — add rag section (alongside existing guardrails)
rag:
  embedding:
    provider: openai           # openai | local
    model: text-embedding-3-small
    dimensions: 1536
    api_key_env: OPENAI_API_KEY
  vector_db:
    provider: qdrant
    grpc_host: localhost
    grpc_port: 6334
    default_collection: dynamo-sage-docs
  chunking:
    strategy: semantic         # fixed | semantic
    max_tokens: 500
    overlap: 50
  retrieval:
    top_k: 20
    score_threshold: 0.75
    rerank: true
    final_k: 4
    rerank_model: cohere       # cohere | cross-encoder
```

---

## Phase 2: Ingestion Pipeline

### 2.1 Document Chunking

Two strategies, configurable:

**Fixed-size chunking** (simple, fast):
```go
func (c *Chunker) ChunkFixed(text string) []string {
    // Split by token count (~4 chars/token)
    // Overlap of `overlap` tokens between chunks
    // Returns []string
}
```

**Semantic chunking** (smarter, better retrieval):
```go
func (c *Chunker) ChunkSemantic(text string) []string {
    // 1. Split on sentence boundaries (regex or NLP splitter)
    // 2. Group sentences until token budget reached
    // 3. If a sentence bridges a topic shift (embedding cosine diff > threshold),
    //    start a new chunk even if under budget
    // 4. Overlap last sentence of previous chunk into next chunk
}
```

Semantic chunking matters because it prevents "Lost in the Middle" at the chunk level — each chunk is a coherent thought, not an arbitrary byte slice.

### 2.2 Ingestion MCP Tool

```go
// server/types.go
type IngestDocumentArgs struct {
    TableName string `json:"tableName"`     // DynamoDB table with source documents
    Key       map[string]any `json:"key"`   // primary key of the document item
    TextField string `json:"textField"`     // which attribute contains the text
    Source    string `json:"source"`        // friendly name like "ops-manual-v2"
}
```

Handler flow:
1. Read item from DynamoDB
2. Extract text from specified field
3. Publish to Kafka topic `rag.ingest` (async — same pattern as heavy ops)
4. Return job ID immediately

### 2.3 Kafka Consumer for Ingestion

New topic: `rag.ingest` (add to `config/kafka.yaml`)

```go
func (srv *Server) processIngestJob(key string, payload []byte) error {
    // 1. Parse payload → document ID, text, source
    // 2. Chunk text
    // 3. Embed each chunk (batch API call)
    // 4. Upsert points to Qdrant
    // 5. Record document metadata in internal SQLite tracking table
    // 6. Publish notification on completion
}
```

This reuses:
- `KafkaClient.Send()` → same producer
- `srv.notificationService.SendNotification()` → same desktop alerts
- `JobResult` / `sync.Map` → same polling pattern
- Consumer group `dynamodb-sage-workers` → same consumer

### 2.4 Ingestion via Direct DynamoDB Scan (Bulk Import)

```go
type BulkIndexTableArgs struct {
    TableName string `json:"tableName"`
    TextField string `json:"textField"`
    Filter    string `json:"filter,optional"`  // DynamoDB scan filter
}
```

Handler:
1. Scan the DynamoDB table (with pagination)
2. For each item, extract text field
3. Send to Kafka `rag.ingest` in batches
4. Return job ID

This allows an LLM agent to say: *"Index the entire `incident-reports` table so I can search it later."*

---

## Phase 3: Retrieval Pipeline

### 3.1 Search MCP Tool (The Core Differentiator)

```go
type SearchKnowledgeBaseArgs struct {
    Query       string `json:"query"`
    Collection  string `json:"collection,optional"`  // defaults to configured collection
    Limit       int    `json:"limit,optional"`       // top-k from vector search
    MinScore    float64 `json:"minScore,optional"`   // override threshold
    Rerank      bool   `json:"rerank,optional"`      // enable reranking step
}
```

Handler:

```go
func (srv *Server) searchKnowledgeBase(ctx context.Context, req *mcp.CallToolRequest, args *SearchKnowledgeBaseArgs) (*mcp.CallToolResult, any, error) {
    // 1. Embed query
    vector, err := srv.embedder.Embed(ctx, args.Query)

    // 2. Vector search → top 20
    results, err := srv.vectorDB.Search(ctx, collection, vector, topK)

    // 3. Filter by score threshold
    results = filterByThreshold(results, threshold)

    // 4. Rerank with cross-encoder (optional)
    if args.Rerank {
        results = srv.reranker.Rerank(ctx, args.Query, results, finalK)
    }

    // 5. Format context for LLM
    return formatContext(results), nil, nil
}
```

### 3.2 Reranking Step

The reranker is what separates junior from senior RAG implementations.

**Why:** Vector search finds semantically similar chunks. Reranking orders them by relevance to the *specific query*. A chunk about "DynamoDB error handling" might be vector-close to "database failover," but the reranker will boost the one about failover specifically.

```go
// internal/rag/reranker.go
type Reranker interface {
    Rerank(ctx context.Context, query string, candidates []ScoredPoint, topK int) ([]ScoredPoint, error)
}
```

Cohere implementation:
```go
func (c *CohereReranker) Rerank(ctx context.Context, query string, candidates []ScoredPoint, topK int) ([]ScoredPoint, error) {
    // POST https://api.cohere.com/v1/rerank
    // model: "rerank-english-v3.0"
    // Returns reordered results with new relevance scores
}
```

Local cross-encoder (stretch): Use a tiny ONNX model (~80MB) loaded in-process. Adds 150ms latency but zero API cost and works offline. Interview gold: *"I reduced reliance on external APIs by running a distilled cross-encoder locally, cutting p99 latency by 60% and eliminating a dependency."*

### 3.3 Score Thresholding

Without thresholds, vector DB returns garbage for out-of-domain queries.

```go
func filterByThreshold(results []ScoredPoint, threshold float64) []ScoredPoint {
    // Qdrant cosine similarity range: [0, 1]
    // 0.75 is the calibrated cutoff for text-embedding-3-small
    filtered := results[:0]
    for _, r := range results {
        if r.Score >= threshold {
            filtered = append(filtered, r)
        }
    }
    return filtered
}
```

When threshold yields 0 results:

> *"No verified domain documents matched this query. Please rephrase or ask about a topic covered in the indexed knowledge base."*

This prevents hallucination by giving the LLM an explicit "I don't know" signal instead of weakly-relevant noise.

### 3.4 Collection Management Tools

```go
type ListDocumentsArgs struct {
    Collection string `json:"collection,optional"`
}
// Returns: [{id, source, chunk_count, ingested_at}, ...]

type DeleteDocumentArgs struct {
    DocumentID string `json:"documentId"`
    Collection string `json:"collection,optional"`
}
// Deletes all chunks for that document from Qdrant + metadata store
```

---

## Phase 4: Data Synchronization (CDC)

### 4.1 The Problem

If a user updates/removes a document in DynamoDB, the vector index becomes stale. The LLM then answers based on outdated information — a subtle, hard-to-detect hallucination source.

### 4.2 DynamoDB Streams → Kafka → Re-embed

Architecture:

```
DynamoDB Table
    │
    ▼ [DynamoDB Streams (NEW_AND_OLD_IMAGES)]
    │
    ▼ Lambda / Poller
    │
    ▼
Kafka topic: rag.cdc
    │
    ▼ [Existing consumer group]
    │
Go Worker: re-embed updated text, upsert vector
           or delete points if row deleted
```

For the Lightsail deployment, avoid Lambda (adds complexity). Instead:

```go
// Background goroutine in server startup
func (srv *Server) startCDCPoller(ctx context.Context, tableARN string) {
    ticker := time.NewTicker(30 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            srv.pollDynamoDBStreams(ctx, tableARN)
        }
    }
}
```

This polls every 30 seconds using `DescribeStream` + `GetShardIterator` + `GetRecords`. For a $5 Lightsail instance, this is more practical than Lambda.

### 4.3 Kafka CDC Topic

```yaml
# config/kafka.yaml — add:
topics:
  heavy_ops: dynamodb-sage-heavy-ops
  notifications: dynamodb-sage-notifications
  rag_ingest: rag.ingest
  rag_cdc: rag.cdc
```

When an update event arrives:
1. Fetch the updated item from DynamoDB (or use the `NEW_IMAGE` from stream)
2. Re-chunk, re-embed, replace points in Qdrant
3. Log the sync in audit trail

---

## Phase 5: Observability & Interview Talking Points

### 5.1 RAG-Specific Metrics (Prometheus)

```go
// Add to internal/metrics/rag_metrics.go
sage_rag_ingest_documents_total{source, status}
sage_rag_ingest_duration_seconds{source}
sage_rag_chunks_per_document{source}
sage_rag_search_total{collection, had_results}
sage_rag_search_duration_seconds{collection}
sage_rag_rerank_duration_seconds
sage_rag_search_score_distribution   // histogram of raw similarity scores
sage_rag_cdc_events_total{table, event_type}
sage_rag_cdc_lag_seconds             // how stale the vector index is
```

Metric `sage_rag_search_score_distribution` is particularly interview-worthy: *"I track the similarity score distribution to monitor embedding drift. If the mean score drops below 0.7 over a week, it signals the indexed content has drifted from the query distribution and needs re-indexing."*

### 5.2 Audit Log Extension

```go
// Add to audit entry:
type AuditEntry struct {
    // existing fields...
    RAGQuery    string  `json:"rag_query,omitempty"`
    RAGScore    float64 `json:"rag_score,omitempty"`
    RAGSource   string  `json:"rag_source,omitempty"`
}
```

### 5.3 The "Advanced Mode" Tool Flag

Senior engineers don't just build features — they build escape hatches. The search tool should have an `advanced` parameter:

```go
type SearchKnowledgeBaseArgs struct {
    Query           string  `json:"query"`
    Advanced        bool    `json:"advanced,optional"`
    // Overrides — only used when advanced=true
    OverrideMinScore *float64 `json:"overrideMinScore,omitempty"`
    OverrideTopK     *int     `json:"overrideTopK,omitempty"`
}
```

When `advanced=true`, the LLM can override thresholds for edge cases like fuzzy matching or exploratory queries. This signals: *"I design systems that handle both happy path and break-glass scenarios."*

---

## Phase 6: Implementation Order

| Step | What | Why this order |
|------|------|----------------|
| 6.1 | Add Qdrant to docker-compose, add Go deps | Foundation — nothing works without this |
| 6.2 | Define `embedding.Embedder` + `vector.VectorDB` interfaces | Contracts before implementations |
| 6.3 | Implement OpenAI embedder + Qdrant client | Core primitives |
| 6.4 | Implement chunker (fixed strategy first) | Fastest path to end-to-end |
| 6.5 | Implement `ingest_document` MCP tool + Kafka consumer | First working RAG pipeline |
| 6.6 | Implement `search_knowledge_base` tool | Second working pipeline |
| 6.7 | Add score threshold + reranking | Polish retrieval quality |
| 6.8 | Add CDC poller | Address staleness |
| 6.9 | Add metrics + audit logging | Visibility |
| 6.10 | Production hardening: config tuning, error handling | Reliability |

### Go Dependencies to Add

```
github.com/qdrant/go-client/qdrant    # vector DB
github.com/cohere-ai/cohere-go/v2     # reranking API
github.com/openai/openai-go           # embedding API
github.com/pkoukk/tiktoken-go         # token counting for chunking
```

---

## Interview Narrative Summary

When asked about this project in an interview, the narrative arc is:

> *"I extended a DynamoDB MCP gateway into a unified RAG engine. Instead of building a separate microservice, I added RAG as a second category of MCP tool on the same Go server — sharing the Kafka pipeline, audit system, and metrics infrastructure. The key design decisions were:"
>
> * **Semantic chunking** with overlapping context windows to preserve thought coherence across chunk boundaries.*
> * **Two-stage retrieval** — cheap vector search to get 20 candidates, then an expensive cross-encoder reranker to pick the 4 best. This reduces LLM context from noise to signal.*
> * **Score thresholding** at 0.75 cosine similarity to reject out-of-domain queries entirely, preventing garbage-in-garbage-out.*
> * **DynamoDB Streams CDC** with a 30-second poller that keeps the vector index fresh without introducing Lambda cold starts or additional infrastructure.*
> * **All async ingestion** goes through the existing Kafka pipeline — the MCP tool just publishes a message and returns a job ID. The LLM never blocks on embedding.*

## What This Plan Omits (Intentionally)

| Omitted | Why |
|---------|-----|
| Multi-modal RAG (images, audio) | Overkill for a DynamoDB ops manual |
| Graph RAG (Knowledge Graphs) | High complexity, low ROI for this domain |
| Hybrid search (BM25 + vector) | Qdrant supports this natively with sparse vectors — trivial to add later |
| User feedback loop (thumbs up/down) | Requires a UI, unnecessary complexity for MCP-only interface |
| A/B testing different embeddings | Fun but not production-critical for a $5 Lightsail instance |

These omissions signal prioritization judgment: *"I could build all of these, but they don't serve the core use case. I shipped the 80% solution that delivers 95% of the value."*
