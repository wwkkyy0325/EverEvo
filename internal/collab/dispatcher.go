package collab

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentRunner runs a local agent persona on a task. Implemented by the App
// (wrapping runAgentLoop) — collab depends on this interface, not on app
// directly, to avoid an import cycle. collabSessionID, when non-empty,
// auto-scopes the agent's blackboard tool calls to that session.
type AgentRunner interface {
	RunAgent(ctx context.Context, agentID, task, collabSessionID string) (string, error)
}

// Dispatcher is the in-process agent-message router. It lets one agent send a
// task to another LOCAL agent via a direct function call (no HTTP), while
// remote agents still go through the a2a.Client. This unifies local and remote
// agent messaging behind a single Send API and fixes the existing gap where
// local-local A2A round-trips through loopback TCP.
// maxCollabDepth caps nested collaboration dispatches. Even though collab
// tools are blacklisted from sub-agents (they can't call collab_dispatch
// themselves), this guards against any future re-entry path through the
// dispatcher. Mirrors workflow.maxWorkflowExecDepth.
const maxCollabDepth = 4

type Dispatcher struct {
	mu       sync.RWMutex
	local    map[string]bool       // set of locally-registered agent IDs
	runner   AgentRunner           // executes local agent tasks
	kernel   *Kernel
	remote   RemoteSender          // optional remote delivery (a2a.Client adapter)
	timeout  time.Duration
	depth    int                   // current nesting depth (defense-in-depth)
}

// RemoteSender delivers a message to a REMOTE agent (implemented by a2a adapter).
type RemoteSender interface {
	Send(ctx context.Context, agentCardURL, secret, task string) (string, error)
}

// NewDispatcher creates a dispatcher. runner executes local agents; remote is
// optional (nil for local-only operation).
func NewDispatcher(k *Kernel, runner AgentRunner, remote RemoteSender) *Dispatcher {
	return &Dispatcher{
		local:   map[string]bool{},
		runner:  runner,
		kernel:  k,
		remote:  remote,
		timeout: 300 * time.Second,
	}
}

// SetRemote wires the remote sender (a2a client adapter) after construction,
// once the A2A manager is ready. Safe to call once during startup.
func (d *Dispatcher) SetRemote(r RemoteSender) {
	d.mu.Lock()
	d.remote = r
	d.mu.Unlock()
}

// RegisterLocal marks an agent ID as locally available (routing skips HTTP).
func (d *Dispatcher) RegisterLocal(agentID string) {
	d.mu.Lock()
	d.local[agentID] = true
	d.mu.Unlock()
}

// Unregister removes a local agent.
func (d *Dispatcher) Unregister(agentID string) {
	d.mu.Lock()
	delete(d.local, agentID)
	d.mu.Unlock()
}

// IsLocal reports whether an agent ID is locally resolvable.
func (d *Dispatcher) IsLocal(agentID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.local[agentID]
}

// Send delivers a task to an agent. Local agents run in-process via the
// AgentRunner; remote agents go through the RemoteSender. collabSessionID, when
// non-empty, auto-scopes the agent's blackboard access. Returns final text.
func (d *Dispatcher) Send(ctx context.Context, agentID, task, collabSessionID string) (string, error) {
	d.mu.Lock()
	if d.depth >= maxCollabDepth {
		d.mu.Unlock()
		return "", fmt.Errorf("协同派发嵌套过深 (>%d)，已中止以防递归", maxCollabDepth)
	}
	d.depth++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.depth--
		d.mu.Unlock()
	}()

	if d.IsLocal(agentID) {
		if d.runner == nil {
			return "", fmt.Errorf("no local agent runner configured")
		}
		return d.runner.RunAgent(ctx, agentID, task, collabSessionID)
	}
	d.mu.RLock()
	remote := d.remote
	d.mu.RUnlock()
	if remote == nil {
		return "", fmt.Errorf("agent %q is not local and no remote sender configured", agentID)
	}
	// Remote delivery requires a card URL lookup — the adapter handles that.
	return remote.Send(ctx, agentID, "", task)
}
