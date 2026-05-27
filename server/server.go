package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/engine"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	db        *dynamodb.Client
	s         *mcp.Server
	guardrail *engine.Guardrail
	auditLog  *audit.AuditLog
	userID    string
	userARN   string
}

type AuditBackend interface {
	LogActivity(entry audit.AuditEntry)
}

func New(db *dynamodb.Client, userID, userARN, configPath, dbPath string) *Server {
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
		userID:    userID,
		userARN:   userARN,
	}
	srv.addTools()

	return srv
}

func (srv *Server) SSEHandler() http.Handler {
	sseHandler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)

	streamableHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, &mcp.StreamableHTTPOptions{
		JSONResponse: true,
		Stateless:    true,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.URL.Path == "/health" {
			srv.HealthHandler(w, r)
			return
		}

		sessionIDQuery := r.URL.Query().Get("sessionid")
		sessionIDHeader := r.Header.Get("Mcp-Session-Id")

		switch r.Method {
		case http.MethodGet:
			sseHandler.ServeHTTP(w, r)
		case http.MethodPost:
			if sessionIDQuery != "" && sessionIDHeader != "" {
				http.Error(w, "sessionid and Mcp-Session-Id are both present", http.StatusBadRequest)
				return
			}
			if sessionIDHeader != "" {
				q := r.URL.Query()
				q.Set("sessionid", sessionIDHeader)
				r.URL.RawQuery = q.Encode()
			}
			if sessionIDQuery != "" || sessionIDHeader != "" {
				rec := httptest.NewRecorder()
				sseHandler.ServeHTTP(rec, r)
				if rec.Code == http.StatusAccepted && rec.Body.Len() == 0 {
					for k, v := range rec.Header() {
						w.Header()[k] = append([]string(nil), v...)
					}
					w.WriteHeader(rec.Code)
					return
				}
				if rec.Code == http.StatusNotFound {
					streamableHandler.ServeHTTP(w, r)
					return
				}
				for k, v := range rec.Header() {
					w.Header()[k] = append([]string(nil), v...)
				}
				w.WriteHeader(rec.Code)
				rec.Body.WriteTo(w)
				return
			}
			streamableHandler.ServeHTTP(w, r)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})
}

func (srv *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (srv *Server) ServeStdio() error {
	return srv.s.Run(context.Background(), &mcp.StdioTransport{})
}

func (srv *Server) ServeHTTP(addr string) error {
	if v := os.Getenv("DYNAMO_SAGE_ADDR"); v != "" {
		addr = v
	}
	return http.ListenAndServe(addr, srv.SSEHandler())
}

func (srv *Server) RecordActionLog(backend AuditBackend, entry audit.AuditEntry) {
	backend.LogActivity(entry)
}
