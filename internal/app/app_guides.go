//go:build windows

package app

import (
	"encoding/json"
	"os/exec"

	"everevo/internal/guides"
)

// ─── Guide/Tutorial APIs ──────────────────────────────────────────

// ListGuides returns all synced guide documents.
func (a *App) ListGuides(query string) []guides.Guide {
	if a.guideManager == nil { return []guides.Guide{} }
	return a.guideManager.SearchGuides(query)
}

// ReadGuide returns the full markdown content of a guide by ID.
func (a *App) ReadGuide(id string) (string, error) {
	return a.guideManager.ReadGuide(id)
}

// ListGuideSources returns all configured guide sources.
func (a *App) ListGuideSources() []guides.Source {
	if a.guideManager == nil { return []guides.Source{} }
	return a.guideManager.ListSources()
}

// AddGuideSource adds or updates a guide source.
func (a *App) AddGuideSource(name, title, url, branch, sourceType string) error {
	src := guides.Source{
		Name: name, Title: title, URL: url, Branch: branch, Type: sourceType, Enabled: true,
	}
	if err := a.guideManager.AddSource(src); err != nil {
		return err
	}
	a.emitChanged("guides:changed", "update", name)
	return nil
}

// RemoveGuideSource removes a guide source by name.
func (a *App) RemoveGuideSource(name string) error {
	if err := a.guideManager.RemoveSource(name); err != nil {
		return err
	}
	a.emitChanged("guides:changed", "update", name)
	return nil
}

// SyncGuides syncs all enabled guide sources.
func (a *App) SyncGuides() []string {
	out := a.guideManager.SyncAll()
	a.emitChanged("guides:changed", "update", "")
	return out
}

// SyncOneGuide syncs a single guide source by name.
func (a *App) SyncOneGuide(name string) (string, error) {
	out, err := a.guideManager.SyncOne(name)
	if err != nil {
		return "", err
	}
	a.emitChanged("guides:changed", "update", name)
	return out, nil
}

// OpenGuidesDir opens the guides directory in file explorer.
func (a *App) OpenGuidesDir() {
	cmd := exec.Command("explorer", guides.GuidesDir())
	cmd.Start()
}

// ─── Guide ResourceProvider methods ───────────────────────────────

// ListGuidesJSON returns all guides as JSON.
func (a *App) ListGuidesJSON() (json.RawMessage, error) {
	return json.Marshal(a.guideManager.ListGuides())
}

// ReadGuideJSON returns a guide's content as JSON (wraps markdown text).
func (a *App) ReadGuideJSON(id string) (json.RawMessage, error) {
	content, err := a.guideManager.ReadGuide(id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"id": id, "content": content})
}
