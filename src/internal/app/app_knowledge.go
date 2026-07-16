//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"everevo/internal/memory"
	"everevo/internal/config"
	"everevo/internal/rag"
	knowledgePlugin "everevo/internal/plugins/tools/knowledge"
)

// ── Types (re-exported from plugin for Wails frontend binding) ────────────

type RagContextResult = knowledgePlugin.RagContextResult
type ChatFileInfo = knowledgePlugin.ChatFileInfo

// ── Plugin accessor ──────────────────────────────────────────────────────

func getKnowledgePlugin() knowledgePlugin.Service {
	return knowledgePlugin.Get()
}

// ── Rag store singleton (used by app.go and app_memory.go) ───────────────

var ragStore *rag.Store
var ragStoreOnce sync.Once

func (a *App) getRagStore() (*rag.Store, error) {
	var initErr error
	ragStoreOnce.Do(func() {
		ragStore, initErr = rag.NewStore()
	})
	return ragStore, initErr
}

// ── Knowledge base CRUD (Wails-exposed) ──────────────────────────────────

// CreateKnowledgeBase creates a new knowledge base bound to a domain library.
func (a *App) CreateKnowledgeBase(name, modelDir, libraryID string) (rag.KnowledgeBase, error) {
	if err := a.validateLibraryID(libraryID); err != nil {
		return rag.KnowledgeBase{}, fmt.Errorf("创建知识库失败: %w", err)
	}
	kb, err := getKnowledgePlugin().CreateKB(name, modelDir, libraryID)
	if err == nil {
		a.emitChanged("kb:changed", "update", kb.ID)
	}
	return kb, err
}

// AddTexts chunks, embeds and stores text documents into a knowledge base.
func (a *App) AddTexts(kbID string, texts []string, metadata map[string]string) (int, error) {
	n, err := getKnowledgePlugin().AddTexts(kbID, texts, metadata)
	if err == nil {
		a.emitChanged("kb:changed", "update", kbID)
	}
	return n, err
}

// SearchKnowledge performs semantic + keyword hybrid search in a knowledge base.
func (a *App) SearchKnowledge(kbID, query string, k int, filter map[string]string) ([]rag.SearchResult, error) {
	return getKnowledgePlugin().SearchKnowledge(kbID, query, k, filter)
}

// ListKnowledgeBases lists all knowledge bases, optionally filtered by library.
func (a *App) ListKnowledgeBases(libraryID string) ([]rag.KnowledgeBase, error) {
	return getKnowledgePlugin().ListKnowledgeBases(libraryID)
}

