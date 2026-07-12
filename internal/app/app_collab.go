//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/collab"
	"everevo/internal/memory"
	"everevo/internal/storage"
	"everevo/internal/tools"
)

// ─── Collaboration API ────────────────────────────────────────────

// a2aRemoteAdapter adapts the A2A manager to collab.RemoteSender, so
// agent_message / collab_dispatch can reach REMOTE agents over HTTP. The
// agentID is resolved against registered remote agents; secret is unused
// (the A2A client holds its own configured secret).
type a2aRemoteAdapter struct{ a *App }

func (r a2aRemoteAdapter) Send(ctx context.Context, agentID, _, task string) (string, error) {
	if r.a.a2aManager == nil {
		return "", fmt.Errorf("A2A 管理器未就绪")
	}
	t, err := r.a.a2aManager.SendTask(agentID, task)
	if err != nil {
		return "", err
	}
	// Extract the final text artifact.
	for i := len(t.Artifacts) - 1; i >= 0; i-- {
		for _, p := range t.Artifacts[i].Parts {
			if p.Kind == "text" && p.Text != "" {
				return p.Text, nil
			}
		}
	}
	return "", nil
}

// agentRunnerAdapter bridges collab.Dispatcher → App.runAgentLoop. Implements
// collab.AgentRunner so the collab package can execute local agents without
// importing the app package (avoids an import cycle).
type agentRunnerAdapter struct{ a *App }

func (ar agentRunnerAdapter) RunAgent(ctx context.Context, agentID, task, collabSessionID string) (string, error) {
	app := ar.a
	if app.agentManager == nil {
		return "", fmt.Errorf("agent manager not ready")
	}
	agent, err := app.agentManager.Get(agentID)
	if err != nil {
		// Try name lookup as a fallback.
		agent, err = app.agentManager.FindByName(agentID)
		if err != nil {
			return "", fmt.Errorf("agent %q not found", agentID)
		}
	}
	// Derive a deadline from the caller's context (default 5 min).
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return app.runAgentLoopCollab(ctx, agent, task, collabSessionID)
}

// ─── Sessions ─────────────────────────────────────────────────────

// CollabSession is the API-facing shape of a collaboration session.
type CollabSession struct {
	ID             string             `json:"id"`
	Goal           string             `json:"goal"`
	OrchestratorID string             `json:"orchestratorId"`
	Status         string             `json:"status"`
	Members        []collab.Member    `json:"members"`
	BlackboardID   string             `json:"blackboardId"`
	CreatedAt      time.Time          `json:"createdAt"`
}

// CollabCreate starts a new multi-agent collaboration session.
// members is a list of {agentId, role}. A blackboard is allocated for shared state.
func (a *App) CollabCreate(goal, orchestratorID string, members []collab.Member) (*CollabSession, error) {
	if a.collab == nil {
		return nil, fmt.Errorf("协同内核未就绪")
	}
	if orchestratorID == "" {
		return nil, fmt.Errorf("orchestratorId 不能为空")
	}
	id := "collab_" + uuid.NewString()[:12]
	s := a.collab.Sessions.Create(id, goal, orchestratorID, members)
	a.persistCollabSession(s)
	return toCollabSession(s), nil
}

// CollabListSessions returns all active collaboration sessions (live + persisted).
func (a *App) CollabListSessions() []CollabSession {
	if a.collab == nil {
		return []CollabSession{}
	}
	out := []CollabSession{}
	for _, s := range a.collab.Sessions.List() {
		out = append(out, *toCollabSession(s))
	}
	return out
}

