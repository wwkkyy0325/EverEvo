# Memory Graph — Conversation Persistence + Long/Short-Term Memory

> Three-phase build of an agent memory system for EverEvo: from "chat lost on
> restart" to a **temporal knowledge graph** (Graphiti / LightRAG ideas) running
> fully embedded in Go/Wails.
> **Status: P0+P1+P1.5 + model-mgmt + P2 + P3 + P4 + P5 DONE (2026-07-08) — persistence + fusion memory + flexible embedding-model + temporal knowledge graph + quality/cost/UI/summary + viewer power + temporal view + edit + extraction coref + layered memory (core/decay/TTL) + hardware-adaptive policy.**

## 1. Background

Current state (verified):

- **Vector DB: exists.** `chromem-go` v0.7.0 powers RAG knowledge bases
  ([internal/rag/store.go](internal/rag/store.go)), persisted under
  `data/knowledge/chromem/`. Pure Go, embedded, no CGo.
- **Graph DB: none.** No graph store anywhere in the repo.
- **Chat history: in-memory only.** [chatStore.ts](frontend/src/stores/chatStore.ts)
  holds `messages = ref([])`; the backend ([app_chat.go](app_chat.go)) is fully
  stateless — it only receives `messagesJSON` from the frontend. Restart loses
  everything; there is no session concept, no truncation, no summarization, no
  cross-session recall.
- **Memory (long/short term): none.** `data/agents.json` stores Agent *personas*
  only, not conversation facts or recall.

## 2. Goals

| # | Capability | Phase |
|---|------------|-------|
| 1 | Persist conversation history across restarts; multi-session CRUD/switch | **P0** |
| 2 | Long-term vector recall — remember user facts/preferences across sessions | **P1** |
| 3 | Temporal knowledge graph — entity/relation extraction, multi-hop, bi-temporal recall | **P2** |

**Architecture in one line:** borrow Zep/Graphiti's bi-temporal knowledge-graph
memory and LightRAG's dual-level hybrid retrieval, implemented in pure Go with
embedded storage — no external services.

## 3. Design

### 3.1 Tech stack

| Component | Tech | Role |
|-----------|------|------|
| Graph + chat + facts store | `modernc.org/sqlite` (**pure Go, no CGo**) | sessions/messages, nodes/edges (bi-temporal), user_facts |
| Vector layer | **reuse** chromem-go (existing) | new `mem_*` collections for entity/snippet embeddings |
| Embeddings | **reuse** ONNX MiniLM via [rag.EmbedQuery](internal/rag/embedder.go) | no new model |
| Memory engine | **new** `internal/memory/` | Store + Graph + Ingestor + Retriever |
| Integration point | [chatStore.ts](frontend/src/stores/chatStore.ts) `apiMsgs` assembly + [app_chat.go](app_chat.go) | inject long-term memory; new memory API |

> Discipline: **vectors stay in chromem-go, relations/chat/facts stay in SQLite.**
> The two are joined by `embedding_id` / `entity_id`. SQLite does not do vectors;
> chromem-go does not do relations.

### 3.2 Module layout (new)

```
internal/memory/
├─ store.go      SQLite wrapper: open/migrate + sessions/messages/nodes/edges/facts CRUD + bi-temporal writes
├─ graph.go      multi-hop recursive CTE, entity disambiguation/merge, bi-temporal soft-invalidate
├─ ingestor.go   async extraction pipeline: LLM entity/relation extraction → disambiguate → write to graph
└─ retriever.go  hybrid retrieval: chromem vector seeds → graph expansion → temporal filter → assemble context
```

### 3.3 Data model (SQLite, all bi-temporal where it matters)

