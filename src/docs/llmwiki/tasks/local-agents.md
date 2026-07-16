# Local Agents (智能体) — Definition, Management & Orchestration

> Multi-agent support: define local Agent personas, manage them in a UI, let the
> main agent create/delegate to them, and switch personas in chat.
> **Status: IMPLEMENTED (2026-07-07) — all 8 phases done; `go build ./...` ✓, `npm run build` ✓.**
> Runtime behavior (create/delegate/switch) is compile-verified but not yet exercised end-to-end in a running app.

## 1. Background

The A2A stack (`internal/a2a/`, `LLMAgent.vue`) handles **inter-agent networking**
(local server + remote clients). That is "对内对外底层". What is missing is the
**Agent itself** — local, named, reusable personas. Today the chat is a single
global assistant: system prompt = base + all enabled skills' prompts; tools =
enabled skills' tools + MCP; model = global active provider. There is no concept
of multiple agents.

## 2. Goals (confirmed with user)

| # | Capability | Scope |
|---|------------|-------|
| 1 | **Management page + CRUD** | Agent list / create / edit / delete + persistence |
| 2 | **Main agent runtime creation** | `agent_create` tool — LLM defines a new persona, saved to list |
| 3 | **Main agent delegation** | `agent_run` tool — run a sub-agent on a task, return its result |
| 4 | **Chat persona switching** | Chat panel agent selector; talk to a specific agent |

**Agent data model** = "完整独立（最强）": name/description/icon + full systemPrompt +
provider/model override + skill subset (or inherit-all) + extra tools + MCP tools +
temperature + maxTokens.

**Placement**: new "智能体" Tab in `LLMConfig.vue` (alongside 能力清单/MCP/代理/飞书).
Existing "代理" (A2A) Tab untouched.

## 3. Design

### 3.1 Data model — `internal/agents/` (mirrors `internal/skills/`)

```go
type Agent struct {
    ID             string   `json:"id"`
    Name           string   `json:"name"`
    Description    string   `json:"description"`
    Icon           string   `json:"icon,omitempty"`
    SystemPrompt   string   `json:"systemPrompt"`
    ProviderID     string   `json:"providerId,omitempty"`  // override; "" = active
    Model          string   `json:"model,omitempty"`        // override model name
    InheritSkills  bool     `json:"inheritSkills"`          // true = use all enabled skills
    Skills         []string `json:"skills,omitempty"`       // skill names (when not inheriting)
    Tools          []string `json:"tools,omitempty"`        // extra built-in tool names
    MCPTools       []string `json:"mcpTools,omitempty"`     // external MCP tool names
    Temperature    *float64 `json:"temperature,omitempty"`
    MaxTokens      int      `json:"maxTokens,omitempty"`
    IsDefault      bool     `json:"isDefault,omitempty"`    // main agent; cannot delete
    CreatedAt      int64    `json:"createdAt"`
    UpdatedAt      int64    `json:"updatedAt"`
}
```

- Persistence: `data/agents.json` (atomic write, same pattern as `skills.json`).
- Seed a **default main agent** on first run that reproduces current chat:
  `SystemPrompt` = current base prompt, `InheritSkills` = true, no provider override.

### 3.2 Effective prompt / tools resolution (backend)

- `resolveAgentProvider(agent)` → agent.ProviderID (with agent.Model override) or active.
- `buildAgentSystemPrompt(agent)` → agent.SystemPrompt + selected skills' prompts.
  - Selected skills = all enabled (if InheritSkills) else `agent.Skills ∩ enabled`.
- `buildAgentToolNames(agent)` → selected skills' tools ∪ agent.Tools ∪ agent.MCPTools.
  - **Excludes orchestration tools** (`agent_*`) for sub-agents → no self-recursion.

### 3.3 Execution core — refactor `app_chat.go`

Extract `chatCompletion(p, messagesJSON, toolsJSON, opts)` (non-stream) from `ChatProxy`
so both `ChatProxy` (active) and `runAgentLoop` (specific provider) reuse it.

`runAgentLoop(ctx, agent, userText) (string, error)` — bounded tool loop (max 5 rounds):
LLM call → if tool_calls, `CallTool` each → feed back → repeat → return final text.
Used by the `agent_run` delegation tool.

### 3.4 Orchestration tools — `internal/tools/agent_tools.go`

