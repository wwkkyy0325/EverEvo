//go:build windows

package app

import (
	"encoding/json"

	"everevo/internal/marketplace"
	skillsPlugin "everevo/internal/plugins/tools/skills"
	"everevo/internal/skills"
)

// Compile-time check: *App satisfies the skills.SkillsDelegate interface.
var _ skillsPlugin.SkillsDelegate = (*App)(nil)

// ─── SkillsDelegate implementation (for plugins/tools/skills) ─────

// SkillList returns all skills for the plugin delegate.
func (a *App) SkillList(libraryID string) ([]skillsPlugin.SkillInfo, error) {
	if a.skillManager == nil {
		return []skillsPlugin.SkillInfo{}, nil
	}
	list := a.skillManager.ListByLibrary(libraryID)
	out := make([]skillsPlugin.SkillInfo, len(list))
	for i, s := range list {
		out[i] = skillsPlugin.SkillInfo{
			Name:        s.Name,
			Title:       s.Title,
			Description: s.Description,
			Category:    s.Category,
			Package:     s.Package,
			Icon:        s.Icon,
			Enabled:     s.Enabled,
			Tools:       s.Tools,
			LibraryID:   s.LibraryID,
		}
	}
	return out, nil
}

// SkillEnable enables or disables a skill for the plugin delegate.
func (a *App) SkillEnable(name string, enabled bool) error {
	if a.skillManager == nil {
		return nil
	}
	a.skillManager.SetEnabled(name, enabled)
	return a.skillManager.Save()
}

// SkillExport exports all skills as JSON for the plugin delegate.
func (a *App) SkillExport() (json.RawMessage, error) {
	if a.skillManager == nil {
		return json.RawMessage("[]"), nil
	}
	return a.skillManager.Export()
}

// SkillImport imports skills from JSON for the plugin delegate.
func (a *App) SkillImport(data json.RawMessage) error {
	if a.skillManager == nil {
		return nil
	}
	return a.skillManager.Import(data)
}

// SkillMarketList returns the skill marketplace for the plugin delegate.
func (a *App) SkillMarketList() ([]marketplace.SkillPackage, error) {
	pkgs := a.ListMarketSkills()
	return pkgs, nil
}

// SkillMarketInstall installs a skill from the marketplace for the plugin delegate.
func (a *App) SkillMarketInstall(name string) (marketplace.InstallResult, error) {
	pkgs, err := marketplace.FetchMarket()
	if err != nil {
		return marketplace.InstallResult{}, err
	}
	for _, pkg := range pkgs {
		if pkg.Name == name {
			return a.InstallMarketSkill(pkg)
		}
	}
	// Try built-in marketplace as fallback
	for _, pkg := range marketplace.BuiltinMarket() {
		if pkg.Name == name {
			return a.InstallMarketSkill(pkg)
		}
	}
	return marketplace.InstallResult{}, nil
}

// ── helpers ──

// listSkillsAsSkillInfo converts []skills.Skill to []skillsPlugin.SkillInfo.
func listSkillsAsSkillInfo(list []skills.Skill) []skillsPlugin.SkillInfo {
	out := make([]skillsPlugin.SkillInfo, len(list))
	for i, s := range list {
		out[i] = skillsPlugin.SkillInfo{
			Name:        s.Name,
			Title:       s.Title,
			Description: s.Description,
			Category:    s.Category,
			Package:     s.Package,
			Icon:        s.Icon,
			Enabled:     s.Enabled,
			Tools:       s.Tools,
			LibraryID:   s.LibraryID,
		}
	}
	return out
}
