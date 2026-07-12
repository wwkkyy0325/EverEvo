//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"everevo/internal/config"
	"everevo/internal/memory"
	"everevo/internal/rag"
	"everevo/internal/sysinfo"
)

// ─── Sessions ─────────────────────────────────────────────────────

// MemorySessionList returns all chat sessions, newest first.
func (a *App) MemorySessionList() ([]memory.Session, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	list, err := a.memoryStore.ListSessions()
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Session{}
	}
	return list, nil
}

// MemorySessionCreate creates a new session. An empty title defaults to "新对话".
func (a *App) MemorySessionCreate(title, agentID string) (*memory.Session, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	if title == "" {
		title = "新对话"
	}
	id := uuid.NewString()
	if err := a.memoryStore.CreateSession(id, title, agentID); err != nil {
		return nil, err
	}
	a.emitChanged("memory:changed", "create", id)
	return a.memoryStore.GetSession(id)
}

// MemorySessionRename updates a session title.
func (a *App) MemorySessionRename(id, title string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameSession(id, title); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "rename", id)
	return nil
}

// MemorySessionDelete removes a session and all of its messages.
func (a *App) MemorySessionDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteSession(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", id)
	return nil
}

// ─── Messages ─────────────────────────────────────────────────────

// MemoryMessageList returns a session's messages ordered by seq.
func (a *App) MemoryMessageList(sessionID string) ([]memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	list, err := a.memoryStore.ListMessages(sessionID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Message{}
	}
	return list, nil
}

// MemoryMessageListRecent returns the last `limit` messages for a session.
// Use this instead of MemoryMessageList on session switch to avoid loading
// the full history for long conversations. limit <= 0 means all messages.
func (a *App) MemoryMessageListRecent(sessionID string, limit int) ([]memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	if limit <= 0 {
		limit = 50
	}
	list, err := a.memoryStore.ListMessagesRecent(sessionID, limit)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.Message{}
	}
	return list, nil
}

// MemoryMessageCount returns the total number of messages in a session.
func (a *App) MemoryMessageCount(sessionID string) int {
	if a.memoryStore == nil {
		return 0
	}
	return a.memoryStore.CountMessages(sessionID)
}

// MemoryMessageUpdateToolJSON updates a message's tool_json field (for appending tool results).
func (a *App) MemoryMessageUpdateToolJSON(msgID, toolJSON string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.UpdateMessageToolJSON(msgID, toolJSON)
}

// MemoryMessageAppend appends a message to a session and returns the stored row.
// toolJSON carries tool_calls / tool results as opaque JSON ("" when none).
func (a *App) MemoryMessageAppend(sessionID, role, content, toolJSON string) (*memory.Message, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆库未就绪")
	}
	msg, err := a.memoryStore.AppendMessage(sessionID, uuid.NewString(), role, content, toolJSON)
	// Unified scheduler: facts, graph, summarize, reflect, link — all from one counter.
	if role == "user" {
		go a.scheduler()
	}
	return msg, err
}

// MemoryMessageClear deletes all messages of a session, keeping the session row.
func (a *App) MemoryMessageClear(sessionID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.ClearMessages(sessionID)
}

// ─── Long-term semantic memory (P1.5) ─────────────────────────────

// MemoryRecall runs a two-pass recall scoped to the current conversation's
// domain (inferred from the last turn). Kept at 2 args for Wails binding
// compatibility with the frontend. Use MemoryRecallScoped to pass a library.
func (a *App) MemoryRecall(query string, k int) (map[string]any, error) {
	return a.MemoryRecallScoped(query, k, "")
}

// MemoryRecallScoped runs a two-pass recall (turn + fact) for cross-session
// context. libraryID scopes the recall to a domain library ("" → infer from
// last turn). Returns {turns,facts,graph,graphTrace,core}; empty arrays when
// no embedding model is bound or no memories exist.
func (a *App) MemoryRecallScoped(query string, k int, libraryID string) (map[string]any, error) {
	empty := map[string]any{"turns": []any{}, "facts": []any{}, "graph": "", "graphTrace": map[string]any{"seedIds": []any{}, "edgeIds": []any{}}, "core": []any{}}
	if a.memoryStore == nil || !a.memoryStore.HasVector() {
		return empty, nil
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return empty, nil
	}
	if k <= 0 {
		k = 3
	}
	emb, err := rag.EmbedQuery(dir, query)
	if err != nil {
		return empty, fmt.Errorf("记忆查询嵌入失败: %w", err)
	}
	// Resolve scope: explicit libraryID wins; otherwise infer from the most
	// recent turn (the active conversation's domain).
	if libraryID == "" {
		libraryID = a.memoryStore.LastTurnLibrary()
	}
	// from other domains don't leak into this chat's context.
	turns, facts, err := a.memoryStore.QueryMemory(emb, k, libraryID)
	if err != nil {
		return empty, err
	}
	// P2: graph retrieval reuses the same embedding (no double-embedding).
	graph, _ := a.memoryStore.RetrieveGraph(emb, k, libraryID)
	graphTrace := a.memoryStore.RetrieveGraphTrace(emb, k, libraryID)
	core, _ := a.memoryStore.ListUserFacts(libraryID)
	if core == nil {
		core = []memory.UserFact{}
	}
	// P8: adaptive importance bump — recalled items get stronger (Ebbinghaus).
	var recalledIDs []string
	for _, t := range turns { recalledIDs = append(recalledIDs, t.ItemID) }
	for _, f := range facts { recalledIDs = append(recalledIDs, f.ItemID) }
	if len(recalledIDs) > 0 {
		a.memoryStore.BumpScore(recalledIDs, true, time.Now().UnixMilli())
	}
	return map[string]any{"turns": turns, "facts": facts, "graph": graph, "graphTrace": graphTrace, "core": core}, nil
}

// MemoryRemember stores a finalized user→assistant turn (the question is
// vectorized; the reply is carried in metadata) and, every N turns, kicks off
// asynchronous fact extraction. Best-effort: skips when no model is bound.
// MemoryRecallExperience returns distilled insights (experience_items) for the
// current workspace. Called by the frontend before each chat turn.
// MemoryEntityLinks returns all cross-domain entity links for visualization.
func (a *App) MemoryEntityLinks() ([]memory.EntityLink, error) {
	if a.memoryStore == nil {
		return nil, nil
	}
	links, err := a.memoryStore.ListEntityLinks()
	if err != nil {
		return nil, err
	}
	if links == nil {
		links = []memory.EntityLink{}
	}
	return links, nil
}

