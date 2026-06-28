package notification

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

type KafkaClient interface {
	Send(topic string, key string, value []byte) error
}

type NotificationService struct {
	kafkaClient       KafkaClient
	notificationTopic string
}

type JobPayload struct {
	Input     map[string]any `json:"input"`
	Operation string         `json:"operation"`
}
type PayloadResult struct {
	TableName string
	Operation string
}

type NotificationPayload struct {
	JobID     string `json:"jobId"`
	Table     string `json:"table"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

func NewNotificationService(kafkaClient KafkaClient, notificationTopic string) *NotificationService {
	return &NotificationService{
		kafkaClient:       kafkaClient,
		notificationTopic: notificationTopic,
	}
}

func (ntf *NotificationService) SendNotification(tableName, status, message string) {
	if ntf.kafkaClient == nil {
		return
	}

	id := uuid.New().String()
	notification := NotificationPayload{
		JobID:     id,
		Table:     tableName,
		Severity:  status,
		Message:   message,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(notification)
	if err := ntf.kafkaClient.Send(ntf.notificationTopic, id, data); err != nil {
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

	return PayloadResult{TableName: tableName, Operation: jobPayload.Operation}, nil
}

func (ntf *NotificationService) ConstructMessage(payload PayloadResult) string {
	return fmt.Sprintf("Job on table \"%s\" for operation \"%s\" has been completed successfully", payload.TableName, payload.Operation)
}

func escapeForAppleScript(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
