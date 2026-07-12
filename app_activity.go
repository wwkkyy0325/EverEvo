//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"everevo/internal/collab"
	"everevo/internal/memory"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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
