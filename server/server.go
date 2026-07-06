// Package server implements the MCP server for DynamoDB Sage.
package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/engine"
	"dynamodb-sage/internal/notification"
	"dynamodb-sage/internal/queue"
	"dynamodb-sage/internal/risk"
	"encoding/json"
	"iter"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Version is set via ldflags at build time.
// Defaults to "dev" when running with go run.
var Version = "dev"

type Server struct {
	db                  *dynamodb.Client
	s                   *mcp.Server
	guardrail           *engine.Guardrail
	auditLog            *audit.AuditLog
	store               *Store
	userID              string
	userARN             string
	transport           string
	riskAnalyzer        *risk.RiskAnalyzer
	sseHandler          *mcp.SSEHandler
	queue               *queue.QueueManager
	queueCancel         context.CancelFunc
	queueCtx            context.Context
	mcpCtx              context.Context
	mcpCancel           context.CancelFunc
	jobStorage          sync.Map
	kafkaClient         KafkaClient
	heavyOpsTopic       string
	notificationsTopic  string
	notificationService *notification.NotificationService
	notifMu             sync.Mutex
	sseClients          sync.Map
	notificationDedup   sync.Map
	metricsAddr         string
	toolList []ToolInfo
}

type ToolInfo struct {
	Name        string
	Description string
}

type notifKey struct {
	table, operation, inputHash string
}

type AuditBackend interface {
	LogActivity(entry audit.AuditEntry)
}

func New(db *dynamodb.Client, userID, userARN, configPath, kafkaConfigPath, dbPath string) *Server {
	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":2112"
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "dynamo-sage",
		Version: Version,
	}, nil)

	config, err := engine.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	guardrail := engine.NewGuardrail(config)
	riskAnalyzer := risk.NewRiskAnalyzer(config, &risk.DynamoAdapter{Client: db}, guardrail)
	auditLog, err := audit.NewAuditLog(dbPath)
	if err != nil {
		log.Fatalf("Failed to create audit log: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	srv := &Server{
		db:           db,
		auditLog:     auditLog,
		store:        store,
		s:            mcpServer,
		guardrail:    guardrail,
		userID:       userID,
		userARN:      userARN,
		riskAnalyzer: riskAnalyzer,
		metricsAddr:  metricsAddr,
	}
	if err := srv.initKafkaClient(kafkaConfigPath); err != nil {
		srv.startWorkerPool()
		log.Printf("In-process queue started: %v", err)
	} else {
		srv.kafkaClient.RegisterHandler(srv.heavyOpsTopic, srv.processHeavyOp)
		srv.kafkaClient.RegisterHandler(srv.notificationsTopic, srv.processNotification)
		go func() {
			if err := srv.kafkaClient.Start(); err != nil {
				log.Printf("Failed to start kafka client: %v", err)
			}
		}()
		srv.notificationService = notification.NewNotificationService(srv.kafkaClient, srv.notificationsTopic, srv)
	}
	srv.mcpCtx, srv.mcpCancel = context.WithCancel(context.Background())
	srv.sseHandler = mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)
	srv.addTools()
	srv.promServer()

	return srv
}

func (srv *Server) HTTPHandler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, &mcp.StreamableHTTPOptions{
		JSONResponse:               true,
		Stateless:                  true,
		DisableLocalhostProtection: true,
	})

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.URL.Path == "/health" {
			srv.HealthHandler(w, r)
			return
		}

		if r.URL.Path == "/sse" {
			srv.sseHandler.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if strings.HasPrefix(r.URL.Path, "/static/") {
				r.URL.Path = strings.TrimPrefix(r.URL.Path, "/static/")
				staticFileServer().ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/" {
				data, err := dashboardFS.ReadFile("static/index.html")
				if err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Write(data)
					return
				}
			}
			if r.URL.Path == "/api/chat" {
				http.Error(w, "Use POST", http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/api/metrics" {
				srv.metricsProxy(w, r)
				return
			}
			if r.URL.Path == "/api/events" {
				srv.handleSSEEvents(w, r)
				return
			}
			if r.URL.Path == "/api/notifications" {
				notifications, err := srv.store.GetNotifications()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(notifications)
				return
			}
		}

		if r.URL.Path == "/api/chat" {
			srv.handleChat(w, r)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

type chatRequest struct {
	Message string `json:"message"`
}

type chatResponse struct {
	Response string `json:"response"`
}

func (srv *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		json.NewEncoder(w).Encode(chatResponse{Response: "Please enter a message."})
		return
	}

	now := time.Now().Unix()
	srv.store.AddChatHistory("user", "", msg, now)

	resp := srv.generateChatResponse(msg)
	now = time.Now().Unix()
	srv.store.AddChatHistory("assistant", "", resp, now)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatResponse{Response: resp})
}