| Tool | Params | Returns |
|------|--------|---------|
| `agent_list` | — | `[{id, name, description, isDefault}]` |
| `agent_create` | name, description, systemPrompt, skills[]? | created agent |
| `agent_run` | agentId\|name, task | sub-agent final text |

Registered via `registerAgentTools()` in `RegisterAll`; handlers `hAgentList/Create/Run`
in `app_tools.go`. Exposed to the main chat via a new built-in skill
**`agent-orchestration`** (enabled by default) listing these three tools.

### 3.5 Chat streaming with provider override

Extend `app_chat.go`: `runChatStream` takes an explicit `*config.LLMProvider` + opts
(temperature, maxTokens). `ChatStream` resolves active provider; new
`ChatStreamAs(streamID, messages, tools, providerId, model)` resolves a specific
provider (model override). Frontend uses `ChatStreamAs` when the selected agent has
a provider override, else `ChatStream`.

New binding `GetAgentChatContext(agentId)` → `{systemPrompt, tools[], providerId, model}`
so the frontend chatLoop doesn't re-implement skill/tool resolution in JS.

### 3.6 Frontend

- `frontend/src/api/agents.ts` (plural — avoids clash with A2A `agent.ts`): `LocalAgent`
  type + `agentsApi` (list/get/create/update/delete/getChatContext).
- `api/index.ts`: barrel export.
- `frontend/src/components/llm/LLMAgents.vue`: management page — agent cards + create/edit
  dialog (all fields; provider/model dropdown from `providersApi`, skills multi-select
  from `skillsApi`). `useDataChanged('agents:changed', …)`.
- `LLMConfig.vue`: new "智能体" Tab → `<LLMAgents />`.
- `chatStore.ts`: `currentAgentId`, `agents`, `loadAgents()`; chatLoop becomes agent-aware
  (uses `GetAgentChatContext` when an agent is selected).
- `ChatPanel.vue`: agent selector dropdown in header.

## 4. Implementation steps

### Phase 1 — Backend data model & CRUD
- [ ] 1.1 `internal/agents/agent.go` (Agent + Manager + persistence + seed default)
- [ ] 1.2 `app_agents.go` (List/Get/Create/Update/Delete + GetAgentChatContext + GetAgentToolNames)
- [ ] 1.3 `app.go` (init `agentManager` in startup)
- **verify**: `go build ./...`

### Phase 2 — Execution core
- [ ] 2.1 Extract `chatCompletion(p, messages, tools, opts)` from `ChatProxy`
- [ ] 2.2 `resolveAgentProvider` / `buildAgentSystemPrompt` / `buildAgentToolNames`
- [ ] 2.3 `runAgentLoop(ctx, agent, userText)`
- **verify**: `go build ./...`

### Phase 3 — Orchestration tools
- [ ] 3.1 `internal/tools/agent_tools.go` (agent_list/create/run schemas)
- [ ] 3.2 handlers in `app_tools.go` + init-map + `RegisterAll`
- [ ] 3.3 built-in skill `agent-orchestration` (enabled) in `internal/skills/skill.go`
- **verify**: `go build ./...`

### Phase 4 — Chat streaming override
- [ ] 4.1 `runChatStream` takes provider + opts; add `ChatStreamAs`
- **verify**: `go build ./...`

### Phase 5 — Frontend API
- [ ] 5.1 `frontend/src/api/agents.ts` + `index.ts` export
- **verify**: frontend typecheck

### Phase 6 — Management page
- [ ] 6.1 `LLMAgents.vue` (list + create/edit dialog)
- [ ] 6.2 `LLMConfig.vue` new Tab
- **verify**: frontend build

### Phase 7 — Chat persona switching
- [ ] 7.1 `chatStore.ts` agent awareness
- [ ] 7.2 `ChatPanel.vue` selector
- **verify**: frontend build

### Phase 8 — Docs
- [ ] 8.1 `design.md` update + `changelog.md` entry

## 5. Verification checklist

- [ ] Default main agent seeded on first run; current chat behavior unchanged
- [ ] Create/edit/delete agents in UI; persists across restart
- [ ] Main agent can `agent_create` → new agent appears in list
- [ ] Main agent can `agent_run` → sub-agent runs its own tool loop, returns text
- [ ] Sub-agent cannot recurse into `agent_run`
- [ ] Chat selector switches persona (system prompt + tools + model override)
- [ ] `go build ./...` clean; frontend typecheck/build clean