```sql
-- P0: conversation persistence
sessions(id TEXT PK, title TEXT, agent_id TEXT, summary TEXT, created_at INT, updated_at INT)
messages(id TEXT PK, session_id TEXT, seq INT, role TEXT, content TEXT, tool_json TEXT, created_at INT)

-- P2: knowledge graph (bi-temporal edges)
nodes(id TEXT PK, type TEXT, name TEXT, props JSON, embedding_id TEXT, created_at INT)
edges(id TEXT PK, src_id TEXT, dst_id TEXT, type TEXT, props JSON,
      valid_from INT, valid_to INT,   -- fact lifetime; valid_to IS NULL = currently valid
      recorded_at INT)                 -- when the system learned it (2nd temporal axis)

-- P2/P1: structured hard facts (whole-picture injection)
user_facts(key TEXT PK, value TEXT, source TEXT, recorded_at INT, valid_to INT)
```

### 3.4 Bi-temporal semantics (Graphiti's core idea)

Each edge carries two time axes: `valid_time` (when the fact holds in reality)
and `recorded_at` (when the system recorded it). When a new fact contradicts an
old one, the old edge's `valid_to` is closed — **not deleted** — so we can query
both "current belief" (`valid_to IS NULL`) and "what we used to believe"
(history). This is the capability vector DBs cannot provide and graphs excel at.

### 3.5 Hybrid retrieval (LightRAG dual-level)

1. user input → ONNX MiniLM embed → chromem-go recall top-K **seed entities**
2. from seeds, recursive CTE expands **1–2 hop neighbors**
3. temporal filter: keep only currently-`valid` edges
4. assemble into the "long-term memory" section of the system prompt

### 3.6 Extraction cost control (GraphRAG's main cost driver)

- **Async**: never block the chat loop; background goroutine.
- **Batched**: extract every N turns (e.g. 5), not every turn.
- **Incremental**: skip already-known entities.
- **Cheap model**: extraction can target a cheaper provider (multi-provider already supported).

## 4. Implementation steps

### Phase 1 (P0) — Dependencies + storage ✅
- [x] 1.1 `go.mod`: add `modernc.org/sqlite`
- [x] 1.2 [storage.go](internal/storage/storage.go) `EnsureDataDir`: add `"memory"` subdir
- [x] 1.3 `internal/memory/store.go`: Open/Close + schema migration (sessions, messages) via `CREATE TABLE IF NOT EXISTS` + WAL
- **verify**: `go build ./...` ✓

### Phase 2 (P0) — Conversation CRUD ✅
- [x] 2.1 sessions CRUD: Create / List / Get / Rename / Delete (cascade) / UpdateSummary
- [x] 2.2 messages CRUD: Append (auto-seq + touch updated_at) / ListBySession / ClearBySession
- **verify**: `go build ./...` ✓

### Phase 3 (P0) — App integration ✅
- [x] 3.1 [app.go](app.go) `App`: add `memoryStore *memory.Store` field
- [x] 3.2 startup: `a.memoryStore, err = memory.NewStore()`; shutdown: `a.memoryStore.Close()`
- **verify**: `go build ./...` ✓

### Phase 4 (P0) — Wails bindings ✅
- [x] 4.1 [app_memory.go](app_memory.go): `MemorySessionList/Create/Rename/Delete` + `MemoryMessageList/Append/Clear`; `emitChanged('memory:changed', ...)` on session create/rename/delete (message append is not broadcast — too frequent). No backend seed: the frontend auto-creates a session on first `loadSessions`.
- **verify**: `go build ./...` ✓

### Phase 5 (P0) — Frontend API + store ✅
- [x] 5.1 `frontend/src/api/memory.ts` + `index.ts` export
- [x] 5.2 [chatStore.ts](frontend/src/stores/chatStore.ts): `sessions`/`currentSessionId` + `loadSessions/createSession/selectSession/renameSession/deleteSession`; `persist()` writes each finalized user/assistant turn; `clearMessages` now starts a fresh session (history preserved)
- **verify**: `vue-tsc --noEmit` ✓

