package collab

import (
	"context"
	"sync"
	"time"
)

// Kernel is the top-level collaboration orchestrator. The App holds one
// instance and injects it into runAgentLoop and the workflow engine. It owns:
//   - the EventBus (in-process pub/sub)
//   - active blackboards (keyed by session ID)
//   - the local agent dispatcher (in-process A2A-style messaging)
//   - collaboration sessions (multi-agent work groups)
type Kernel struct {
	Bus       *EventBus
	Sessions  *SessionManager
	Dispatch  *Dispatcher
	Plans     *PlanManager
	mu                   sync.RWMutex
	boards               map[string]*Blackboard // blackboardID → board
	runs                 map[string]*AgentRun   // runID → agent execution state
	runsByA              map[string]*AgentRun   // agentID → most recent run (convenience)
	blackboardPersistFn  PersistFn                   // auto-wired to every new blackboard
	blackboardLoadFn     func(boardID string) []Entry // loads persisted entries for a board
}

// NewKernel creates a kernel with an optional event-forwarding sink. The
// Dispatcher must be wired after construction (SetDispatcher) because it needs
// an AgentRunner, which the App provides.
func NewKernel(forward func(topic string, data any)) *Kernel {
	k := &Kernel{
		Bus:     NewEventBus(forward),
		boards:  map[string]*Blackboard{},
		runs:    map[string]*AgentRun{},
		runsByA: map[string]*AgentRun{},
	}
	k.Sessions = NewSessionManager(k)
	k.Plans = NewPlanManager(k.Bus)
	return k
}

// SetDispatcher wires the local/remote agent message router.
func (k *Kernel) SetDispatcher(d *Dispatcher) {
	k.mu.Lock()
	k.Dispatch = d
	k.mu.Unlock()
}

// SetBlackboardPersistFn wires a persistence callback that is automatically set
// on every blackboard created by this kernel. Pair with SetBlackboardLoadFn.
func (k *Kernel) SetBlackboardPersistFn(fn PersistFn) {
	k.mu.Lock()
	k.blackboardPersistFn = fn
	k.mu.Unlock()
}

// SetBlackboardLoadFn wires a loader that RestoreBlackboard uses to reload
// persisted entries. Pair with SetBlackboardPersistFn.
func (k *Kernel) SetBlackboardLoadFn(fn func(boardID string) []Entry) {
	k.mu.Lock()
	k.blackboardLoadFn = fn
	k.mu.Unlock()
}

// CreateBlackboard makes a new blackboard and registers it.
func (k *Kernel) CreateBlackboard(id string) *Blackboard {
	bb := NewBlackboard(id, k.Bus)
	k.mu.Lock()
	if k.blackboardPersistFn != nil {
		bb.SetPersistFn(k.blackboardPersistFn)
	}
	k.boards[id] = bb
	k.mu.Unlock()
	return bb
}

// RestoreBlackboard creates a new blackboard and loads persisted entries into
// it (used by session restore). Returns the board.
func (k *Kernel) RestoreBlackboard(id string) *Blackboard {
	bb := k.CreateBlackboard(id)
	if k.blackboardLoadFn != nil {
		entries := k.blackboardLoadFn(id)
		if len(entries) > 0 {
			bb.LoadEntries(entries)
		}
	}
	return bb
}

// Blackboard returns a registered board by ID (nil if absent).
func (k *Kernel) Blackboard(id string) *Blackboard {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.boards[id]
}

// DropBlackboard removes a board (session teardown).
func (k *Kernel) DropBlackboard(id string) {
	k.mu.Lock()
	delete(k.boards, id)
	k.mu.Unlock()
}

// maxConcurrentRuns bounds in-flight agent runs to avoid runaway fan-out.
const maxConcurrentRuns = 16

// RegisterRun records an agent execution so callers can poll/wait on it.
// Returns false if the run cap is reached (caller should back off).
func (k *Kernel) RegisterRun(r *AgentRun) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	active := 0
	for _, rr := range k.runs {
		if rr.Status == StatusRunning {
			active++
		}
	}
	if active >= maxConcurrentRuns {
		return false
	}
	k.runs[r.ID] = r
	k.runsByA[r.AgentID] = r
	return true
}

// Run returns an agent run by ID.
func (k *Kernel) Run(runID string) *AgentRun {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.runs[runID]
}

// CompleteRun finalizes a run: sets status + result and closes Done.
func (k *Kernel) CompleteRun(runID string, result string, runErr error) {
	k.mu.Lock()
	r, ok := k.runs[runID]
	k.mu.Unlock()
	if !ok {
		return
	}
	r.complete(result, runErr)
	k.Bus.Publish("agent."+r.AgentID+".done", Event{
		Source: r.AgentID, Type: "done", Payload: map[string]any{"runId": runID, "collabSessionId": r.CollabSessionID},
	})
}

// EvictRun removes a finished run from the registry (memory hygiene).
func (k *Kernel) EvictRun(runID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if r, ok := k.runs[runID]; ok {
		delete(k.runs, runID)
		if k.runsByA[r.AgentID] == r {
			delete(k.runsByA, r.AgentID)
		}
	}
}

// Agent run statuses.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusCanceled = "canceled"
)

// AgentRun tracks one local agent execution, mirroring workflow.ExecutionState.
// Done is closed exactly once when the run terminates (any terminal status).
type AgentRun struct {
	ID             string
	AgentID        string
	CollabSessionID string
	Task           string
	Status         string
	Result         string
	Err            error
	StartedAt      time.Time
	Done           chan struct{}
	cancel         context.CancelFunc
	once           sync.Once
}

// SetCancel attaches the cancel func (from context.WithCancel) so the run can
// be aborted externally.
func (r *AgentRun) SetCancel(c context.CancelFunc) { r.cancel = c }

// Cancel aborts the run (best-effort — the agent loop must respect its ctx).
func (r *AgentRun) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *AgentRun) complete(result string, err error) {
	r.once.Do(func() {
		r.Result = result
		r.Err = err
		if err != nil {
			r.Status = StatusFailed
		} else {
			r.Status = StatusDone
		}
		close(r.Done)
	})
}
