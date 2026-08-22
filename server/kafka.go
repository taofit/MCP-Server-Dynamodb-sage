package server

import (
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/kafka"
	"dynamodb-sage/internal/notification"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

type KafkaClient interface {
	Send(topic string, key string, value []byte) error
	Start() error
	Ping() error
	RegisterHandler(topic string, handler kafka.Handler)
	SetOnComplete(fn func(key string))
	Close() error
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
	srv.kafkaClient.RegisterHandler(srv.heavyOpsTopic, srv.processHeavyOp)
	srv.kafkaClient.RegisterHandler(srv.notificationsTopic, srv.processNotification)
	srv.kafkaClient.RegisterHandler(kafkaConfig.DLQ.Topic, srv.processDLQ)
	srv.kafkaClient.SetOnComplete(func(key string) {}) // no-op: results stored in SQLite
	go func() {
		if err := srv.kafkaClient.Start(); err != nil {
			log.Printf("Failed to start kafka client: %v", err)
		}
	}()
	return nil
}


func (srv *Server) processDLQ(key string, payload []byte) error {
	log.Printf("Dead Letter Queue message for key %s: %s", key, string(payload))
	tableName, operation, retryCount, jobID := srv.parseDLQPayload(payload)
	notificationData := getDLQNotificationPayload(key, tableName, operation, retryCount, jobID)
	srv.processNotification(key, notificationData)
	srv.auditTrail(tableName, operation, payload, key, jobID)
	return nil
}

func (srv *Server) parseDLQPayload(payload []byte) (tableName string, operation string, retryCount int, jobID string) {
	tableName = "unknown"
	operation = "unknown"
	jobID = ""
	retryCount = 0
	var dlqPayload kafka.DLQPayload
	var jobPayload notification.JobPayload
	if err := json.Unmarshal(payload, &dlqPayload); err != nil {
		log.Printf("Failed to unmarshal DLQ payload: %v", err)
		return
	}
	jobPayloadByte := dlqPayload.OriginalPayload
	retryCount = dlqPayload.RetryCount
	if err := json.Unmarshal(jobPayloadByte, &jobPayload); err == nil {
		if val, ok := jobPayload.Input["tableName"]; ok {
			tableName = val.(string)
		} else if val, ok := jobPayload.Input["TableName"]; ok {
			tableName = val.(string)
		}
		operation = jobPayload.Operation
		return
	}
	var notificationPayload notification.NotificationPayload
	if err := json.Unmarshal(jobPayloadByte, &notificationPayload); err == nil {
		tableName = notificationPayload.Table
		operation = notificationPayload.Operation
		jobID = notificationPayload.JobID
	}
	return
}

func (srv *Server) sweepStaleClaims() {
	n, err := srv.store.deleteStaleClaimedOps(time.Minute * 10)
	if err != nil {
		log.Printf("Failed to delete stale claimed ops: %v", err)
		return
	}
	if n > 0 {
		log.Printf("Deleted %d stale claimed ops", n)
	}
}

func (srv *Server) auditTrail(tableName, operation string, payload []byte, key string, jobID string) {
	if jobID == "" {
		jobID = key
	}
	srv.auditLog.LogActivity(audit.AuditEntry{
		Timestamp:             time.Now(),
		Operation:             operation,
		TableName:             tableName,
		User:                  srv.userID,
		CapacityUnitsConsumed: 0,
		CapacityType:          "dlq",
		Status:                "error",
		Message:               fmt.Sprintf("DLQ message for key %s: %s", key, string(payload)),
		JobID:                 jobID,
	})
}

func getDLQNotificationPayload(key string, table, operation string, retryCount int, jobID string) []byte {
	notification := notification.NotificationPayload{
		Title:     "DLQ Message",
		JobID:     jobID,
		Table:     table,
		Severity:  "error",
		Operation: operation,
		Message:   fmt.Sprintf("Operation '%s' on table '%s' failed after %d retries", operation, table, retryCount),
		InputHash: key,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(notification)
	return data
}