// MemoryExperienceDelete removes a single experience item.
func (a *App) MemoryExperienceDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not ready")
	}
	if err := a.memoryStore.DeleteExperience(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

// MemoryItemDelete removes a single memory item by ID.
func (a *App) MemoryItemDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not ready")
	}
	if err := a.memoryStore.DeleteMemoryItem(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

func (a *App) MemoryRecallExperience(workspaceID string, k int) ([]memory.ExperienceItem, error) {
	if a.memoryStore == nil {
		return nil, nil
	}
	if k <= 0 {
		k = 5
	}
	items, err := a.memoryStore.ListExperience(workspaceID, k)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []memory.ExperienceItem{}
	}
	return items, nil
}

func (a *App) MemoryRemember(userText, reply, sessionID, libraryID string) error {
	if a.memoryStore == nil || userText == "" || reply == "" {
		return nil
	}
	// Try to embed; if no model, still store the turn (just without vector).
	var emb []float32
	if a.memoryStore.HasVector() {
		dir := a.memoryStore.EmbeddingModelDir()
		if dir != "" {
			e, err := rag.EmbedQuery(dir, userText)
			if err == nil {
				emb = e
			}
		}
	}
	itemID := uuid.NewString()
	if err := a.memoryStore.AddTurnMemory(itemID, userText, reply, sessionID, libraryID, emb); err != nil {
		return err
	}
	// Session summarization — runs async, never blocks the chat loop.
	go a.maybeSummarize(sessionID)
	return nil
}

// MemoryStatus reports the embedding-model binding and per-kind counts.
// When libraryID is not empty, graph counts are scoped to that domain.
func (a *App) MemoryStatus(libraryID string) (map[string]any, error) {
	if a.memoryStore == nil {
		return map[string]any{"bound": false, "modelDir": "", "turnCount": 0, "factCount": 0, "nodeCount": 0, "edgeCount": 0}, nil
	}
	dir := a.memoryStore.EmbeddingModelDir()
	tc, fc := a.memoryStore.CountMemory(libraryID)
	return map[string]any{
		"bound":     dir != "" && a.memoryStore.HasVector(),
		"modelDir":  dir,
		"turnCount": tc,
		"factCount": fc,
		"nodeCount": a.memoryStore.NodeCountByLibrary(libraryID),
		"edgeCount": a.memoryStore.CurrentEdgeCountByLibrary(libraryID),
	}, nil
}

// MemoryList returns the k most recent memory items (turn + fact) for the UI,
// optionally scoped to libraryID.
func (a *App) MemoryList(k int, libraryID string) ([]memory.MemoryItem, error) {
	if a.memoryStore == nil {
		return []memory.MemoryItem{}, nil
	}
	if k <= 0 {
		k = 20
	}
	list, err := a.memoryStore.ListMemoryItems(k, libraryID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []memory.MemoryItem{}
	}
	return list, nil
}

// MemoryClear wipes all long-term memory (manifest + vectors).
func (a *App) MemoryClear() error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.ClearMemory(); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "clear", "")
	return nil
}

// MemorySetEmbeddingModel binds a specific embedding model for long-term memory,
// overriding the startup auto-detection. Validates the model can embed first.
func (a *App) MemorySetEmbeddingModel(dir string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if dir == "" {
		return fmt.Errorf("模型目录为空")
	}
	if _, err := rag.EmbedQuery(dir, "test"); err != nil {
		return fmt.Errorf("模型不可用: %w", err)
	}
	if err := a.memoryStore.SetEmbeddingModel(dir); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "rebind", "")
	return nil
}

// MemoryMigrateModel re-embeds all memories with a new model and rebinds.
// Use when switching to a model with a different embedding dimension.
func (a *App) MemoryMigrateModel(newDir string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if newDir == "" {
		return fmt.Errorf("模型目录为空")
	}
	embedBatch := func(texts []string) ([][]float32, error) {
		return rag.EmbedChunks(newDir, texts)
	}
	if err := a.memoryStore.MigrateModel(newDir, embedBatch); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "migrate", "")
	return nil
}

// MemoryGraphList returns the current knowledge graph (nodes + valid edges) for
// the UI viewer.
func (a *App) MemoryGraphList(history bool, libraryID string) (map[string]any, error) {
	if a.memoryStore == nil {
		return map[string]any{"nodes": []any{}, "edges": []any{}}, nil
	}
	nodes, err := a.memoryStore.ListNodesByLibrary(libraryID)
	if err != nil {
		return nil, err
	}
	var edges []memory.GraphEdge
	if history {
		edges, err = a.memoryStore.ListAllEdgesIncludeHistoryByLibrary(libraryID)
	} else {
		edges, err = a.memoryStore.ListAllEdgesByLibrary(libraryID)
	}
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []memory.GraphNode{}
	}
	if edges == nil {
		edges = []memory.GraphEdge{}
	}
	return map[string]any{"nodes": nodes, "edges": edges}, nil
}

