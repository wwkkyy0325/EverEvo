//go:build windows

package app

import (
	"fmt"

	"github.com/google/uuid"

	"everevo/internal/memory"
)

// ─── Sessions ─────────────────────────────────────────────────────

// MemorySessionList returns all chat sessions, newest first.
func (a *App) MemorySessionList() ([]memory.Session, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	list, err := a.memoryStore.ListSessions()
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Session{}
	}
	return list, nil
}

// MemorySessionCreate creates a new session. An empty title defaults to "新对话".
func (a *App) MemorySessionCreate(title, agentID string) (*memory.Session, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	if title == "" {
		title = "新对话"
	}
	id := uuid.NewString()
	if err := a.memoryStore.CreateSession(id, title, agentID); err != nil {
		return nil, err
	}
	a.emitChanged("memory:changed", "create", id)
	return a.memoryStore.GetSession(id)
}

// MemorySessionRename updates a session title.
func (a *App) MemorySessionRename(id, title string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameSession(id, title); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "rename", id)
	return nil
}

// MemorySessionDelete removes a session and all of its messages.
func (a *App) MemorySessionDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteSession(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", id)
	return nil
}

// ─── Messages ─────────────────────────────────────────────────────

// MemoryMessageList returns a session's messages ordered by seq.
func (a *App) MemoryMessageList(sessionID string) ([]memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	list, err := a.memoryStore.ListMessages(sessionID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Message{}
	}
	return list, nil
}

// MemoryMessageListRecent returns the last `limit` messages for a session.
// Use this instead of MemoryMessageList on session switch to avoid loading
// the full history for long conversations. limit <= 0 means all messages.
func (a *App) MemoryMessageListRecent(sessionID string, limit int) ([]memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	if limit <= 0 {
		limit = 50
	}
	list, err := a.memoryStore.ListMessagesRecent(sessionID, limit)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Message{}
	}
	return list, nil
}

// MemoryMessageCount returns the total number of messages in a session.
func (a *App) MemoryMessageCount(sessionID string) int {
	if a.memoryStore == nil {
		return 0
	}
	return a.memoryStore.CountMessages(sessionID)
}

// MemoryMessageUpdateToolJSON updates a message's tool_json field (for appending tool results).
func (a *App) MemoryMessageUpdateToolJSON(msgID, toolJSON string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.UpdateMessageToolJSON(msgID, toolJSON)
}

// MemoryMessageAppend appends a message to a session and returns the stored row.
// toolJSON carries tool_calls / tool results as opaque JSON ("" when none).
func (a *App) MemoryMessageAppend(sessionID, role, content, toolJSON string) (*memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	msg, err := a.memoryStore.AppendMessage(sessionID, uuid.NewString(), role, content, toolJSON)
	// Unified scheduler: facts, graph, summarize, reflect, link — all from one counter.
	if role == "user" {
		go a.scheduler()
	}
	return msg, err
}

// MemoryMessageClear deletes all messages of a session, keeping the session row.
func (a *App) MemoryMessageClear(sessionID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.ClearMessages(sessionID)
}