// CollabComplete finishes a session and drops its blackboard.
func (a *App) CollabComplete(sessionID string) error {
	if a.collab == nil {
		return fmt.Errorf("协同内核未就绪")
	}
	if a.collab.Sessions.Get(sessionID) == nil {
		return fmt.Errorf("协同会话 %q 不存在", sessionID)
	}
	a.collab.Sessions.Complete(sessionID)
	// Clean up blackboard entries from SQLite.
	if a.memoryStore != nil {
		boardID := "bb_" + sessionID
		_ = a.memoryStore.BBClearBoard(boardID)
	}
	// Persist the terminal status.
	if a.memoryStore != nil {
		if s := a.collab.Sessions.Get(sessionID); s != nil {
			a.persistCollabSession(s)
		}
	}
	return nil
}

// persistCollabSession saves a session to SQLite (best-effort).
func (a *App) persistCollabSession(s *collab.Session) {
	if a.memoryStore == nil || s == nil {
		return
	}
	row := memory.CollabSessionRow{
		ID: s.ID, Goal: s.Goal, OrchestratorID: s.OrchestratorID,
		BlackboardID: s.BlackboardID, Status: s.Status,
		CreatedAt: s.CreatedAt.UnixMilli(), UpdatedAt: s.UpdatedAt.UnixMilli(),
	}
	for _, m := range s.Members {
		row.Members = append(row.Members, memory.CollabMemberRow{
			AgentID: m.AgentID, Role: m.Role, JoinedAt: m.JoinedAt.UnixMilli(),
		})
	}
	if err := a.memoryStore.SaveCollabSession(row); err != nil {
		log.Printf("[collab] persist session %s failed: %v", s.ID, err)
	}
}

// ─── Blackboard ───────────────────────────────────────────────────

// CollabBbSet writes a key to a session's blackboard.
func (a *App) CollabBbSet(sessionID, key, value, author, kind string) (bool, error) {
	bb := a.sessionBoard(sessionID)
	if bb == nil {
		return false, fmt.Errorf("协同会话 %q 黑板不存在", sessionID)
	}
	return bb.Set(key, value, author, kind), nil
}

// CollabBbGet reads one key.
func (a *App) CollabBbGet(sessionID, key string) (collab.Entry, bool, error) {
	bb := a.sessionBoard(sessionID)
	if bb == nil {
		return collab.Entry{}, false, fmt.Errorf("黑板不存在")
	}
	e, ok := bb.Get(key)
	return e, ok, nil
}

// CollabBbList returns all blackboard entries.
func (a *App) CollabBbList(sessionID string) ([]collab.Entry, error) {
	bb := a.sessionBoard(sessionID)
	if bb == nil {
		return nil, fmt.Errorf("黑板不存在")
	}
	return bb.List(), nil
}

func (a *App) sessionBoard(sessionID string) *collab.Blackboard {
	if a.collab == nil {
		return nil
	}
	s := a.collab.Sessions.Get(sessionID)
	if s == nil {
		return nil
	}
	return a.collab.Blackboard(s.BlackboardID)
}

// ─── Dispatch (local A2A) ─────────────────────────────────────────

