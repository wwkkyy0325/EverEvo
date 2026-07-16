//go:build windows

package app

import (
	"everevo/internal/plugin"
	extplugins "everevo/internal/plugins/tools/extplugins"
)

// ─── External plugin API (delegates to extplugins ToolPlugin) ───────────
//
// All external plugin lifecycle and RPC logic has moved to
// internal/plugins/tools/extplugins/plugin.go. The App methods below are
// thin delegation wrappers that preserve the Wails frontend binding surface.

// ListPlugins scans and returns all installed external plugins.
func (a *App) ListPlugins() ([]plugin.Spec, error) {
	return extplugins.Get().ListPlugins()
}

// GetPluginStatus returns the runtime status of a plugin.
func (a *App) GetPluginStatus(name string) plugin.Status {
	return extplugins.Get().GetPluginStatus(name)
}

// StartPlugin starts an external plugin by name.
func (a *App) StartPlugin(name string) error {
	return extplugins.Get().StartPlugin(name)
}

// StopPlugin stops an external plugin by name.
func (a *App) StopPlugin(name string) error {
	return extplugins.Get().StopPlugin(name)
}

// RestartPlugin restarts an external plugin by name.
func (a *App) RestartPlugin(name string) error {
	return extplugins.Get().RestartPlugin(name)
}

// RunPlugin calls a method on an external plugin (auto-starts if needed).
func (a *App) RunPlugin(name, method string, params map[string]any) (map[string]any, error) {
	return extplugins.Get().RunPlugin(name, method, params)
}

// PickPluginFile opens a file dialog to select a .zip plugin package.
func (a *App) PickPluginFile() string {
	path, _ := pickPluginDialog()
	return path
}

// InstallPlugin installs an external plugin from a .zip or directory path.
func (a *App) InstallPlugin(path string) (plugin.Spec, error) {
	return extplugins.Get().InstallPlugin(path)
}

// DeletePlugin uninstalls an external plugin (stops first if running).
func (a *App) DeletePlugin(name string) error {
	return extplugins.Get().DeletePlugin(name)
}

// GetPluginLogs returns the plugin's recent stderr log.
func (a *App) GetPluginLogs(name string) string {
	return extplugins.Get().GetPluginLogs(name)
}

// PluginCreate writes a new plugin from Agent-provided code, installs it,
// and optionally hot-starts it. Supports python, go, node runtimes.
func (a *App) PluginCreate(name, runtime, description, code, methodsStr, deps string, autoStart bool) (map[string]any, error) {
	return extplugins.Get().PluginCreate(name, runtime, description, code, methodsStr, deps, autoStart)
}