### Phase 6 (P0) — Chat session UI ✅
- [x] 6.1 [ChatPanel.vue](frontend/src/components/ChatPanel.vue): session switcher in header (select + new / rename / delete); `loadSessions` on mount; `memory:changed` live-sync
- **verify**: `npm run build` ✓ (manual GUI run pending)

### Phase 7 (P0) — Docs ✅
- [x] 7.1 `design.md` (new Memory module row + storage note) + `changelog.md` entry

### Phase 8 (P1) — Long-term vector memory ✅
- [x] 8.1 [internal/memory/vector.go](internal/memory/vector.go): chromem-go `mem_longterm` collection (independent DB under `data/memory/chromem`, distinct from KB)
- [x] 8.2 [app_memory.go](app_memory.go) `MemoryRemember`: app-layer embeds (`rag.EmbedQuery`) a "user→assistant" snippet → store; `store.go` `AddMemory` forwards to the vector layer
- [x] 8.3 [app_memory.go](app_memory.go) `MemoryRecall`: embed query → top-K snippet texts; `store.go` `QueryMemory`
- [x] 8.4 [chatStore.ts](frontend/src/stores/chatStore.ts): recall(top-3) injected into the system prompt before `apiMsgs`; `remember` fired on finalized (non-tool) assistant turns
- **verify**: `go build` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓ (cross-session GUI recall pending)

### Phase 8.5 (P1.5) — Fusion recall rebuild ✅
P1's recall was broken: it stored `"用户:X\n助手:Y"` as a single vector but queried with the question only → vector-space mismatch on weak MiniLM → no cross-session recall. Rebuilt per the approved fusion plan (separated storage + LLM fact extraction + UI):
- [x] 8.5.1 [vector.go](internal/memory/vector.go): `QueryWithEmbedding(+filter)`, `AddTurn`/`AddFact`/`QueryTurns`/`QueryFacts`/`Clear`
- [x] 8.5.2 [store.go](internal/memory/store.go): `memory_items` manifest table; `AddTurnMemory`/`AddFactMemory` (SQLite+chromem双写, 同 itemId); `QueryMemory → (turns, facts)`; `ListMemoryItems`/`CountMemory`/`ClearMemory`
- [x] 8.5.3 [app_memory.go](app_memory.go): `MemoryRemember(userText, reply, sessionID)` 存 user 问题向量 + 每 5 轮异步 `extractFacts`; `MemoryRecall → {turns,facts}`; `MemoryStatus`/`MemoryList`/`MemoryClear`; `callExtractFacts` (chatCompletion + extract_facts tool) + `parseExtractFacts`
- [x] 8.5.4 前端: [api/memory.ts](frontend/src/api/memory.ts) 结构化类型 + status/list/clear; [chatStore.ts](frontend/src/stores/chatStore.ts) 双路注入; [Knowledge.vue](frontend/src/components/Knowledge.vue) 记忆区块; [App.vue](frontend/src/App.vue) 移除清空
- **verify**: `go build` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓ (GUI recall + 记忆区块 pending)

### Phase 8.6 — Embedding model 灵活绑定 + 迁移 ✅
Fixes "downloaded a model but shows unbound" (startup auto-detect runs once + Detect missed slim/ONNX-only packages). Gives full manual bind/rebind/migrate.
- [x] [detect.go](internal/toolbox/detect.go): `readHFConfig` 兜底 `onnx/config.json` + `isTextModel` 扩 model_type 白名单 (roberta/e5/bge/gte/nomic/jina)
- [x] [memory/store.go](internal/memory/store.go) `MigrateModel` (memory_items re-embed) + [rag/store.go](internal/rag/store.go) `UpdateKBModelDir`/`MigrateKBModel` (manifest IDs → GetByID → re-embed → drop+recreate)
- [x] [app_memory.go](app_memory.go) `MemorySetEmbeddingModel`/`MemoryMigrateModel` + [app_knowledge.go](app_knowledge.go) `UpdateKBModelDir`/`MigrateKBModel`
- [x] 前端 [Knowledge.vue](frontend/src/components/Knowledge.vue): 对话记忆区块 + KB 卡片各加「嵌入模型」下拉 + 绑定/迁移按钮（空→绑，有数据→迁移弹确认）
- **verify**: `go build` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓ (GUI bind/migrate pending)

