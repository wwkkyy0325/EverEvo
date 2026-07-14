//go:build windows

package app

import (
	"everevo/internal/config"
	"everevo/internal/downloader"
	"everevo/internal/model"
	"everevo/internal/plugins/tools/models"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── models.Backend implementation (App provides infrastructure) ─────

var _ models.Backend = (*App)(nil)

func (a *App) Config() *config.Config {
	if a.cfg == nil {
		a.cfg = config.Defaults()
	}
	return a.cfg
}

func (a *App) SaveConfig() error {
	return config.Save(a.cfg)
}

func (a *App) EmitEvent(event string, data any) {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, event, data)
	}
}

func (a *App) DownloadManager() *downloader.Manager {
	return a.dlManager
}

// ── modelsAdapter wraps *App to satisfy models.ModelsService ─────

type modelsAdapter struct {
	a *App
}

var _ models.ModelsService = (*modelsAdapter)(nil)

// ── Local model management ──

func (m *modelsAdapter) ListModels() []model.ModelInfo {
	return m.a.ListModels()
}

func (m *modelsAdapter) LoadModelFile(id, name, modelPath string) (model.ModelInfo, error) {
	return m.a.LoadModelFile(id, name, modelPath)
}

func (m *modelsAdapter) UnloadModel(id string) error {
	return m.a.UnloadModel(id)
}

func (m *modelsAdapter) RunModel(id, input string) (string, error) {
	return m.a.RunModel(id, input)
}

func (m *modelsAdapter) ListDownloadedModels() any {
	return m.a.ListDownloadedModels()
}

func (m *modelsAdapter) ListToolModels() any {
	return m.a.ListToolModels()
}

// ── Toolbox ──

func (m *modelsAdapter) EmbedTexts(modelDir string, texts []string) ([][]float32, error) {
	return m.a.EmbedTexts(modelDir, texts)
}

// ── LLM Providers ──

func (m *modelsAdapter) ListProviders() []config.LLMProvider {
	return m.a.ListProviders()
}

func (m *modelsAdapter) GetActiveProvider() any {
	p := m.a.GetActiveProvider()
	if p == nil {
		return nil
	}
	return p
}

func (m *modelsAdapter) SetActiveProvider(id string) error {
	return m.a.SetActiveProvider(id)
}

func (m *modelsAdapter) TestProviderConnection(id string) (string, error) {
	return m.a.TestProviderConnection(id)
}

func (m *modelsAdapter) ListPresets() []config.Preset {
	return m.a.ListPresets()
}
