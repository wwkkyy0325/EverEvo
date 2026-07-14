//go:build windows

package app

import (
	"everevo/internal/auth"
	"everevo/internal/catalog"
	"everevo/internal/plugins/tools/models"
)

// ─── 模型市场 API ──────────────────────────────────────────────
//
// All catalog logic now lives in internal/plugins/tools/models/.
// App methods below are thin delegations for the Wails frontend.

func (a *App) GetCatalogSources() []string { return models.GetCatalogSources() }

func (a *App) GetModelDetail(source, repoID string) *models.ModelDetail {
	return models.GetModelDetail(source, repoID)
}

func (a *App) GetAccounts() []models.AccountInfo {
	return models.GetAccounts(a)
}

func (a *App) VerifyAccount(source string) *auth.UserInfo {
	return models.VerifyAccount(a, source)
}

func (a *App) SetAccountToken(source, token string) error {
	return models.SetAccountToken(a, source, token)
}

func (a *App) OpenLoginPage(source string) {
	models.OpenLoginPage(source)
}

func (a *App) LoginToSource(source string) (string, error) {
	return models.LoginToSource(a, source)
}

func (a *App) SetCatalogCredential(source, token, cookie string) {
	models.SetCatalogCredential(source, token, cookie)
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
