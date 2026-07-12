//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"

	"everevo/internal/skills"
)

// ─── Skills 管理 API ────────────────────────────────────────────

// ListSkills returns all skills with their enabled state.
// Pass empty libraryId to list all skills (backward-compatible).
func (a *App) ListSkills(libraryId string) []skills.Skill {
	if a.skillManager == nil { return []skills.Skill{} }
	list := a.skillManager.ListByLibrary(libraryId)
	log.Printf("[api] ListSkills(library=%q) → %d skills", libraryId, len(list))
	return list
}

// ListEnabledSkills returns only enabled skills, optionally filtered by library.
func (a *App) ListEnabledSkills(libraryId string) []skills.Skill {
	if a.skillManager == nil { return []skills.Skill{} }
	return a.skillManager.ListEnabledByLibrary(libraryId)
}

// SetSkillEnabled enables or disables a skill and saves.
func (a *App) SetSkillEnabled(name string, enabled bool) bool {
	ok := a.skillManager.SetEnabled(name, enabled)
	if ok {
		if err := a.skillManager.Save(); err != nil {
			log.Printf("[skills] 保存失败: %v", err)
		}
	}
	return ok
}

// CreateSkill creates a new skill from the frontend and saves to disk.
// LibraryID is required. Skills with empty LibraryID are treated as global.
func (a *App) CreateSkill(s skills.Skill) error {
	// Global skills (LibraryID == "") are allowed — they appear in all domains.
	if s.LibraryID != "" {
		if err := a.validateLibraryID(s.LibraryID); err != nil {
			return fmt.Errorf("创建 Skill 失败: %w", err)
		}
	}
	return a.skillManager.Create(s)
}

// UpdateSkill updates an existing skill and saves to disk.
func (a *App) UpdateSkill(name string, s skills.Skill) error {
	return a.skillManager.Update(name, s)
}

// MoveSkill moves a skill to a different package.
func (a *App) MoveSkill(name string, newPackage string) error {
	return a.skillManager.MoveSkill(name, newPackage)
}

// DeleteSkill removes a skill by name and saves to disk.
func (a *App) DeleteSkill(name string) error {
	return a.skillManager.Delete(name)
}

// ResetSkills restores all skills to built-in defaults.
func (a *App) ResetSkills() error {
	return a.skillManager.Reset()
}

// ExportSkills returns all skills as JSON for export.
func (a *App) ExportSkills() (json.RawMessage, error) {
	data, err := a.skillManager.Export()
	return data, err
}

// ImportSkills merges incoming JSON skills into the manager.
func (a *App) ImportSkills(data json.RawMessage) error {
	return a.skillManager.Import(data)
}

// GetEnabledToolNames returns the enabled tool names (filtered by skills).
func (a *App) GetEnabledToolNames() []string {
	if a.skillManager == nil { return []string{} }
	return a.skillManager.GetEnabledTools()
}