// CollabSend delivers a task to an agent (local in-process, or remote via A2A).
// Blocks until the target agent finishes and returns its text.
func (a *App) CollabSend(targetAgentID, task string) (string, error) {
	if a.collab == nil || a.collab.Dispatch == nil {
		return "", fmt.Errorf("协同内核未就绪")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return a.collab.Dispatch.Send(ctx, targetAgentID, task, "")
}

// CollabDispatchAsync starts an agent task in the background and returns a
// run ID immediately. Use CollabWait to gather results.
func (a *App) CollabDispatchAsync(sessionID, agentID, task string) (string, error) {
	if a.collab == nil || a.collab.Dispatch == nil {
		return "", fmt.Errorf("协同内核未就绪")
	}
	runID := "run_" + uuid.NewString()[:12]
	ctx, cancel := context.WithCancel(context.Background())
	r := &collab.AgentRun{
		ID: runID, AgentID: agentID, CollabSessionID: sessionID, Task: task,
		Status: collab.StatusRunning, StartedAt: time.Now(),
		Done: make(chan struct{}),
	}
	r.SetCancel(cancel)
	if !a.collab.RegisterRun(r) {
		cancel()
		return "", fmt.Errorf("并发 agent 运行数已达上限")
	}
	go func() {
		// Surface the run start (task text) so the workbench can show what the
		// agent is doing now, not just that it is busy.
		a.collab.Bus.Publish("agent."+agentID+".start", collab.Event{
			Source: agentID, Type: "start",
			Payload: map[string]any{"task": task, "runId": runID, "sessionId": sessionID},
		})
		text, err := a.collab.Dispatch.Send(ctx, agentID, task, sessionID)
		a.collab.CompleteRun(runID, text, err)
	}()
	return runID, nil
}

// CollabWait blocks until all given run IDs complete, then returns their results.
func (a *App) CollabWait(runIDs []string) ([]map[string]any, error) {
	if a.collab == nil {
		return nil, fmt.Errorf("协同内核未就绪")
	}
	out := make([]map[string]any, 0, len(runIDs))
	for _, rid := range runIDs {
		r := a.collab.Run(rid)
		if r == nil {
			out = append(out, map[string]any{"runId": rid, "error": "未知 run"})
			continue
		}
		select {
		case <-r.Done:
		case <-time.After(5 * time.Minute):
			out = append(out, map[string]any{"runId": rid, "error": "等待超时"})
			continue
		}
		entry := map[string]any{"runId": rid, "agentId": r.AgentID, "status": r.Status}
		if r.Err != nil {
			entry["error"] = r.Err.Error()
		} else {
			entry["result"] = r.Result
		}
		out = append(out, entry)
	}
	return out, nil
}

// ─── Tool handlers (LLM-callable) ─────────────────────────────────

func hCollabCreate(a *App, p map[string]any) tools.ToolResult {
	goal := tools.GetString(p, "goal")
	orch := tools.GetString(p, "orchestratorId")
	var members []collab.Member
	if raw, ok := p["members"].([]any); ok {
		for _, m := range raw {
			if mm, ok := m.(map[string]any); ok {
				role, _ := mm["role"].(string)
				if role == "" {
					role = "member"
				}
				members = append(members, collab.Member{
					AgentID: tools.GetString(mm, "agentId"), Role: role,
				})
			}
		}
	}
	s, err := a.CollabCreate(goal, orch, members)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"sessionId": s.ID, "blackboardId": s.BlackboardID,
		"message": "协同会话已创建，使用 blackboard_set/collab_dispatch 协作",
	})
}

func hCollabDispatch(a *App, p map[string]any) tools.ToolResult {
	target := tools.GetString(p, "targetAgentId")
	task := tools.GetString(p, "task")
	text, err := a.CollabSend(target, task)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"agentId": target, "result": text})
}

func hCollabDispatchAsync(a *App, p map[string]any) tools.ToolResult {
	sessionID := tools.GetString(p, "sessionId")
	target := tools.GetString(p, "targetAgentId")
	task := tools.GetString(p, "task")
	runID, err := a.CollabDispatchAsync(sessionID, target, task)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"runId": runID, "message": "已异步派发，用 collab_wait 收集"})
}

func hCollabWait(a *App, p map[string]any) tools.ToolResult {
	ids := tools.GetStringSlice(p, "runIds")
	results, err := a.CollabWait(ids)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(results)
}

func hBlackboardSet(a *App, p map[string]any) tools.ToolResult {
	sessionID := tools.GetString(p, "sessionId")
	key := tools.GetString(p, "key")
	value := tools.GetString(p, "value")
	kind := tools.GetString(p, "kind")
	author := tools.GetString(p, "_author")
	ok, err := a.CollabBbSet(sessionID, key, value, author, kind)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"ok": ok})
}

func hBlackboardGet(a *App, p map[string]any) tools.ToolResult {
	sessionID := tools.GetString(p, "sessionId")
	key := tools.GetString(p, "key")
	e, ok, err := a.CollabBbGet(sessionID, key)
	if err != nil {
		return tools.ErrResult(err)
	}
	if !ok {
		return tools.OkResult(map[string]any{"found": false})
	}
	return tools.OkResult(map[string]any{"found": true, "entry": e})
}

