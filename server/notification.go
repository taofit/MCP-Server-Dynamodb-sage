package server

import (
	"dynamodb-sage/internal/notification"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

func (srv *Server) isDuplicateNotification(notf notification.NotificationPayload) bool {
	key := notifKey{notf.Table, notf.Operation, notf.InputHash}
	if _, loaded := srv.notificationDedup.LoadOrStore(key, struct{}{}); loaded {
		return true
	}
	time.AfterFunc(30*time.Second, func() {
		srv.notificationDedup.Delete(key)
	})
	return false
}

// TODO send message to ui, Slack, Email
func (srv *Server) processNotification(key string, payload []byte) error {
	var notf notification.NotificationPayload
	if err := json.Unmarshal(payload, &notf); err != nil {
		log.Printf("Failed to unmarshal notification: %v", err)
		return err
	}

	if srv.isDuplicateNotification(notf) {
		log.Printf("Dropping duplicate notification: table=%s operation=%s, inputHash=%s", notf.Table, notf.Operation, notf.InputHash)
		return nil
	}

	srv.notificationService.SendMCPNotification(key, notf)
	srv.notificationService.SendUINotify(notf.Severity, notf.Message)
	srv.broadcastToSSE(notf)
	if err := srv.store.AddNotification(notf); err != nil {
		log.Printf("Failed to store notification to SQLite: %v", err)
		return err
	}
	return nil
}

func (srv *Server) broadcastToSSE(notf notification.NotificationPayload) {
	srv.sseClients.Range(func(_ any, value any) bool {
		ch, ok := value.(chan notification.NotificationPayload)
		if !ok {
			return true
		}
		select {
		case ch <- notf:
		default:
			log.Printf("broadcastToSSE: client channel full, dropping notification")
		}
		return true
	})
}

func (srv *Server) recordNotification(table, operation, severity, message string) {
	notf := notification.NotificationPayload{
		Title:     fmt.Sprintf("%s on %s", operation, table),
		JobID:     uuid.New().String(),
		Table:     table,
		Severity:  severity,
		Operation: operation,
		Message:   message,
		Timestamp: time.Now().Unix(),
	}
	srv.broadcastToSSE(notf)
	if err := srv.store.AddNotification(notf); err != nil {
		log.Printf("Failed to store notification: %v", err)
	}
}
