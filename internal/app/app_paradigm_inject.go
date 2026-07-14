//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"everevo/internal/memory"
)

// injectParadigmRecommendation appends a compact list of recommended paradigms
// to the system message. The LLM picks the best match, then calls the
// `paradigm_select` tool to load the full methodology.
//
// This is the SELECT step of SELECT-ADAPT-EXECUTE:
//
//	SELECT: LLM reads compact list → picks best paradigm → calls paradigm_select(id)
//	ADAPT:  paradigm_select returns full methodology → LLM adapts it to the task
//	EXECUTE: LLM executes the task following the methodology → appends @paradigm marker
func (a *App) injectParadigmRecommendation(messagesJSON json.RawMessage) json.RawMessage {
	if a.paradigmManager == nil {
		return messagesJSON
	}

	var msgs []map[string]any
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return messagesJSON
	}

	// Find the last user message as the task description
	var task string
	for i := len(msgs) - 1; i >= 0; i-- {
		if role, _ := msgs[i]["role"].(string); role == "user" {
			if content, ok := msgs[i]["content"].(string); ok {
				task = content
			}
			break
		}
	}
	if task == "" {
		return messagesJSON
	}

	// Get top 5 recommended paradigms (compact overview, not full methodology)
	recs := a.paradigmManager.Recommend(task, 5)
	if len(recs) == 0 {
		return messagesJSON
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\n## 🧠 可用思维范式\n\n")
	sb.WriteString("以下是匹配当前任务的范式列表。**选择一个最合适的，调用 `paradigm_select` 工具加载完整方法论**，然后按其步骤执行。\n\n")

	sb.WriteString("| # | 范式 | 适用场景 | 匹配度 |\n")
	sb.WriteString("|---|------|---------|--------|\n")
	for i, p := range recs {
		sb.WriteString(fmt.Sprintf("| %d | %s **%s** | %s | %.0f%% |\n",
			i+1, p.Icon, p.Name,
			paradigmSummary(summarizeApplicable(p), 60),
			p.Strength*100,
		))
	}

	sb.WriteString("\n**操作指南**：\n")
	sb.WriteString("1. 从表中选择最合适的范式\n")
	sb.WriteString("2. 调用 `paradigm_select` 工具，传入该范式的 ID 加载完整方法论\n")
	sb.WriteString("3. 严格按方法论步骤执行任务\n")
	sb.WriteString("4. 任务完成后调用 `paradigm_feedback` 提交效果反馈\n")
	sb.WriteString("5. 回复末尾附加：`@paradigm{\"id\":\"范式ID\",\"name\":\"范式名称\"}`\n")

	sb.WriteString("\n范式 ID 参考：\n")
	for _, p := range recs {
		sb.WriteString(fmt.Sprintf("- `%s` = %s %s\n", p.ID, p.Icon, p.Name))
	}

	// Inject into system message
	for i, m := range msgs {
		if role, _ := m["role"].(string); role == "system" {
			if content, ok := m["content"].(string); ok {
				msgs[i]["content"] = content + sb.String()
			}
			result, err := json.Marshal(msgs)
			if err != nil {
				return messagesJSON
			}
			return json.RawMessage(result)
		}
	}

	// No system message — prepend one
	sysMsg := map[string]any{"role": "system", "content": sb.String()}
	msgs = append([]map[string]any{sysMsg}, msgs...)
	result, err := json.Marshal(msgs)
	if err != nil {
		return messagesJSON
	}
	return json.RawMessage(result)
}

// injectParadigmForce enforces a specific paradigm by injecting its FULL
// methodology directly into the system prompt (no tool call needed — the
// LLM doesn't get to choose). Used when the user locks a paradigm via
// ParadigmSetForce(id).
func (a *App) injectParadigmForce(messagesJSON json.RawMessage, paradigmID string) json.RawMessage {
	if a.paradigmManager == nil {
		return messagesJSON
	}

	p, err := a.paradigmManager.Get(paradigmID)
	if err != nil || p == nil {
		return messagesJSON
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\n## 🧠 强制思维范式：")
	sb.WriteString(p.Icon)
	sb.WriteString(" ")
	sb.WriteString(p.Name)
	sb.WriteString("\n\n")
	sb.WriteString("⚠️ 你必须严格按以下方法论执行本次任务，不可跳过任何步骤。\n\n")
	sb.WriteString(p.Methodology)
	sb.WriteString("\n\n**强制要求**：回复末尾必须附加标记：")
	sb.WriteString("\n`@paradigm{\"id\":\"")
	sb.WriteString(p.ID)
	sb.WriteString("\",\"name\":\"")
	sb.WriteString(p.Name)
	sb.WriteString("\"}`")

	var msgs []map[string]any
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return messagesJSON
	}
	for i, m := range msgs {
		if role, _ := m["role"].(string); role == "system" {
			if content, ok := m["content"].(string); ok {
				msgs[i]["content"] = content + sb.String()
			}
			result, _ := json.Marshal(msgs)
			return json.RawMessage(result)
		}
	}
	sysMsg := map[string]any{"role": "system", "content": sb.String()}
	msgs = append([]map[string]any{sysMsg}, msgs...)
	result, _ := json.Marshal(msgs)
	return json.RawMessage(result)
}

func paradigmSummary(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func summarizeApplicable(p memory.Paradigm) string {
	// Use the first line or sentence of the applicable field
	s := p.Applicable
	if idx := strings.IndexAny(s, "\n。；;"); idx > 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
