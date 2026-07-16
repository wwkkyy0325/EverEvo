//go:build windows

package app

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"everevo/internal/memory"
	"everevo/internal/rag"
)

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
	empty := map[string]any{"turns": []any{}, "facts": []any{}, "graph": "", "graphTrace": map[string]any{"seedIds": []any{}, "edgeIds": []any{}}, "core": []any{}, "coreSearch": []any{}, "experience": []any{}}
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
	// Core facts: semantic vector search (new, kind=core_fact).
	// Falls back to SQL ListUserFacts for legacy un-vectorized facts.
	coreSearch, _ := a.memoryStore.QueryCoreFacts(emb, k, libraryID)
	if coreSearch == nil {
		coreSearch = []memory.FactHit{}
	}
	// Experience: semantic vector search (kind=experience).
	expHits, _ := a.memoryStore.QueryExperience(emb, k, libraryID)
	if expHits == nil {
		expHits = []memory.FactHit{}
	}
	// Legacy SQL core facts — supplement when vector results are sparse
	// (pre-vectorization facts have no chromem entry).
	core, _ := a.memoryStore.ListUserFacts("")
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
	return map[string]any{"turns": turns, "facts": facts, "graph": graph, "graphTrace": graphTrace, "core": core, "coreSearch": coreSearch, "experience": expHits}, nil
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