### Phase 9 (P2) — Temporal knowledge graph
- [x] 9.1 schema migration: `kg_nodes` / `kg_edges` (bi-temporal) in [store.go](internal/memory/store.go)
- [x] 9.2 [memory/graph.go](internal/memory/graph.go): multi-hop recursive CTE + entity disambiguation (normalized-name merge) + bi-temporal soft-invalidate
- [x] 9.3 [memory/ingestor.go](internal/memory/ingestor.go) + [app_memory.go](app_memory.go) `callExtractGraph`: LLM entity/relation extraction (function-calling schema) → disambiguate → write graph
- [x] 9.4 [memory/retriever.go](internal/memory/retriever.go): vector seeds → graph expansion → temporal filter
- **verify**: answer "what is A's relation to B"; recall history of a changed preference *(compile + unit-verified; GUI pending)*

### Phase 10 (P3) — Quality, cost, retrieval, UI, summaries
- [x] 10.1 Graph quality: predicate normalization (synonym table) + consistent extraction (few-shot prompt, 「用户」统一称谓) — [graph.go](internal/memory/graph.go) `normalizePredicate`, [ingestor.go](internal/memory/ingestor.go), [app_memory.go](app_memory.go) prompt
- [x] 10.2 Temporal semantics: `replaces` flag (switch vs add) — `extract_graph` schema + `AddEdge(...,replaces)` + tests
- [x] 10.3 Cost: incremental extraction (`ListMemoryItemsSince` + meta `lastExtractAt`) + dedicated `ExtractionProvider` ([config.go](internal/config/config.go), `resolveExtractionProvider`, `SetExtractionProvider`, [LLMConfig.vue](frontend/src/components/llm/LLMConfig.vue) dropdown)
- [x] 10.4 Retrieval: adaptive hops + freshness sort + 12-line cap — [retriever.go](internal/memory/retriever.go); recall block cap in [chatStore.ts](frontend/src/stores/chatStore.ts)
- [x] 10.5 Session summaries: `CountMessages` + `maybeSummarize` (every 10 msgs → `UpdateSummary`) + injected into the system prompt
- [x] 10.6 Graph UI: store `ListNodes/ListAllEdges/DeleteNode/DeleteEdge` + `MemoryGraphList/NodeDelete/EdgeDelete` Wails + [Knowledge.vue](frontend/src/components/Knowledge.vue) SVG viewer (dagre layout, click-to-delete)
- **verify**: predicate merge (`用/采用`→`使用`); `replaces` coexistence; incremental extraction; extraction-model dropdown; graph viewer shows/deletes nodes; session summary in prompt *(compile + unit-verified; GUI pending)*

### Phase 11 (P4) — Viewer power + temporal view + edit + extraction coref
- [x] 11.1 Viewer: detail panel, search/filter, layout switch (force/hier), cluster, neighbor highlight, history toggle + contradiction highlight (vis-network)
- [x] 11.2 Temporal: `GraphEdge.RecordedAt` + `ListAllEdgesIncludeHistory` + `MemoryGraphList(history)`; superseded edges render dashed/grey
- [x] 11.3 Extraction quality: `normalizeType` (type synonyms) + entity coreference via embedding similarity (≥0.92 → reuse/merge; chromem `Similarity`)
- [x] 11.4 Edit + stats: `RenameNode`/`MergeNodes`/`Stats` + Wails bindings; viewer rename + stats header (merge/addEdge API ready, UI follow-up)
- [x] 11.5 Recall highlight: `RetrieveGraphTrace` + `MemoryRecall.graphTrace` + chatStore cross-view trace + viewer ⚡召回 button (`setSelection`)
- **verify**: detail panel relations; search filters; layout flips; history shows superseded preference dashed; rename works; ⚡召回 highlights used subgraph *(compile + unit-verified; GUI pending)*

