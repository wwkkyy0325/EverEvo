//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"everevo/internal/agents"
	"everevo/internal/collab"
	"everevo/internal/config"
	"everevo/internal/skills"
	"everevo/internal/tools"
)

// ─── Local Agent execution core ────────────────────────────────
//
// Resolves an Agent persona into concrete LLM call inputs (provider, system
// prompt, tool set) and runs a bounded tool loop. Used by the agent_run
// delegation tool. ChatPanel uses GetAgentChatContext to drive an agent
// persona through the streaming path.

// AgentChatContext is the pre-resolved chat configuration for an agent, used by
// the frontend chatLoop so it does not need to re-implement skill/tool resolution.
type AgentChatContext struct {
	AgentID      string           `json:"agentId"`
	Name         string           `json:"name"`
	SystemPrompt string           `json:"systemPrompt"`
	Tools        []*tools.ToolDef `json:"tools"`
	ProviderID   string           `json:"providerId"`
	Model        string           `json:"model"`
}

// resolveAgentProvider returns the provider an agent will use, honoring its
// explicit ProviderID / Model override; falls back to the active provider.
// Returns a copy so the caller may freely read Model without mutating config.
func (a *App) resolveAgentProvider(agent *agents.Agent) (*config.LLMProvider, error) {
	if agent.ProviderID != "" {
		for i := range a.cfg.LLM.Providers {
			p := &a.cfg.LLM.Providers[i]
			if p.ID == agent.ProviderID {
				if !p.Enabled {
					return nil, fmt.Errorf("agent 指定的供应商 %q 未启用", p.Name)
				}
				clone := *p
				if agent.Model != "" {
					clone.Model = agent.Model
				}
				return &clone, nil
			}
		}
		return nil, fmt.Errorf("agent 指定的供应商 %q 不存在", agent.ProviderID)
	}
	ap, err := a.resolveActiveProvider()
	if err != nil {
		return nil, err
	}
	if agent.Model != "" {
		clone := *ap
		clone.Model = agent.Model
		return &clone, nil
	}
	return ap, nil
}

