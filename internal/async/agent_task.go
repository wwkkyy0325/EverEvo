// Package async — agent-specific task extensions for parallel execution.
//
// Design follows Claude Code's AgentTool routing pattern:
//   - sync: foreground execution, main loop blocks
//   - async: fire-and-forget, returns immediately, result enqueued
//   - fork: lightweight sub-agent sharing prompt cache (future)
//
// Main agent ↔ sub-agent separation:
//   - Main agent: full context, all tools, user-facing
//   - Sub-agent: isolated context, agent-scoped tools, returns result only
//   - Each sub-agent gets its own context.Context for independent cancellation

package async

import (
	"context"
	"sync"
)

// AgentTask extends Task with agent-specific execution metadata.
// Stored alongside the base Task in SQLite via the existing async_tasks
// table (agent fields stored in the context JSON column).
type AgentTask struct {
	Task

	// Agent identity
	AgentName   string `json:"agentName"`
	AgentID     string `json:"agentId"`
	ProviderID  string `json:"providerId"`
	Model       string `json:"model"`

	// Execution control
	CancelFn    context.CancelFunc `json:"-"` // independent abort
	IsAsync     bool               `json:"isAsync"`
}

// AgentTaskState is the in-memory registry of running agent tasks.
// Mirrors Claude Code's AppState.tasks pattern — a single registry
// for all concurrent work (agents, workflows, shell, dream).
type AgentTaskState struct {
	mu    sync.RWMutex
	tasks map[string]*AgentTask
}

// NewAgentTaskState creates the in-memory task registry.
func NewAgentTaskState() *AgentTaskState {
	return &AgentTaskState{tasks: make(map[string]*AgentTask)}
}

// Register adds a running agent task to the registry.
func (s *AgentTaskState) Register(t *AgentTask) {
	s.mu.Lock()
	s.tasks[t.ID] = t
	s.mu.Unlock()
}

// Remove deletes a task from the registry (called on completion).
func (s *AgentTaskState) Remove(id string) {
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
}

// Get returns a task by ID.
func (s *AgentTaskState) Get(id string) *AgentTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[id]
}

// List returns all currently tracked tasks.
func (s *AgentTaskState) List() []*AgentTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AgentTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// Cancel cancels a running task by ID (calls its CancelFn).
func (s *AgentTaskState) Cancel(id string) {
	s.mu.RLock()
	t := s.tasks[id]
	s.mu.RUnlock()
	if t != nil && t.CancelFn != nil {
		t.CancelFn()
	}
}

// Count returns the number of currently tracked tasks.
func (s *AgentTaskState) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}