// MemoryNodeDelete removes a graph node (and its edges).
func (a *App) MemoryNodeDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteNode(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

// MemoryEdgeDelete removes a single graph edge.
func (a *App) MemoryEdgeDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteEdge(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

// MemoryNodeRename renames a graph entity.
func (a *App) MemoryNodeRename(id, name string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameNode(id, name); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryNodesMerge folds dropID into keepID.
func (a *App) MemoryNodesMerge(keepID, dropID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.MergeNodes(keepID, dropID); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryEdgeRename renames the relation type of an edge.
func (a *App) MemoryEdgeRename(id, newType string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameEdge(id, newType); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryEdgeAdd manually adds a relation (replaces=false coexists).
func (a *App) MemoryEdgeAdd(srcID, dstID, relType string, replaces bool) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.AddEdge(srcID, dstID, relType, "{}", "", "[]", replaces); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryRecallGraphContext does a lightweight keyword search of graph nodes
// matching the query, returning formatted "entity → relation → entity" lines.
// Unlike the full graph retrieval in MemoryRecall, this needs no embedding model.
func (a *App) MemoryRecallGraphContext(query, libraryID string) string {
	if a.memoryStore == nil || query == "" {
		return ""
	}
	nodes, _ := a.memoryStore.SearchNodesByKeyword(query, 8)
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	seen := map[string]bool{}
	for _, n := range nodes {
		edges, _ := a.memoryStore.ListEdgesForNode(n.ID, 5)
		for _, e := range edges {
			key := e.SrcName + e.Type + e.DstName
			if seen[key] { continue }
			seen[key] = true
			sb.WriteString(e.SrcName + " → " + e.Type + " → " + e.DstName + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// MemoryGraphStats returns edge-counts-per-type + top hub nodes.
func (a *App) MemoryGraphStats() (*memory.GraphStats, error) {
	if a.memoryStore == nil {
		return &memory.GraphStats{EdgesPerType: map[string]int{}}, nil
	}
	return a.memoryStore.Stats()
}

// MemoryPolicy returns the current hardware-adaptive memory policy.
func (a *App) MemoryPolicy() (memory.MemoryPolicy, error) {
	if a.memoryStore == nil {
		return memory.DefaultMemoryPolicy(), nil
	}
	return a.memoryStore.Policy(), nil
}

// applyMemoryPolicy computes a hardware-adaptive policy from host RAM/disk and
// persists it to the memory store. Called once at startup.
func (a *App) applyMemoryPolicy() {
	if a.memoryStore == nil {
		return
	}
	dyn := sysinfo.CollectDynamic()
	availRAM := dyn.MemoryTotalGB - dyn.MemoryUsedGB
	if availRAM < 0 {
		availRAM = 0
	}
	var diskFree float64
	for _, d := range dyn.Disks {
		if d.FreeGB > diskFree {
			diskFree = d.FreeGB
		}
	}
	p := memoryPolicyFor(availRAM, diskFree)
	if raw, err := json.Marshal(p); err == nil {
		_ = a.memoryStore.SetPolicyJSON(string(raw))
		log.Printf("[memory] 策略: tier=%s 半衰期=%dd TTL=%dd (availRAM=%.1fGB diskFree=%.1fGB)", p.Tier, p.HalfLifeDays, p.TTLDays, availRAM, diskFree)
	}
}

// memoryPolicyFor maps available RAM (GB) + disk free (GB) to a MemoryPolicy tier.
// RAM-driven; a disk-constrained host (<20GB free) drops one tier (more aggressive).
func memoryPolicyFor(availRAM, diskFreeGB float64) memory.MemoryPolicy {
	std := memory.MemoryPolicy{Tier: "standard", HalfLifeDays: 14, TTLDays: 90, RecallK: 3, ItemCap: 2000, CoreCap: 200, Alpha: 0.7}
	var p memory.MemoryPolicy
	switch {
	case availRAM < 6:
		p = memory.MemoryPolicy{Tier: "low", HalfLifeDays: 7, TTLDays: 30, RecallK: 2, ItemCap: 500, CoreCap: 50, Alpha: 0.7}
	case availRAM <= 16:
		p = std
	default:
		p = memory.MemoryPolicy{Tier: "high", HalfLifeDays: 30, TTLDays: 180, RecallK: 5, ItemCap: 10000, CoreCap: 1000, Alpha: 0.7}
	}
	if diskFreeGB < 20 {
		switch p.Tier {
		case "high":
			p = std
		case "standard":
			p = memory.MemoryPolicy{Tier: "low", HalfLifeDays: 7, TTLDays: 30, RecallK: 2, ItemCap: 500, CoreCap: 50, Alpha: 0.7}
		}
	}
	return p
}

// MemoryCoreList returns permanent core-memory facts (global view, all libraries).
// Kept no-arg for Wails binding compatibility. Use MemoryCoreListByLibrary for
// per-library scoping.
func (a *App) MemoryCoreList() ([]memory.UserFact, error) {
	return a.MemoryCoreListByLibrary("")
}

// MemoryCoreListByLibrary returns core-memory facts scoped to a domain library.
// libraryID "" → all (global); otherwise scoped to that library + legacy 'default'.
func (a *App) MemoryCoreListByLibrary(libraryID string) ([]memory.UserFact, error) {
	if a.memoryStore == nil {
		return []memory.UserFact{}, nil
	}
	facts, err := a.memoryStore.ListUserFacts(libraryID)
	if err != nil {
		return nil, err
	}
	if facts == nil {
		facts = []memory.UserFact{}
	}
	return facts, nil
}

// MemoryCoreAdd inserts a core-memory fact (manual).
func (a *App) MemoryCoreAdd(key, value, category string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.AddUserFact(uuid.NewString(), key, value, category, "high", "manual", ""); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "core", "")
	return nil
}

// MemoryCoreLock sets/clears the locked flag on a core-memory fact.
func (a *App) MemoryCoreLock(id string, locked bool) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LockUserFact(id, locked)
}

// ─── Workspaces (P7) — multi-project isolation ───────────────────

// ─── Domain Libraries (P7) — AI-managed knowledge domains ──────

// LibraryList returns all domain libraries.
func (a *App) LibraryList() ([]map[string]any, error) {
	if a.memoryStore == nil {
		return []map[string]any{}, nil
	}
	list, err := a.memoryStore.LibraryList()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(list))
	for i, lib := range list {
		out[i] = map[string]any{"id": lib.ID, "name": lib.Name, "description": lib.Description, "tags": lib.Tags, "autoCreated": lib.AutoCreated, "useCount": lib.UseCount, "createdAt": lib.CreatedAt}
	}
	return out, nil
}

// LibraryCreate adds a domain library and returns its id.
func (a *App) LibraryCreate(name, description, icon string, autoCreated bool) (string, error) {
	if a.memoryStore == nil {
		return "", fmt.Errorf("记忆库未就绪")
	}
	libID, err := a.memoryStore.LibraryCreate(name, description, icon, autoCreated)
	if err != nil {
		return "", err
	}
	// 三位一体: auto-create a default RAG KB for this domain library.
	dir := detectEmbeddingModelDir()
	if dir != "" {
		if _, kbErr := a.CreateKnowledgeBase(name+"-知识库", dir, libID); kbErr != nil {
			log.Printf("[library] 自动创建 KB 失败: %v", kbErr)
		}
	}
	return libID, nil
}

// LibraryUpdate updates a domain library's mutable fields.
func (a *App) LibraryUpdate(id, name, description, icon string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryUpdate(id, name, description, icon)
}

// LibraryDelete removes a domain library. Caller should cascade data first.
func (a *App) LibraryDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryDelete(id)
}

// LibraryBumpUse increments the usage counter for a domain library. Called by
// the frontend whenever the user switches to that domain.
func (a *App) LibraryBumpUse(id string) {
	if a.memoryStore != nil {
		a.memoryStore.BumpLibraryUse(id)
	}
}

// LibraryMerge repoints all knowledge from dropID to keepID and deletes dropID.
func (a *App) LibraryMerge(keepID, dropID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryMerge(keepID, dropID)
}

// WorkspaceList returns all workspaces.
func (a *App) WorkspaceList() ([]map[string]any, error) {
	if a.memoryStore == nil {
		return []map[string]any{}, nil
	}
	list, err := a.memoryStore.WorkspaceList()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(list))
	for i, ws := range list {
		out[i] = map[string]any{"id": ws.ID, "name": ws.Name, "createdAt": ws.CreatedAt}
	}
	return out, nil
}

// WorkspaceCreate adds a workspace and returns its id.
func (a *App) WorkspaceCreate(name string) (string, error) {
	if a.memoryStore == nil {
		return "", fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.WorkspaceCreate(name)
}

// WorkspaceDelete removes a workspace row.
func (a *App) WorkspaceDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.WorkspaceDelete(id)
}

// MemoryCoreDelete removes a core-memory fact.
func (a *App) MemoryCoreDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteUserFact(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "core", "")
	return nil
}

// runMemorySweep runs a one-shot expired-episodic sweep at boot, then daily. It
// stops when memSweepDone is closed (shutdown). Best-effort; logs only.
// user_facts (core) is never swept.
func (a *App) runMemorySweep() {
	if a.memoryStore == nil {
		return
	}
	sweep := func() {
		changed := false
		// TTL expiry
		if ids, err := a.memoryStore.SweepExpiredPolicy(); err == nil && len(ids) > 0 {
			log.Printf("[memory] TTL 清理 %d 条过期情节记忆", len(ids))
			changed = true
		}
		// P8: soft cap — compress low-importance items via LLM summary before trimming.
		a.maybeCompress()
		// P8: hard capacity cap — prevent unbounded memory growth.
		policy := a.memoryStore.Policy()
		if n, err := a.memoryStore.TrimMemoryCapacity(policy.ItemCap); err == nil && n > 0 {
			log.Printf("[memory] 容量裁剪 %d 条低优先级记忆 (上限 %d)", n, policy.ItemCap)
			changed = true
		}
		// Notify frontend so counts don't silently drift after background cleanup.
		if changed {
			a.emitChanged("memory:changed", "sweep", "")
		}
	}
	sweep() // boot
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sweep()
		case <-a.memSweepDone:
			return
		}
	}
}

// resolveOrCreateLibrary returns the id of the library with the given name,
// creating it (auto_created) if it doesn't exist (P7 domain auto-discovery).
func (a *App) resolveOrCreateLibrary(name string) string {
	if a.memoryStore == nil {
		return "default"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "default"
	}
	list, _ := a.memoryStore.LibraryList()
	for _, lib := range list {
		if strings.EqualFold(lib.Name, name) {
			return lib.ID
		}
	}
	id, err := a.memoryStore.LibraryCreate(name, "由 AI 自动发现", "🤖", true)
	if err != nil {
		log.Printf("[memory] 自动创建领域库失败: %v", err)
		return "default"
	}
	log.Printf("[memory] 自动发现新领域库: %s", name)
	return id
}

// factExtractEvery controls how often fact extraction runs (every N turns).
const factExtractEvery = 5

// maybeExtractFacts extracts stable facts from the last N turns when the turn
// count crosses a multiple of N. All failures are logged only.
func (a *App) maybeExtractFacts() {
	if a.memoryStore == nil {
		return
	}
	tc, _ := a.memoryStore.CountMemory("")
	if tc == 0 || tc%factExtractEvery != 0 {
		return
	}
	// Incremental: only turns newer than the last successful extraction. First
	// run (lastExtractAt unset → 0) → all turns. Items come back ASC (oldest first).
	lastTS, _ := strconv.ParseInt(a.memoryStore.GetMeta("lastExtractAt"), 10, 64)
	items, err := a.memoryStore.ListMemoryItemsSince(lastTS)
	if err != nil || len(items) == 0 {
		return
	}
	var sb strings.Builder
	for _, it := range items {
		if it.Kind == "turn" && it.Content != "" {
			sb.WriteString("用户: ")
			sb.WriteString(it.Content)
			sb.WriteString("\n助手: ")
			sb.WriteString(it.Reply)
			sb.WriteString("\n\n")
		}
	}
	dialogue := strings.TrimSpace(sb.String())
	if dialogue == "" {
		return
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return
	}
	// Extraction can target a cheaper model than chat (LLMConfig.ExtractionProvider).
	p, err := a.resolveExtractionProvider()
	if err != nil {
		log.Printf("[memory] 抽取跳过：无可用供应商 (%v)", err)
		return
	}
	facts, err := a.callExtractFacts(p, dialogue)
	if err != nil {
		log.Printf("[memory] 事实抽取失败: %v", err)
		return
	}
	for _, f := range facts {
		// P7: resolve domains → libraryID + crossTags. New domains auto-created.
		libID, crossTags := "default", "[]"
		if len(f.Domains) > 0 {
			libID = a.resolveOrCreateLibrary(f.Domains[0])
			if len(f.Domains) > 1 {
				ids := make([]string, 0, len(f.Domains)-1)
				for _, n := range f.Domains[1:] {
					ids = append(ids, a.resolveOrCreateLibrary(n))
				}
				b, _ := json.Marshal(ids)
				crossTags = string(b)
			}
		}
		if f.Importance == "high" {
			// Core: identity/preference/constraint — permanent, no decay/TTL.
			if err := a.memoryStore.AddUserFact(uuid.NewString(), f.Category, f.Content, f.Category, "high", "extract", libID); err != nil {
				log.Printf("[memory] 核心事实写入失败: %v", err)
			}
			continue
		}
		emb, err := rag.EmbedQuery(dir, f.Content)
		if err != nil {
			continue
		}
		if err := a.memoryStore.AddFactMemory(uuid.NewString(), f.Content, f.Category, f.Importance, libID, crossTags, emb); err != nil {
			log.Printf("[memory] 事实写入失败: %v", err)
		}
	}
	if len(facts) > 0 {
		log.Printf("[memory] 抽取并写入 %d 条事实", len(facts))
	}
	// P2: extract entities/relations into the temporal knowledge graph (same
	// dialogue, best-effort, shares the every-N-turns gate).
	a.maybeExtractGraph(p, dialogue, dir)
	// P8: reflection loop — distill reusable insights from recent experience.
	// Runs on a slower cadence (every 20 turns), independently of fact extraction.
	go a.maybeReflect()
	// P8: cross-domain entity linking — auto-discover semantic anchors across libraries.
	go a.maybeLinkEntities(dir)
	// Mark these turns as extracted so the next cycle only processes new turns.
	_ = a.memoryStore.SetMeta("lastExtractAt", strconv.FormatInt(time.Now().UnixMilli(), 10))
	a.emitChanged("memory:changed", "extract", "")
}

// maybeExtractGraph pulls entities/relations from the dialogue into the temporal
// knowledge graph (bi-temporal edges). Best-effort: failures are logged only.
func (a *App) maybeExtractGraph(p *config.LLMProvider, dialogue, dir string) {
	if a.memoryStore == nil || dir == "" {
		return
	}
	entities, relations, err := a.callExtractGraph(p, dialogue)
	if err != nil {
		log.Printf("[memory] 图谱抽取失败: %v", err)
		return
	}
	if len(entities) == 0 && len(relations) == 0 {
		return
	}
	embedFn := func(text string) ([]float32, error) { return rag.EmbedQuery(dir, text) }
	libID, _ := a.memoryStore.DefaultLibrary()
	if err := a.memoryStore.IngestGraph(entities, relations, "", libID, embedFn); err != nil {
		log.Printf("[memory] 图谱写入失败: %v", err)
		return
	}
	log.Printf("[memory] 图谱抽取 %d 实体 / %d 关系", len(entities), len(relations))
	a.emitChanged("memory:changed", "extract", "")
}

// ─── P9 Dream Pipeline (Light → REM → Deep) ────────────────────────────

var dreamMu sync.Mutex

// runDreamPipeline executes the full Light→REM→Deep memory consolidation
// pipeline. Safe to call from cron or manually; lock prevents concurrent runs.
func (a *App) runDreamPipeline() {
	dreamMu.Lock()
	defer dreamMu.Unlock()
	log.Println("[dream] === Light → REM → Deep 管线开始 ===")
	now := time.Now().UnixMilli()

	// ── Light: scan recent turns, extract candidates ──
	tc, fc := a.memoryStore.CountMemory("")
	log.Printf("[dream] Light: 扫描 %d turns + %d facts", tc, fc)
	// Collect recent memory items as light candidates.
	items, _ := a.memoryStore.ListMemoryItems(30, "")
	lightCount := 0
	for _, it := range items {
		id := fmt.Sprintf("dc_%x_%s", now, it.ID[:8])
		if err := a.memoryStore.AddDreamCandidate(id, it.ID, it.Kind, "light", now); err == nil {
			lightCount++
		}
	}
	log.Printf("[dream] Light: %d 候选入队", lightCount)

	// ── REM: cross-domain linking + pattern discovery ──
	remCount := 0
	if lightCount > 0 {
		// Update candidates to REM stage.
		a.memoryStore.PromoteDreamStage("light", "rem")
		// Cross-domain entity linking.
		if a.memoryStore != nil {
			libs, _ := a.memoryStore.LibraryList()
			for i := 0; i < len(libs); i++ {
				for j := i + 1; j < len(libs); j++ {
					n, _ := a.memoryStore.LinkEntitiesAcrossLibraries(libs[i].ID, libs[j].ID)
					remCount += n
				}
			}
		}
		// Reflect on candidates for patterns.
		if dir := a.memoryStore.EmbeddingModelDir(); dir != "" {
			a.reflectOnCandidates(items)
		}
		_, _ = a.memoryStore.PromoteDreamStage("rem", "deep")
	}
	log.Printf("[dream] REM: %d 跨域链接 + 模式提炼", remCount)

	// ── Deep: multi-dimensional scoring + promotion/demotion ──
	policy := a.memoryStore.Policy()
	promoted, demoted, deleted := a.memoryStore.PromoteByScore(policy.ItemCap)
	log.Printf("[dream] Deep: ↑%d 晋升 ↓%d 降级 ✕%d 裁剪 (上限 %d)",
		promoted, demoted, deleted, policy.ItemCap)

	// ── Dream diary ──
	a.generateDreamDiary(lightCount, remCount, promoted, demoted, deleted)
	a.memoryStore.ClearDreamCandidates()
	log.Println("[dream] === 管线完成 ===")
	a.emitChanged("memory:changed", "dream", "")
}

// reflectOnCandidates does a lightweight LLM pass over recent items to find
// patterns and connections. Results are stored as experience_items.
func (a *App) reflectOnCandidates(items []memory.MemoryItem) {
	if len(items) < 5 {
		return
	}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString("- [" + it.Kind + "] " + it.Content + "\n")
	}
	p, err := a.resolveExtractionProvider()
	if err != nil {
		return
	}
	insights, err := a.callReflect(p, sb.String())
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	libID, _ := a.memoryStore.DefaultLibrary()
	for _, in := range insights {
		if in.Confidence < 0.5 {
			continue
		}
		id := uuid.NewString()
		_ = a.memoryStore.AddExperience(id, libID, in.Kind, in.Content, in.Context, in.Confidence, now)
	}
}

