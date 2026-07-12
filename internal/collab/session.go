package collab

import (
	"sync"
	"time"
)

// Session statuses.
const (
	SessionActive   = "active"
	SessionPaused   = "paused"
	SessionCompleted = "completed"
)

// Member is one agent participating in a collaboration session.
type Member struct {
	AgentID string `json:"agentId"`
	Role    string `json:"role"` // e.g. "orchestrator" | "researcher" | "critic"
	JoinedAt time.Time `json:"joinedAt"`
}

// Session is a multi-agent collaboration workspace: a shared goal, a set of
// member agents, and a blackboard for shared working state. Distinct from
// memory.Session (which is a single-agent chat log) — a collab Session
// coordinates multiple agents and their runs.
type Session struct {
	ID                string    `json:"id"`
	Goal              string    `json:"goal"`
	OrchestratorID    string    `json:"orchestratorId"`
	BlackboardID      string    `json:"blackboardId"`
	Status            string    `json:"status"`
	Members           []Member  `json:"members"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`

	mu sync.RWMutex
}

// SessionManager holds active collaboration sessions in-process. Persistence
// to SQLite is optional (via callbacks) — the primary store is in-memory
// because sessions are ephemeral work groups.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	kernel   *Kernel
}

// NewSessionManager creates a manager bound to a kernel (for blackboard
// creation on session start).
func NewSessionManager(k *Kernel) *SessionManager {
	return &SessionManager{
		sessions: map[string]*Session{},
		kernel:   k,
	}
}

// Create starts a new collaboration session: allocates a blackboard and
// registers the member agents.
func (sm *SessionManager) Create(id, goal, orchestratorID string, members []Member) *Session {
	now := time.Now()
	s := &Session{
		ID:             id,
		Goal:           goal,
		OrchestratorID: orchestratorID,
		BlackboardID:   "bb_" + id,
		Status:         SessionActive,
		Members:        members,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	// Ensure the orchestrator is a member.
	if !hasMember(s.Members, orchestratorID) {
		s.Members = append(s.Members, Member{AgentID: orchestratorID, Role: "orchestrator", JoinedAt: now})
	}
	sm.kernel.CreateBlackboard(s.BlackboardID)
	sm.mu.Lock()
	sm.sessions[id] = s
	sm.mu.Unlock()
	sm.kernel.Bus.Publish("collab."+id+".created", Event{Source: orchestratorID, Type: "created", Payload: s})
	return s
}

// Get returns a session by ID.
func (sm *SessionManager) Get(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

// List returns all active sessions.
func (sm *SessionManager) List() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		out = append(out, s)
	}
	return out
}

// Restore re-hydrates a previously persisted session (e.g. loaded from SQLite
// on startup). Does NOT re-publish the created event. Recreates the blackboard.
func (sm *SessionManager) Restore(id, goal, orchestratorID, blackboardID, status string, members []Member, createdAt time.Time) *Session {
	s := &Session{
		ID: id, Goal: goal, OrchestratorID: orchestratorID, BlackboardID: blackboardID,
		Status: status, Members: members, CreatedAt: createdAt, UpdatedAt: time.Now(),
	}
	if s.BlackboardID == "" {
		s.BlackboardID = "bb_" + id
	}
	if s.Status == "" {
		s.Status = SessionActive
	}
	sm.kernel.RestoreBlackboard(s.BlackboardID)
	sm.mu.Lock()
	sm.sessions[id] = s
	sm.mu.Unlock()
	return s
}

// Complete marks a session finished and drops its blackboard.
func (sm *SessionManager) Complete(id string) {
	sm.mu.Lock()
	s, ok := sm.sessions[id]
	if ok {
		s.mu.Lock()
		s.Status = SessionCompleted
		s.UpdatedAt = time.Now()
		s.mu.Unlock()
	}
	sm.mu.Unlock()
	if ok {
		sm.kernel.DropBlackboard(s.BlackboardID)
		sm.kernel.Bus.Publish("collab."+id+".completed", Event{Type: "completed"})
	}
}

func hasMember(members []Member, agentID string) bool {
	for _, m := range members {
		if m.AgentID == agentID {
			return true
		}
	}
	return false
}
