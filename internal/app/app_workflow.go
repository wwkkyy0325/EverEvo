//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/tools"
	"everevo/internal/workflow"
)

// ─── Workflow CRUD ───────────────────────────────────────────────

// ListWorkflows returns summaries of all saved workflows.
func (a *App) ListWorkflows() []workflow.WorkflowSummary {
	if a.workflowManager == nil { return []workflow.WorkflowSummary{} }
	return a.workflowManager.List()
}

// GetWorkflow returns the full workflow definition by ID.
func (a *App) GetWorkflow(id string) (workflow.WorkflowDef, error) {
	if a.workflowManager == nil { return workflow.WorkflowDef{}, fmt.Errorf("workflow manager not ready") }
	wf, err := a.workflowManager.Get(id)
	if err != nil { return workflow.WorkflowDef{}, err }
	return *wf, nil
}

// CreateWorkflow saves a new workflow.
func (a *App) CreateWorkflow(wf workflow.WorkflowDef) error {
	if a.workflowManager == nil { return fmt.Errorf("workflow manager not ready") }
	id, err := a.workflowManager.Create(wf)
	if err != nil {
		return err
	}
	a.emitChanged("workflow:changed", "create", id)
	return nil
}

// UpdateWorkflow saves an existing workflow.
func (a *App) UpdateWorkflow(id string, wf workflow.WorkflowDef) error {
	if a.workflowManager == nil { return fmt.Errorf("workflow manager not ready") }
	wf.ID = id
	if err := a.workflowManager.Update(wf); err != nil {
		return err
	}
	a.emitChanged("workflow:changed", "update", id)
	return nil
}

// DeleteWorkflow removes a workflow by ID.
func (a *App) DeleteWorkflow(id string) error {
	if a.workflowManager == nil { return fmt.Errorf("workflow manager not ready") }
	if err := a.workflowManager.Delete(id); err != nil {
		return err
	}
	a.emitChanged("workflow:changed", "delete", id)
	return nil
}

// emitChanged notifies the frontend that data mutated, so any open page showing
// it can refresh in real time (e.g. while the LLM operates via tool calls).
// `event` is the entity-scoped channel name, e.g. "workflow:changed", "models:changed".
func (a *App) emitChanged(event, action, id string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, event, map[string]any{"action": action, "id": id})
}

// ExportWorkflow exports a workflow as JSON.
func (a *App) ExportWorkflow(id string) (json.RawMessage, error) {
	if a.workflowManager == nil { return nil, fmt.Errorf("workflow manager not ready") }
	return a.workflowManager.Export(id)
}

// ImportWorkflow imports a workflow from JSON.
func (a *App) ImportWorkflow(data json.RawMessage) error {
	if a.workflowManager == nil { return fmt.Errorf("workflow manager not ready") }
	return a.workflowManager.Import(data)
}

// DuplicateWorkflow creates a copy of an existing workflow.
func (a *App) DuplicateWorkflow(id string) (workflow.WorkflowDef, error) {
	if a.workflowManager == nil { return workflow.WorkflowDef{}, fmt.Errorf("workflow manager not ready") }
	wf, err := a.workflowManager.Duplicate(id)
	if err != nil { return workflow.WorkflowDef{}, err }
	return *wf, nil
}

// ─── Workflow Execution ──────────────────────────────────────────

// maxWorkflowExecDepth caps nested ExecuteWorkflow invocations. Even though
// the Tool node now blocks workflow_execute, this is a defense-in-depth guard
// against any future re-entry path.
const maxWorkflowExecDepth = 4

// workflowExecDepth tracks the current nesting depth per call chain. Keys are
// ephemeral; the counter is bumped on entry and decremented on exit.
var workflowExecDepth int

// ExecuteWorkflow runs a workflow and returns the execution ID.
func (a *App) ExecuteWorkflow(id string, inputs map[string]any) (string, error) {
	if a.workflowManager == nil { return "", fmt.Errorf("workflow manager not ready") }
	if workflowExecDepth >= maxWorkflowExecDepth {
		return "", fmt.Errorf("workflow 执行嵌套过深 (>%d)，已中止以防递归", maxWorkflowExecDepth)
	}
	workflowExecDepth++
	defer func() { workflowExecDepth-- }()

	wf, err := a.workflowManager.Get(id)
	if err != nil { return "", err }

	// Build adapter from App to engine interfaces
	llmAdapter := &workflowLLMAdapter{app: a}
	toolAdapter := &workflowToolAdapter{app: a}
	toolProvAdapter := &workflowToolProviderAdapter{app: a}
	emitter := &workflowEventEmitter{ctx: a.ctx, app: a} // Use shutdown-safe ctx

	eng := workflow.NewEngine(wf, llmAdapter, toolAdapter, toolProvAdapter, emitter, a.workflowManager)
	eng.SetAgentRunner(&workflowAgentAdapter{app: a})
	// Register the cancel func so CancelExecution/CancelAll can truly interrupt
	// the run, and a blocking workflow_execute can wait on its completion.
	a.workflowManager.RegisterCancel(eng.State(), eng.Cancel)
	go eng.Run(inputs)
	return eng.ExecID(), nil
}