// generateDreamDiary writes a human-readable dream log entry.
func (a *App) generateDreamDiary(light, rem, promoted, demoted, deleted int) {
	entry := fmt.Sprintf("# EverEvo 梦境日记 — %s\n"+
		"## Light: 扫描 %d 条候选\n"+
		"## REM: %d 跨域链接\n"+
		"## Deep: ↑%d 晋升 ↓%d 降级 ✕%d 裁剪\n",
		time.Now().Format("2006-01-02 15:04"), light, rem, promoted, demoted, deleted)
	log.Printf("[dream] %s", entry)
	// Also emit to frontend via event.
}

// startDreamScheduler runs the dream pipeline on a cron schedule.
func (a *App) startDreamScheduler() {
	go func() {
		// Run once at startup after a delay to avoid blocking boot.
		time.Sleep(5 * time.Minute)
		a.runDreamPipeline()
		// Then every 6 hours.
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.runDreamPipeline()
			case <-a.memSweepDone:
				return
			}
		}
	}()
}

// summaryEvery controls how often a session is re-summarized (every N messages).
const summaryEvery = 10

// ─── P8 Cross-Domain Entity Linking ──────────────────────────────────────

// maybeLinkEntities discovers semantic anchors between entities in different
// domain libraries. Triggered after graph extraction; uses vector similarity
// to find candidate linked entities, then LLM to confirm the relationship.
func (a *App) maybeLinkEntities(dir string) {
	if a.memoryStore == nil || dir == "" {
		return
	}
	// Only run every 10th fact extraction cycle to avoid excessive LLM calls.
	tc, _ := a.memoryStore.CountMemory("")
	if tc%50 != 0 || tc == 0 {
		return
	}
	// Get all libraries
	libs, _ := a.memoryStore.LibraryList()
	if len(libs) < 2 {
		return // Need at least 2 libraries for cross-domain linking
	}
	log.Printf("[link] 开始跨域实体链接扫描")
	linked := 0
	for i := 0; i < len(libs); i++ {
		for j := i + 1; j < len(libs); j++ {
			n, _ := a.memoryStore.LinkEntitiesAcrossLibraries(libs[i].ID, libs[j].ID)
			linked += n
		}
	}
	if linked > 0 {
		log.Printf("[link] 跨域链接完成: %d 条", linked)
		a.emitChanged("memory:changed", "link", "")
	}
}

