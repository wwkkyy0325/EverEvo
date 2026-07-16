//go:build windows

package app

import (
	"everevo/internal/catalog"
	"everevo/internal/plugins/tools/models"
)

// ─── Model catalog API (thin Wails bindings) ────────────────────────

func (a *App) GetCatalogSources() []string { return models.GetCatalogSources() }

func (a *App) GetModelDetail(source, repoID string) *models.ModelDetail {
	return models.GetModelDetail(source, repoID)
}

func (a *App) SearchAllCatalog(query string, filter *catalog.SearchFilter) *catalog.SearchResult {
	return models.SearchAllCatalog(query, filter)
}

func (a *App) SearchCatalog(source, query string, filter *catalog.SearchFilter) *catalog.SearchResult {
	return models.SearchCatalog(source, query, filter)
}

func (a *App) ListModelRevisions(source, repoID string) []string {
	return models.ListModelRevisions(source, repoID)
}

func (a *App) ListModelFiles(source, repoID string) []catalog.FileEntry {
	return models.ListModelFiles(source, repoID)
}

func (a *App) InvalidateCache(key string) {
	models.InvalidateCache(key)
}
