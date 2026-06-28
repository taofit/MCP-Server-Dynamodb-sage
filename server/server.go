// Package server implements the MCP server for DynamoDB Sage.
package server

import (
	"context"
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/engine"
	"dynamodb-sage/internal/kafka"
	"dynamodb-sage/internal/notification"
	"dynamodb-sage/internal/queue"
	"dynamodb-sage/internal/risk"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is set via ldflags at build time.
// Defaults to "dev" when running with go run.
var Version = "dev"

type KafkaClient interface {
	Send(topic string, key string, value []byte) error
	Start() error
	RegisterHandler(topic string, handler kafka.Handler)
	Close() error
}

type Server struct {
	db                  *dynamodb.Client
	s                   *mcp.Server
	guardrail           *engine.Guardrail
	auditLog            *audit.AuditLog
	userID              string
	userARN             string
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
}

type AuditBackend interface {
	LogActivity(entry audit.AuditEntry)
}

func New(db *dynamodb.Client, userID, userARN, configPath, kafkaConfigPath, dbPath string) *Server {
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
	srv := &Server{
		db:           db,
		auditLog:     auditLog,
		s:            mcpServer,
		guardrail:    guardrail,
		userID:       userID,
		userARN:      userARN,
		riskAnalyzer: riskAnalyzer,
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
		srv.notificationService = notification.NewNotificationService(srv.kafkaClient, srv.notificationsTopic)
	}
	srv.mcpCtx, srv.mcpCancel = context.WithCancel(context.Background())
	srv.sseHandler = mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)
	srv.addTools()

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

		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		handler.ServeHTTP(w, r)
	})
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

func (srv *Server) RecordActionLog(backend AuditBackend, entry audit.AuditEntry) {
	backend.LogActivity(entry)
}

func (srv *Server) initKafkaClient(kafkaConfigPath string) error {
	kafkaConfig, err := kafka.LoadConfig(kafkaConfigPath)
	if err != nil {
		return err
	}
	if !kafkaConfig.Enabled {
		return fmt.Errorf("kafka client disabled")
	}

	if os.Getenv("AWS_BASE_ENDPOINT") != "" {
		kafkaConfig.ConsumerGroupName = fmt.Sprintf("%s-%d", kafkaConfig.ConsumerGroupName, os.Getpid())
	}

	kafkaClient, err := kafka.NewClient(kafkaConfig)
	if err != nil {
		return err
	}

	srv.kafkaClient = kafkaClient
	srv.heavyOpsTopic = kafkaConfig.Topics["heavy_ops"]
	srv.notificationsTopic = kafkaConfig.Topics["notifications"]
	return nil
}

func (srv *Server) processHeavyOp(key string, payload []byte) error {
	payloadResult, err := srv.notificationService.ParsePayload(payload)
	if err != nil {
		srv.notificationService.SendNotification("unknown", "error", err.Error())
		return err
	}
	jobResult, ok := srv.jobStorage.Load(key)
	if !ok {
		srv.notificationService.SendNotification(payloadResult.TableName, "error", fmt.Sprintf("job not found: %s", key))
		return fmt.Errorf("job not found: %s", key)
	}

	jr := jobResult.(*JobResult)
	srv.executeJobOp(jr, payload)

	if jr.Error != nil {
		srv.notificationService.SendNotification(payloadResult.TableName, "error", jr.Error.Error())
		return jr.Error
	}
	srv.notificationService.SendNotification(payloadResult.TableName, "success", srv.notificationService.ConstructMessage(payloadResult))

	return nil
}

func (srv *Server) processHeavyOpForQueue(key string, payload []byte) error {
	jobResult, ok := srv.jobStorage.Load(key)
	if !ok {
		return fmt.Errorf("job not found: %s", key)
	}
	jr := jobResult.(*JobResult)
	srv.executeJobOp(jr, payload)
	if jr.Error != nil {
		return jr.Error
	}
	return nil
}

func (srv *Server) executeJobOp(jr *JobResult, payload []byte) {
	defer func() {
		if jr != nil && jr.Done != nil {
			close(jr.Done)
		}
	}()

	jobPayload := struct {
		Input     json.RawMessage `json:"input"`
		Operation string          `json:"operation"`
	}{}
	if err := json.Unmarshal(payload, &jobPayload); err != nil {
		jr.Error = fmt.Errorf("failed to unmarshal job payload: %v", err)
		return
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	switch jobPayload.Operation {
	case "batch_put_items":
		var input BatchPutItemsArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse batch_put_items args: %v", err)
		} else {
			result, _, err := srv.batchPutItems(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute batch_put_items: %v", err)
			} else {
				jr.Result = result
			}
		}
	case "batch_delete_items":
		var input BatchDeleteItemsArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse batch_delete_items args: %v", err)
		} else {
			result, _, err := srv.batchDeleteItems(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute batch_delete_items: %v", err)
			} else {
				jr.Result = result
			}
		}
	case "create_optimized_table":
		var input CreateOptimizedTableArgs
		if err := json.Unmarshal(jobPayload.Input, &input); err != nil {
			jr.Error = fmt.Errorf("failed to parse create_optimized_table args: %v", err)
		} else {
			result, _, err := srv.createOptimizedTable(ctx, req, &input)
			if err != nil {
				jr.Error = fmt.Errorf("failed to execute create_optimized_table: %v", err)
			} else {
				jr.Result = result
			}
		}
	default:
		jr.Error = fmt.Errorf("unknown operation: %s", jobPayload.Operation)
	}
}

// TODO send message to ui, Slack, Email
func (srv *Server) processNotification(key string, payload []byte) error {
	var notification notification.NotificationPayload
	if err := json.Unmarshal(payload, &notification); err != nil {
		log.Printf("failed to unmarshal notification: %v", err)
		return err
	}

	srv.notificationService.SendUINotify(notification.Severity, notification.Message)
	return nil
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