// ─── P8 Reflection Loop (Self-Evolution) ─────────────────────────────────

// reflectEvery controls how often the reflection loop runs (every N turns).
const reflectEvery = 20

// maybeReflect runs the self-evolution reflection loop: it collects recent
// conversation turns + extracted facts + graph context, then asks the LLM to
// distill reusable insights. High-confidence insights are stored as
// experience_items for future recall.
func (a *App) maybeReflect() {
	if a.memoryStore == nil {
		return
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return
	}
	tc, _ := a.memoryStore.CountMemory("")
	if tc < reflectEvery || tc%reflectEvery != 0 {
		return
	}
	log.Printf("[reflect] 开始反思蒸馏 (第 %d 轮)", tc)

	items, _ := a.memoryStore.ListMemoryItems(16, "")
	var dialogue strings.Builder
	for _, it := range items {
		if it.Kind == "turn" {
			dialogue.WriteString("用户: " + it.Content + "\n")
			if it.Reply != "" {
				dialogue.WriteString("助手: " + it.Reply + "\n")
			}
		}
	}
	if dialogue.Len() == 0 {
		return
	}
	p, err := a.resolveExtractionProvider()
	if err != nil {
		log.Printf("[reflect] 无法获取提取供应商: %v", err)
		return
	}
	insights, err := a.callReflect(p, dialogue.String())
	if err != nil {
		log.Printf("[reflect] 反思蒸馏失败: %v", err)
		return
	}
	now := time.Now().UnixMilli()
	libID, _ := a.memoryStore.DefaultLibrary()
	stored := 0
	for _, in := range insights {
		if in.Confidence < 0.5 {
			continue
		}
		id := uuid.NewString()
		if err := a.memoryStore.AddExperience(id, libID, in.Kind, in.Content, in.Context, in.Confidence, now); err != nil {
			continue
		}
		stored++
	}
	if stored > 0 {
		log.Printf("[reflect] 蒸馏完成: %d 条经验", stored)
	}
}

