# Context Window Optimization — Comprehensive Plan

> **Goal**: Apply Claude Code's proven context optimization patterns to EverEvo's agent architecture.  
> Target: reduce per-request token overhead by **85-95%** for tool schemas, **60-80%** for tool outputs.

---

## 1. Current State Analysis

### 1.1 Tool loading: upfront registration

`tools.RegisterAll()` (registry.go:93) registers ALL tools at startup — ~80+ internal tools across 18 categories:

| Category | Count | Typical size/tool | Total |
|----------|-------|-------------------|-------|
| model | 6 | ~200 tokens | ~1,200 |
| plugin | 10 | ~250 tokens | ~2,500 |
| kb | 10 | ~300 tokens | ~3,000 |
| catalog | 6 | ~250 tokens | ~1,500 |
| download | 5 | ~250 tokens | ~1,250 |
| system | 4 | ~200 tokens | ~800 |
| toolbox | 2 | ~300 tokens | ~600 |
| guide | 5 | ~200 tokens | ~1,000 |
| workflow | 7 | ~350 tokens | ~2,450 |
| mcp | 7 | ~250 tokens | ~1,750 |
| a2a | 5 | ~300 tokens | ~1,500 |
| agent | 7 | ~400 tokens | ~2,800 |
| provider | 5 | ~250 tokens | ~1,250 |
| proxy | 4 | ~200 tokens | ~800 |
| shell/search/fetch | 3 | ~350 tokens | ~1,050 |
| app_control | ~5 | ~200 tokens | ~1,000 |
| zone | ~3 | ~200 tokens | ~600 |
| collab/plan/taskboard | ~8 | ~300 tokens | ~2,400 |
| **Total** | **~110** | | **~27,000 tokens** |

**Plus external MCP tools**: each connected MCP server adds its own tool schemas. A single GitHub MCP can add 40k+ tokens.

### 1.2 Tool results: unbounded

`ToolResult` (types.go:61) carries raw `json.RawMessage` — no size limit, no truncation. Large results include:

| Tool | Typical result size |
|------|-------------------|
| `read_file` | Up to entire file (MBs) |
| `shell_exec` | Up to full terminal output (100k+) |
| `catalog_search` | Dozens of model entries with metadata |
| `kb_search` | Multiple chunks with full text |
| `guide_read` | Full markdown document |

### 1.3 What's already good

- `BuildDomainSystemPrompt()` already scopes agents/skills/MCP to domains (~60-70% savings vs global inject)
- `ToolAnnotations.ReadOnlyHint` exists — can be reused for tiering
- `RawParameters` field exists for MCP schema fidelity
- Subagent execution (`agent_run`) already exists

---

## 2. Architecture: Three-Layer Context Model

Reference: Claude Code's [Lazy Context Loading #44536](https://github.com/anthropics/claude-code/issues/44536), [MCP Hierarchical Tools #532](https://github.com/orgs/modelcontextprotocol/discussions/532), CMU/Samsung JIT Schema Passing (arXiv:2511.03728).

```
┌─────────────────────────────────────────────────┐
│ LAYER 1 — Always Loaded (~1,500 tokens)        │
│ • Core navigation tools (tool_search, read,     │
│   shell, agent_run)                             │
│ • Lightweight tool index: category→tool names   │
│ • Domain identity + active context              │
├─────────────────────────────────────────────────┤
│ LAYER 2 — On-Demand (~200 tokens/fetch)        │
│ • Full tool schemas fetched via tool_search     │
│ • Cached for remainder of turn                  │
│ • Purged after tool result consumed             │
├─────────────────────────────────────────────────┤
│ LAYER 3 — Ephemeral (~500 tokens avg)          │
│ • Tool execution results — auto-truncated       │
│ • Compaction markers replace consumed results   │
│ • On-disk cache for re-fetch if needed          │
└─────────────────────────────────────────────────┘
```

### 2.1 Layer 1: Lightweight Tool Index

Instead of loading all ~110 tool schemas (27k tokens), load a categorized index:

```json
{
  "tool_index": {
    "model": ["model_list", "model_load", "model_unload", "model_run", "model_list_downloaded", "model_list_tool"],
    "plugin": ["plugin_list", "plugin_status", "plugin_start", "plugin_stop", "plugin_restart", "plugin_run", "plugin_install", "plugin_delete", "plugin_logs", "plugin_create"],
    "kb": ["kb_list", "kb_create", "kb_add_texts", "kb_search", "kb_delete", "kb_clear", "kb_set_library", "kb_list_docs", "read_file", "read_media_file", "kb_delete_chunks"],
    "...": "..."
  }
}
```

