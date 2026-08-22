package server

import (
	"database/sql"
	"dynamodb-sage/internal/metrics"
	"dynamodb-sage/internal/notification"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type writeOp struct {
	execute func(db *sql.DB) (sql.Result, error)
	done    chan struct{sql.Result; error}
}

type Store struct {
	db      *sql.DB
	writeCh chan writeOp
}

type ChatMessage struct {
	User      string
	ToolName  string
	Content   string
	Timestamp int64
}

const maxNotifications = 100

func NewStore(dbPath string) (*Store, error) {
	dsn := dbPath
	if !strings.Contains(dsn, "?") {
		dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	queryNotification := `
		CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			job_id TEXT,
			table_name TEXT,
			severity TEXT NOT NULL,
			operation TEXT,
			message TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			read BOOLEAN DEFAULT FALSE
		)
	`
	queryChatHistory := `
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user TEXT NOT NULL,
			tool_name TEXT,
			content TEXT,
			timestamp INTEGER NOT NULL
		)
	`

	queryProcessedOpsByJob := `
		CREATE TABLE IF NOT EXISTS processed_ops_by_job (
			job_id TEXT PRIMARY KEY,
			operation TEXT NOT NULL,
			table_name TEXT,
			result_json TEXT,
			created_at INTEGER NOT NULL
		)
	`
	queryAuditLog := `
		CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			operation TEXT NOT NULL,
			table_name TEXT,
			user TEXT NOT NULL,
			capacity_units_consumed INTEGER,
			capacity_type TEXT,
			status TEXT NOT NULL,
			message TEXT,
			job_id TEXT
		)
	`
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryNotification); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryChatHistory); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryProcessedOpsByJob); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryAuditLog); err != nil {
		return nil, err
	}
	// ALTER TABLE is a no-op on fresh DBs (column already exists in CREATE TABLE), safe to ignore error
	_, _ = db.Exec(`ALTER TABLE audit_logs ADD COLUMN job_id TEXT;`)
	// DROP is required: CREATE INDEX IF NOT EXISTS will not update an existing index definition
	_, _ = db.Exec(`DROP INDEX IF EXISTS idx_audit_logs_dedup`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_logs_dedup ON audit_logs(job_id, operation, table_name, status, capacity_type) WHERE job_id IS NOT NULL AND job_id != ''`)
	s := &Store{db: db, writeCh: make(chan writeOp, 100)}
	go s.writerLoop()
	return s, nil
}

func (s *Store) writerLoop() {
	for op := range s.writeCh {
		start := time.Now()
		var result sql.Result
		var err error
		for i := 0; i < 10; i++ {
			result, err = op.execute(s.db)
			if isSQLiteBusy(err) && i < 9 {
				time.Sleep(time.Duration(50*(i+1)) * time.Millisecond)
				continue
			}
			break
		}
		op.done <- struct{sql.Result; error}{result, err}
		if err != nil {
			log.Printf("Write to DB failed: %v", err)
		}
		metrics.DBWriteDurationSeconds.Observe(time.Since(start).Seconds())
	}
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "database is locked") || strings.Contains(msg, "busy")
}

func (s *Store) asyncWrite(executor func(db *sql.DB) (sql.Result, error)) {
	select {
	case s.writeCh <- writeOp{execute: executor, done: make(chan struct{sql.Result; error}, 1)}:
	case <-time.After(2 * time.Second):
		log.Printf("Write channel full after 2s timeout, dropping write")
	}
}

func (s *Store) syncWrite(executor func(db *sql.DB) (sql.Result, error)) (sql.Result, error) {
	op := writeOp{execute: executor, done: make(chan struct{sql.Result; error})}
	select {
	case s.writeCh <- op:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("write channel timeout")
	}
	res := <-op.done
	return res.Result, res.error
}

func (s *Store) getDB() *sql.DB {
	return s.db
}

func (s *Store) writeFunc() func(fn func(db *sql.DB) (sql.Result, error)) {
	return s.asyncWrite
}

func (s *Store) addNotification(n notification.NotificationPayload) {
	s.asyncWrite(func(db *sql.DB) (sql.Result, error) {
		title := n.Operation
		if title == "" {
			title = "Unknown"
		}
		result, err := db.Exec(`INSERT INTO notifications (title, job_id, table_name, severity, operation, message, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`, title, n.JobID, n.Table, n.Severity, n.Operation, n.Message, n.Timestamp)
		if err != nil {
			return result, err
		}
		result, err = db.Exec(`DELETE FROM notifications WHERE id NOT IN (SELECT id FROM notifications ORDER BY timestamp DESC LIMIT ?)`, maxNotifications)
		return result, err
	})
}

func (s *Store) getNotifications() ([]notification.NotificationPayload, error) {
	rows, err := s.db.Query(`SELECT title, COALESCE(job_id, ''), COALESCE(table_name, ''), COALESCE(severity, ''), COALESCE(operation, ''), COALESCE(message, ''), COALESCE(timestamp, 0) FROM notifications ORDER BY timestamp DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notifications []notification.NotificationPayload
	for rows.Next() {
		var n notification.NotificationPayload
		if err := rows.Scan(&n.Title, &n.JobID, &n.Table, &n.Severity, &n.Operation, &n.Message, &n.Timestamp); err != nil {
			return nil, err
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notifications, nil
}

func (s *Store) addChatHistory(user, toolName, content string, timestamp int64) (sql.Result, error) {
	return s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`INSERT INTO chat_history (user, tool_name, content, timestamp) VALUES (?, ?, ?, ?)`, user, toolName, content, timestamp)
		return result, err
	})
}

