// Package agents provides local agent persona, domain-library, task, and guide
// tools as a self-registering ToolPlugin. The execution logic (agent loop,
// provider resolution, prompt building) lives here as well, driven by Deps.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"everevo/internal/agents"
	"everevo/internal/collab"
	"everevo/internal/config"
	"everevo/internal/memory"
	mcpclient "everevo/internal/mcp/client"
	"everevo/internal/skills"
	"everevo/internal/tools"
)

// ─── Dependencies ──────────────────────────────────────────────────────

// Deps holds the Agent execution runtime dependencies, wired once at startup
// by the App. All agent-loop functions (runAgentLoop, BuildDomainSystemPrompt,
// etc.) access these fields instead of a monolithic App pointer.
type Deps struct {
	Cfg            *config.Config
	SkillManager   *skills.Manager
	AgentManager   *agents.Manager
	MemoryStore    *memory.Store
	MCPClient      *mcpclient.Manager
	ChatCompletion func(p *config.LLMProvider, messagesJSON, toolsJSON json.RawMessage, opts ChatOpts) (map[string]any, error)
	CallTool       func(name string, params map[string]any) tools.ToolResult
	Collab         *collab.Kernel
	// EnrichSystemPrompt is called by buildAgentSystemPrompt to inject
	// per-turn context: thinking language control, paradigm recommendations, etc.
	// base is the agent's persona prompt; userQuery is the current user message.
	// Returns the enriched system prompt.
	EnrichSystemPrompt func(base string, userQuery string) string
}

