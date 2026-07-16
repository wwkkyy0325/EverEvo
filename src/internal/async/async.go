// Package async provides a background task manager for long-running AI operations.
// Tasks are persisted in SQLite and stream status updates to the frontend via
// Wails events (async-task:created, async-task:started, async-task:completed,
// async-task:failed).
package async

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Task is a background async operation spawned from a chat conversation.
type Task struct {
	ID          string `json:"id"`
	SessionID   string `json:"sessionId"`
	Title       string `json:"title"`
	Status      string `json:"status"` // pending | running | done | failed | cancelled
	ToolName    string `json:"toolName"`
	ToolArgs    string `json:"toolArgs"`
	Context     string `json:"context"`
	Result      string `json:"result"`
	Error       string `json:"error"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	CompletedAt int64  `json:"completedAt,omitempty"`
}

// EmitFn is a callback that sends Wails events to the frontend.
type EmitFn func(event string, data any)

// Manager holds the async task registry and the SQLite connection.
type Manager struct {
	mu     sync.RWMutex
	db     *sql.DB
	emitFn EmitFn
}

// NewManager creates an async task manager backed by the given SQLite DB.
func NewManager(db *sql.DB, emitFn EmitFn) (*Manager, error) {
	m := &Manager{db: db, emitFn: emitFn}
	if err := m.migrate(); err != nil {
		return nil, err
	}
	if err := m.loadExisting(); err != nil {
		return nil, err
	}
	return m, nil
}

// migrate creates the async_tasks table if it doesn't exist.
func (m *Manager) migrate() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS async_tasks (
		id           TEXT PRIMARY KEY,
		session_id   TEXT NOT NULL,
		title        TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'pending',
		tool_name    TEXT NOT NULL DEFAULT '',
		tool_args    TEXT NOT NULL DEFAULT '{}',
		context      TEXT NOT NULL DEFAULT '{}',
		result       TEXT NOT NULL DEFAULT '',
		error        TEXT NOT NULL DEFAULT '',
		created_at   INTEGER NOT NULL,
		updated_at   INTEGER NOT NULL,
		completed_at INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// loadExisting recovers in-memory state from SQLite on startup. Tasks that were
// "running" when the app crashed are marked as failed.
func (m *Manager) loadExisting() error {
	rows, err := m.db.Query(`SELECT id, session_id, title, status, tool_name, tool_args,
		context, result, error, created_at, updated_at, completed_at
		FROM async_tasks ORDER BY created_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now().UnixMilli()
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Title, &t.Status, &t.ToolName,
			&t.ToolArgs, &t.Context, &t.Result, &t.Error, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt); err != nil {
			log.Printf("[async] scan error: %v", err)
			continue
		}
		if t.Status == "running" {
			t.Status = "failed"
			t.Error = "应用重启，任务中断"
			t.UpdatedAt = now
			m.db.Exec(`UPDATE async_tasks SET status='failed', error=?, updated_at=? WHERE id=?`,
				t.Error, now, t.ID)
		}
	}
	return nil
}

// Create creates a new async task and persists it.
func (m *Manager) Create(sessionID, title, toolName, toolArgs, contextJSON string) (*Task, error) {
	now := time.Now().UnixMilli()
	t := &Task{
		ID:        "at_" + uuid.NewString(),
		SessionID: sessionID,
		Title:     title,
		Status:    "pending",
		ToolName:  toolName,
		ToolArgs:  toolArgs,
		Context:   contextJSON,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.mu.Lock()
	_, err := m.db.Exec(`INSERT INTO async_tasks(id, session_id, title, status, tool_name,
		tool_args, context, result, error, created_at, updated_at, completed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,0)`,
		t.ID, t.SessionID, t.Title, t.Status, t.ToolName,
		t.ToolArgs, t.Context, t.Result, t.Error, t.CreatedAt, t.UpdatedAt)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("创建异步任务失败: %w", err)
	}
	m.emit("async-task:created", t)
	return t, nil
}