func hBlackboardList(a *App, p map[string]any) tools.ToolResult {
	sessionID := tools.GetString(p, "sessionId")
	entries, err := a.CollabBbList(sessionID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(entries)
}

func hAgentMessage(a *App, p map[string]any) tools.ToolResult {
	target := tools.GetString(p, "targetAgentId")
	msg := tools.GetString(p, "message")
	text, err := a.CollabSend(target, msg)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"agentId": target, "reply": text})
}

// ─── Plans ────────────────────────────────────────────────────────

// PlanCreate makes a new task plan (goal → ordered steps).
func (a *App) PlanCreate(goal string, steps []string, author string) (*collab.Plan, error) {
	if a.collab == nil {
		return nil, fmt.Errorf("协同内核未就绪")
	}
	if goal == "" || len(steps) == 0 {
		return nil, fmt.Errorf("goal 和 steps 不能为空")
	}
	id := "plan_" + uuid.NewString()[:12]
	p := a.collab.Plans.Create(id, goal, "", author, steps)
	a.persistPlans()
	return p, nil
}

// PlanStepUpdate advances a step's status.
func (a *App) PlanStepUpdate(planID string, stepIndex int, status, note, agentID string) error {
	if a.collab == nil {
		return fmt.Errorf("协同内核未就绪")
	}
	if !a.collab.Plans.UpdateStep(planID, stepIndex, status, note, agentID) {
		return fmt.Errorf("计划 %q 步骤 %d 不存在", planID, stepIndex)
	}
	a.persistPlans()
	return nil
}

// plansPath returns the JSON file path for plan persistence (under appData).
func (a *App) plansPath() string {
	dir, err := storage.AppDataDir()
	if err != nil {
		return "plans.json"
	}
	return filepath.Join(dir, "plans.json")
}

// persistPlans saves active plans to disk (best-effort, non-blocking).
func (a *App) persistPlans() {
	if a.collab == nil {
		return
	}
	go func() {
		if err := a.collab.Plans.PersistTo(a.plansPath()); err != nil {
			log.Printf("[collab] persist plans failed: %v", err)
		}
	}()
}

// PlanList returns all plans.
func (a *App) PlanList() []collab.Plan {
	if a.collab == nil {
		return []collab.Plan{}
	}
	live := a.collab.Plans.List()
	out := make([]collab.Plan, 0, len(live))
	for _, p := range live {
		out = append(out, *p)
	}
	return out
}

// hCollabListSessions is the LLM-callable handler for listing collaboration sessions.
func hCollabListSessions(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.CollabListSessions())
}

// hCollabComplete ends a collaboration session.
func hCollabComplete(a *App, p map[string]any) tools.ToolResult {
	sessionID := tools.GetString(p, "sessionId")
	if err := a.CollabComplete(sessionID); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"ok": true, "message": "协同会话已结束"})
}

// ─── Plan tool handlers ───────────────────────────────────────────

func hPlanCreate(a *App, p map[string]any) tools.ToolResult {
	goal := tools.GetString(p, "goal")
	steps := tools.GetStringSlice(p, "steps")
	author := tools.GetString(p, "_author")
	plan, err := a.PlanCreate(goal, steps, author)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"planId": plan.ID, "stepCount": len(plan.Steps),
		"message": "计划已创建，用 plan_step_update 逐步推进",
	})
}

func hPlanStepUpdate(a *App, p map[string]any) tools.ToolResult {
	planID := tools.GetString(p, "planId")
	idx := tools.GetInt(p, "stepIndex")
	status := tools.GetString(p, "status")
	note := tools.GetString(p, "note")
	author := tools.GetString(p, "_author")
	if err := a.PlanStepUpdate(planID, idx, status, note, author); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"ok": true})
}

func hPlanList(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.PlanList())
}

