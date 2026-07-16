package workflow

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Engine executes a single workflow run.
// It is created per execution and discarded afterwards.
type Engine struct {
	wf          *WorkflowDef
	execState   *ExecutionState
	ctx         context.Context
	cancel      context.CancelFunc
	nodeOutputs    map[string]any       // accumulated outputs keyed by node ID
	outputsMu      sync.RWMutex          // guards nodeOutputs for parallel node access
	inputs      map[string]any       // exec inputs from Run() (actual values for input nodes)
	topoOrder   []string             // topological sort result
	skipped     map[string]bool      // nodes to skip (from condition branching)

	llmCaller    LLMCaller
	toolCaller   ToolCaller
	toolProvider ToolProvider
	agentRunner  AgentRunner
	emitter      EventEmitter
	tracker      ExecutionTracker
	finishOnce   sync.Once
}

// ExecutionTracker is the minimal interface for storing execution state.
type ExecutionTracker interface {
	TrackExecution(state *ExecutionState)
}

// NewEngine creates a new workflow engine.
func NewEngine(wf *WorkflowDef, llm LLMCaller, tool ToolCaller, toolProv ToolProvider, emitter EventEmitter, tracker ExecutionTracker) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	execID := "exec_" + nowString() + "_" + wf.ID

	state := &ExecutionState{
		ExecID:     execID,
		WorkflowID: wf.ID,
		Status:     ExecPending,
		NodeStates: make(map[string]NodeRun),
		Outputs:    make(map[string]any),
		StartedAt:  nowMillis(),
		Done:       make(chan struct{}),
	}
	for _, n := range wf.Nodes {
		state.NodeStates[n.ID] = NodeRun{Status: NodePending}
	}

	tracker.TrackExecution(state)

	return &Engine{
		wf:           wf,
		execState:    state,
		ctx:          ctx,
		cancel:       cancel,
		nodeOutputs:  make(map[string]any),
		skipped:      make(map[string]bool),
		llmCaller:    llm,
		toolCaller:   tool,
		toolProvider: toolProv,
		emitter:      emitter,
		tracker:      tracker,
	}
}

// workflowBlockedTools are tools a workflow Tool node may NEVER call. They are
// the orchestration + destructive tools that could cause unbounded recursion,
// self-modification, or system damage. This mirrors the agent-side
// isOrchestrationTool guard but is enforced at the workflow engine boundary.
var workflowBlockedTools = map[string]bool{
	// orchestration (recursion / re-entry)
	"workflow_execute": true, "workflow_create": true, "workflow_update": true, "workflow_delete": true,
	"agent_run": true, "agent_create": true, "agent_set_library": true, "agent_delegate_to_domain": true, "agent_delegate_multi_domain": true,
	"agent_synthesize_tool": true,
	"collab_create": true, "collab_dispatch": true, "collab_dispatch_async": true, "collab_wait": true,
	"agent_message": true,
	// destructive system tools
	"shell_exec": true,
	"evolve_swap": true, "evolve_build": true,
	"zone_merge": true, "zone_discard": true,
	"backup_restore": true, "backup_delete": true,
	"plugin_create": true, "plugin_delete": true,
}

// isWorkflowBlockedTool reports whether a workflow Tool node is forbidden from
// calling the named tool.
func isWorkflowBlockedTool(name string) bool {
	return workflowBlockedTools[name]
}

// SetAgentRunner wires the local-agent runner used by the "agent" node type.
// Optional: an engine without a runner still executes every other node type;
// an agent node simply errors if no runner is configured. Kept as a setter so
// NewEngine's signature (and its existing test callers) stays stable.
func (eng *Engine) SetAgentRunner(r AgentRunner) { eng.agentRunner = r }

