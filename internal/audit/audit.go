package audit

import (
	"database/sql"
	"time"
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
	JobID                 string    `json:"job_id,omitempty"`
}

type AuditLog struct {
	writeFunc func(fn func(db *sql.DB) (sql.Result, error))
	readDB    *sql.DB
}

func NewAuditLog(readDB *sql.DB, writeFunc func(fn func(db *sql.DB) (sql.Result, error))) (*AuditLog, error) {
	return &AuditLog{
		readDB:    readDB,
		writeFunc: writeFunc,
	}, nil
}

func (a *AuditLog) ReadAuditHistory(limit int32, startTime time.Time, endTime time.Time) ([]AuditEntry, error) {
	query := `SELECT timestamp, operation, table_name, user, capacity_units_consumed, capacity_type, status, message, COALESCE(job_id, '') FROM audit_logs WHERE timestamp BETWEEN ? AND ? ORDER BY timestamp DESC LIMIT ?`
	rows, err := a.readDB.Query(query, startTime.Unix(), endTime.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var tsUnix int64
		entry := AuditEntry{}
		if err := rows.Scan(&tsUnix, &entry.Operation, &entry.TableName, &entry.User, &entry.CapacityUnitsConsumed, &entry.CapacityType, &entry.Status, &entry.Message, &entry.JobID); err != nil {
			return nil, err
		}
		entry.Timestamp = time.Unix(tsUnix, 0)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	return entries, nil
}

func (a *AuditLog) LogActivity(entry AuditEntry) {
	a.writeFunc(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`INSERT OR IGNORE INTO audit_logs (timestamp, operation, table_name, user, capacity_units_consumed, capacity_type, status, message, job_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.Timestamp.Unix(),
			entry.Operation,
			entry.TableName,
			entry.User,
			entry.CapacityUnitsConsumed,
			entry.CapacityType,
			entry.Status,
			entry.Message,
			entry.JobID)
		return result, err
	})
}
