//go:build windows

package app

import (
	"encoding/json"

	"everevo/internal/memory"
)

// ClassifyThinkLang returns the thinking language rule for a user query.
// Called by the frontend to embed the rule into the system prompt per turn.
func (a *App) ClassifyThinkLang(query string) map[string]any {
	tl := memory.ClassifyThinkLang(query)
	return map[string]any{
		"anchor":     tl.Anchor,
		"confidence": tl.Confidence,
		"rule":       tl.Rule,
	}
}

// injectThinkLang appends the thinking language control rule to the system
// message. Used by ChatStream/ChatProxy/ChatStreamAs as a backend fallback.
func (a *App) injectThinkLang(messagesJSON json.RawMessage) json.RawMessage {
	var msgs []map[string]any
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return messagesJSON
	}

	var lastUserContent string
	for i := len(msgs) - 1; i >= 0; i-- {
		if role, _ := msgs[i]["role"].(string); role == "user" {
			if content, ok := msgs[i]["content"].(string); ok {
				lastUserContent = content
			}
			break
		}
	}

	if lastUserContent == "" {
		return messagesJSON
	}

	tl := memory.ClassifyThinkLang(lastUserContent)
	if tl.Rule == "" {
		return messagesJSON
	}

	for i, m := range msgs {
		if role, _ := m["role"].(string); role == "system" {
			if content, ok := m["content"].(string); ok {
				msgs[i]["content"] = content + "\n\n" + tl.Rule
			}
			result, err := json.Marshal(msgs)
			if err != nil {
				return messagesJSON
			}
			return json.RawMessage(result)
		}
	}

	sysMsg := map[string]any{"role": "system", "content": tl.Rule}
	msgs = append([]map[string]any{sysMsg}, msgs...)
	result, err := json.Marshal(msgs)
	if err != nil {
		return messagesJSON
	}
	return json.RawMessage(result)
}
