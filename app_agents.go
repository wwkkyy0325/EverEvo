//go:build windows

package main

import (
	"fmt"

	"everevo/internal/agents"
)

// ─── Local Agent (persona) Management API ──────────────────────
//
// These bindings manage LOCAL agent personas (internal/agents) — named LLM
// profiles with a system prompt, optional model override, and a skill/tool
// subset. This is distinct from the A2A remote-agent bindings in app_agent.go.

// ListAgents returns all local agents.
func (a *App) ListAgents() []agents.Agent {
	if a.agentManager == nil {
		return []agents.Agent{}
	}
	return a.agentManager.List()
}

// GetAgent returns a single local agent by ID.
func (a *App) GetAgent(id string) (*agents.Agent, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	return a.agentManager.Get(id)
}

// CreateAgent creates a new local agent. LibraryID is required.
func (a *App) CreateAgent(agent agents.Agent) (*agents.Agent, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	if err := a.validateLibraryID(agent.LibraryID); err != nil {
		return nil, fmt.Errorf("创建 Agent 失败: %w", err)
	}
	created, err := a.agentManager.Create(agent)
	if err != nil {
		return nil, err
	}
	a.emitChanged("agents:changed", "create", created.ID)
	return created, nil
}

// UpdateAgent updates an existing local agent by ID.
func (a *App) UpdateAgent(id string, agent agents.Agent) (*agents.Agent, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	updated, err := a.agentManager.Update(id, agent)
	if err != nil {
		return nil, err
	}
	a.emitChanged("agents:changed", "update", id)
	return updated, nil
}

// DeleteAgent removes a local agent by ID. The default main agent cannot be deleted.
func (a *App) DeleteAgent(id string) error {
	if a.agentManager == nil {
		return fmt.Errorf("agent 管理器未初始化")
	}
	if err := a.agentManager.Delete(id); err != nil {
		return err
	}
	a.emitChanged("agents:changed", "delete", id)
	return nil
}

// ─── Library-scoped agent queries ─────────────────────────────────

// ListAgentsByLibrary returns agents belonging to the given domain library.
// When libraryID is empty, returns all agents (backward compatible).
func (a *App) ListAgentsByLibrary(libraryID string) []agents.Agent {
	if a.agentManager == nil {
		return []agents.Agent{}
	}
	if libraryID == "" {
		return a.agentManager.List()
	}
	return a.agentManager.ListByLibrary(libraryID)
}

// GetCoreAgent returns the default agent of the core domain (the orchestrator).
// Falls back to the global default agent if no core agent is configured.
func (a *App) GetCoreAgent() (*agents.Agent, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	if a.memoryStore == nil {
		// Fall back to any default agent
		list := a.agentManager.List()
		for i := range list {
			if list[i].IsDefault {
				return &list[i], nil
			}
		}
		return nil, fmt.Errorf("no default agent found")
	}
	libID, _ := a.memoryStore.DefaultLibrary()
	return a.agentManager.GetCoreAgent(libID)
}