// agentSelectedSkills returns the enabled skills an agent may use. When the
// agent has a LibraryID, only skills from that domain (plus global skills with
// empty LibraryID) are included. When InheritSkills is set, all domain-scoped +
// global skills are returned; otherwise the intersection with agent.Skills.
func (a *App) agentSelectedSkills(agent *agents.Agent) []skills.Skill {
	if a.skillManager == nil {
		return nil
	}
	// Domain-scoped: only this domain's skills + global skills (empty LibraryID).
	candidates := a.skillManager.ListEnabled()
	if agent.LibraryID != "" {
		candidates = a.skillManager.ListEnabledByLibrary(agent.LibraryID)
	}
	if agent.InheritSkills {
		return candidates
	}
	wanted := map[string]bool{}
	for _, s := range agent.Skills {
		wanted[s] = true
	}
	var out []skills.Skill
	for _, s := range candidates {
		if wanted[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// buildAgentSystemPrompt composes the agent persona prompt with the usage hints
// of its selected skills. Mirrors the composition in chatStore.ts so the
// default (inherit-all) agent reproduces the original chat prompt exactly.
func (a *App) buildAgentSystemPrompt(agent *agents.Agent) string {
	base := agent.SystemPrompt
	if strings.TrimSpace(base) == "" {
		base = agents.BaseSystemPrompt
	}
	var parts []string
	for _, s := range a.agentSelectedSkills(agent) {
		if strings.TrimSpace(s.SystemPrompt) != "" {
			parts = append(parts, fmt.Sprintf("【%s】%s", s.Title, s.SystemPrompt))
		}
	}
	if len(parts) == 0 {
		return base
	}
	return base + "\n\n当前启用的能力角色：\n" + strings.Join(parts, "\n")
}

// buildAgentToolNames returns the union of the agent's granted tool names:
// selected skills' tools + the agent's explicit Tools + MCPTools.
func (a *App) buildAgentToolNames(agent *agents.Agent) []string {
	seen := map[string]bool{}
	var out []string
	add := func(names ...string) {
		for _, n := range names {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, s := range a.agentSelectedSkills(agent) {
		add(s.Tools...)
		add(s.MCPTools...)
	}
	add(agent.Tools...)
	add(agent.MCPTools...)
	return out
}

// isOrchestrationTool reports whether a tool re-enters the agent/workflow
// orchestration layer. These are stripped from sub-agent tool sets (runAgentLoop)
// to prevent unbounded recursion: an agent_run chain, or a workflow_execute call
// that spawns a fresh engine (which can itself run agent nodes), would recurse
// without a depth guard. Read-only workflow tools (list/get/status/validate) are
// left in — only the spawning/mutating ones are blocked.
// isMemoryScopedTool reports whether a tool reads/writes per-library memory or
// knowledge. For these, runAgentLoop injects the agent's bound libraryID so
// recall and writes stay domain-isolated instead of leaking via LastTurnLibrary.
func isMemoryScopedTool(name string) bool {
	switch name {
	case "memory_search", "memory_list", "memory_add_fact",
		"core_list", "core_add",
		"graph_list", "graph_add_edge",
		"wiki_list", "wiki_read", "wiki_save", "wiki_search",
		"experience_list":
		return true
	}
	return false
}

// isBlackboardTool reports whether a tool touches the collaboration blackboard.
// For these, runAgentLoopCollab auto-injects the session's sessionId + author.
func isBlackboardTool(name string) bool {
	switch name {
	case "blackboard_set", "blackboard_get", "blackboard_list":
		return true
	}
	return false
}

func isOrchestrationTool(name string) bool {
	switch name {
	// orchestration (recursion / re-entry)
	case "agent_list", "agent_create", "agent_run",
		"library_list", "agent_delegate_to_domain", "agent_delegate_multi_domain",
		"workflow_execute", "workflow_create", "workflow_update", "workflow_delete",
		"agent_synthesize_tool",
		"collab_create", "collab_dispatch", "collab_dispatch_async", "collab_wait",
		"collab_complete", "agent_message",
		"plan_create", "plan_step_update":
		return true
	// destructive system tools — sub-agents must not escalate
	case "shell_exec", "evolve_swap", "evolve_build",
		"zone_merge", "zone_discard",
		"backup_restore", "backup_delete",
		"plugin_create", "plugin_delete":
		return true
	}
	return false
}

// resolveAgentToolDefs builds the callable ToolDef list for an agent.
//
// Tool inclusion rules:
//   - Built-in tools in the agent's allowed set (skills + explicit Tools).
//   - MCP tools: if the agent declares an explicit MCPTools list, ONLY those are
//     granted (whitelist enforced). Otherwise (InheritSkills or none declared)
//     all external MCP tools are included (matching main-chat behavior).
//   - excludeOrchestration: strip orchestration/dangerous tools (sub-agents).
func (a *App) resolveAgentToolDefs(agent *agents.Agent, excludeOrchestration bool) []*tools.ToolDef {
	allowed := map[string]bool{}
	for _, n := range a.buildAgentToolNames(agent) {
		allowed[n] = true
	}
	// Whether the agent declared an explicit MCP whitelist. If so, external
	// tools NOT in the whitelist are blocked (instead of all being allowed).
	hasExplicitMCP := len(agent.MCPTools) > 0 && !agent.InheritSkills
	mcpWhitelist := map[string]bool{}
	for _, n := range agent.MCPTools {
		mcpWhitelist[n] = true
	}

	var out []*tools.ToolDef
	for _, t := range tools.List() {
		if excludeOrchestration && isOrchestrationTool(t.Name) {
			continue
		}
		if allowed[t.Name] {
			out = append(out, t)
			continue
		}
		// Read-only collaboration tools (plan_list, blackboard_list/get,
		// collab_list_sessions) are safe for ALL agents including sub-agents —
		// a delegated agent may need to read the shared plan/blackboard.
		if isReadOnlyCollabTool(t.Name) {
			out = append(out, t)
			continue
		}
		// Mutating collaboration + planning tools (collab_create, dispatch,
		// plan_create, …) are only for the main chat agent, not sub-agents.
		// Without this, Evo never sees them → never triggers collaboration →
		// the workbench stays empty even though the backend is wired.
		if !excludeOrchestration && isCoreCollabTool(t.Name) {
			out = append(out, t)
			continue
		}
		// External MCP tool not in the built-in allowed set.
		if tools.IsExternal(t.Name) {
			if hasExplicitMCP && !mcpWhitelist[t.Name] {
				continue // agent whitelisted MCP tools; this one isn't on the list
			}
			out = append(out, t)
		}
	}
	return out
}

// isReadOnlyCollabTool reports whether a tool is a read-only collaboration
// view. These are safe for ALL agents (including sub-agents) — a delegated
// agent may need to read the shared plan or blackboard to do its job.
func isReadOnlyCollabTool(name string) bool {
	switch name {
	case "plan_list", "blackboard_list", "blackboard_get", "collab_list_sessions":
		return true
	}
	return false
}

// isCoreCollabTool reports whether a tool is a core collaboration/planning
// primitive that the main agent should always be able to invoke (so it can
// decompose tasks into plans and orchestrate other agents). These are gated
// out of sub-agents via isOrchestrationTool.
func isCoreCollabTool(name string) bool {
	switch name {
	case "plan_create", "plan_step_update", "plan_list",
		"collab_create", "collab_dispatch", "collab_dispatch_async", "collab_wait",
		"blackboard_set", "blackboard_get", "blackboard_list",
		"agent_message":
		return true
	}
	return false
}

// marshalAgentToolDefs serializes ToolDefs into the OpenAI function-tool array.
func marshalAgentToolDefs(defs []*tools.ToolDef) json.RawMessage {
	type fnTool struct {
		Type     string `json:"type"`
		Function struct {
			Name        string           `json:"name"`
			Description string           `json:"description"`
			Parameters  *tools.ToolParams `json:"parameters"`
		} `json:"function"`
	}
	out := make([]fnTool, 0, len(defs))
	for _, d := range defs {
		t := fnTool{Type: "function"}
		t.Function.Name = d.Name
		t.Function.Description = d.Description
		t.Function.Parameters = d.Parameters
		out = append(out, t)
	}
	b, _ := json.Marshal(out)
	return b
}

// runAgentLoop runs an agent on a single user task with a bounded tool loop.
// Max 50 rounds with dead-loop detection: if the same tool+args repeats 3
// consecutive times, the loop is terminated. Orchestration tools are excluded
// so a sub-agent cannot recurse.
func (a *App) runAgentLoop(ctx context.Context, agent *agents.Agent, userText string) (string, error) {
	return a.runAgentLoopCollab(ctx, agent, userText, "")
}

// runAgentLoopCollab is the collab-aware variant. collabSessionID, when set,
// is auto-injected into blackboard_* tool calls so the sub-agent accesses its
// session's shared state without needing the ID passed explicitly.
func (a *App) runAgentLoopCollab(ctx context.Context, agent *agents.Agent, userText, collabSessionID string) (string, error) {
	provider, err := a.resolveAgentProvider(agent)
	if err != nil {
		return "", err
	}
	systemPrompt := a.buildAgentSystemPrompt(agent)
	toolsJSON := marshalAgentToolDefs(a.resolveAgentToolDefs(agent, true))

	msgs := []map[string]any{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userText},
	}
	opts := chatOpts{Temperature: agent.Temperature, MaxTokens: agent.MaxTokens}

	// No round cap (requested): the loop runs until the agent returns a final
	// answer (no more tool calls) or the context is cancelled. No tool-call /
	// round limits, no dead-loop abort.
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		msgsJSON, _ := json.Marshal(msgs)
		data, err := a.chatCompletion(provider, msgsJSON, toolsJSON, opts)
		if err != nil {
			return "", err
		}
		choices, ok := data["choices"].([]any)
		if !ok || len(choices) == 0 {
			return "", fmt.Errorf("agent %q 无响应", agent.Name)
		}
		choice, _ := choices[0].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		if message == nil {
			return "", fmt.Errorf("agent %q 响应格式无效", agent.Name)
		}

		toolCalls, _ := message["tool_calls"].([]any)
		if len(toolCalls) == 0 {
			content, _ := message["content"].(string)
			if strings.TrimSpace(content) == "" {
				content = "（" + agent.Name + " 无输出）"
			}
			return content, nil
		}

		// Feed the assistant turn back, then execute each tool call.
		msgs = append(msgs, message)
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]any)
			id, _ := tcm["id"].(string)
			fn, _ := tcm["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			var args map[string]any
			if argsStr != "" {
				_ = json.Unmarshal([]byte(argsStr), &args)
			}
			if args == nil {
				args = map[string]any{}
			}
			// Inject this agent's bound domain library into memory/kb tools so
			// their recall + writes are scoped correctly (not the global
			// LastTurnLibrary, which can be another domain's).
			if agent.LibraryID != "" && isMemoryScopedTool(name) {
				if _, set := args["libraryId"]; !set {
					args["libraryId"] = agent.LibraryID
				}
			}
			// Auto-scope blackboard tools to the agent's collaboration session,
			// so sub-agents don't need the sessionId passed explicitly.
			if collabSessionID != "" && isBlackboardTool(name) {
				if _, set := args["sessionId"]; !set {
					args["sessionId"] = collabSessionID
				}
			}
			// Tag author for blackboard writes as this agent.
			if isBlackboardTool(name) {
				args["_author"] = agent.Name
			}
			result := a.CallTool(name, args)
			// Surface every tool call so the workbench can show what each agent is
			// doing (tool-call feed) and derive agent→agent communication edges for
			// dispatch/message tools (their args carry targetAgentId).
			if a.collab != nil && a.collab.Bus != nil {
				a.collab.Bus.Publish("tool."+agent.ID+".call", collab.Event{
					Source: agent.ID, Type: "tool",
					Payload: map[string]any{
						"tool": name, "args": argsStr, "ok": result.Success,
						"agentName": agent.Name, "sessionId": collabSessionID,
					},
				})
			}
			resultJSON, _ := json.Marshal(result)
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      string(resultJSON),
			})
		}
	}
}