type reflectInsight struct {
	Kind       string  `json:"kind"`
	Content    string  `json:"content"`
	Context    string  `json:"context"`
	Confidence float64 `json:"confidence"`
}

func (a *App) callReflect(p *config.LLMProvider, dialogue string) ([]reflectInsight, error) {
	sysPrompt := `你是反思提炼 Agent。分析以下对话，提炼可复用的经验教训。只输出有长期价值的洞察。

输出 JSON: [{"kind":"insight|lesson|strategy|error_pattern","content":"...","context":"触发场景","confidence":0.8}]

规则:
- confidence 0.5-1.0
- 只提炼可跨对话复用的经验
- 无可用经验时返回 []`

	toolsJSON := json.RawMessage(`[{"type":"function","function":{"name":"reflect","parameters":{"type":"object","properties":{"insights":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"content":{"type":"string"},"context":{"type":"string"},"confidence":{"type":"number"}}}}}}}}]`)

	msgs := []map[string]any{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": dialogue},
	}
	msgsJSON, _ := json.Marshal(msgs)
	result, err := a.chatCompletion(p, msgsJSON, toolsJSON, chatOpts{})
	if err != nil {
		return nil, err
	}
	return parseReflectResult(result), nil
}

func parseReflectResult(result map[string]any) []reflectInsight {
	choices, _ := result["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	content, _ := msg["content"].(string)
	if content == "" {
		return nil
	}
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx > 0 {
			content = content[idx+1:]
		}
		if idx := strings.LastIndex(content, "```"); idx > 0 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}
	var insights []reflectInsight
	if err := json.Unmarshal([]byte(content), &insights); err != nil {
		return nil
	}
	return insights
}

// ─── P8 Soft-Cap Compression ─────────────────────────────────────────────

// maybeCompress runs when memory approaches the soft cap (80% of hard cap).
// Low-importance items are summarized via LLM into a single consolidated fact
// and then deleted — preserving knowledge while freeing capacity.
func (a *App) maybeCompress() {
	if a.memoryStore == nil {
		return
	}
	// Only compress when above 80% of hard cap.
	tc, _ := a.memoryStore.CountMemory("")
	policy := a.memoryStore.Policy()
	if tc < policy.ItemCap*80/100 {
		return
	}
	items, err := a.memoryStore.ListLowImportanceItems(10)
	if err != nil || len(items) < 5 {
		return
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return
	}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString("- [" + it.Kind + "] " + it.Content + "\n")
	}
	if sb.Len() == 0 {
		return
	}
	provider, err := a.resolveExtractionProvider()
	if err != nil {
		return
	}
	summary, err := a.callCompress(provider, sb.String())
	if err != nil || summary == "" {
		return
	}
	libID, _ := a.memoryStore.DefaultLibrary()
	emb, _ := rag.EmbedQuery(dir, summary)
	id := uuid.NewString()
	_ = a.memoryStore.AddFactMemory(id, summary, "summary", "normal", libID, "[]", emb)
	for _, it := range items {
		_ = a.memoryStore.DeleteMemoryItem(it.ID)
	}
	log.Printf("[memory] 摘要压缩: %d 条低优记忆 → 1 条摘要", len(items))
}

func (a *App) callCompress(p *config.LLMProvider, items string) (string, error) {
	sysPrompt := "你是知识压缩 Agent。将以下记忆条目提炼为一段简洁的摘要，保留关键信息，不要有多余解释。"
	msgs := []map[string]any{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": "记忆条目:\n" + items + "\n\n摘要（一段话）："},
	}
	msgsJSON, _ := json.Marshal(msgs)
	result, err := a.chatCompletion(p, msgsJSON, json.RawMessage("[]"), chatOpts{})
	if err != nil {
		return "", err
	}
	choices, _ := result["choices"].([]any)
	if len(choices) == 0 {
		return "", fmt.Errorf("no response")
	}
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	content, _ := msg["content"].(string)
	return strings.TrimSpace(content), nil
}