func toCollabSession(s *collab.Session) *CollabSession {
	if s == nil {
		return nil
	}
	return &CollabSession{
		ID: s.ID, Goal: s.Goal, OrchestratorID: s.OrchestratorID,
		Status: s.Status, Members: s.Members, BlackboardID: s.BlackboardID,
		CreatedAt: s.CreatedAt,
	}
}

// ─── Unified AI-work activity log ──────────────────────────────────
//
// Every collab event (agent/plan/blackboard/session) and every bridged workflow
// event (wf-*) flows through ONE recordActivity() chokepoint into a background
// SQLite writer. The same events mirror to the frontend as 'collab:event', so
// the workbench + history view share a single source of truth. The timeline is
// queryable via ListActivity for replay ("查整个流程").

// activityQueueCap bounds the in-flight write queue. When saturated the oldest
// entry is dropped (keep-recent) so a flood of events can't block the event bus
// on disk IO. Best-effort persistence (documented in changelog).
const activityQueueCap = 1024

// recordActivity maps one event to a timeline row and queues it for the writer.
// Called from the collab bus forward callback (app.go) and the workflow event
// bridge (app_workflow.go). No-op until the queue is allocated at startup.
func (a *App) recordActivity(topic string, ev collab.Event) {
	if a.activityQueue == nil {
		return
	}
	row := a.mapEventToActivity(topic, ev)
	select {
	case a.activityQueue <- row:
	default:
		// saturated — drop the oldest to make room, keep recent
		select {
		case <-a.activityQueue:
		default:
		}
		select {
		case a.activityQueue <- row:
		default:
			log.Printf("[activity] queue saturated, dropped event topic=%s", topic)
		}
	}
}

// runActivityWriter drains the queue into SQLite in a single goroutine, started
// once after the memory store is ready. Exits when the queue channel is closed.
func (a *App) runActivityWriter() {
	for r := range a.activityQueue {
		if a.memoryStore == nil {
			continue
		}
		if err := a.memoryStore.LogActivity(r); err != nil {
			log.Printf("[activity] log write failed: %v", err)
		}
	}
}

// agentDisplayName resolves an agentID to its human Name (Get → FindByName →
// fallback ID) so cards/history show names, not opaque IDs.
func (a *App) agentDisplayName(agentID string) string {
	if agentID == "" || a.agentManager == nil {
		return agentID
	}
	if ag, err := a.agentManager.Get(agentID); err == nil && ag.Name != "" {
		return ag.Name
	}
	if ag, err := a.agentManager.FindByName(agentID); err == nil && ag.Name != "" {
		return ag.Name
	}
	return agentID
}

// bridgeWorkflowEvent funnels a workflow engine event (originally emitted
// straight to Wails as wf-*) into the same log + 'collab:event' stream as collab
// events, so the workbench needs only one subscription.
func (a *App) bridgeWorkflowEvent(topic string, data map[string]any) {
	src, _ := data["execId"].(string)
	ev := collab.Event{Topic: topic, Source: src, Type: topic, Payload: data, At: time.Now()}
	a.recordActivity(topic, ev)
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "collab:event", map[string]any{"topic": topic, "data": ev})
	}
}

// mapEventToActivity converts a collab/workflow event into a timeline row.
func (a *App) mapEventToActivity(topic string, ev collab.Event) memory.ActivityRow {
	src := ev.Source
	sourceName := a.resolveSourceName(topic, src, ev.Payload)
	row := memory.ActivityRow{
		Topic:      topic,
		Kind:       kindOf(topic, ev.Type),
		Source:     src,
		SourceName: sourceName,
		SessionID:  sessionOf(ev.Payload),
		Summary:    summarize(topic, ev.Type, ev.Payload, sourceName),
	}
	if !ev.At.IsZero() {
		row.Ts = ev.At.UnixMilli()
	}
	if ev.Payload != nil {
		if b, err := json.Marshal(ev.Payload); err == nil {
			row.Payload = string(b)
		}
	}
	return row
}