// Run executes the workflow and returns the final outputs.
func (eng *Engine) Run(execInputs map[string]any) {
	eng.inputs = execInputs
	// Also expose inputs under their bare keys so a template can reference a
	// top-level input as {{fieldName}} in addition to {{inputNode.fieldName}}.
	for k, v := range execInputs {
		eng.nodeOutputs[k] = v
	}

	// Compute topological order
	order, err := topologicalSort(eng.wf)
	if err != nil {
		eng.fail(err.Error())
		return
	}
	eng.topoOrder = order

	eng.execState.Status = ExecRunning
	eng.emit("wf-exec-start", map[string]any{
		"execId":       eng.execState.ExecID,
		"workflowId":   eng.wf.ID,
		"workflowName": eng.wf.Name,
		"totalNodes":   len(order),
	})

	for _, nodeID := range order {
		if eng.skipped[nodeID] {
			eng.emitNodeSkip(nodeID, eng.wf.NodeByID(nodeID).Title)
			continue
		}

		// Check cancellation
		select {
		case <-eng.ctx.Done():
			eng.execState.Status = ExecCancelled
			eng.emit("wf-exec-cancel", map[string]any{"execId": eng.execState.ExecID})
			eng.markDone()
			return
		default:
		}

		node := eng.wf.NodeByID(nodeID)
		if node == nil {
			continue
		}

		eng.emitNodeStart(nodeID, node.Title)

		output, err := eng.executeNode(node)
		if err != nil {
			if eng.ctx.Err() != nil {
				// Cancelled mid-node: Cancel() already set ExecCancelled and
				// emitted wf-exec-cancel — don't overwrite with a failure.
				eng.markDone()
				return
			}
			eng.emitNodeError(nodeID, node.Title, err.Error())
			eng.fail(fmt.Sprintf("节点 %s (%s) 失败: %v", node.Title, nodeID, err))
			return
		}

		eng.nodeOutputs[nodeID] = output
		// Alias by node Title so templates can reference upstream nodes by their
		// visible title ({{节点标题.字段}}), not only the auto-generated ID.
		if node.Title != "" {
			if _, exists := eng.nodeOutputs[node.Title]; !exists {
				eng.nodeOutputs[node.Title] = output
			}
		}
		eng.emitNodeDone(nodeID, node.Title, output)

		// Handle condition branching
		if node.Type == NodeCondition {
			if m, ok := output.(map[string]any); ok {
				if branch, _ := m["branch"].(string); branch != "" {
					eng.skipped = computeSkipped(eng.wf, nodeID, branch)
				}
			}
		}
	}

	// Collect final outputs from all Output nodes
	final := make(map[string]any)
	for _, node := range eng.wf.Nodes {
		if node.Type == NodeOutput {
			if out, ok := eng.nodeOutputs[node.ID]; ok {
				final[node.ID] = out
			}
		}
	}
	eng.execState.Outputs = final
	eng.execState.Status = ExecDone
	eng.execState.FinishedAt = nowMillis()
	eng.emit("wf-exec-done", map[string]any{
		"execId":  eng.execState.ExecID,
		"outputs": final,
	})
	eng.markDone()
}

// executeNode dispatches to the correct executor based on node type.
func (eng *Engine) executeNode(node *WorkflowNode) (any, error) {
	exec, ok := GetNodeExecutor(node.Type)
	if !ok {
		return nil, fmt.Errorf("unknown node type: %s", node.Type)
	}

	// Timeout per node type
	timeout := 30 * time.Second
	if node.Type == NodeLLM {
		timeout = 120 * time.Second
	} else if node.Type == NodeLoop {
		timeout = 300 * time.Second
	} else if node.Type == NodeAgent {
		timeout = agentNodeTimeout
	} else if node.Type == NodeCode || node.Type == NodeCondition {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(eng.ctx, timeout)
	defer cancel()

	type result struct {
		val any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		val, err := exec(eng, node)
		ch <- result{val, err}
	}()

	// Progress heartbeat — emit every 2s while the node runs so long nodes
	// (LLM/agent ~30s) are not silent to the editor UI or to workflow_status
	// pollers. The ticker arm just emits and loops; result/timeout arms return.
	progress := progressTextFor(node.Type)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case r := <-ch:
			ns := eng.execState.NodeStates[node.ID]
			ns.FinishedAt = nowMillis()
			ns.Progress = ""
			if r.err != nil {
				ns.Status = NodeError
				ns.Error = r.err.Error()
			} else {
				ns.Status = NodeDone
				ns.Output = r.val
			}
			eng.execState.NodeStates[node.ID] = ns
			log.Printf("[workflow] node %s (%s) → done", node.ID, node.Type)
			return r.val, r.err
		case <-ctx.Done():
			ns := eng.execState.NodeStates[node.ID]
			ns.Status = NodeError
			ns.Error = "timeout"
			ns.FinishedAt = nowMillis()
			ns.Progress = ""
			eng.execState.NodeStates[node.ID] = ns
			return nil, fmt.Errorf("timeout")
		case <-ticker.C:
			eng.emitProgress(node.ID, progress)
		}
	}
}

// ─── Event emission ──────────────────────────────────────────────

func (eng *Engine) emitNodeStart(nodeID, title string) {
	progress := ""
	if n := eng.wf.NodeByID(nodeID); n != nil {
		progress = progressTextFor(n.Type)
	}
	ns := eng.execState.NodeStates[nodeID]
	ns.Status = NodeRunning
	ns.StartedAt = nowMillis()
	ns.Progress = progress
	eng.execState.NodeStates[nodeID] = ns

	eng.emit("wf-node-start", map[string]any{
		"execId": eng.execState.ExecID,
		"nodeId": nodeID,
		"title":  title,
	})
	// Immediate heartbeat so the UI shows the progress text without waiting
	// for the first 2s tick.
	eng.emitProgress(nodeID, progress)
}

// progressTextFor returns a localized status string shown while a node runs.
// Empty for instant node types (input/output).
func progressTextFor(t NodeType) string {
	switch t {
	case NodeLLM, NodeAgent:
		return "正在生成…"
	case NodeTool:
		return "调用工具中…"
	case NodeCode:
		return "执行代码…"
	case NodeLoop:
		return "循环执行中…"
	case NodeCondition:
		return "判断条件…"
	default:
		return ""
	}
}

