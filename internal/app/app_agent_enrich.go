//go:build windows

package app

import (
	"fmt"
	"strings"

	"everevo/internal/memory"
)

// enrichAgentPrompt injects domain-scoped memory and knowledge context for
// sub-agents. The agent's LibraryID scopes recall to its domain — a "Go 后端内核"
// agent in the core library gets core-library memory, KB docs, and graph data.
//
// Main agent (Evo, libraryID="") skips injection — the frontend already handles
// its context assembly via chatStore.ts memory recall.
func (a *App) enrichAgentPrompt(base string, userQuery string, libraryID string) string {
	if userQuery == "" || libraryID == "" {
		return base
	}
	if a.memoryStore == nil || !a.memoryStore.HasVector() {
		return base
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return base
	}

	// Domain-scoped memory recall — same pipeline as frontend MemoryRecallScoped.
	result, err := a.MemoryRecallScoped(userQuery, 2, libraryID)
	if err != nil {
		return base
	}

	var parts []string

	// Core facts from this domain
	if core, ok := result["coreSearch"].([]memory.FactHit); ok && len(core) > 0 {
		var lines []string
		for _, f := range core {
			lines = append(lines, fmt.Sprintf("- [%s] %s", f.Category, f.Content))
		}
		parts = append(parts, "领域核心记忆：\n"+strings.Join(lines, "\n"))
	}

	// Extracted facts
	if facts, ok := result["facts"].([]memory.FactHit); ok && len(facts) > 0 {
		var lines []string
		for _, f := range facts {
			lines = append(lines, fmt.Sprintf("- [%s] %s", f.Category, f.Content))
		}
		parts = append(parts, "领域已知事实：\n"+strings.Join(lines, "\n"))
	}

	// Knowledge graph
	if graph, ok := result["graph"].(string); ok && graph != "" {
		parts = append(parts, "领域知识图谱：\n"+graph)
	}

	if len(parts) > 0 {
		base += "\n\n## 领域上下文（" + libraryID + "）\n\n" + strings.Join(parts, "\n\n")
	}

	return base
}