// DeleteKnowledgeBase deletes a knowledge base and all its data.
func (a *App) DeleteKnowledgeBase(kbID string) error {
	if err := getKnowledgePlugin().DeleteKB(kbID); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// ClearKnowledgeBase removes all documents from a KB while preserving metadata.
func (a *App) ClearKnowledgeBase(kbID string) error {
	if err := getKnowledgePlugin().ClearKB(kbID); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// DeleteKBChunks deletes specific documents from a knowledge base by ID.
func (a *App) DeleteKBChunks(kbID string, ids []string) (int, error) {
	n, err := getKnowledgePlugin().DeleteKBChunks(kbID, ids)
	if err == nil {
		a.emitChanged("kb:changed", "update", kbID)
	}
	return n, err
}

// ListKBDocuments lists all documents in a knowledge base.
func (a *App) ListKBDocuments(kbID string) ([]rag.DocEntry, error) {
	return getKnowledgePlugin().ListKBDocuments(kbID)
}

// SetKnowledgeBaseLibrary moves a KB to a different domain library.
func (a *App) SetKnowledgeBaseLibrary(kbID, libraryID string) error {
	if err := a.validateLibraryID(libraryID); err != nil {
		return fmt.Errorf("设置知识库领域失败: %w", err)
	}
	if err := getKnowledgePlugin().SetKBLibrary(kbID, libraryID); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// UpdateKBModelDir rebinds a KB's embedding model. Only allowed for empty KBs.
func (a *App) UpdateKBModelDir(kbID, newDir string) error {
	if err := getKnowledgePlugin().UpdateKBModelDir(kbID, newDir); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// MigrateKBModel re-embeds all docs with a new model and rebinds the KB.
func (a *App) MigrateKBModel(kbID, newDir string) error {
	if err := getKnowledgePlugin().MigrateKBModel(kbID, newDir); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// ── Cross-KB search ──────────────────────────────────────────────────────

// SearchAllKnowledgeBases searches across all KBs in a domain library, with
// optional LLM reranking for relevance when enough candidates are available.
func (a *App) SearchAllKnowledgeBases(query, libraryID string, k, perKB int) ([]RagContextResult, error) {
	results, err := getKnowledgePlugin().SearchAllKBs(query, libraryID, k, perKB)
	if err != nil {
		return nil, err
	}

	// Best-effort LLM rerank when we have enough candidates.
	if len(results) > 3 {
		reranked, err := a.rerankWithLLM(query, results)
		if err == nil && len(reranked) > 0 {
			if len(reranked) > k {
				reranked = reranked[:k]
			}
			return reranked, nil
		}
	}

	return results, nil
}

// ── File parsing ─────────────────────────────────────────────────────────

func (a *App) ParseFileForKB(filePath string) (string, error) {
	return getKnowledgePlugin().ParseFileForKB(filePath)
}

func (a *App) ParseFileBytes(b64Data string, filename string) (string, error) {
	return getKnowledgePlugin().ParseFileBytes(b64Data, filename)
}

func (a *App) SaveChatFile(b64Data string, filename string) (ChatFileInfo, error) {
	return getKnowledgePlugin().SaveChatFile(b64Data, filename)
}

func (a *App) ReadChatFile(path string) (content string, isScanned bool, err error) {
	return getKnowledgePlugin().ReadChatFile(path)
}

func (a *App) ReadMediaFile(path string) (map[string]any, error) {
	return getKnowledgePlugin().ReadMediaFile(path)
}

// ── LLM reranking (stays in App — needs a.cfg + a.chatCompletion) ────────

// rerankWithLLM re-scores candidates via the active LLM provider.
func (a *App) rerankWithLLM(query string, candidates []RagContextResult) ([]RagContextResult, error) {
	if a.cfg == nil || a.cfg.LLM.ActiveProvider == "" {
		return nil, fmt.Errorf("no active provider")
	}

	var prov *config.LLMProvider
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == a.cfg.LLM.ActiveProvider && a.cfg.LLM.Providers[i].Enabled {
			prov = &a.cfg.LLM.Providers[i]
			break
		}
	}
	if prov == nil {
		return nil, fmt.Errorf("active provider not available")
	}

	var sb strings.Builder
	sb.WriteString("评估以下文本片段对回答用户问题的有用程度。对每个片段给出1-5的整数分数（1=完全无关，5=高度相关）。仅输出\"片段N: X分\"格式，一行一个。\n\n")
	sb.WriteString("用户问题: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")
	for i, c := range candidates {
		content := c.Content
		if len([]rune(content)) > 300 {
			content = string([]rune(content)[:300]) + "…"
		}
		sb.WriteString(fmt.Sprintf("片段%d: %s\n\n", i+1, content))
	}

	messages := []map[string]string{
		{"role": "user", "content": sb.String()},
	}
	messagesJSON, _ := json.Marshal(messages)
	toolsJSON := json.RawMessage(`[]`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := a.chatCompletion(prov, messagesJSON, toolsJSON, chatOpts{
		MaxTokens: 512,
		Ctx:       ctx,
	})
	if err != nil {
		log.Printf("[rag] rerank LLM call failed: %v", err)
		return nil, err
	}

	content := extractAssistantContent(result)
	if content == "" {
		return nil, fmt.Errorf("empty rerank response")
	}

	re := regexp.MustCompile(`片段(\d+):\s*(\d)`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		log.Printf("[rag] rerank: could not parse scores from: %s", content)
		return nil, fmt.Errorf("unparseable rerank response")
	}

	scores := make(map[int]int)
	for _, m := range matches {
		var idx, score int
		fmt.Sscanf(m[1], "%d", &idx)
		fmt.Sscanf(m[2], "%d", &score)
		scores[idx-1] = score
	}

	sorted := make([]RagContextResult, len(candidates))
	copy(sorted, candidates)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			si := scores[i]
			sj := scores[j]
			if sj > si {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var high []RagContextResult
	for i, c := range sorted {
		if s, ok := scores[i]; ok && s >= 3 {
			high = append(high, c)
		}
	}
	if len(high) > 0 {
		return high, nil
	}
	return sorted, nil
}

func extractAssistantContent(result map[string]any) string {
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
}

// DomainSearch performs a read-only cross-domain search: KB + memory + graph
// from the target domain library. Results are filtered by a divergence gate
// (minSimilarity=0.15) per Agent KB (arXiv 2507.06229): cross-domain knowledge
// must be relevant to the query to avoid noise injection.
func (a *App) DomainSearch(query, targetLibraryID string, minSimilarity float64) (map[string]any, error) {
	if targetLibraryID == "" {
		return nil, fmt.Errorf("targetLibraryID is required")
	}
	if minSimilarity <= 0 {
		minSimilarity = 0.15 // divergence gate default
	}
	result := map[string]any{}

	// 1. KB search
	if kbResults, err := a.SearchAllKnowledgeBases(query, targetLibraryID, 5, 3); err == nil {
		var filtered []RagContextResult
		for _, r := range kbResults {
			if float64(r.Similarity) >= minSimilarity {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			result["kb"] = filtered
		}
	}

	// 2. Memory facts (semantic) + divergence gate
	if a.memoryStore != nil && a.memoryStore.HasVector() {
		dir := a.memoryStore.EmbeddingModelDir()
		if dir != "" {
			emb, err := rag.EmbedQuery(dir, query)
			if err == nil {
				if _, facts, err := a.memoryStore.QueryMemory(emb, 3, targetLibraryID); err == nil {
					var filteredFacts []memory.FactHit
					for _, f := range facts {
						if float64(f.Similarity) >= minSimilarity {
							filteredFacts = append(filteredFacts, f)
						}
					}
					if len(filteredFacts) > 0 {
						result["facts"] = filteredFacts
					}
				}
				if graph, _ := a.memoryStore.RetrieveGraph(emb, 2, targetLibraryID); graph != "" {
					result["graph"] = graph
				}
			}
		}
	}

	return result, nil
}