// Start marks a task as running and returns immediately. The caller is responsible
// for calling Complete or Fail when the work finishes.
func (m *Manager) Start(id string) error {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	_, err := m.db.Exec(`UPDATE async_tasks SET status='running', updated_at=? WHERE id=?`, now, id)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.emit("async-task:started", map[string]any{"id": id, "status": "running"})
	return nil
}

// Complete marks a task as done and stores the result.
func (m *Manager) Complete(id string, resultJSON string) error {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	_, err := m.db.Exec(`UPDATE async_tasks SET status='done', result=?, updated_at=?, completed_at=? WHERE id=?`,
		resultJSON, now, now, id)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	m.emit("async-task:completed", t)
	return nil
}

// Fail marks a task as failed with an error message.
func (m *Manager) Fail(id, errMsg string) error {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	_, err := m.db.Exec(`UPDATE async_tasks SET status='failed', error=?, updated_at=? WHERE id=?`,
		errMsg, now, id)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	m.emit("async-task:failed", t)
	return nil
}

// Cancel marks a task as cancelled.
func (m *Manager) Cancel(id string) error {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	_, err := m.db.Exec(`UPDATE async_tasks SET status='cancelled', updated_at=? WHERE id=?`, now, id)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.emit("async-task:failed", map[string]any{"id": id, "status": "cancelled", "error": "用户取消"})
	return nil
}

// Get returns a single task by ID.
func (m *Manager) Get(id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var t Task
	err := m.db.QueryRow(`SELECT id, session_id, title, status, tool_name, tool_args,
		context, result, error, created_at, updated_at, completed_at
		FROM async_tasks WHERE id=?`, id).
		Scan(&t.ID, &t.SessionID, &t.Title, &t.Status, &t.ToolName,
			&t.ToolArgs, &t.Context, &t.Result, &t.Error, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("异步任务不存在: %s", id)
	}
	return &t, nil
}

// List returns tasks, optionally filtered by sessionID and/or status.
func (m *Manager) List(sessionID, status string) ([]Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var clauses []string
	var args []any
	if sessionID != "" {
		clauses = append(clauses, "session_id=?")
		args = append(args, sessionID)
	}
	if status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, status)
	}
	q := `SELECT id, session_id, title, status, tool_name, tool_args,
		context, result, error, created_at, updated_at, completed_at
		FROM async_tasks`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC LIMIT 100"

	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	cutoff := time.Now().UnixMilli() - 24*3600*1000 // 24h TTL for done/failed
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Title, &t.Status, &t.ToolName,
			&t.ToolArgs, &t.Context, &t.Result, &t.Error, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt); err != nil {
			continue
		}
		// Lazy-clean: skip stale completed tasks.
		if (t.Status == "done" || t.Status == "failed" || t.Status == "cancelled") && t.CompletedAt < cutoff {
			continue
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Task{}
	}
	return out, nil
}

// ContextAndResult returns the raw context and result JSON for a task. Used by
// ResumeAsyncTask to inject them back into a conversation.
func (m *Manager) ContextAndResult(id string) (contextJSON, resultJSON string, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	err = m.db.QueryRow(`SELECT context, result FROM async_tasks WHERE id=?`, id).
		Scan(&contextJSON, &resultJSON)
	return
}

func (m *Manager) emit(event string, data any) {
	if m.emitFn != nil {
		m.emitFn(event, data)
	}
}

// MarshalContext serialises chat messages and optional plan state for storage.
func MarshalContext(messages, plan any) string {
	b, _ := json.Marshal(map[string]any{"messages": messages, "plan": plan})
	return string(b)
}

// UnmarshalContext deserialises the stored context back into a map.
func UnmarshalContext(ctxJSON string) (map[string]any, error) {
	var m map[string]any
	err := json.Unmarshal([]byte(ctxJSON), &m)
	return m, err
}
