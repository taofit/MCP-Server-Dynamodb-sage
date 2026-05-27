package audit

import (
	"database/sql"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

type AuditEntry struct {
	Timestamp             time.Time `json:"timestamp"`
	Operation             string    `json:"operation"`
	TableName             string    `json:"table_name"`
	User                  string    `json:"user"`
	CapacityUnitsConsumed float64   `json:"capacity_units_consumed"`
	CapacityType          string    `json:"capacity_type"`
	Status                string    `json:"status"`
	Message               string    `json:"message,omitempty"`
}

type AuditLog struct {
	auditChan chan AuditEntry
	db        *sql.DB
}

func NewAuditLog(dbPath string) (*AuditLog, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	query := `CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		operation TEXT NOT NULL,
		table_name TEXT,
		user TEXT NOT NULL,
		capacity_units_consumed INTEGER,
		capacity_type TEXT,
		status TEXT NOT NULL,
		message TEXT
	)`
	if _, err := db.Exec(query); err != nil {
		return nil, err
	}

	auditLog := &AuditLog{
		db:        db,
		auditChan: make(chan AuditEntry, 100),
	}
	auditLog.processQueue()
	return auditLog, nil
}

func (a *AuditLog) processQueue() {
	go func() {
		for entry := range a.auditChan {
			log.Printf("AUDIT: %+v", entry)
			a.saveAuditHistory(entry)
		}
	}()
}

func (a *AuditLog) ReadAuditHistory(limit int32, startTime time.Time, endTime time.Time) ([]AuditEntry, error) {
	query := `SELECT timestamp, operation, table_name, user, capacity_units_consumed, capacity_type, status, message FROM audit_logs WHERE timestamp BETWEEN ? AND ? ORDER BY timestamp DESC LIMIT ?`
	rows, err := a.db.Query(query, startTime.Unix(), endTime.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var tsUnix int64
		entry := AuditEntry{}
		if err := rows.Scan(&tsUnix, &entry.Operation, &entry.TableName, &entry.User, &entry.CapacityUnitsConsumed, &entry.CapacityType, &entry.Status, &entry.Message); err != nil {
			return nil, err
		}
		entry.Timestamp = time.Unix(tsUnix, 0)
		entries = append(entries, entry)
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	return entries, nil
}

func (a *AuditLog) LogActivity(entry AuditEntry) {
	select {
	case a.auditChan <- entry:
	default:
		log.Printf("audit channel is full, dropping entry: %v", entry)
	}
}

func (a *AuditLog) saveAuditHistory(entry AuditEntry) {
	_, err := a.db.Exec(`INSERT INTO audit_logs (timestamp, operation, table_name, user, capacity_units_consumed, capacity_type, status, message) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp.Unix(),
		entry.Operation,
		entry.TableName,
		entry.User,
		entry.CapacityUnitsConsumed,
		entry.CapacityType,
		entry.Status,
		entry.Message)

	if err != nil {
		log.Printf("failed to save audit entry: %v", err)
	}
}
