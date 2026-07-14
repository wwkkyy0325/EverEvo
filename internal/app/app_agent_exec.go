//go:build windows

package app

import (
	"context"
	"fmt"

	"everevo/internal/agents"
	agentPlugin "everevo/internal/plugins/tools/agents"
)

// ─── Delegation wrappers ──────────────────────────────────────────────
//
// Agent execution logic lives in plugins/tools/agents/exec.go. These App
// methods are thin wrappers that call the plugin functions via the Deps
// wired at startup, preserving the Wails-facing public API surface
// (GetAgentChatContext, BuildDomainSystemPrompt) and the internal
// method signatures used by agent_delegate, app_collab, app_agent, and
// app_workflow.

// GetAgentChatContext delegates to the agents plugin.
func (a *App) GetAgentChatContext(id string) (*agentPlugin.AgentChatContext, error) {
	ctx, err := agentPlugin.GetAgentChatContext(id)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	return ctx, nil
}

// BuildDomainSystemPrompt delegates to the agents plugin.
func (a *App) BuildDomainSystemPrompt(domainId string) string {
	return agentPlugin.BuildDomainSystemPrompt(domainId)
}

// runAgentLoop delegates to the agents plugin.
func (a *App) runAgentLoop(ctx context.Context, agent *agents.Agent, userText string) (string, error) {
	return agentPlugin.RunAgentLoop(ctx, agent, userText)
}

// runAgentLoopCollab delegates to the agents plugin.
func (a *App) runAgentLoopCollab(ctx context.Context, agent *agents.Agent, userText, collabSessionID string) (string, error) {
	return agentPlugin.RunAgentLoopCollab(ctx, agent, userText, collabSessionID)
}
