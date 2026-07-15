// Package async — command queue for injecting async task results back into
// the main conversation. Mirrors Claude Code's MessageQueueManager pattern:
//
//   - All async results flow through a single priority queue
//   - Main conversation loop drains pending notifications between turns
//   - Notifications are injected as system-role messages
//
// Priority ordering:
//   - "now": inject immediately (agent completion, errors)
//   - "next": inject before next user turn (workflow progress)
//   - "later": inject when idle (dream results, memory consolidation)

package async

import (
	"encoding/json"
	"sync"
)

// Priority for queue entries.
const (
	PriorityNow  = 0 // immediate: agent results, errors
	PriorityNext = 1 // next turn: workflow node completion
	PriorityLater = 2 // idle: dream results, memory consolidation
)

// QueueEntry is one pending notification from a background task.
type QueueEntry struct {
	Priority  int    `json:"priority"`
	TaskID    string `json:"taskId"`
	AgentName string `json:"agentName,omitempty"`
	Kind      string `json:"kind"` // "agent_done" | "agent_error" | "workflow_node" | "dream"
	Content   string `json:"content"`
}

// CommandQueue is the unified notification queue. All background tasks
// enqueue results here; the main conversation loop drains them.
type CommandQueue struct {
	mu     sync.Mutex
	queue  []*QueueEntry
	cond   *sync.Cond
}

// NewCommandQueue creates the notification queue.
func NewCommandQueue() *CommandQueue {
	q := &CommandQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds a notification to the queue. Safe for concurrent use.
func (q *CommandQueue) Enqueue(entry *QueueEntry) {
	q.mu.Lock()
	// Insert sorted by priority (ascending: now < next < later)
	inserted := false
	for i, e := range q.queue {
		if entry.Priority < e.Priority {
			q.queue = append(q.queue[:i], append([]*QueueEntry{entry}, q.queue[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		q.queue = append(q.queue, entry)
	}
	q.mu.Unlock()
	q.cond.Signal()
}

// Drain returns and removes all pending notifications. Called between
// conversation turns to inject results into the main chat context.
func (q *CommandQueue) Drain() []*QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queue) == 0 {
		return nil
	}
	out := q.queue
	q.queue = nil
	return out
}

// DrainJSON returns pending notifications as a JSON array.
// Used by the Wails binding to expose results to the frontend.
func (q *CommandQueue) DrainJSON() json.RawMessage {
	entries := q.Drain()
	if len(entries) == 0 {
		return json.RawMessage("[]")
	}
	b, _ := json.Marshal(entries)
	return b
}

// HasPending returns true if there are queued notifications.
func (q *CommandQueue) HasPending() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue) > 0
}

// Wait blocks until a notification is available or the context is cancelled.
// Used when the main loop is idle and waiting for background tasks.
func (q *CommandQueue) Wait(stopCh <-chan struct{}) *QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.queue) == 0 {
		q.mu.Unlock()
		select {
		case <-stopCh:
			q.mu.Lock()
			return nil
		default:
		}
		q.mu.Lock()
		q.cond.Wait()
	}
	entry := q.queue[0]
	q.queue = q.queue[1:]
	return entry
}
