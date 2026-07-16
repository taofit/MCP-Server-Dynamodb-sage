package notification

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type KafkaClient interface {
	Send(topic string, key string, value []byte) error
}

type SessionProvider interface {
	GetMcpSessions() iter.Seq[*mcp.ServerSession]
}

type NotificationService struct {
	kafkaClient       KafkaClient
	notificationsTopic string
	sessProvider      SessionProvider
}

type JobPayload struct {
	Input     map[string]any `json:"input"`
	Operation string         `json:"operation"`
}
type PayloadResult struct {
	TableName string
	Operation string
	InputHash string `json:"inputHash"`
}

type NotificationPayload struct {
	Title     string `json:"title"`
	JobID     string `json:"jobId"`
	Table     string `json:"table"`
	Severity  string `json:"severity"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
	InputHash string `json:"inputHash"`
	Timestamp int64  `json:"timestamp"`
}

func NewNotificationService(kafkaClient KafkaClient, notificationsTopic string, sp SessionProvider) *NotificationService {
	return &NotificationService{
		kafkaClient:        kafkaClient,
		notificationsTopic: notificationsTopic,
		sessProvider:       sp,
	}
}

func (ntf *NotificationService) SendNotification(tableName, status, operation, inputHash, message string) {
	if ntf.kafkaClient == nil {
		log.Printf("SendNotification: kafkaClient is nil, dropping: table=%s status=%s msg=%q", tableName, status, message)
		return
	}

	id := uuid.New().String()
	title := fmt.Sprintf("%s on %s", operation, tableName)
	notification := NotificationPayload{
		Title:     title,
		JobID:     id,
		Table:     tableName,
		Severity:  status,
		Operation: operation,
		Message:   message,
		InputHash: inputHash,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(notification)
	if err := ntf.kafkaClient.Send(ntf.notificationsTopic, id, data); err != nil {
		log.Printf("Failed to send notification: %v", err)
	}
}

func (ntf *NotificationService) SendUINotify(severity, message string) {
	if runtime.GOOS == "darwin" {
		go func() {
			safeMessage := escapeForAppleScript(message)
			safeSeverity := escapeForAppleScript(severity)
			cmd := exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "DynamoDB Sage job - %s"`, safeMessage, safeSeverity))
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Printf("Failed to display notification: %v: %s", err, string(out))
			}
		}()
	}
}

func (ntf *NotificationService) SendMCPNotification(key string, notificationPayload NotificationPayload) error {
	var firstErr error
	logLvl := logLevel(notificationPayload.Severity)
	if ntf.sessProvider == nil {
		return nil
	}
	for session := range ntf.sessProvider.GetMcpSessions() {
		if err := session.Log(context.Background(), &mcp.LoggingMessageParams{
			Level:  logLvl,
			Data:   notificationPayload,
			Logger: "dynamo-sage-notifications",
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (ntf *NotificationService) ParsePayload(payload []byte) (PayloadResult, error) {
	jobPayload := JobPayload{}
	if err := json.Unmarshal(payload, &jobPayload); err != nil {
		log.Printf("Failed to unmarshal job payload: %v", err)
		return PayloadResult{}, fmt.Errorf("failed to unmarshal job payload: %v", err)
	}
	var tableName string
	if table, ok := jobPayload.Input["tableName"].(string); ok {
		tableName = table
	} else {
		log.Printf("Failed to parse table name from job payload: %v", jobPayload.Input["tableName"])
		return PayloadResult{}, fmt.Errorf("failed to parse table name from job payload: %v", jobPayload.Input["tableName"])
	}

	inputJSON, _ := json.Marshal(jobPayload.Input)
	inputHash := fmt.Sprintf("%x", sha256.Sum256(inputJSON))

	return PayloadResult{TableName: tableName, Operation: jobPayload.Operation, InputHash: inputHash}, nil
}

func (ntf *NotificationService) ConstructMessage(payload PayloadResult) string {
	return fmt.Sprintf("Job on table \"%s\" for operation \"%s\" has been completed successfully", payload.TableName, payload.Operation)
}

func escapeForAppleScript(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

func logLevel(severity string) mcp.LoggingLevel {
	switch strings.ToLower(severity) {
	case "error":
		return mcp.LoggingLevel("error")
	case "warning":
		return mcp.LoggingLevel("warning")
	case "info":
		return mcp.LoggingLevel("info")
	case "success":
		return mcp.LoggingLevel("success")
	case "debug":
		return mcp.LoggingLevel("debug")
	default:
		return mcp.LoggingLevel("info")
	}
}