// emitProgress fires a per-execution heartbeat carrying the running node id +
// progress text. The dynamic event name (workflow-progress-<execId>) mirrors
// chat-done-<streamId> so only the owner tab receives it.
func (eng *Engine) emitProgress(nodeID, progress string) {
	eng.emit(fmt.Sprintf("workflow-progress-%s", eng.execState.ExecID), map[string]any{
		"execId":   eng.execState.ExecID,
		"nodeId":   nodeID,
		"progress": progress,
	})
}

func (eng *Engine) emitNodeDone(nodeID, title string, output any) {
	eng.emit("wf-node-done", map[string]any{
		"execId": eng.execState.ExecID,
		"nodeId": nodeID,
		"title":  title,
		"output": output,
	})
}

func (eng *Engine) emitNodeError(nodeID, title, errMsg string) {
	eng.emit("wf-node-error", map[string]any{
		"execId": eng.execState.ExecID,
		"nodeId": nodeID,
		"title":  title,
		"error":  errMsg,
	})
}

func (eng *Engine) emitNodeSkip(nodeID, title string) {
	ns := eng.execState.NodeStates[nodeID]
	ns.Status = NodeSkipped
	eng.execState.NodeStates[nodeID] = ns

	eng.emit("wf-node-skip", map[string]any{
		"execId": eng.execState.ExecID,
		"nodeId": nodeID,
		"title":  title,
	})
}

func (eng *Engine) emit(event string, data map[string]any) {
	if eng.emitter != nil {
		eng.emitter.Emit(event, data)
	}
}

// markDone closes the execution's Done channel exactly once so blocked callers
// (e.g. a blocking workflow_execute tool) can return. Idempotent.
func (eng *Engine) markDone() {
	eng.finishOnce.Do(func() {
		if eng.execState.Done != nil {
			close(eng.execState.Done)
		}
	})
}

func (eng *Engine) fail(msg string) {
	eng.execState.Status = ExecError
	eng.execState.Error = msg
	eng.execState.FinishedAt = nowMillis()
	eng.emit("wf-exec-error", map[string]any{
		"execId": eng.execState.ExecID,
		"error":  msg,
	})
	eng.markDone()
}

// Cancel stops a running execution.
func (eng *Engine) Cancel() {
	eng.cancel()
	eng.execState.Status = ExecCancelled
	eng.execState.FinishedAt = nowMillis()
	eng.emit("wf-exec-cancel", map[string]any{
		"execId": eng.execState.ExecID,
	})
	eng.markDone()
}

// ExecID returns this execution's unique ID.
func (eng *Engine) ExecID() string { return eng.execState.ExecID }

// State returns the current execution state (for polling).
func (eng *Engine) State() *ExecutionState { return eng.execState }

// Ctx returns the execution context (for cancellation propagation).
func (eng *Engine) Ctx() context.Context { return eng.ctx }

// SnapshotOutputs returns a shallow copy of nodeOutputs under the read lock.
// Used by parallel/map nodes to give each concurrent branch an isolated view.
func (eng *Engine) SnapshotOutputs() map[string]any {
	eng.outputsMu.RLock()
	defer eng.outputsMu.RUnlock()
	out := make(map[string]any, len(eng.nodeOutputs))
	for k, v := range eng.nodeOutputs {
		out[k] = v
	}
	return out
}

// WithOutputs swaps nodeOutputs to the given map for the duration of fn, then
// restores the original. Lets a parallel branch execute sub-nodes against an
// isolated view without races.
func (eng *Engine) WithOutputs(view map[string]any, fn func()) {
	eng.outputsMu.Lock()
	orig := eng.nodeOutputs
	eng.nodeOutputs = view
	eng.outputsMu.Unlock()
	defer func() {
		eng.outputsMu.Lock()
		eng.nodeOutputs = orig
		eng.outputsMu.Unlock()
	}()
	fn()
}

// SetOutput writes a single key into nodeOutputs under the write lock.
func (eng *Engine) SetOutput(key string, value any) {
	eng.outputsMu.Lock()
	eng.nodeOutputs[key] = value
	eng.outputsMu.Unlock()
}

// Validate checks the workflow for common problems.
func Validate(wf *WorkflowDef) map[string]any {
	report := map[string]any{"valid": true, "issues": []string{}}
	var issues []string

	hasInput := false
	hasOutput := false
	nodeIDs := make(map[string]bool)
	for _, n := range wf.Nodes {
		nodeIDs[n.ID] = true
		if n.Type == NodeInput {
			hasInput = true
		}
		if n.Type == NodeOutput {
			hasOutput = true
		}
	}

	if !hasInput {
		issues = append(issues, "缺少输入节点 (input)")
	}
	if !hasOutput {
		issues = append(issues, "缺少输出节点 (output)")
	}

	// Check edge validity
	for _, e := range wf.Edges {
		if !nodeIDs[e.Source] {
			issues = append(issues, "连线引用了不存在的源节点: "+e.Source)
		}
		if !nodeIDs[e.Target] {
			issues = append(issues, "连线引用了不存在的目标节点: "+e.Target)
		}
	}

	// Check for cycles
	if _, err := topologicalSort(wf); err != nil {
		issues = append(issues, "工作流存在循环引用")
	}

	if len(issues) > 0 {
		report["valid"] = false
	}
	report["issues"] = issues
	return report
}
