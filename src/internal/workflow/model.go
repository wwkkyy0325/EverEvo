package workflow

import "time"

// NodeType enumerates the supported workflow node kinds.
type NodeType string

const (
	NodeInput     NodeType = "input"
	NodeLLM       NodeType = "llm"
	NodeTool      NodeType = "tool"
	NodeCondition NodeType = "condition"
	NodeCode      NodeType = "code"
	NodeLoop      NodeType = "loop"
	NodeAgent     NodeType = "agent"
	NodeOutput    NodeType = "output"
	NodeParallel  NodeType = "parallel" // concurrent branches, gather all
	NodeMap       NodeType = "map"      // concurrent map over an array
)

// ExecutionStatus tracks the overall workflow execution state.
type ExecutionStatus string

const (
	ExecPending   ExecutionStatus = "pending"
	ExecRunning   ExecutionStatus = "running"
	ExecDone      ExecutionStatus = "done"
	ExecError     ExecutionStatus = "error"
	ExecCancelled ExecutionStatus = "cancelled"
)

// NodeStatus tracks a single node's execution state.
type NodeStatus string

const (
	NodePending NodeStatus = "pending"
	NodeRunning NodeStatus = "running"
	NodeDone    NodeStatus = "done"
	NodeError   NodeStatus = "error"
	NodeSkipped NodeStatus = "skipped"
)

// ─── Persisted Types ────────────────────────────────────────────

// WorkflowDef is the top-level workflow definition saved to disk.
type WorkflowDef struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Nodes       []WorkflowNode `json:"nodes"`
	Edges       []WorkflowEdge `json:"edges"`
	Variables   map[string]any `json:"variables,omitempty"`
	CreatedAt   int64          `json:"createdAt"`
	UpdatedAt   int64          `json:"updatedAt"`
}

// WorkflowSummary is a lightweight listing entry (no full graph).
type WorkflowSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	NodeCount   int    `json:"nodeCount"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// WorkflowNode is a single node in the workflow graph.
type WorkflowNode struct {
	ID          string         `json:"id"`
	Type        NodeType       `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config"`
	// Position is the canvas coordinate (top-left, pixels). Pure layout state:
	// the LLM never sends it; the frontend sets it (dagre auto-layout or user
	// drag) and persists it. nil means "not yet laid out" (frontend lays out).
	Position *NodePosition `json:"position,omitempty"`
}

// NodePosition is the canvas coordinate of a node's top-left corner, in pixels.
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WorkflowEdge is a directed connection between two nodes.
type WorkflowEdge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"` // "true"/"false" for condition, "output" otherwise
}

// ─── Execution State (not persisted) ────────────────────────────

// ExecutionState holds the runtime state of an active execution.
type ExecutionState struct {
	ExecID     string             `json:"execId"`
	WorkflowID string             `json:"workflowId"`
	Status     ExecutionStatus    `json:"status"`
	NodeStates map[string]NodeRun `json:"nodeStates"`
	Outputs    map[string]any     `json:"outputs"`
	StartedAt  int64              `json:"startedAt"`
	FinishedAt int64              `json:"finishedAt"`
	Error      string             `json:"error,omitempty"`
	// Done is closed when the execution reaches a terminal state (done/error/
	// cancelled), unblocking callers that wait on completion (e.g. a blocking
	// workflow_execute tool). Not serialized.
	Done chan struct{} `json:"-"`
}

// NodeRun holds the runtime state of a single node during execution.
type NodeRun struct {
	Status     NodeStatus `json:"status"`
	Output     any        `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Progress   string     `json:"progress,omitempty"` // localized "正在生成…" shown while a long node runs
	StartedAt  int64      `json:"startedAt"`
	FinishedAt int64      `json:"finishedAt"`
}

// ─── Helpers ─────────────────────────────────────────────────────

// NewWorkflow creates a WorkflowDef with sensible defaults.
func NewWorkflow(name string) WorkflowDef {
	now := nowMillis()
	return WorkflowDef{
		ID:        "wf_" + nowString(),
		Name:      name,
		Nodes:     []WorkflowNode{},
		Edges:     []WorkflowEdge{},
		Variables: map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Summary returns a WorkflowSummary for listing.
func (wf *WorkflowDef) Summary() WorkflowSummary {
	return WorkflowSummary{
		ID:          wf.ID,
		Name:        wf.Name,
		Description: wf.Description,
		NodeCount:   len(wf.Nodes),
		UpdatedAt:   wf.UpdatedAt,
	}
}

// NodeByID returns the node with the given ID, or nil.
func (wf *WorkflowDef) NodeByID(id string) *WorkflowNode {
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == id {
			return &wf.Nodes[i]
		}
	}
	return nil
}

// OutEdges returns all edges originating from the given node ID.
func (wf *WorkflowDef) OutEdges(nodeID string) []WorkflowEdge {
	var out []WorkflowEdge
	for _, e := range wf.Edges {
		if e.Source == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// InEdges returns all edges targeting the given node ID.
func (wf *WorkflowDef) InEdges(nodeID string) []WorkflowEdge {
	var in []WorkflowEdge
	for _, e := range wf.Edges {
		if e.Target == nodeID {
			in = append(in, e)
		}
	}
	return in
}

// EdgeID builds a deterministic edge ID from source + target + handle.
func EdgeID(source, target, handle string) string {
	if handle == "" {
		handle = "output"
	}
	return source + "→" + handle + "→" + target
}

// MergePositions copies Position from old into any incoming node that lacks one
// (matched by ID). The LLM's workflow_update replaces the whole node list and
// never sends coordinates; without this, every edit would wipe the layout.
// New nodes the LLM added keep a nil Position for the frontend to lay out.
func MergePositions(old, incoming []WorkflowNode) []WorkflowNode {
	prevByID := make(map[string]*NodePosition, len(old))
	for i := range old {
		if old[i].Position != nil {
			p := *old[i].Position // copy so callers don't share pointers
			prevByID[old[i].ID] = &p
		}
	}
	for i := range incoming {
		if incoming[i].Position == nil {
			if p, ok := prevByID[incoming[i].ID]; ok {
				incoming[i].Position = p
			}
		}
	}
	return incoming
}

func nowMillis() int64 { return time.Now().UnixMilli() }
func nowString() string { return time.Now().Format("060102150405") }
