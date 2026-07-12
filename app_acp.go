//go:build windows

package main

import (
	"context"
	"time"
)

// ─── ACP Bridge — OpenCode delegation for code modification tasks ───

// AcpCheck verifies that OpenCode is available on this system.
func (a *App) AcpCheck() map[string]any {
	if a.acpBridge == nil {
		return map[string]any{"ok": false, "error": "ACP 桥接未初始化"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if err := a.acpBridge.Check(ctx); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	ver, _ := a.acpBridge.Version()
	return map[string]any{"ok": true, "version": ver}
}

// AcpRun delegates a natural-language task to OpenCode in the project directory
// and returns structured results (text, tool calls, token usage, cost).
func (a *App) AcpRun(message string) map[string]any {
	if a.acpBridge == nil {
		return map[string]any{"ok": false, "error": "ACP 桥接未初始化"}
	}
	if a.sourceDir == "" {
		return map[string]any{"ok": false, "error": "源码目录未找到，无法执行 ACP 任务"}
	}

	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()

	result, err := a.acpBridge.Run(ctx, a.sourceDir, message, nil)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}

	return map[string]any{
		"ok":        result.Error == "",
		"text":      result.Text,
		"sessionId": result.SessionID,
		"toolCalls": result.ToolCalls,
		"snapshots": result.Snapshots,
		"tokens": map[string]int{
			"total":     result.Tokens.Total,
			"input":     result.Tokens.Input,
			"output":    result.Tokens.Output,
			"reasoning": result.Tokens.Reasoning,
		},
		"cost":     result.Cost,
		"duration": result.Duration.String(),
		"error":    result.Error,
	}
}
