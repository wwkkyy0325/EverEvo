//go:build windows

package app

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/memory"
	"everevo/internal/rag"
	"everevo/internal/wiki"
)

// LlmwikiFS is set by main.go at startup — the embedded docs/llmwiki tree.
var LlmwikiFS embed.FS

// getWikiStore returns the wiki store for a given library, creating it lazily.
func (a *App) getWikiStore(libraryID string) *wiki.Store {
	if libraryID == "" {
		return nil
	}
	a.wikiStoreMu.Lock()
	defer a.wikiStoreMu.Unlock()
	if a.wikiStores == nil {
		a.wikiStores = make(map[string]*wiki.Store)
	}
	if ws, ok := a.wikiStores[libraryID]; ok {
		return ws
	}
	ws, err := wiki.NewStore(libraryID)
	if err != nil {
		log.Printf("[wiki] 创建领域 wiki 失败 (%s): %v", libraryID, err)
		return nil
	}
	a.wikiStores[libraryID] = ws
	return ws
}

// WikiStatus reports the index size for a domain library.
func (a *App) WikiStatus(libraryID string) (map[string]any, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return map[string]any{"pages": 0, "chunks": 0}, nil
	}
	p, c := ws.Status()
	return map[string]any{"pages": p, "chunks": c}, nil
}

// WikiReindex clears and rebuilds the wiki index for a domain library.
// The core library indexes embedded llmwiki docs; other libraries start empty.
func (a *App) WikiReindex(libraryID string) (map[string]any, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return map[string]any{"pages": 0, "chunks": 0}, fmt.Errorf("wiki 未就绪")
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return map[string]any{"pages": 0, "chunks": 0}, nil
	}
	_ = ws.ClearLLMWiki()
	pages, chunks := 0, 0
	var allLinks []wiki.Link
	_ = fs.WalkDir(LlmwikiFS, "docs/llmwiki", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, e := LlmwikiFS.ReadFile(path)
		if e != nil {
			return nil
		}
		pageID := strings.TrimSuffix(filepath.Base(path), ".md")
		chunksList, links := wiki.ParseMarkdown(pageID, string(data))
		if len(chunksList) == 0 {
			return nil
		}
		texts := make([]string, len(chunksList))
		for i, c := range chunksList {
			texts[i] = c.Content
		}
		embs, e := rag.EmbedChunks(dir, texts)
		if e != nil {
			return nil
		}
		if e := ws.IndexPage(pageID, pageID, path, 0, chunksList, embs); e == nil {
			pages++
			chunks += len(chunksList)
			// Register chunk→source bidirectional mapping
			if a.memoryStore != nil {
				a.registerWikiChunks(ws.LibraryID(), pageID, chunksList)
			}
		}
		allLinks = append(allLinks, links...)
		return nil
	})
	_ = ws.IndexLinks(allLinks)
	log.Printf("[wiki] 索引完成 (%s): %d 页 / %d 段", libraryID, pages, chunks)
	return map[string]any{"pages": pages, "chunks": chunks}, nil
}

// WikiSearch returns raw chunks for a query within a domain library.
func (a *App) WikiSearch(libraryID, q string) ([]wiki.Chunk, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil || q == "" {
		return nil, nil
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return nil, nil
	}
	emb, err := rag.EmbedQuery(dir, q)
	if err != nil {
		return nil, nil
	}
	return ws.Search(emb, 5)
}

// WikiRecall returns a formatted text block of the top wiki hits for the chat
// system prompt, scoped to a domain library.
// Uses hierarchical retrieval: if enough leaf chunks share a common parent,
// the parent is returned instead (AutoMergingRetriever pattern).
func (a *App) WikiRecall(libraryID, query string) (string, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil || query == "" {
		return "", nil
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return "", nil
	}
	emb, err := rag.EmbedQuery(dir, query)
	if err != nil {
		return "", nil
	}
	// Retrieve more candidates so AutoMerging has enough signals.
	hits, err := ws.Search(emb, 8)
	if err != nil || len(hits) == 0 {
		return "", nil
	}

	// Hierarchical retrieval via chunk_registry (if available).
	var expanded []wiki.Chunk
	if a.memoryStore != nil {
		// Collect chunk IDs for AutoMerging check.
		chunkIDs := make([]string, 0, len(hits))
		for _, h := range hits {
			if h.ID != "" {
				chunkIDs = append(chunkIDs, h.ID)
			}
		}
		// AutoMergingRetriever: if ≥ 50% of top results share a parent, use parent.
		if parent := a.memoryStore.ParentForLeafs(chunkIDs, 0.5); parent != nil {
			// Fetch parent content from the wiki store page content.
			content, _ := ws.GetPageContent(parent.SourceID)
			if content != "" {
				expanded = append(expanded, wiki.Chunk{
					Page:    parent.SourceID,
					Heading: "[Section Context]",
					Content: truncate(content, 600),
				})
			}
		}
		// Expand siblings for remaining leaf chunks.
		for _, id := range chunkIDs {
			_, prev, next, _ := a.memoryStore.GetChunkSiblings(id)
			if prev != nil {
				if c, err := ws.GetPageContent(prev.SourceID); err == nil {
					expanded = append(expanded, wiki.Chunk{
						Page:    prev.SourceID,
						Heading: "[Adjacent Section]",
						Content: truncate(c, 300),
					})
				}
			}
			if next != nil {
				if c, err := ws.GetPageContent(next.SourceID); err == nil {
					expanded = append(expanded, wiki.Chunk{
						Page:    next.SourceID,
						Heading: "[Adjacent Section]",
						Content: truncate(c, 300),
					})
				}
			}
		}
	}

	// Format results.
	var lines []string
	seen := make(map[string]bool)
	allHits := append(hits, expanded...)
	for _, h := range allHits {
		key := h.Page + h.Heading
		if seen[key] {
			continue
		}
		seen[key] = true
		preview := strings.ReplaceAll(h.Content, "\n", " ")
		preview = truncate(preview, 300)
		head := h.Page
		if h.Heading != "" {
			head += " › " + h.Heading
		}
		lines = append(lines, fmt.Sprintf("📄 %s\n%s", head, preview))
	}
	return strings.Join(lines, "\n\n"), nil
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
// WikiSavePage creates or updates a user wiki page.
func (a *App) WikiSavePage(libraryID, pageID, title, content string) error {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return fmt.Errorf("wiki 未就绪")
	}
	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return fmt.Errorf("未绑定嵌入模型")
	}
	embedFn := func(texts []string) ([][]float32, error) { return rag.EmbedChunks(dir, texts) }
	if err := ws.SavePage(pageID, title, content, embedFn); err != nil {
		return err
	}
	// Register chunks for bidirectional indexing (re-register since old IDs replicate)
	if a.memoryStore != nil {
		chunksList, _ := wiki.ParseMarkdown(pageID, content)
		a.registerWikiChunks(ws.LibraryID(), pageID, chunksList)
	}
	a.emitChanged("wiki:changed", "save", pageID)
	return nil
}

