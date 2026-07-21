// Package server implements the MCP server for DynamoDB Sage.
package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/engine"
	"dynamodb-sage/internal/llm"
	"dynamodb-sage/internal/notification"
	"dynamodb-sage/internal/queue"
	"dynamodb-sage/internal/rag"
	"dynamodb-sage/internal/risk"
	"encoding/json"
	"io/fs"
	"iter"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"
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
	sseClients          sync.Map
	notificationDedup   sync.Map
	metricsAddr         string
	toolList            []ToolInfo
	toolDefs            []llm.ToolDef
	llm                 *llm.Client
	rateLimiter         *rate.Limiter
	connCount           atomic.Int64
	startTime           time.Time
	rag                 *rag.RagPipeline
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
	store, err := NewStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	guardrail := engine.NewGuardrail(config)
	riskAnalyzer := risk.NewRiskAnalyzer(config, &risk.DynamoAdapter{Client: db}, guardrail)
	auditLog, err := audit.NewAuditLog(store.GetDB())
	if err != nil {
		log.Fatalf("Failed to create audit log: %v", err)
	}

	rag, err := rag.NewRagPipeline(config.Rag)
	if err != nil {
		log.Printf("Rag is unavailable: %v", err)
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
		startTime:    time.Now(),
		rag:          rag,
	}

	rps := 5.0
	if rpsStr := os.Getenv("LLM_RATE_LIMIT_RPS"); rpsStr != "" {
		if val, err := strconv.ParseFloat(rpsStr, 64); err == nil && val > 0 {
			rps = val
		}
	}
	burst := 10
	if burstStr := os.Getenv("LLM_RATE_LIMIT_BURST"); burstStr != "" {
		if val, err := strconv.Atoi(burstStr); err == nil && val > 0 {
			burst = val
		}
	}
	srv.rateLimiter = rate.NewLimiter(rate.Limit(rps), burst)
	if err := srv.initKafkaClient(kafkaConfigPath); err != nil {
		srv.startWorkerPool()
		log.Printf("In-process queue started: %v", err)
	}
	if srv.kafkaClient != nil {
		srv.notificationService = notification.NewNotificationService(srv.kafkaClient, srv.notificationsTopic, srv)
	}
	srv.mcpCtx, srv.mcpCancel = context.WithCancel(context.Background())
	srv.sseHandler = mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)
	srv.addTools()
	srv.toolDefs = srv.buildToolDefs()
	srv.prometheusServer()
	if err := srv.initLLM(); err != nil {
		log.Printf("Failed to init LLM: %v", err)
		srv.llm = nil
	}

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
		origin := os.Getenv("CORS_ORIGIN")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
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
			// Serve Next.js static assets
			if strings.HasPrefix(r.URL.Path, "/_next/") {
				staticFileServer().ServeHTTP(w, r)
				return
			}

			// API routes
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
			if r.URL.Path == "/api/health" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(srv.health())
				return
			}
			if r.URL.Path == "/api/stats" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(srv.stats())
				return
			}
			if r.URL.Path == "/api/tools" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(srv.toolList)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/tables/") {
				if strings.HasSuffix(r.URL.Path, "/items") {
					srv.getTableItems(w, r)
					return
				}
				srv.getTableInfo(w, r)
				return
			}
			if r.URL.Path == "/api/tables" {
				tables, err := srv.listTablesInfo()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tables)
				return
			}
			// SPA routes: try to serve the file, fall back to index.html for client-side routing
			staticFS, err := fs.Sub(dashboardFS, "static")
			if err == nil {
				name := strings.TrimPrefix(r.URL.Path, "/")
				if name == "" {
					name = "index.html"
				}
				// Try to serve the exact file first
				data, readErr := fs.ReadFile(staticFS, name)
				if readErr == nil {
					w.Header().Set("Content-Type", extContentType[ext(name)])
					w.Write(data)
					return
				}
				// Try with .html extension
				if !strings.HasSuffix(name, ".html") {
					data, readErr = fs.ReadFile(staticFS, name+".html")
					if readErr == nil {
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						w.Write(data)
						return
					}
				}
				// Fall back to root index.html for SPA routing
				data, readErr = fs.ReadFile(staticFS, "index.html")
				if readErr == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Write(data)
					return
				}
			}
			http.NotFound(w, r)
			return
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

func (srv *Server) initLLM() error {
	var err error
	srv.llm, err = llm.NewClient(srv.mcpCtx)
	return err
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
	if srv.store != nil {
		srv.store.Close()
	}

	return nil
}