func (s *Store) clearChatHistory() (sql.Result, error) {
	return s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`DELETE FROM chat_history`)
		return result, err
	})
}

func (s *Store) countNotifications() (int, error) {
	return s.getTotal("notifications")
}

func (s *Store) countChatMessages() (int, error) {
	return s.getTotal("chat_history")
}

func (s *Store) getTotal(tableName string) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	rows, err := s.db.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) getChatHistory(limit int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT user, tool_name, content, timestamp FROM chat_history ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chatHistory []ChatMessage
	for rows.Next() {
		var chatMessage ChatMessage
		if err := rows.Scan(&chatMessage.User, &chatMessage.ToolName, &chatMessage.Content, &chatMessage.Timestamp); err != nil {
			return nil, err
		}
		chatHistory = append(chatHistory, chatMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chatHistory, nil
}

func (s *Store) addProcessedOp(jobID string, operation, tableName string, resultJSON string) (bool, error) {
	result, err := s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`INSERT INTO processed_ops_by_job (job_id, operation, table_name, result_json, created_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(job_id) DO NOTHING`, jobID, operation, tableName, resultJSON, time.Now().Unix())
		if err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return false, err
	}
	r, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if r == 1 {
		return true, nil
	}
	return false, nil
}

func (s *Store) updateProcessedOpResult(jobID string, resultJSON string) (bool, error) {
	result, err := s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`UPDATE processed_ops_by_job SET result_json = ? WHERE job_id = ?`, resultJSON, jobID)
		if err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return false, err
	}
	r, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if r == 1 {
		return true, nil
	}
	return false, nil
}

func (s *Store) deleteClaimedOp(jobID string) (bool, error) {
	result, err := s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`DELETE FROM processed_ops_by_job WHERE job_id = ? AND result_json = ''`, jobID)
		if err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return false, err
	}
	r, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if r >= 1 {
		return true, nil
	}
	return false, nil
}

func (s *Store) deleteStaleClaimedOps(timeout time.Duration) (int, error) {
	result, err := s.syncWrite(func(db *sql.DB) (sql.Result, error) {
		result, err := db.Exec(`DELETE FROM processed_ops_by_job WHERE result_json = '' AND created_at < ?`, time.Now().Unix()-int64(timeout.Seconds()))
		if err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return 0, err
	}
	r, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(r), nil
}

func (s *Store) getProcessedOpResult(jobID string) (string, error) {
	rows, err := s.db.Query(`SELECT result_json FROM processed_ops_by_job WHERE job_id = ?`, jobID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var resultJSON string
	if rows.Next() {
		if err := rows.Scan(&resultJSON); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return resultJSON, nil
}

func (s *Store) Close() {
	close(s.writeCh)
	s.db.Close()
}
