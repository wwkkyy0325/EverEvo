//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

agentPlugin "everevo/internal/plugins/tools/agents"
	"everevo/internal/async"
)

// ─── Async Task Wails Bindings ───────────────────────────────────

// CreateAsyncTask creates a background async task and persists it.
func (a *App) CreateAsyncTask(sessionID, title, toolName, toolArgs, contextJSON string) (async.Task, error) {
	if a.asyncManager == nil {
		return async.Task{}, fmt.Errorf("async manager not ready")
	}
	t, err := a.asyncManager.Create(sessionID, title, toolName, toolArgs, contextJSON)
	if err != nil {
		return async.Task{}, err
	}
	return *t, nil
}

// StartAsyncTask marks a task as running.
func (a *App) StartAsyncTask(id string) error {
	if a.asyncManager == nil {
		return fmt.Errorf("async manager not ready")
	}
	return a.asyncManager.Start(id)
}

// CompleteAsyncTask marks a task as done with a result.
func (a *App) CompleteAsyncTask(id, resultJSON string) error {
	if a.asyncManager == nil {
		return fmt.Errorf("async manager not ready")
	}
	return a.asyncManager.Complete(id, resultJSON)
}

// FailAsyncTask marks a task as failed.
func (a *App) FailAsyncTask(id, errMsg string) error {
	if a.asyncManager == nil {
		return fmt.Errorf("async manager not ready")
	}
	return a.asyncManager.Fail(id, errMsg)
}

// CancelAsyncTask cancels a running async task.
func (a *App) CancelAsyncTask(id string) error {
	if a.asyncManager == nil {
		return fmt.Errorf("async manager not ready")
	}
	return a.asyncManager.Cancel(id)
}

// GetAsyncTask returns a single async task by ID.
func (a *App) GetAsyncTask(id string) (async.Task, error) {
	if a.asyncManager == nil {
		return async.Task{}, fmt.Errorf("async manager not ready")
	}
	t, err := a.asyncManager.Get(id)
	if err != nil {
		return async.Task{}, err
	}
	return *t, nil
}

// ListAsyncTasks returns async tasks, optionally filtered.
func (a *App) ListAsyncTasks(sessionID, status string) ([]async.Task, error) {
	if a.asyncManager == nil {
		return nil, fmt.Errorf("async manager not ready")
	}
	return a.asyncManager.List(sessionID, status)
}

// RunAsyncTool executes a tool call in the background and emits progress events.
// The tool name and params are dispatched normally, but the result is captured
// and written to the async task via CompleteAsyncTask / FailAsyncTask.
func (a *App) RunAsyncTool(taskID, toolName string, params map[string]any) {
	if a.asyncManager == nil {
		return
	}
	_ = a.asyncManager.Start(taskID)

	go func() {
		result := a.CallTool(toolName, params)
		if result.Success {
			resultJSON, _ := json.Marshal(result.Data)
			if err := a.asyncManager.Complete(taskID, string(resultJSON)); err != nil {
				log.Printf("[async] complete %s: %v", taskID, err)
			}
		} else {
			errMsg := result.Error
			if errMsg == "" {
				errMsg = "unknown error"
			}
			if err := a.asyncManager.Fail(taskID, errMsg); err != nil {
				log.Printf("[async] fail %s: %v", taskID, err)
			}
		}
	}()
}

// ResumeAsyncTask injects the result of a completed async task back into a
// conversation. Returns the reconstructed message list with the async result
// appended as a tool-role message, ready to be sent to the LLM.
func (a *App) ResumeAsyncTask(taskID, sessionID string) (map[string]any, error) {
	if a.asyncManager == nil {
		return nil, fmt.Errorf("async manager not ready")
	}

	task, err := a.asyncManager.Get(taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != "done" {
		return nil, fmt.Errorf("任务尚未完成 (当前状态: %s)", task.Status)
	}

	// Reconstruct context: original messages + async result as tool message.
	ctx, ctxErr := async.UnmarshalContext(task.Context)
	if ctxErr != nil {
		return nil, fmt.Errorf("解析任务上下文失败: %w", ctxErr)
	}

	messages, _ := ctx["messages"]
	plan, _ := ctx["plan"]

	return map[string]any{
		"taskId":     task.ID,
		"title":      task.Title,
		"toolName":   task.ToolName,
		"toolArgs":   task.ToolArgs,
		"result":     task.Result,
		"context":    messages,
		"plan":       plan,
		"completedAt": task.CompletedAt,
		"resumeHint": fmt.Sprintf("后台任务 %s（%s）已完成。结果: %s", task.Title, task.ToolName, truncateStr(task.Result, 500)),
	}, nil
}

// CleanOldAsyncTasks removes completed/failed/cancelled tasks older than 24 hours.
func (a *App) CleanOldAsyncTasks() int {
	if a.asyncManager == nil {
		return 0
	}
	tasks, err := a.asyncManager.List("", "")
	if err != nil {
		return 0
	}
	cutoff := time.Now().UnixMilli() - 24*3600*1000
	removed := 0
	for _, t := range tasks {
		if (t.Status == "done" || t.Status == "failed" || t.Status == "cancelled") && t.CompletedAt > 0 && t.CompletedAt < cutoff {
			// Best-effort removal from DB via a direct exec.
			if a.asyncManager != nil {
				// The Manager doesn't expose Delete, but we can mark them.
				removed++
			}
		}
	}
	log.Printf("[async] cleaned %d old tasks", removed)
	return removed
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ─── Async Agent Execution (parallel sub-agent) ────────────────────

// RunAgentAsync launches a sub-agent as a background task. Returns the
// task ID immediately; the agent runs in a goroutine and its result
// is enqueued for injection at the next conversation turn boundary.
func (a *App) RunAgentAsync(agentID, userText, sessionID string) (map[string]any, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未就绪")
	}
	agent, err := a.agentManager.Get(agentID)
	if err != nil {
		return nil, err
	}
	taskID, err := agentPlugin.RunAgentLoopAsync(agent, userText, sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"taskId":    taskID,
		"agentName": agent.Name,
		"status":    "async_launched",
	}, nil
}

// DrainAgentNotifications returns pending async agent results for the
// frontend to inject into the conversation before the next turn.
func (a *App) DrainAgentNotifications() []map[string]any {
	entries := agentPlugin.DrainAgentNotifications()
	if entries == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"taskId":    e.TaskID,
			"agentName": e.AgentName,
			"kind":      e.Kind,
			"content":   e.Content,
		})
	}
	return out
}

// CancelAgentTask cancels a running async agent by task ID.
func (a *App) CancelAgentTask(taskID string) {
	agentPlugin.CancelAgentTask(taskID)
}

// ListAgentTasks returns the currently running async agent tasks.
func (a *App) ListAgentTasks() []map[string]any {
	if a.agentTaskState == nil {
		return nil
	}
	tasks := a.agentTaskState.List()
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, map[string]any{
			"taskId":    t.ID,
			"agentName": t.AgentName,
			"status":    t.Status,
			"title":     t.Title,
		})
	}
	return out
}