// WikiDeletePage removes a user wiki page.
func (a *App) WikiDeletePage(libraryID, pageID string) error {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return fmt.Errorf("wiki 未就绪")
	}
	if err := ws.DeletePage(pageID); err != nil {
		return err
	}
	a.emitChanged("wiki:changed", "delete", pageID)
	return nil
}

// WikiMovePage moves a wiki page from one domain library to another.
// Reads from source domain, saves to target domain, optionally deletes source copy.
func (a *App) WikiMovePage(fromLibID, toLibID, pageID string, deleteSource bool) error {
	// Read from source.
	page, err := a.WikiReadPage(fromLibID, pageID)
	if err != nil {
		return fmt.Errorf("读取源页面失败: %w", err)
	}
	title, _ := page["title"].(string)
	content, _ := page["content"].(string)
	if title == "" {
		return fmt.Errorf("源页面无标题")
	}
	// Save to target.
	if err := a.WikiSavePage(toLibID, pageID, title, content); err != nil {
		return fmt.Errorf("写入目标领域失败: %w", err)
	}
	// Optionally clean source.
	if deleteSource {
		_ = a.WikiDeletePage(fromLibID, pageID)
	}
	a.emitChanged("wiki:changed", "move", pageID)
	return nil
}

// WikiListPages returns all indexed wiki pages for browsing.
func (a *App) WikiListPages(libraryID string) ([]wiki.WikiPageInfo, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return nil, nil
	}
	pages, err := ws.ListPages()
	if err != nil {
		return nil, err
	}
	if pages == nil {
		pages = []wiki.WikiPageInfo{}
	}
	return pages, nil
}

// WikiReadPage reads a raw markdown page — user pages from DB, llmwiki from embedded FS.
func (a *App) WikiReadPage(libraryID, pageID string) (map[string]any, error) {
	ws := a.getWikiStore(libraryID)
	if ws == nil {
		return nil, fmt.Errorf("wiki not ready")
	}
	// Try user page content first (stored in DB).
	if content, err := ws.GetPageContent(pageID); err == nil && content != "" {
		return map[string]any{"id": pageID, "path": "", "content": content, "source": "user"}, nil
	}
	// Fall back to embedded llmwiki.
	pages, _ := ws.ListPages()
	var pagePath string
	for _, p := range pages {
		if p.ID == pageID {
			pagePath = p.Path
			break
		}
	}
	if pagePath == "" {
		return nil, fmt.Errorf("page not found: %s", pageID)
	}
	data, err := LlmwikiFS.ReadFile(pagePath)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	return map[string]any{
		"id":      pageID,
		"path":    pagePath,
		"content": string(data),
	}, nil
}

// registerWikiChunks registers wiki chunks into the bidirectional chunk_registry.
func (a *App) registerWikiChunks(libraryID, pageID string, chunks []wiki.Chunk) {
	if a.memoryStore == nil || len(chunks) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	entries := make([]memory.ChunkRegistryEntry, len(chunks))
	for i := range chunks {
		chunkID := fmt.Sprintf("%s_%d", pageID, i)
		entries[i] = memory.ChunkRegistryEntry{
			ChunkID:    chunkID,
			SourceType: "wiki",
			SourceID:   pageID,
			ChunkIndex: i,
			ChunkType:  "leaf",
			CreatedAt:  now,
		}
	}
	if err := a.memoryStore.RegisterChunks(entries); err != nil {
		log.Printf("[wiki] chunk registry 注册失败 page=%s: %v", pageID, err)
	}
}