// CancelWorkflowExecution cancels a running workflow.
func (a *App) CancelWorkflowExecution(execID string) error {
	if a.workflowManager == nil { return fmt.Errorf("workflow manager not ready") }
	a.workflowManager.CancelExecution(execID)
	return nil
}

// GetWorkflowExecutionStatus returns the current state of a workflow execution.
func (a *App) GetWorkflowExecutionStatus(execID string) *workflow.ExecutionState {
	if a.workflowManager == nil { return nil }
	return a.workflowManager.GetExecution(execID)
}

// ValidateWorkflow checks a workflow definition for problems.
func (a *App) ValidateWorkflow(wf workflow.WorkflowDef) map[string]any {
	return workflow.Validate(&wf)
}

// ─── Engine Adapters ─────────────────────────────────────────────

type workflowLLMAdapter struct{ app *App }

func (a *workflowLLMAdapter) ChatProxy(messagesJSON, toolsJSON json.RawMessage) (map[string]any, error) {
	return a.app.ChatProxy(messagesJSON, toolsJSON)
}

// ChatStream streams the LLM response, forwarding each text delta to onChunk
// (which the engine routes to the editor as workflow progress). The engine ctx
// is passed through so cancelling the workflow stops the stream promptly.
func (a *workflowLLMAdapter) ChatStream(ctx context.Context, messagesJSON, toolsJSON json.RawMessage, onChunk func(string)) (map[string]any, error) {
	p, err := a.app.resolveActiveProvider()
	if err != nil {
		return nil, err
	}
	return a.app.runChatStream("wf-llm", messagesJSON, toolsJSON, p, chatOpts{OnChunk: onChunk, Ctx: ctx})
}

type workflowToolAdapter struct{ app *App }

func (a *workflowToolAdapter) CallTool(name string, params map[string]any) workflow.ToolResult {
	result := a.app.CallTool(name, params)
	return workflow.ToolResult{
		Success: result.Success,
		Data:    result.Data,
		Error:   result.Error,
	}
}

type workflowToolProviderAdapter struct{ app *App }

func (a *workflowToolProviderAdapter) GetToolDefs(names []string) []workflow.ToolDefInfo {
	all := tools.List()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names { nameSet[n] = true }

	var out []workflow.ToolDefInfo
	for _, t := range all {
		if nameSet[t.Name] {
			out = append(out, workflow.ToolDefInfo{
				Name:          t.Name,
				Description:   t.Description,
				Parameters:    t.Parameters,
				RawParameters: t.RawParameters,
			})
		}
	}
	return out
}

type workflowEventEmitter struct {
	ctx context.Context
	app *App
}

func (e *workflowEventEmitter) Emit(event string, data any) {
	// Keep the raw wf-* Wails emit for backward compatibility (no current listener,
	// but cheap to retain), and bridge into the unified collab:event stream +
	// activity log so the workbench (single subscription) and history see workflow
	// activity alongside agent/plan/blackboard events.
	wailsRuntime.EventsEmit(e.ctx, event, data)
	if e.app != nil {
		if m, ok := data.(map[string]any); ok {
			e.app.bridgeWorkflowEvent(event, m)
		}
	}
}

// workflowAgentAdapter lets the workflow engine run a local Agent persona.
// It resolves the agent by ID (falling back to name), then delegates to the
// App's runAgentLoop (which applies the agent's persona, tool subset, and
// provider/model override with a bounded tool loop).
type workflowAgentAdapter struct{ app *App }

func (a *workflowAgentAdapter) RunAgent(ctx context.Context, agentID, userText string) (string, error) {
	if a.app.agentManager == nil {
		return "", fmt.Errorf("agent manager not ready")
	}
	agent, err := a.app.agentManager.Get(agentID)
	if err != nil {
		agent, err = a.app.agentManager.FindByName(agentID)
		if err != nil {
			return "", err
		}
	}
	return a.app.runAgentLoop(ctx, agent, userText)
}
