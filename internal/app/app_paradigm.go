//go:build windows

package app

import (
	"fmt"

	"everevo/internal/memory"
)

// ─── Paradigm Library (P10) — Wails-exposed API ────────────────────────

// ParadigmList returns all paradigms, optionally filtered by library.
func (a *App) ParadigmList(libraryID string) ([]memory.Paradigm, error) {
	if a.paradigmManager == nil {
		return []memory.Paradigm{}, nil
	}
	return a.paradigmManager.ListEnabled(libraryID), nil
}

// ParadigmGet returns a single paradigm by ID.
func (a *App) ParadigmGet(id string) (*memory.Paradigm, error) {
	if a.paradigmManager == nil {
		return nil, fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.Get(id)
}

// ParadigmAdd adds a new paradigm manually.
func (a *App) ParadigmAdd(p memory.Paradigm) (*memory.Paradigm, error) {
	if a.paradigmManager == nil {
		return nil, fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.Add(p)
}

// ParadigmUpdate modifies an existing paradigm.
func (a *App) ParadigmUpdate(id string, p memory.Paradigm) error {
	if a.paradigmManager == nil {
		return fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.Update(id, p)
}

// ParadigmDelete removes or disables a paradigm.
func (a *App) ParadigmDelete(id string) error {
	if a.paradigmManager == nil {
		return fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.Delete(id)
}

// ParadigmToggle enables or disables a paradigm.
func (a *App) ParadigmToggle(id string, enabled bool) error {
	if a.paradigmManager == nil {
		return fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.SetEnabled(id, enabled)
}

// ParadigmFeedback records structured 3D feedback for a paradigm.
// matchQuality: was this the right paradigm for the task? (0-1)
// executionQuality: was it applied well? (0-1)
// outcomeQuality: was the result useful? (0-1)
// reason: textual explanation — the critical input for evolution.
//   Low matchQuality → maybe wrong paradigm for task type
//   Low executionQuality → methodology needs refinement
//   Low outcomeQuality → paradigm itself may be flawed
func (a *App) ParadigmFeedback(id string, matchQuality, executionQuality, outcomeQuality float64, reason string) error {
	if a.paradigmManager == nil {
		return fmt.Errorf("范式库未就绪")
	}
	return a.paradigmManager.Feedback(id, matchQuality, executionQuality, outcomeQuality, reason)
}

// ParadigmSelect returns the methodology prompt for a paradigm (for AI injection).
// Appends an instruction to include a @paradigm JSON marker at the end of the
// response for traceability and feedback collection.
//
// Design follows UpFormat (Gomadam et al., 2025) — coordination metadata
// is embedded in the response payload, decoupled from the content.
func (a *App) ParadigmSelect(id string) (string, error) {
	if a.paradigmManager == nil {
		return "", fmt.Errorf("范式库未就绪")
	}
	p, err := a.paradigmManager.Get(id)
	if err != nil {
		return "", err
	}
	marker := fmt.Sprintf(
		"\n\n---\n**范式标记**：回复末尾附加一行：\n`@paradigm{\"id\":\"%s\",\"name\":\"%s\",\"match\":0.0,\"exec\":0.0,\"outcome\":0.0}`\n"+
			"match=该范式对任务的匹配度 exec=执行到位度 outcome=结果有用度，均为0-1。用于统计和优化范式库。",
		p.ID, p.Name)
	return p.Methodology + marker, nil
}

// ParadigmRefine analyzes feedback history for a paradigm and proposes
// methodology improvements using the LLM (GEPA-style reflective evolution).
// Returns the proposed new methodology text for user review.
func (a *App) ParadigmRefine(id string) (map[string]any, error) {
	if a.paradigmManager == nil {
		return nil, fmt.Errorf("范式库未就绪")
	}
	p, err := a.paradigmManager.Get(id)
	if err != nil {
		return nil, err
	}
	// Collect recent feedback entries for this paradigm.
	history := a.paradigmManager.FeedbackHistory(id, 10)
	if len(history) == 0 {
		return map[string]any{"proposal": "", "reason": "暂无反馈数据，无法生成改进建议"}, nil
	}
	// Build a reflection prompt summarizing feedback patterns.
	var lowMatch, lowExec, lowOutcome []string
	for _, f := range history {
		if f.MatchQuality < 0.5 {
			lowMatch = append(lowMatch, fmt.Sprintf("- [匹配%.0f%%] %s", f.MatchQuality*100, f.Reason))
		}
		if f.ExecutionQuality < 0.5 {
			lowExec = append(lowExec, fmt.Sprintf("- [执行%.0f%%] %s", f.ExecutionQuality*100, f.Reason))
		}
		if f.OutcomeQuality < 0.5 {
			lowOutcome = append(lowOutcome, fmt.Sprintf("- [结果%.0f%%] %s", f.OutcomeQuality*100, f.Reason))
		}
	}
	summary := fmt.Sprintf(
		"范式「%s」最近 %d 次反馈分析：\n\n"+
			"**匹配问题**（范式与任务类型不匹配）：\n%s\n\n"+
			"**执行问题**（方法论执行不到位）：\n%s\n\n"+
			"**结果问题**（最终输出质量不足）：\n%s\n\n"+
			"基于以上反馈，对当前方法论提出具体、可执行的改进建议。重点优化执行问题，考虑为匹配问题添加适用/不适用的场景说明。",
		p.Name, len(history),
		joinOrNone(lowMatch), joinOrNone(lowExec), joinOrNone(lowOutcome))
	return map[string]any{
		"current":     p.Methodology,
		"proposal":    "", // caller (LLM tool) fills this
		"analysis":    summary,
		"historySize": len(history),
	}, nil
}

// ParadigmDistill prompts the LLM to analyze a conversation and extract
// thinking patterns as candidate paradigm definitions.
func (a *App) ParadigmDistill(conversationText, workspaceID string) (map[string]any, error) {
	if a.paradigmManager == nil {
		return nil, fmt.Errorf("范式库未就绪")
	}
	return map[string]any{
		"instruction": fmt.Sprintf(
			"分析以下对话内容，识别其中使用的思考范式和方法论。"+
				"对每个发现的范式，输出：name（名称）、category（analysis/decision/creative/debug/planning）、"+
				"description（一句话描述）、methodology（详细步骤）、applicable（适用场景）。"+
				"已有的范式列表：%s。只输出全新或实质不同的范式。"+
				"\n\n---\n%s",
			a.paradigmManager.ListNames(), conversationText),
		"existing": a.paradigmManager.ListNames(),
	}, nil
}

// ParadigmMatch recommends the best paradigm(s) for a given task description
// based on historical performance data (context-aware matching).
func (a *App) ParadigmMatch(taskDescription string) ([]memory.Paradigm, error) {
	if a.paradigmManager == nil {
		return []memory.Paradigm{}, nil
	}
	return a.paradigmManager.Recommend(taskDescription, 3), nil
}

// ─── Feedback history ──────────────────────────────────────────────────

// ParadigmFeedbackHistory returns recent feedback for a paradigm.
func (a *App) ParadigmFeedbackHistory(id string, limit int) ([]memory.FeedbackEntry, error) {
	if a.paradigmManager == nil {
		return []memory.FeedbackEntry{}, nil
	}
	return a.paradigmManager.FeedbackHistory(id, limit), nil
}

// ParadigmForceMode returns the current paradigm force mode.
// "" = auto-recommend, "<id>" = specific forced paradigm.
var paradigmForceMode string

// ParadigmSetForce sets or clears a forced paradigm. Pass "" to clear (auto mode).
func (a *App) ParadigmSetForce(paradigmID string) error {
	if paradigmID == "" {
		paradigmForceMode = ""
		return nil
	}
	if a.paradigmManager == nil {
		return fmt.Errorf("paradigm manager not ready")
	}
	if _, err := a.paradigmManager.Get(paradigmID); err != nil {
		return err
	}
	paradigmForceMode = paradigmID
	return nil
}

// ParadigmGetForce returns the currently forced paradigm ID or "".
func (a *App) ParadigmGetForce() string {
	return paradigmForceMode
}

// ParadigmForceInfo returns info about the forced paradigm for UI display.
func (a *App) ParadigmForceInfo() map[string]any {
	if paradigmForceMode == "" || a.paradigmManager == nil {
		return map[string]any{"enabled": false}
	}
	p, err := a.paradigmManager.Get(paradigmForceMode)
	if err != nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"enabled":    true,
		"id":         p.ID,
		"name":       p.Name,
		"icon":       p.Icon,
		"category":   p.Category,
		"strength":   p.Strength,
		"methodology": p.Methodology,
	}
}

func joinOrNone(ss []string) string {
	if len(ss) == 0 {
		return "（无）"
	}
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}