// ChatOpts mirrors the app.chatOpts struct so the plugin doesn't import app.
type ChatOpts struct {
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"maxTokens,omitempty"`
	ThinkEffort string           `json:"thinkEffort,omitempty"`
	OnChunk     func(text string) `json:"-"`
	Ctx         context.Context  `json:"-"`
}

// d is the package-level dependency bundle, set by SetDeps during startup.
var d *Deps

// SetDeps wires the execution runtime dependencies. Call once at startup.
func SetDeps(deps *Deps) {
	d = deps
}

// ─── Agent Chat Context (frontend-facing) ─────────────────────────────

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

// GetAgentChatContext resolves an agent into the inputs the frontend chatLoop
// needs (system prompt, tool defs, provider/model).
func GetAgentChatContext(id string) (*AgentChatContext, error) {
	if d == nil || d.AgentManager == nil {
		return nil, fmt.Errorf("agent 管理器未初始化")
	}
	agent, err := d.AgentManager.Get(id)
	if err != nil {
		return nil, err
	}
	provider, err := resolveAgentProvider(agent)
	if err != nil {
		return nil, err
	}
	return &AgentChatContext{
		AgentID:      agent.ID,
		Name:         agent.Name,
		SystemPrompt: buildAgentSystemPrompt(agent),
		Tools:        resolveAgentToolDefs(agent, false),
		ProviderID:   provider.ID,
		Model:        provider.Model,
	}, nil
}

// ─── Provider Resolution ──────────────────────────────────────────────

// resolveActiveProvider returns the currently-active LLM provider from config.
func resolveActiveProvider() (*config.LLMProvider, error) {
	activeID := d.Cfg.LLM.ActiveProvider
	for i := range d.Cfg.LLM.Providers {
		if d.Cfg.LLM.Providers[i].ID == activeID && d.Cfg.LLM.Providers[i].Enabled {
			return &d.Cfg.LLM.Providers[i], nil
		}
	}
	return nil, fmt.Errorf("没有活动的供应商")
}

// resolveAgentProvider returns the provider an agent will use, honoring its
// explicit ProviderID / Model override; falls back to the active provider.
func resolveAgentProvider(agent *agents.Agent) (*config.LLMProvider, error) {
	if agent.ProviderID != "" {
		for i := range d.Cfg.LLM.Providers {
			p := &d.Cfg.LLM.Providers[i]
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
	ap, err := resolveActiveProvider()
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

// ─── Skill / Tool Resolution ──────────────────────────────────────────

// agentSelectedSkills returns the enabled skills an agent may use.
func agentSelectedSkills(agent *agents.Agent) []skills.Skill {
	if d == nil || d.SkillManager == nil {
		return nil
	}
	candidates := d.SkillManager.ListEnabled()
	if agent.LibraryID != "" {
		candidates = d.SkillManager.ListEnabledByLibrary(agent.LibraryID)
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

// buildOrchestratorPrompt is the single system prompt assembly point for all
// agent interactions — main chat, sub-agent delegation, and collab sessions.
//
// Assembly order (matches Anthropic / OpenClaw best practices):
//  1. Agent persona (SystemPrompt or BaseSystemPrompt)
//  2. Skill context (compact: titles only, full prompts loaded on-demand)
//  3. ThinkLang rule (per-turn, query-driven)
//  4. Paradigm hint (lightweight: suggests paradigm_match tool, not full table)
//  5. Per-turn enrichments from Deps (memory/wiki/rag — provided by caller)
func buildOrchestratorPrompt(agent *agents.Agent, userQuery string) string {
	// 1. Agent persona
	base := agent.SystemPrompt
	if strings.TrimSpace(base) == "" {
		base = agents.BaseSystemPrompt
	}
	// Ensure ReAct framework is present
	if !strings.Contains(base, "ReAct") && !strings.Contains(base, "推理-行动") {
		base = "你是 EverEvo 的 AI 助手，遵循 ReAct（推理-行动）框架工作。\n\n## 工作流程\n1. 分析需求 → 2. 调用工具 → 3. 观察结果 → 4. 重复直至完成 → 5. 最终回答（简洁中文，不照搬 JSON）\n\n## 工具规则\n- 先思考再行动，失败换方案\n- JSON 提取关键字段，不要整套贴出\n- 不需要工具就直接回答\n\n---\n\n" + base
	}

	// 2. Skill context — compact: titles only
	var skillTitles []string
	for _, s := range agentSelectedSkills(agent) {
		skillTitles = append(skillTitles, fmt.Sprintf("%s %s", s.Icon, s.Title))
	}
	if len(skillTitles) > 0 {
		base += "\n\n已启用的能力角色：" + strings.Join(skillTitles, "、")
	}

	// 3. ThinkLang rule (per-turn)
	if userQuery != "" && d != nil {
		tl := classifyThinkLang(userQuery)
		if tl != "" {
			base += "\n\n---\n" + tl
		}
	}

	// 4. Paradigm list — injected by frontend (full catalog, not just hint)

	// 5. Per-turn enrichments from Deps (memory/wiki/rag context provided by caller)
	if d != nil && d.EnrichSystemPrompt != nil {
		base = d.EnrichSystemPrompt(base, userQuery)
	}

	return base
}

// buildAgentSystemPrompt is the legacy entry point kept for backward compatibility
// with GetAgentChatContext (which doesn't have userQuery). New code should use
// buildOrchestratorPrompt directly.
func buildAgentSystemPrompt(agent *agents.Agent) string {
	return buildOrchestratorPrompt(agent, "")
}

// classifyThinkLang returns the thinking language rule for a user query.
func classifyThinkLang(userQuery string) string {
	if userQuery == "" {
		return ""
	}
	tl := memory.ClassifyThinkLang(userQuery)
	return tl.Rule
}

// buildAgentToolNames returns the union of the agent's granted tool names:
// selected skills' tools + the agent's explicit Tools + MCPTools.
func buildAgentToolNames(agent *agents.Agent) []string {
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
	for _, s := range agentSelectedSkills(agent) {
		add(s.Tools...)
		add(s.MCPTools...)
	}
	add(agent.Tools...)
	add(agent.MCPTools...)
	return out
}

// resolveAgentToolDefs builds the callable ToolDef list for an agent.
func resolveAgentToolDefs(agent *agents.Agent, excludeOrchestration bool) []*tools.ToolDef {
	if excludeOrchestration {
		core := tools.CoreToolsDef()
		allowed := map[string]bool{}
		for _, n := range buildAgentToolNames(agent) {
			allowed[n] = true
		}
		for _, t := range tools.List() {
			if allowed[t.Name] && !tools.IsCoreTool(t.Name) && !tools.IsOrchestrationTool(t.Name) {
				tools.CacheSchema(t)
				core = append(core, t)
			}
		}
		return core
	}

	allowed := map[string]bool{}
	for _, n := range buildAgentToolNames(agent) {
		allowed[n] = true
	}
	// Always include core tools (paradigm, tool_search, agent_run, etc.)
	hasExplicitMCP := len(agent.MCPTools) > 0 && !agent.InheritSkills
	mcpWhitelist := map[string]bool{}
	for _, n := range agent.MCPTools {
		mcpWhitelist[n] = true
	}

	var out []*tools.ToolDef
	// Always start with core tools (paradigm, tool_search, etc.)
	if !excludeOrchestration {
		out = append(out, tools.CoreToolsDef()...)
	}
	for _, t := range tools.List() {
		if excludeOrchestration && tools.IsOrchestrationTool(t.Name) {
			continue
		}
		// Skip core tools (already added above) and orchestration tools when excluded
		if tools.IsCoreTool(t.Name) {
			continue
		}
		if allowed[t.Name] {
			out = append(out, t)
			continue
		}
		if tools.IsReadOnlyCollabTool(t.Name) {
			out = append(out, t)
			continue
		}
		if !excludeOrchestration && tools.IsCoreCollabTool(t.Name) {
			out = append(out, t)
			continue
		}
		if tools.IsExternal(t.Name) {
			if hasExplicitMCP && !mcpWhitelist[t.Name] {
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// ─── Tool Marshal ─────────────────────────────────────────────────────

// marshalAgentToolDefs serializes ToolDefs into the OpenAI function-tool array.
func marshalAgentToolDefs(defs []*tools.ToolDef) json.RawMessage {
	type fnTool struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	out := make([]fnTool, 0, len(defs))
	for _, td := range defs {
		t := fnTool{Type: "function"}
		t.Function.Name = td.Name
		t.Function.Description = td.Description
		if len(td.RawParameters) > 0 {
			t.Function.Parameters = td.RawParameters
		} else if td.Parameters != nil {
			b, _ := json.Marshal(td.Parameters)
			t.Function.Parameters = b
		}
		out = append(out, t)
	}
	b, _ := json.Marshal(out)
	return b
}

// ─── Agent Loop ───────────────────────────────────────────────────────

// RunAgentLoop runs an agent on a single user task with a bounded tool loop.
func RunAgentLoop(ctx context.Context, agent *agents.Agent, userText string) (string, error) {
	return RunAgentLoopCollab(ctx, agent, userText, "")
}

// RunAgentLoopCollab is the collab-aware variant. collabSessionID, when set,
// is auto-injected into blackboard_* tool calls.
func RunAgentLoopCollab(ctx context.Context, agent *agents.Agent, userText, collabSessionID string) (string, error) {
	provider, err := resolveAgentProvider(agent)
	if err != nil {
		return "", err
	}
	systemPrompt := buildOrchestratorPrompt(agent, userText)
	toolsJSON := marshalAgentToolDefs(resolveAgentToolDefs(agent, true))

	msgs := []map[string]any{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userText},
	}
	opts := ChatOpts{Temperature: agent.Temperature, MaxTokens: agent.MaxTokens}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		msgsJSON, _ := json.Marshal(msgs)
		data, err := d.ChatCompletion(provider, msgsJSON, toolsJSON, opts)
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
			if agent.LibraryID != "" && tools.IsMemoryScopedTool(name) {
				if _, set := args["libraryId"]; !set {
					args["libraryId"] = agent.LibraryID
				}
			}
			if collabSessionID != "" && tools.IsBlackboardTool(name) {
				if _, set := args["sessionId"]; !set {
					args["sessionId"] = collabSessionID
				}
			}
			if tools.IsBlackboardTool(name) {
				args["_author"] = agent.Name
			}
			result := d.CallTool(name, args)
			if d.Collab != nil && d.Collab.Bus != nil {
				d.Collab.Bus.Publish("tool."+agent.ID+".call", collab.Event{
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

// ─── Domain System Prompt ─────────────────────────────────────────────

// BuildDomainSystemPrompt generates a domain-scoped system prompt fragment.
func BuildDomainSystemPrompt(domainId string) string {
	if domainId == "" || d == nil || d.MemoryStore == nil {
		return ""
	}
	libs := d.MemoryStore.ListLibraryIDs()
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
	domainName := domainId
	if libsFull, err := d.MemoryStore.LibraryList(); err == nil {
		for _, l := range libsFull {
			if l.ID == domainId {
				domainName = l.Name
				break
			}
		}
	}

	// Agents in this domain.
	var agentLines []string
	if d.AgentManager != nil {
		for _, ag := range d.AgentManager.ListByLibrary(domainId) {
			agentLines = append(agentLines, fmt.Sprintf("- **%s**：%s", ag.Name, ag.Description))
		}
	}
	agentsBlock := ""
	if len(agentLines) > 0 {
		agentsBlock = "\n## 可委派 Agent\n" + strings.Join(agentLines, "\n") + "\n"
	}

	// Enabled skills in this domain (plus global).
	var skillLines []string
	if d.SkillManager != nil {
		for _, sk := range d.SkillManager.ListEnabledByLibrary(domainId) {
			skillLines = append(skillLines, fmt.Sprintf("- **%s**（%s）：%s", sk.Title, sk.Name, sk.Description))
		}
	}
	skillsBlock := ""
	if len(skillLines) > 0 {
		skillsBlock = "\n## 已启用能力\n" + strings.Join(skillLines, "\n") + "\n"
	}

	// MCP servers in this domain.
	var mcpLines []string
	if d.MCPClient != nil {
		for _, cfg := range d.MCPClient.ListServersByLibrary(domainId) {
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
