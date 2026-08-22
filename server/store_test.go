package server

import (
	"dynamodb-sage/internal/audit"
	"dynamodb-sage/internal/notification"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreConcurrentWritesAndReads(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_audit.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	numWorkers := 10
	recordsPerWorker := 5

	// Launch concurrent writers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < recordsPerWorker; j++ {
				store.addNotification(notification.NotificationPayload{
					Title:     "test_op",
					JobID:     "job-1",
					Table:     "gale",
					Severity:  "info",
					Operation: "batch_put_items",
					Message:   "test message",
					Timestamp: time.Now().Unix(),
				})
			}
		}(i)
	}

	// Launch concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = store.getNotifications()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Wait briefly for write channel to drain
	time.Sleep(200 * time.Millisecond)

	count, err := store.countNotifications()
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}
	expectedCount := numWorkers * recordsPerWorker
	if count != expectedCount {
		t.Fatalf("Expected %d notifications to be written, but found %d", expectedCount, count)
	}
}

func TestAuditLogConcurrentLogging(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_audit_log.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	auditLog, err := audit.NewAuditLog(store.getDB(), store.writeFunc())
	if err != nil {
		t.Fatalf("Failed to create audit log: %v", err)
	}

	var wg sync.WaitGroup
	numWorkers := 10
	entriesPerWorker := 10

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for j := 0; j < entriesPerWorker; j++ {
				auditLog.LogActivity(audit.AuditEntry{
					Timestamp:             time.Now(),
					Operation:             "batch_put_items",
					TableName:             "gale",
					User:                  "admin",
					CapacityUnitsConsumed: 1.0,
					CapacityType:          "WCU",
					Status:                "success",
					Message:               fmt.Sprintf("batch_put_items performed on table gale %d-%d", w, j),
					JobID:                 fmt.Sprintf("job-%d-%d", w, j),
				})
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	entries, err := auditLog.ReadAuditHistory(200, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Failed to read audit history: %v", err)
	}
	expected := numWorkers * entriesPerWorker
	if len(entries) != expected {
		t.Fatalf("Expected %d audit entries, but got %d", expected, len(entries))
	}
}