// kindOf maps a raw topic (+ finer type) to a timeline kind.
func kindOf(topic, typ string) string {
	switch {
	case topic == "collab.ready":
		return "system"
	case strings.HasPrefix(topic, "agent."):
		switch typ {
		case "start":
			return "agent_start"
		case "done":
			return "agent_done"
		case "message":
			return "agent_message"
		default:
			return "agent"
		}
	case strings.HasPrefix(topic, "tool."):
		return "tool_call"
	case strings.HasPrefix(topic, "plan."):
		return "plan"
	case strings.HasPrefix(topic, "blackboard."):
		return "blackboard"
	case strings.HasPrefix(topic, "collab."):
		return "session"
	case topic == "wf-exec-start":
		return "workflow_start"
	case strings.HasPrefix(topic, "wf-exec-"):
		return "workflow_done"
	case strings.HasPrefix(topic, "wf-node-"), strings.HasPrefix(topic, "workflow-progress-"):
		return "workflow_node"
	default:
		return "other"
	}
}

func (a *App) resolveSourceName(topic, src string, payload any) string {
	if strings.HasPrefix(topic, "wf-") || strings.HasPrefix(topic, "workflow-") {
		if n := str(asMap(payload)["workflowName"]); n != "" {
			return n
		}
		return src
	}
	if strings.HasPrefix(topic, "agent.") || strings.HasPrefix(topic, "tool.") {
		return a.agentDisplayName(src)
	}
	return src
}

func sessionOf(payload any) string {
	m := asMap(payload)
	for _, k := range []string{"sessionId", "collabSessionId", "session_id"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func summarize(topic, typ string, payload any, sourceName string) string {
	m := asMap(payload)
	switch {
	case strings.HasPrefix(topic, "agent."):
		switch typ {
		case "start":
			return "开始：" + trunc(str(m["task"]), 60)
		case "message":
			return sourceName + " → " + str(m["to"]) + "：" + trunc(str(m["text"]), 60)
		case "done":
			if r := str(m["result"]); r != "" {
				return "完成：" + trunc(r, 60)
			}
			return "完成"
		default:
			return typ
		}
	case strings.HasPrefix(topic, "tool."):
		return "调用工具 " + str(m["tool"])
	case strings.HasPrefix(topic, "plan."):
		if i, ok := m["index"]; ok {
			return fmt.Sprintf("步骤 %v → %s", i, str(m["status"]))
		}
		return "计划：" + trunc(str(m["goal"]), 50)
	case strings.HasPrefix(topic, "blackboard."):
		return "写黑板 " + str(m["key"]) + " = " + trunc(str(m["value"]), 40)
	case strings.HasPrefix(topic, "collab."):
		return "协同：" + trunc(str(m["goal"]), 50)
	case topic == "wf-exec-start":
		return "运行工作流：" + str(m["workflowName"])
	case strings.HasPrefix(topic, "wf-node-"):
		return str(m["title"]) + " " + strings.TrimPrefix(topic, "wf-node-")
	case strings.HasPrefix(topic, "wf-exec-"):
		return "工作流 " + str(m["execId"]) + " " + strings.TrimPrefix(topic, "wf-exec-")
	default:
		return topic
	}
}

// asMap normalizes an arbitrary payload (struct/map/scalar) to a map for field
// extraction. Re-marshals through JSON when needed.
func asMap(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ListActivity is the frontend binding: returns the unified AI-work timeline,
// newest-first, optionally filtered (for the history/replay view).
func (a *App) ListActivity(kind, sessionId, source string, since, limit int) ([]memory.ActivityRow, error) {
	if a.memoryStore == nil {
		return []memory.ActivityRow{}, nil
	}
	return a.memoryStore.ListActivity(memory.ActivityFilter{
		Kind: kind, SessionID: sessionId, Source: source,
		Since: int64(since), Limit: limit,
	})
}
