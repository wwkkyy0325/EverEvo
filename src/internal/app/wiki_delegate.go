//go:build windows

package app

import (
	"everevo/internal/rag"
	wikiPlugin "everevo/internal/plugins/tools/wiki"
)

// wikiDelegateAdapter adapts *App to the wiki.WikiDelegate interface, avoiding
// method name conflicts with the existing App.WikiSearch / WikiListPages which
// return different types.
type wikiDelegateAdapter struct {
	a *App
}

// Compile-time check: wikiDelegateAdapter satisfies the wiki.WikiDelegate interface.
var _ wikiPlugin.WikiDelegate = (*wikiDelegateAdapter)(nil)

// ─── WikiDelegate implementation (for plugins/tools/wiki) ─────

func (w *wikiDelegateAdapter) WikiSearch(libraryID, query string) ([]wikiPlugin.Chunk, error) {
	libID := w.a.resolveLibraryID(libraryID)
	ws := w.a.getWikiStore(libID)
	if ws == nil || query == "" {
		return []wikiPlugin.Chunk{}, nil
	}
	dir := w.a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		return []wikiPlugin.Chunk{}, nil
	}
	emb, err := rag.EmbedQuery(dir, query)
	if err != nil {
		return nil, err
	}
	hits, err := ws.Search(emb, 5)
	if err != nil {
		return nil, err
	}
	out := make([]wikiPlugin.Chunk, len(hits))
	for i, h := range hits {
		out[i] = wikiPlugin.Chunk{
			Page:    h.Page,
			Heading: h.Heading,
			Content: h.Content,
		}
	}
	return out, nil
}

func (w *wikiDelegateAdapter) WikiRecall(libraryID, query string) (string, error) {
	libID := w.a.resolveLibraryID(libraryID)
	return w.a.WikiRecall(libID, query)
}

func (w *wikiDelegateAdapter) WikiSavePage(libraryID, pageID, title, content string) error {
	libID := w.a.resolveLibraryID(libraryID)
	return w.a.WikiSavePage(libID, pageID, title, content)
}

func (w *wikiDelegateAdapter) WikiReadPage(libraryID, pageID string) (map[string]any, error) {
	libID := w.a.resolveLibraryID(libraryID)
	return w.a.WikiReadPage(libID, pageID)
}

func (w *wikiDelegateAdapter) WikiListPages(libraryID string) ([]wikiPlugin.WikiPageInfo, error) {
	libID := w.a.resolveLibraryID(libraryID)
	pages, err := w.a.WikiListPages(libID)
	if err != nil {
		return nil, err
	}
	if pages == nil {
		return []wikiPlugin.WikiPageInfo{}, nil
	}
	out := make([]wikiPlugin.WikiPageInfo, len(pages))
	for i, p := range pages {
		out[i] = wikiPlugin.WikiPageInfo{
			ID:    p.ID,
			Title: p.Title,
			Path:  p.Path,
		}
	}
	return out, nil
}