### Phase 12 (P5) — Layered memory + recency decay + TTL + hardware-adaptive policy
- [x] 12.1 Schema + policy: `memory_items` decay cols (last_access/access_count/importance) + `user_facts` table + `MemoryPolicy` + startup compute from `CollectDynamic` (RAM/disk → tier)
- [x] 12.2 Core layer + importance: `user_facts` CRUD + `extract_facts` importance routing (high→core) + forced core injection in recall
- [x] 12.3 Decay recall: `Similarity` passthrough + recency re-rank (`α·cos+(1-α)·0.5^(age/halfLife)`) + access warmth refresh + orphan filter
- [x] 12.4 TTL sweep: `SweepExpiredPolicy` (age≥TTL & score<0.05) + startup goroutine/daily ticker + shutdown cancel
- [x] 12.5 UI: core-memory panel (add/lock/delete) + policy line + chatStore core injection
- **verify**: core survives TTL; recall returns core first; decay re-rank orders by score; low-RAM host tightens policy *(compile + unit-verified; GUI pending)*

### Phase 13 (P6.1) — Project docs recall (llmwiki indexed for chat)
- [x] 13.1 wiki package ([internal/wiki/wiki.go](internal/wiki/wiki.go)): goldmark heading-chunk + `Store` (chromem wiki_docs + SQLite page graph) + `ParseMarkdown` link extraction
- [x] 13.2 embed+reindex: `//go:embed all:docs/llmwiki` in [app_wiki.go](app_wiki.go), startup reindex, `WikiStatus`/`WikiSearch`/`WikiRecall`
- [x] 13.3 chat injection: [chatStore.ts](frontend/src/stores/chatStore.ts) wiki recall block (`项目文档...`)
- [x] 13.4 UI: [Knowledge.vue](frontend/src/components/Knowledge.vue) project-docs panel (pages/chunks status, reindex button, search-test input)
- **verify**: chat "记忆系统怎么设计的" → system prompt includes design.md chunks *(compile + unit-verified; GUI pending)*

### Phase 14 (P7) — Domain Library Grid (replaces workspace)
- [x] 14.1 `domain_libraries` table + `cross_tags` columns + Library CRUD + seed
- [x] 14.2 Auto-classification: `extract_facts`/`extract_graph` `domains` → `resolveOrCreateLibrary` routing
- [x] 14.3 Semantic routing: `classifyQuery` rule-based library matching → chat injection
- [x] 14.4 Cross-library graph: `GraphEdge.CrossTags` + SELECT/INSERT + `AddEdge(...,crossTags)`
- [x] 14.5 UI: library grid cards (🤖 auto-created badge) replacing workspace dropdown
- **verify**: chat "合同法有哪些要点" → router picks 法律库; new domain "数学" auto-appears when chatting about math; cross-library entity bridging visible in graph *(compile + unit-verified; GUI pending)*

## 5. Verification checklist

### P0
- [x] `go build ./...` clean; `npm run build` clean; `vue-tsc --noEmit` clean
- [ ] Conversation history survives app restart *(compile-verified; GUI run pending)*
- [ ] Multiple sessions; create / switch / rename / delete all work *(compile-verified; GUI run pending)*
- [ ] Switching agent persona still works (agent_id recorded per session) *(compile-verified; GUI run pending)*

### P1
- [x] `go build` / `vue-tsc` / `npm run build` clean
- [x] Retrieval adds bounded tokens (top-3 snippets; skipped entirely when no embedding model)
- [ ] A fact stated in session A is recalled in session B via semantic retrieval *(compile-verified; GUI run pending)*

### P2
- [ ] Entities/relations extracted from chat appear in graph
- [ ] Multi-hop query answered (vector seed → graph neighbors)
- [ ] Bi-temporal: contradicting a fact closes the old edge; both states queryable