// GetAgentChatContext resolves an agent into the inputs the frontend chatLoop
// needs (system prompt, tool defs, provider/model). Used when the user selects
// an agent persona in the chat panel. Orchestration tools are kept so the main
// chat can still create/delegate.
func (a *App) GetAgentChatContext(id string) (*AgentChatContext, error) {
	if a.agentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	agent, err := a.agentManager.Get(id)
	if err != nil {
		return nil, err
	}
	provider, err := a.resolveAgentProvider(agent)
	if err != nil {
		return nil, err
	}
	return &AgentChatContext{
		AgentID:      agent.ID,
		Name:         agent.Name,
		SystemPrompt: a.buildAgentSystemPrompt(agent),
		Tools:        a.resolveAgentToolDefs(agent, false),
		ProviderID:   provider.ID,
		Model:        provider.Model,
	}, nil
}

// ─── Domain System Prompt ──────────────────────────────────────────

// BuildDomainSystemPrompt generates a domain-scoped system prompt fragment that
// replaces the global "inject everything" approach. It includes only the agents,
// skills, and MCP servers that belong to the given domain (plus global skills).
// Returns an empty string when the domain can't be resolved.
func (a *App) BuildDomainSystemPrompt(domainId string) string {
	if domainId == "" || a.memoryStore == nil {
		return ""
	}
	// Resolve domain name.
	libs := a.memoryStore.ListLibraryIDs()
	found := false
	for _, id := range libs {
		if id == domainId {
			found = true
			break
		}
	}
	if !found {
		return ""
	}
	// Look up the actual display name for this domain.
	domainName := domainId
	if libsFull, err := a.memoryStore.LibraryList(); err == nil {
		for _, l := range libsFull {
			if l.ID == domainId {
				domainName = l.Name
				break
			}
		}
	}

	// Agents in this domain.
	var agentLines []string
	if a.agentManager != nil {
		for _, ag := range a.agentManager.ListByLibrary(domainId) {
			agentLines = append(agentLines, fmt.Sprintf("- **%s**：%s", ag.Name, ag.Description))
		}
	}
	agentsBlock := ""
	if len(agentLines) > 0 {
		agentsBlock = "\n## 可委派 Agent\n" + strings.Join(agentLines, "\n") + "\n"
	}

	// Enabled skills in this domain (plus global).
	var skillLines []string
	if a.skillManager != nil {
		for _, sk := range a.skillManager.ListEnabledByLibrary(domainId) {
			skillLines = append(skillLines, fmt.Sprintf("- **%s**（%s）：%s", sk.Title, sk.Name, sk.Description))
		}
	}
	skillsBlock := ""
	if len(skillLines) > 0 {
		skillsBlock = "\n## 已启用能力\n" + strings.Join(skillLines, "\n") + "\n"
	}

	// MCP servers in this domain.
	var mcpLines []string
	if a.mcpClient != nil {
		for _, cfg := range a.mcpClient.ListServersByLibrary(domainId) {
			mcpLines = append(mcpLines, fmt.Sprintf("- **%s** (%s)", cfg.Name, cfg.Transport))
		}
	}
	mcpBlock := ""
	if len(mcpLines) > 0 {
		mcpBlock = "\n## 可用 MCP 工具\n" + strings.Join(mcpLines, "\n") + "\n"
	}

	return fmt.Sprintf("\n【当前领域：%s】\n你是该领域的专家。领域上下文将自动注入相关的知识库、记忆和图谱数据。%s%s%s",
		domainName, agentsBlock, skillsBlock, mcpBlock)
}