~300 tokens vs 27,000. **95% reduction.**

Plus **always-available core tools** (6 tools, ~800 tokens):
- `tool_search` — the meta-tool for discovering tools
- `read_file` — reads files (fundamental, used everywhere)
- `shell_exec` — runs commands (fundamental)
- `web_search` / `web_fetch` — information access (fundamental)
- `agent_run` — delegation (fundamental)

### 2.2 Layer 2: On-Demand Schema Loading

New meta-tool:

```
tool_search(query: string, category?: string) → {
  tools: [{name, title, description, parameters (full schema)}]
  suggestion: "next query hint"
}
```

**Flow:**
1. LLM receives request → sees tool index + 6 core tools
2. LLM calls `tool_search(query="list models")` → gets `model_list`, `model_load` schemas
3. LLM calls the actual tool
4. After tool result is consumed, schemas are purged (but can be re-fetched)

**Token savings:**
- Before: 27,000 tokens EVERY request
- After: 300 (index) + 800 (core) + ~200-500 per unique tool category per turn
- Typical 5-tool turn: ~1,100 + ~1,500 = ~2,600 tokens (90% reduction)

### 2.3 Layer 3: Tool Output Compaction

Two mechanisms:

#### A. Per-tool output limits

```go
type ToolOutputPolicy struct {
    MaxOutputBytes  int  // hard cap (default: 8KB)
    KeepHeadBytes   int  // preserve first N bytes (default: 2KB)
    KeepTailBytes   int  // preserve last N bytes (default: 1KB)
    Summarizable    bool // whether to offer LLM-summarized version
}
```

| Tool | MaxOutput | Strategy |
|------|-----------|----------|
| `read_file` | 12KB | head 3KB + tail 2KB + truncation marker |
| `shell_exec` | 8KB | head 2KB + tail 2KB |
| `catalog_search` | 10KB | head 4KB + result count |
| `kb_search` | 8KB | top-5 results full, rest count only |
| `guide_read` | 10KB | head 3KB + tail 1KB |
| `list_*` | 6KB | full (list results are naturally bounded) |

**Truncation marker format:**
```
[output truncated: 45KB total, showing first 3KB + last 2KB.
 Use tool_search("re-read <tool> <id> offset=<N>") to fetch specific section]
```

#### B. Consumed-result compaction

After each assistant message, replace tool results from the PREVIOUS turn with:

```
[tool_result: model_list → 5 models listed (1.2KB). Consumed.]
```

This is injected into the system prompt / as a special role message, reducing context bloat from stale tool outputs. Based on the Anthropic Claude SDK automatic compaction pattern and the "Context Compaction" cookbook.

### 2.4 Subagent Context Isolation

Current: `agent_run` executes with full system prompt + all tools.

Target:

```
agent_run(agent_id, prompt):
  context = {
    system: buildAgentSystemPrompt(agent),    // agent-specific
    domain: BuildDomainSystemPrompt(domainId), // domain-scoped
    tool_index: filterByAgentSkills(toolIndex), // subset of tools
    messages: [prompt]
  }
```

**Key change**: subagents don't inherit ALL tools — only the subset defined in the agent's `Skills` + `Tools` config. Combined with lazy schema loading, subagent context stays under 5k tokens baseline vs current ~35k+.

### 2.5 Web Search/Fetch Integration

Current tools: `web_search`, `web_fetch` (app_tools.go:143-144).

These are already well-structured but need two improvements:

1. **WebFetch → Haiku subagent gate** (matching Claude Code's pattern):
   - HTML → Markdown conversion happens outside main context
   - Haiku-level model reads the page and returns a focused summary
   - Only the summary enters the main context
   - Implementation: `ExtractionProvider` (already exists in config) or dedicated lightweight model

2. **WebSearch → result cap**:
   - Default: top 8 results with title + URL + 1-line snippet
   - Max: configurable, default 10

---

## 3. Implementation Plan

### Phase 1: Tool Output Compaction (low risk, high impact)

**Estimated: 4-6 hours. Immediate benefit: ~50-70% context reduction on tool-heavy turns.**

- [ ] 1.1 Define per-tool `OutputPolicy` map in `tools/` package
- [ ] 1.2 Add `Truncate(data []byte, policy OutputPolicy) []byte` helper
- [ ] 1.3 Modify `CallTool` in app_tools.go to apply truncation after handler returns
- [ ] 1.4 Add consumed-result compaction — after each assistant turn, replace previous tool results with markers
- [ ] **Verify**: `go build ./...` + test with large file read

### Phase 2: Lazy Tool Schema Loading (medium risk, highest impact)

**Estimated: 8-12 hours. Benefit: ~85-95% reduction in tool schema tokens.**

- [ ] 2.1 Define `ToolIndex` struct (category → tool names + keywords)
- [ ] 2.2 Build `BuildToolIndex() []ToolIndexEntry` from existing registry
- [ ] 2.3 Implement `tool_search` meta-tool handler
- [ ] 2.4 Modify `ChatProxy` to use index + core tools instead of all tools
- [ ] 2.5 Add schema cache with TTL (per-turn or per-session)
- [ ] 2.6 Add `--eager-tools` mode for backward compatibility
- [ ] **Verify**: tool_search returns correct schemas; LLM can discover and call any tool

### Phase 3: Subagent Context Isolation

**Estimated: 4-6 hours. Benefit: ~70-80% reduction in subagent context overhead.**

- [ ] 3.1 Filter tool index by agent's configured `Tools` + `Skills`
- [ ] 3.2 Build minimal system prompt for subagents (no global skill/docs injection)
- [ ] 3.3 Route subagent LLM calls through the same lazy loading path
- [ ] **Verify**: subagent with 3 tools + 2 skills uses <8k baseline tokens

### Phase 4: WebFetch Haiku Gate

**Estimated: 3-4 hours. Benefit: ~90% reduction in web content tokens.**

- [ ] 4.1 Use `ExtractionProvider` (or dedicated lightweight model config) for page summarization
- [ ] 4.2 Fetch HTML → convert to Markdown → send to lightweight model → return summary
- [ ] 4.3 Add user-facing `depth` parameter: "summary" | "detailed" | "full"
- [ ] **Verify**: fetching a 50KB web page adds <2KB to context

---

## 4. Token Savings Estimate

| Scenario | Before | After | Reduction |
|----------|--------|-------|-----------|
| Chat turn (no tools) | ~35k | ~8k | 77% |
| Chat turn (5 internal tools) | ~35k + 27k = 62k | ~8k + 2.6k = 10.6k | 83% |
| Chat turn (5 internal + MCP tools) | ~35k + 27k + 20k = 82k | ~8k + 3k = 11k | 87% |
| Subagent execution | ~40k | ~5k | 87% |
| WebFetch (50KB page) | ~50k in context | ~2k summary | 96% |
| Large file read (100KB) | ~100k | ~8k truncated | 92% |

---

## 5. Risk Analysis

| Risk | Level | Mitigation |
|------|-------|-----------|
| LLM can't discover the right tool via search | Medium | Keyword-rich tool descriptions + fuzzy matching in tool_search. Fallback: `tool_search(query="*")` returns all |
| Truncation loses critical info | Medium | Truncation marker includes re-fetch instructions. On-disk cache for all truncated outputs. LLM can re-read specific sections |
| Extra latency from tool_search round-trip | Low | Schema cache per-turn avoids redundant fetches. Core tools skip search entirely |
| Breaking existing workflows/agents | Low | `--eager-tools` mode preserves old behavior. Phase rollout per module |

---

## 6. References

- [Claude Code Lazy Context Loading #44536](https://github.com/anthropics/claude-code/issues/44536) — ToolSearch pattern: 134k → 5k tokens
- [MCP Hierarchical Tool Management #532](https://github.com/orgs/modelcontextprotocol/discussions/532) — 17% context savings with tree nav
- [CMU/Samsung JIT Schema Passing (arXiv:2511.03728)](https://arxiv.org/html/2511.03728v1) — 6x reduction, 5x TTFT improvement
- [claude-lazy-loading PoC](https://github.com/machjesusmoto/claude-lazy-loading) — 95% reduction (108k → 5k)
- [Anthropic Context Compaction Cookbook](https://platform.claude.com/cookbook/tool-use-automatic-context-compaction) — structured summary preservation
- [Factory AI Context Compression Evaluation](https://www.zenml.io/llmops-database/evaluating-context-compression-strategies-for-long-running-ai-agent-sessions) — anchored iterative summarization scores best
- [LangChain Deep Agents SDK](https://blockchain.news/news/langchain-deep-agents-sdk-context-compression-tools) — filesystem offloading for large outputs
- [SUPO: RL for Summarization (ByteDance/Stanford)](https://ar5iv.labs.arxiv.org/html/2510.06727v1) — learned compression optimization
