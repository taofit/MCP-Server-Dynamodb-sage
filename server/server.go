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

		// Serve web dashboard for GET requests
		if r.Method == http.MethodGet {
			if strings.HasPrefix(r.URL.Path, "/static/") {
				http.StripPrefix("/static/", staticFileServer()).ServeHTTP(w, r)
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

		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		handler.ServeHTTP(w, r)
	})
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
