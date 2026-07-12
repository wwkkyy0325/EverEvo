//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"everevo/internal/a2a"
	"everevo/internal/config"
	"everevo/internal/skills"
	"everevo/internal/storage"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ─── A2A Agent API ──────────────────────────────────────────────

// GetA2AConfig returns the current A2A agent configuration.
func (a *App) GetA2AConfig() config.A2AAgentConfig {
	if a.cfg == nil {
		return config.A2AAgentConfig{}
	}
	return a.cfg.LLM.A2AConfig
}

// UpdateA2AConfig updates the A2A agent configuration and applies changes.
func (a *App) UpdateA2AConfig(cfg config.A2AAgentConfig) error {
	a.cfg.LLM.A2AConfig = cfg

	if err := config.Save(a.cfg); err != nil {
		return fmt.Errorf("保存A2A配置失败: %w", err)
	}

	// Update manager's agent card if manager exists
	if a.a2aManager != nil {
		card := a2a.AgentCard{
			Name:        cfg.Name,
			Description: cfg.Description,
			Version:     cfg.Version,
			Capabilities: a2a.AgentCapabilities{
				Streaming:              false,
				PushNotifications:      false,
				StateTransitionHistory: true,
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		}
		a.a2aManager.UpdateCard(card)
		// Secret applies on the next (re)start of the server.
		a.a2aManager.SetServerSecret(cfg.Secret)
	}

	a.emitChanged("agent:changed", "update", "config")
	return nil
}

// GetAgentServerStatus returns the A2A server status.
func (a *App) GetAgentServerStatus() a2a.ServerStatus {
	if a.a2aManager == nil {
		return a2a.ServerStatus{Running: false}
	}
	return a.a2aManager.ServerStatus()
}

// StartAgentServer starts the A2A agent server.
func (a *App) StartAgentServer() error {
	if a.a2aManager == nil {
		return fmt.Errorf("A2A管理器未初始化")
	}

	port := a.cfg.LLM.A2AConfig.Port
	if port <= 0 {
		port = 19801
	}

	if err := a.a2aManager.StartServer(port); err != nil {
		return err
	}

	log.Printf("[agent] A2A server started on port %d", port)
	return nil
}

// StopAgentServer stops the A2A agent server.
func (a *App) StopAgentServer() error {
	if a.a2aManager == nil {
		return nil
	}
	return a.a2aManager.StopServer()
}

// ─── Remote Agent Management ────────────────────────────────────

// ListRemoteAgents returns all configured remote A2A agents.
func (a *App) ListRemoteAgents() []a2a.RemoteAgentConfig {
	if a.a2aManager == nil {
		return []a2a.RemoteAgentConfig{}
	}
	return a.a2aManager.ListRemoteAgents()
}

// AddRemoteAgent adds a new remote A2A agent.
func (a *App) AddRemoteAgent(name, url, secret string) (*a2a.RemoteAgentConfig, error) {
	if a.a2aManager == nil {
		return nil, fmt.Errorf("A2A管理器未初始化")
	}
	agent, err := a.a2aManager.AddRemoteAgent(name, url, secret)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// RemoveRemoteAgent removes a remote A2A agent.
func (a *App) RemoveRemoteAgent(id string) error {
	if a.a2aManager == nil {
		return fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.RemoveRemoteAgent(id)
}

// ConnectRemoteAgent connects to a remote A2A agent.
func (a *App) ConnectRemoteAgent(id string) error {
	if a.a2aManager == nil {
		return fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.ConnectRemoteAgent(id)
}

// DisconnectRemoteAgent disconnects a remote A2A agent.
func (a *App) DisconnectRemoteAgent(id string) error {
	if a.a2aManager == nil {
		return fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.DisconnectRemoteAgent(id)
}

// UpdateRemoteAgent updates name and URL of a remote agent.
func (a *App) UpdateRemoteAgent(id, name, url, secret string) error {
	if a.a2aManager == nil {
		return fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.UpdateRemoteAgent(id, name, url, secret)
}

// ─── Task Operations ────────────────────────────────────────────

// SendAgentTask sends a text task to a connected remote agent.
func (a *App) SendAgentTask(agentID, text string) (*a2a.Task, error) {
	if a.a2aManager == nil {
		return nil, fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.SendTask(agentID, text)
}

// GetAgentTask retrieves a task status from a connected remote agent.
func (a *App) GetAgentTask(agentID, taskID string) (*a2a.Task, error) {
	if a.a2aManager == nil {
		return nil, fmt.Errorf("A2A管理器未初始化")
	}
	return a.a2aManager.GetTask(agentID, taskID)
}

// ─── A2A Task Executor (internal) ────────────────────────────────

// composeA2ATaskText extracts the last user-role text from an A2A message
// stream and folds in prior turns as light context, producing a single task
// string for runAgentLoop (which expects one user message).
func composeA2ATaskText(messages []a2a.Message) string {
	var lastUser string
	for _, m := range messages {
		if m.Role == "user" {
			for _, p := range m.Parts {
				if p.Kind == "text" && p.Text != "" {
					lastUser = p.Text
				}
			}
		}
	}
	return lastUser
}

// executeA2ATask is the TaskExecutor implementation. An inbound A2A task is
// routed through the local CORE agent persona (runAgentLoop) when available,
// so the peer is answered by the real Evo identity — with its tools and
// domain library — instead of a generic system-prompted LLM call. Falls back
// to the legacy ChatProxy path if no agent manager / core agent is ready.
func (a *App) executeA2ATask(ctx context.Context, messages []a2a.Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to process")
	}

	// Prefer the core agent persona for inbound tasks.
	if a.agentManager != nil {
		if libID, err := a.memoryStore.DefaultLibrary(); err == nil && libID != "" {
			if core, err := a.agentManager.GetCoreAgent(libID); err == nil && core != nil {
				// Compose the task text from the last user-role message.
				task := composeA2ATaskText(messages)
				if task != "" {
					return a.runAgentLoop(ctx, core, task)
				}
			}
		}
	}

	// ── Legacy fallback: generic system-prompted ChatProxy ──
	agentName := a.cfg.LLM.A2AConfig.Name
	if agentName == "" {
		agentName = "EverEvo Agent"
	}
	oaiMessages := []map[string]any{
		{
			"role": "system",
			"content": fmt.Sprintf(
				"你是 %s，一个通过 A2A（Agent-to-Agent）协议接收其他 Agent 请求的智能体。"+
					"请直接、准确地回应对方的消息；需要澄清时简短提问，回答以纯文本为主。",
				agentName,
			),
		},
	}
	for _, m := range messages {
		role := m.Role
		if role == "agent" {
			role = "assistant"
		}

		msg := map[string]any{"role": role}
		var contentParts []string
		for _, p := range m.Parts {
			if p.Kind == "text" && p.Text != "" {
				contentParts = append(contentParts, p.Text)
			}
		}
		if len(contentParts) == 1 {
			msg["content"] = contentParts[0]
		} else if len(contentParts) > 1 {
			msg["content"] = contentParts
		}
		oaiMessages = append(oaiMessages, msg)
	}

	messagesJSON, err := json.Marshal(oaiMessages)
	if err != nil {
		return "", fmt.Errorf("marshal messages: %w", err)
	}

	// Call existing ChatProxy (non-streaming) — no tools for A2A tasks
	result, err := a.ChatProxy(messagesJSON, json.RawMessage("[]"))
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	// Extract text from response
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in LLM response")
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no message in choice")
	}

	content, ok := message["content"].(string)
	if !ok || content == "" {
		return "", fmt.Errorf("empty content in LLM response")
	}

	return content, nil
}

// ─── Agent as Skill ─────────────────────────────────────────────

// CreateAgentSkill creates a Skill entry from a connected remote agent.
// The skill will have the agent's name/description and a tool to send messages to it.
func (a *App) CreateAgentSkill(agentID, packageName string) (*skills.Skill, error) {
	if a.a2aManager == nil {
		return nil, fmt.Errorf("A2A管理器未初始化")
	}

	agents := a.a2aManager.ListRemoteAgents()
	var agent *a2a.RemoteAgentConfig
	for i := range agents {
		if agents[i].ID == agentID {
			agent = &agents[i]
			break
		}
	}
	if agent == nil || agent.Status != "connected" {
		return nil, fmt.Errorf("agent not found or not connected")
	}

	skillName := "a2a-agent-" + agentID[:8]
	skillTitle := agent.Name
	skillDesc := fmt.Sprintf("通过 A2A 协议与 %s 通信。", agent.Name)
	if agent.Card != nil && agent.Card.Description != "" {
		skillDesc = agent.Card.Description
	}

	skill := &skills.Skill{
		Name:        skillName,
		Title:       skillTitle,
		Description: skillDesc,
		Category:    "a2a",
		Package:     packageName,
		Icon:        "◉",
		Enabled:     true,
		Tools:       []string{"a2a_list_agents", "a2a_send_to_agent"},
		MCPTools:    []string{},
		SystemPrompt: fmt.Sprintf(
			"你可以通过 a2a_send_to_agent 工具与「%s」通信（agentId: %s）。发送消息给它并解读回复。",
			agent.Name, agent.ID,
		),
	}

	if err := a.skillManager.Create(*skill); err != nil {
		return nil, err
	}
	return skill, nil
}

// RemoveAgentSkill removes a skill created for an agent by agent ID.
func (a *App) RemoveAgentSkill(agentID string) error {
	skillName := "a2a-agent-" + agentID[:8]
	return a.skillManager.Delete(skillName)
}

// GetAgentToolNames returns tool names available for a connected agent.
func (a *App) GetAgentToolNames(agentID string) []string {
	return []string{"a2a_list_agents", "a2a_send_to_agent"}
}

// ─── A2A Manager initialization helpers ─────────────────────────

// initA2AManager creates the A2A manager and wires up the task executor.
func (a *App) initA2AManager() {
	cfg := a.cfg.LLM.A2AConfig

	card := a2a.AgentCard{
		Name:        cfg.Name,
		Description: cfg.Description,
		Version:     cfg.Version,
		Capabilities: a2a.AgentCapabilities{
			Streaming:              false,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	dataDir, err := storage.AppDataDir()
	if err != nil {
		log.Printf("[agent] failed to get data dir: %v", err)
		return
	}

	a.a2aManager = a2a.NewManager(
		dataDir,
		card,
		a.executeA2ATask,
		func(event string, data interface{}) {
			wailsRuntime.EventsEmit(a.ctx, event, data)
		},
		cfg.Secret,
	)

	a.a2aManager.LoadRemoteAgents()

	// Auto-start server if enabled
	if cfg.Enabled {
		port := cfg.Port
		if port <= 0 {
			port = 19801
		}
		go func() {
			if err := a.a2aManager.StartServer(port); err != nil {
				log.Printf("[agent] auto-start failed: %v", err)
			}
		}()
	}
}