// maybeSummarize rolls the session's recent messages into a short summary every
// N messages, stored via UpdateSummary. Best-effort: failures logged only.
func (a *App) maybeSummarize(sessionID string) {
	if a.memoryStore == nil || sessionID == "" {
		return
	}
	mc := a.memoryStore.CountMessages(sessionID)
	if mc == 0 || mc%summaryEvery != 0 {
		return
	}
	p, err := a.resolveExtractionProvider()
	if err != nil {
		return
	}
	msgs, err := a.memoryStore.ListMessages(sessionID)
	if err != nil || len(msgs) == 0 {
		return
	}
	start := 0
	if len(msgs) > summaryEvery {
		start = len(msgs) - summaryEvery
	}
	var sb strings.Builder
	for _, m := range msgs[start:] {
		if m.Content == "" {
			continue
		}
		role := "用户"
		if m.Role == "assistant" {
			role = "助手"
		} else if m.Role != "user" {
			continue
		}
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	dialogue := strings.TrimSpace(sb.String())
	if dialogue == "" {
		return
	}
	msgsObj := []map[string]string{
		{"role": "system", "content": "用一到三句话总结以下对话的持久要点（用户意图、关键决定、未决事项）。只输出总结。"},
		{"role": "user", "content": dialogue},
	}
	msgsBytes, err := json.Marshal(msgsObj)
	if err != nil {
		return
	}
	resp, err := a.chatCompletion(p, json.RawMessage(msgsBytes), nil, chatOpts{})
	if err != nil {
		log.Printf("[memory] 摘要失败: %v", err)
		return
	}
	summary := extractChatText(resp)
	if summary == "" {
		return
	}
	if err := a.memoryStore.UpdateSummary(sessionID, summary); err != nil {
		log.Printf("[memory] 摘要写入失败: %v", err)
		return
	}
	log.Printf("[memory] 会话 %s 摘要已更新", sessionID)
}

// MemoryForceExtract triggers a full extraction pass immediately (user-initiated).
func (a *App) MemoryForceExtract() {
	log.Printf("[memory] force extract triggered by user")
	go a.scheduler()
}

// MemorySessionAutoTitle triggers async LLM-based title generation for a session.
// It is best-effort and fires a goroutine so the caller's round-trip is never
// blocked. The frontend should call this after each finalized assistant message.
// Returns nil immediately; the title generation runs in the background.
func (a *App) MemorySessionAutoTitle(sessionID string) error {
	log.Printf("[autotitle] frontend called for session %s", sessionID)
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	go a.autoTitleSession(sessionID)
	return nil
}

// MemorySaveSummary saves a compression summary to long-term memory for future recall.
func (a *App) MemorySaveSummary(text string) error {
	if a.memoryStore == nil || text == "" {
		return nil
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return nil // no embedding model — can't store, but not an error
	}
	emb, err := rag.EmbedQuery(dir, text)
	if err != nil {
		return err
	}
	libID, _ := a.memoryStore.DefaultLibrary()
	return a.memoryStore.AddFactMemory(uuid.NewString(), text, "summary", "normal", libID, "[]", emb)
}

// autoTitleSession renames a session with an LLM-generated title when it still
// has the default "新对话" name and enough messages have accumulated.
// Best-effort: failures are logged only and never affect the chat loop.
func (a *App) autoTitleSession(sessionID string) {
	if a.memoryStore == nil || sessionID == "" {
		log.Printf("[autotitle] skip: store=%v id=%q", a.memoryStore == nil, sessionID)
		return
	}
	sess, err := a.memoryStore.GetSession(sessionID)
	if err != nil || sess == nil {
		log.Printf("[autotitle] skip: GetSession err=%v", err)
		return
	}
	if sess.Title != "新对话" {
		log.Printf("[autotitle] skip: title already set to %q", sess.Title)
		return
	}
	totalMsgs := a.memoryStore.CountMessages(sessionID)
	if totalMsgs < 4 {
		log.Printf("[autotitle] skip: only %d messages", totalMsgs)
		return
	}
	metaKey := "autoTitleAt_" + sessionID
	if a.memoryStore.GetMeta(metaKey) != "" {
		log.Printf("[autotitle] skip: already attempted")
		return
	}
	p, err := a.resolveExtractionProvider()
	if err != nil {
		log.Printf("[autotitle] skip: no provider (%v)", err)
		return
	}
	msgs, err := a.memoryStore.ListMessages(sessionID)
	if err != nil || len(msgs) == 0 {
		log.Printf("[autotitle] skip: ListMessages err=%v len=%d", err, len(msgs))
		return
	}
	start := 0
	if len(msgs) > 12 {
		start = len(msgs) - 12
	}
	var sb strings.Builder
	for _, m := range msgs[start:] {
		if m.Content == "" {
			continue
		}
		role := "用户"
		if m.Role == "assistant" {
			role = "助手"
		} else if m.Role != "user" {
			continue
		}
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	dialogue := strings.TrimSpace(sb.String())
	if dialogue == "" {
		log.Printf("[autotitle] skip: empty dialogue")
		return
	}
	// Use function-calling (structured JSON output) for reliable title extraction.
	// Avoids ambiguity with reasoning_content / extra text / quote stripping.
	titleTool := json.RawMessage(`[{"type":"function","function":{"name":"set_title","description":"设置对话标题","parameters":{"type":"object","properties":{"title":{"type":"string","description":"简洁标题，最多15个汉字或30个英文字符"}},"required":["title"]}}}]`)
	msgsObj := []map[string]string{
		{"role": "system", "content": "为以下对话生成一个简洁的标题（最多15个汉字或30个英文字符）。调用 set_title 工具输出标题。"},
		{"role": "user", "content": dialogue},
	}
	msgsBytes, err := json.Marshal(msgsObj)
	if err != nil {
		log.Printf("[autotitle] skip: marshal error %v", err)
		return
	}
	resp, err := a.chatCompletion(p, json.RawMessage(msgsBytes), titleTool, chatOpts{MaxTokens: 128})
	if err != nil {
		log.Printf("[autotitle] LLM call failed: %v", err)
		return
	}
	title := extractToolArg(resp, "set_title", "title")
	if title == "" {
		title = extractChatText(resp)
	}
	title = strings.TrimSpace(title)
		// Fallback: use first user message as title if LLM failed.
		if title == "" {
			for _, m := range msgs {
				if m.Role == "user" && m.Content != "" {
					content := strings.TrimSpace(m.Content)
					runes := []rune(content)
					if len(runes) > 5 {
						content = string(runes[:5]) + "…"
					}
					title = content
					break
				}
			}
		}
	log.Printf("[autotitle] title=%q runes=%d", title, len([]rune(title)))
	if title == "" || len([]rune(title)) > 30 {
		log.Printf("[autotitle] skip: invalid title")
		return
	}
	if err := a.memoryStore.RenameSession(sessionID, title); err != nil {
		log.Printf("[autotitle] rename failed: %v", err)
		return
	}
	log.Printf("[autotitle] SUCCESS session=%s title=%q", sessionID, title)
	_ = a.memoryStore.SetMeta(metaKey, fmt.Sprintf("%d", totalMsgs))
	a.emitChanged("memory:changed", "rename", sessionID)
}

// extractToolArg pulls a named argument from the first matching tool call in a
// chatCompletion response. Returns "" if the tool wasn't called.
func extractToolArg(resp map[string]any, toolName, argName string) string {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	ch, _ := choices[0].(map[string]any)
	msg, _ := ch["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	tcs, _ := msg["tool_calls"].([]any)
	for _, tc := range tcs {
		call, _ := tc.(map[string]any)
		fn, _ := call["function"].(map[string]any)
		if name, _ := fn["name"].(string); name != toolName {
			continue
		}
		argsStr, _ := fn["arguments"].(string)
		var args map[string]any
		if json.Unmarshal([]byte(argsStr), &args) == nil {
			if v, ok := args[argName].(string); ok {
				return v
			}
		}
	}
	return ""
}

// extractReasoningText pulls reasoning_content from a chatCompletion response
// (DeepSeek / Qwen reasoning models put the final answer here when content is empty).
func extractReasoningText(resp map[string]any) string {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	ch, _ := choices[0].(map[string]any)
	msg, _ := ch["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	if s, ok := msg["reasoning_content"].(string); ok && s != "" {
		return s
	}
	return ""
}

// extractChatText pulls the assistant text out of a chatCompletion response
// ({choices:[{message:{content}}]}). Returns "" if absent.
func extractChatText(resp map[string]any) string {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	ch, _ := choices[0].(map[string]any)
	msg, _ := ch["message"].(map[string]any)
	s, _ := msg["content"].(string)
	return s
}

// extractedFact is a single fact parsed from the LLM's tool_call.
type extractedFact struct {
	Category   string   `json:"category"`
	Content    string   `json:"content"`
	Importance string   `json:"importance"` // high|medium|low — high → core (user_facts)
	Domains    []string `json:"domains"`    // domain library names (P7 auto-x)
}

// callExtractFacts asks the provider to extract stable facts from a dialogue
// via the extract_facts function-calling tool.
func (a *App) callExtractFacts(p *config.LLMProvider, dialogue string) ([]extractedFact, error) {
	msgsObj := []map[string]string{
		{"role": "system", "content": "从以下对话抽取值得长期记住的事实（用户偏好、关键信息、重要事件）。只抽稳定事实，跳过闲聊和无意义内容。无则返回空数组。"},
		{"role": "user", "content": dialogue},
	}
	msgsBytes, err := json.Marshal(msgsObj)
	if err != nil {
		return nil, err
	}
	tools := json.RawMessage(`[{"type":"function","function":{"name":"extract_facts","description":"从对话抽取值得长期记住的事实或偏好","parameters":{"type":"object","properties":{"facts":{"type":"array","items":{"type":"object","properties":{"category":{"enum":["preference","fact","event","relationship"]},"content":{"type":"string"},"importance":{"enum":["high","medium","low"],"description":"high=身份/长期偏好/项目约束(永久); low=临时闲聊/一次性调试"},"domains":{"type":"array","items":{"type":"string"},"description":"这条事实属于哪些领域库名称(如 法律/数学/生活)。AI 可自动发现新领域"}},"required":["category","content"]}}},"required":["facts"]}}}]`)
	resp, err := a.chatCompletion(p, json.RawMessage(msgsBytes), tools, chatOpts{})
	if err != nil {
		return nil, err
	}
	return parseExtractFacts(resp)
}

// parseExtractFacts pulls facts out of the normalized OpenAI-shape response.
// tool_calls[i].function.arguments is a JSON string (per chatCompletion docs).
func parseExtractFacts(resp map[string]any) ([]extractedFact, error) {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, nil
	}
	ch, _ := choices[0].(map[string]any)
	msg, _ := ch["message"].(map[string]any)
	tcs, _ := msg["tool_calls"].([]any)
	for _, tc := range tcs {
		call, _ := tc.(map[string]any)
		fn, _ := call["function"].(map[string]any)
		argsStr, _ := fn["arguments"].(string)
		if argsStr == "" {
			continue
		}
		var wrapper struct {
			Facts []extractedFact `json:"facts"`
		}
		if json.Unmarshal([]byte(argsStr), &wrapper) == nil {
			return wrapper.Facts, nil
		}
	}
	return nil, nil
}

// callExtractGraph asks the provider to extract entities and relations from a
// dialogue via the extract_graph function-calling tool (knowledge-graph triples).
func (a *App) callExtractGraph(p *config.LLMProvider, dialogue string) ([]memory.ExtractedEntity, []memory.ExtractedRelation, error) {
	msgsObj := []map[string]string{
		{"role": "system", "content": "从以下对话抽取实体和它们之间的关系（知识图谱三元组）。\n规则：\n1. 实体是具体的人/物/地点/项目/技术等；指代用户本人一律用「用户」（不要用 我/俺/User/本人）。\n2. 关系用规范谓词，优先：使用/喜欢/就职于/位于/属于/认识/成立于。避免同义变体（用/采用→使用；喜爱/偏好→喜欢）。\n3. 每条关系标 replaces：语义是「改用/换成/不再」（与旧事实互斥）设 true；「也/还/新增」（与旧事实共存）设 false；拿不准设 false。\n4. 只抽明确出现的事实，跳过闲聊与无意义内容。无则返回空数组。\n示例：对话「我现在主要用 Go，之前是 Python」→ entities:[{name:用户,type:person},{name:Go,type:language},{name:Python,type:language}], relations:[{subject:用户,predicate:使用,object:Go,replaces:true}]（改用，互斥）"},
		{"role": "user", "content": dialogue},
	}
	msgsBytes, err := json.Marshal(msgsObj)
	if err != nil {
		return nil, nil, err
	}
	tools := json.RawMessage(`[{"type":"function","function":{"name":"extract_graph","description":"从对话抽取实体和关系三元组","parameters":{"type":"object","properties":{"entities":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string"},"domains":{"type":"array","items":{"type":"string"},"description":"实体属于哪些领域库"}},"required":["name"]}},"relations":{"type":"array","items":{"type":"object","properties":{"subject":{"type":"string"},"predicate":{"type":"string"},"object":{"type":"string"},"replaces":{"type":"boolean"},"domains":{"type":"array","items":{"type":"string"}}},"required":["subject","predicate","object"]}}},"required":["entities","relations"]}}}]`)
	resp, err := a.chatCompletion(p, json.RawMessage(msgsBytes), tools, chatOpts{})
	if err != nil {
		return nil, nil, err
	}
	return parseExtractGraph(resp)
}

// parseExtractGraph pulls entities/relations out of the normalized OpenAI-shape response.
func parseExtractGraph(resp map[string]any) ([]memory.ExtractedEntity, []memory.ExtractedRelation, error) {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, nil, nil
	}
	ch, _ := choices[0].(map[string]any)
	msg, _ := ch["message"].(map[string]any)
	tcs, _ := msg["tool_calls"].([]any)
	for _, tc := range tcs {
		call, _ := tc.(map[string]any)
		fn, _ := call["function"].(map[string]any)
		argsStr, _ := fn["arguments"].(string)
		if argsStr == "" {
			continue
		}
		var wrapper struct {
			Entities  []memory.ExtractedEntity   `json:"entities"`
			Relations []memory.ExtractedRelation `json:"relations"`
		}
		if json.Unmarshal([]byte(argsStr), &wrapper) == nil {
			return wrapper.Entities, wrapper.Relations, nil
		}
	}
	return nil, nil, nil
}
