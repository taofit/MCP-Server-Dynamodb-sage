package server

import (
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/engine"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	db        *dynamodb.Client
	s         *mcp.Server
	guardrail *engine.Guardrail
	auditLog  *audit.AuditLog
}

func New(db *dynamodb.Client, configPath string, dbPath string) *Server {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "dynamo-sage",
		Version: "1.0.0",
	}, nil)

	config, err := engine.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	guardrail := engine.NewGuardrail(*config)
	auditLog, err := audit.NewAuditLog(dbPath)
	if err != nil {
		log.Fatalf("Failed to create audit log: %v", err)
	}
	srv := &Server{
		db:        db,
		auditLog:  auditLog,
		s:         mcpServer,
		guardrail: guardrail,
	}
	srv.addTools()

	return srv
}

func (srv *Server) SSEHandler() http.Handler {
	sseHandler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		sseHandler.ServeHTTP(w, r)
	})
}
