package server

import (
	"database/sql"
	"dynamodb-sage/internal/notification"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type ChatMessage struct {
	User      string
	ToolName  string
	Content   string
	Timestamp int64
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryNotification); err != nil {
		return nil, err
	}
	if _, err := db.Exec(queryChatHistory); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

const maxNotifications = 100

func (s *Store) AddNotification(n notification.NotificationPayload) error {
	title := n.Operation
	if title == "" {
		title = "Unknown"
	}
	_, err := s.db.Exec(`INSERT INTO notifications (title, job_id, table_name, severity, operation, message, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`, title, n.JobID, n.Table, n.Severity, n.Operation, n.Message, n.Timestamp)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM notifications WHERE id NOT IN (SELECT id FROM notifications ORDER BY timestamp DESC LIMIT ?)`, maxNotifications)
	return err
}

func (s *Store) GetNotifications() ([]notification.NotificationPayload, error) {
	rows, err := s.db.Query(`SELECT title, job_id, table_name, severity, operation, message, timestamp FROM notifications ORDER BY timestamp DESC`)
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
	return notifications, nil
}

func (s *Store) MarkNotificationAsRead(jobID string) error {
	_, err := s.db.Exec(`UPDATE notifications SET read = TRUE WHERE job_id = ?`, jobID)
	return err
}

func (s *Store) AddChatHistory(user, toolName, content string, timestamp int64) error {
	_, err := s.db.Exec(`INSERT INTO chat_history (user, tool_name, content, timestamp) VALUES (?, ?, ?, ?)`, user, toolName, content, timestamp)
	return err
}

func (s *Store) GetChatHistory(limit int) ([]ChatMessage, error) {
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
	return chatHistory, nil
}

func (s *Store) Close() {
	s.db.Close()
}