func (srv *Server) generateChatResponse(msg string) string {
	lower := strings.ToLower(msg)

	switch {
	case lower == "/help" || lower == "/tools" || lower == "help" || lower == "what can you do":
		return srv.formatToolList()

	case strings.Contains(lower, "table") && strings.Contains(lower, "list"):
		return "Use **list_tables** in the Tools tab to list all your DynamoDB tables.\n\nSwitch to the **Tools** tab, select `list_tables`, and click Send (no arguments needed)."

	case strings.Contains(lower, "table") && (strings.Contains(lower, "describe") || strings.Contains(lower, "info")):
		return "Use **describe_table** in the Tools tab to get details about a specific table.\n\nExample arguments:\n```json\n{\"tableName\": \"your-table-name\"}\n```"

	case strings.Contains(lower, "table") && (strings.Contains(lower, "create") || strings.Contains(lower, "new")):
		return "Use **create_optimized_table** in the Tools tab to create a new DynamoDB table.\n\nIt supports GSIs, LSIs, tags, and provisioned or on-demand billing."

	case strings.Contains(lower, "table") && (strings.Contains(lower, "delete") || strings.Contains(lower, "remove")):
		return "Use **delete_table** in the Tools tab to delete a table.\n\n**Warning:** Deleting a table is irreversible. Make sure you have backups!"

	case strings.Contains(lower, "item") && (strings.Contains(lower, "get") || strings.Contains(lower, "find") || strings.Contains(lower, "read")):
		return "Use **get_item** in the Tools tab to retrieve an item by its primary key, or **query_table** for more complex lookups."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "put") || strings.Contains(lower, "add") || strings.Contains(lower, "insert") || strings.Contains(lower, "create")):
		return "Use **put_item** in the Tools tab to insert or overwrite an item.\n\nFor multiple items, use **batch_put_items**."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "update") || strings.Contains(lower, "change")):
		return "Use **update_item** in the Tools tab to modify an existing item.\n\nYou'll need the table name, key, and an update expression."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "delete") || strings.Contains(lower, "remove")):
		return "Use **delete_item** in the Tools tab to remove an item by its primary key."

	case strings.Contains(lower, "scan"):
		return "Use **scan_table** in the Tools tab.\n\n⚠️ **Scanning is expensive** – it reads the entire table. Prefer **query_table** when you know the partition key."

	case strings.Contains(lower, "query"):
		return "Use **query_table** in the Tools tab.\n\nYou need:\n- `tableName` (required)\n- `keyConditionExpression` (required, e.g. `#pk = :pkVal`)\n- `expressionAttributeValues` (required)\n- `expressionAttributeNames` (optional but recommended)"

	case strings.Contains(lower, "audit") || strings.Contains(lower, "log"):
		return "Use **read_audit_logs** in the Tools tab to see recent DynamoDB operations with timestamps, tables, and capacity consumed."

	case strings.Contains(lower, "job") || strings.Contains(lower, "async"):
		return "Use **get_job_result** in the Tools tab to check the status of async operations (table creation, batch operations)."

	case strings.Contains(lower, "ttl") || strings.Contains(lower, "time to live"):
		return "Use **update_table_ttl** in the Tools tab to enable or disable TTL on a table."

	default:
		return srv.formatToolList()
	}
}

func (srv *Server) formatToolList() string {
	var b strings.Builder
	b.WriteString("Here are the available DynamoDB operations:\n\n")
	for _, t := range srv.toolList {
		b.WriteString("• `" + t.Name + "` — " + t.Description + "\n")
	}
	b.WriteString("\nSwitch to the **Tools** tab, select an operation, fill in the arguments as JSON (an example is pre-filled for you), and click Send.")
	return b.String()
}

func (srv *Server) promServer() {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(srv.metricsAddr, metricsMux)
}

func (srv *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (srv *Server) ServeStdio() error {
	return srv.s.Run(srv.mcpCtx, &mcp.StdioTransport{})
}

func (srv *Server) ServeHTTP(addr string) error {
	if v := os.Getenv("DYNAMO_SAGE_ADDR"); v != "" {
		addr = v
	}
	return http.ListenAndServe(addr, srv.HTTPHandler())
}

func (srv *Server) SetTransport(t string) {
	srv.transport = t
}

func (srv *Server) RecordActionLog(backend AuditBackend, entry audit.AuditEntry) {
	backend.LogActivity(entry)
}

func (srv *Server) GetMcpSessions() iter.Seq[*mcp.ServerSession] {
	return srv.s.Sessions()
}

func (srv *Server) startWorkerPool() {
	cpuCount := runtime.NumCPU()
	workerCount := cpuCount * 2
	if workerCount < 4 {
		workerCount = 4
	}
	buffer := workerCount * 2
	srv.queue = queue.New(workerCount, buffer)
	srv.queueCtx, srv.queueCancel = context.WithCancel(context.Background())
	go srv.queue.Start(srv.queueCtx)
}

func (srv *Server) shutdownWorkerPool(ctx context.Context) {
	if srv.queueCancel != nil {
		srv.queueCancel()
	}
	if srv.queue != nil {
		srv.queue.Shutdown(ctx)
	}
}

func (srv *Server) Shutdown(ctx context.Context) error {
	srv.shutdownWorkerPool(ctx)
	if srv.mcpCancel != nil {
		srv.mcpCancel()
	}
	if srv.kafkaClient != nil {
		srv.kafkaClient.Close()
	}
	return nil
}
