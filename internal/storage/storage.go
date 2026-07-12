package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// KnownModelExtensions 已知模型文件扩展名。
var KnownModelExtensions = []string{".gguf", ".onnx", ".safetensors", ".bin", ".pt", ".pth"}

// ─── Path resolution — everything under %APPDATA%/EverEvo/ ──────
//
// Layout:
//
//	%APPDATA%/EverEvo/
//	├── config.json               global config
//	├── models/                   downloaded models (large, redownloadable)
//	├── cache/                    download cache
//	├── plugins/                  plugin subdirectories
//	├── plugin-tmp/               plugin scratch space
//	├── guides/                   synced guide content
//	├── zones/
//	│   └── {zone}/               per-zone isolation
//	│       ├── agents.json
//	│       ├── skills.json
//	│       ├── mcp_servers.json
//	│       ├── a2a_agents.json
//	│       ├── config.json
//	│       ├── memory/           memory.db + chromem
//	│       ├── knowledge/        RAG chromem + meta
//	│       ├── wiki/             wiki chromem + pages
//	│       └── workflows/        workflow JSON files
//
// This layout keeps ALL mutable data under the user's profile, separate
// from the EXE. Self-evolution recompilation never touches user data.

// rootAppData computes the root %APPDATA%\EverEvo prefix.
func rootAppData() (string, error) {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	if dir == "" {
		return "", fmt.Errorf("无法确定 APPDATA 目录")
	}
	return filepath.Join(dir, "EverEvo"), nil
}

// RootAppDataDir returns the user-scoped application data root — survives EXE reinstalls.
func RootAppDataDir() (string, error) {
	return rootAppData()
}

// AppDataDir returns the zone-scoped data directory.
//
//	EVEREVO_ZONE=""       → %APPDATA%\EverEvo\zones\production
//	EVEREVO_ZONE=exper-1  → %APPDATA%\EverEvo\zones\exper-1
func AppDataDir() (string, error) {
	base, err := rootAppData()
	if err != nil {
		return "", err
	}
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	return filepath.Join(base, "zones", zone), nil
}

// ExeDir returns the directory containing the running executable.
func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// DataDir returns the shared data root — %APPDATA%\EverEvo.
// All mutable data lives here, completely separate from the EXE directory.
func DataDir() (string, error) {
	return rootAppData()
}

// ModelsDir returns the shared model storage directory.
// Priority order for finding existing models (for backward compat):
//  1. %APPDATA%/EverEvo/models (new canonical location)
//  2. {EXE}/data/models (legacy, migrated automatically)
//  3. {CWD}/data/models (wails dev mode)
func ModelsDir() (string, error) {
	base, err := rootAppData()
	if err == nil {
		p := filepath.Join(base, "models")
		if st, e := os.Stat(p); e == nil && st.IsDir() {
			return p, nil
		}
	}

	// Legacy paths — find models so the migration can move them.
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "data", "models"))
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "data", "models"))
		candidates = append(candidates, filepath.Join(filepath.Dir(exeDir), "data", "models"))
	}
	for _, d := range candidates {
		if st, e := os.Stat(d); e == nil && st.IsDir() {
			return d, nil
		}
	}

	// Fallback: canonical APPDATA path (will be created if missing).
	if base != "" {
		return filepath.Join(base, "models"), nil
	}
	return "", fmt.Errorf("无法确定模型目录")
}

// CacheDir returns the shared cache directory under APPDATA.
func CacheDir() (string, error) {
	base, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cache"), nil
}

// ─── Migration ─────────────────────────────────────────────────────

// oldExeDataDir returns the legacy data/ directory beside the EXE (may not exist).
func oldExeDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

// oldWdsDataDir returns the legacy data/ directory in the working dir (wails dev).
func oldWdsDataDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(wd, "data")
}

// migrateDir copies a directory tree from oldPath to newPath if oldPath exists
// and newPath doesn't. Symlinks/junctions are NOT followed (safety).
func migrateDir(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil // already migrated
	}
	log.Printf("[migrate] %s → %s", oldPath, newPath)
	// Ensure parent exists.
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	// Use rename when on same volume (fast, atomic); fall back to copy.
	if err := os.Rename(oldPath, newPath); err == nil {
		return nil
	}
	// Cross-volume: copy then rename old as backup.
	if err := copyDir(oldPath, newPath); err != nil {
		return fmt.Errorf("copy %s → %s: %w", oldPath, newPath, err)
	}
	backup := oldPath + ".migrated"
	_ = os.Rename(oldPath, backup)
	log.Printf("[migrate] 旧目录已备份为 %s", backup)
	return nil
}

// migrateFile copies a single file from oldPath to newPath.
func migrateFile(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	log.Printf("[migrate] %s → %s", oldPath, newPath)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	return os.WriteFile(newPath, data, 0644)
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// MigrateLegacyData moves all data from EXE-relative data/ directories into
// the canonical %APPDATA%/EverEvo/ tree. Safe to call at every startup —
// existing data in the new location is never overwritten.
func MigrateLegacyData() {
	appRoot, err := rootAppData()
	if err != nil {
		log.Printf("[migrate] 无法确定 APPDATA 根目录: %v", err)
		return
	}
	zoneDir := filepath.Join(appRoot, "zones", "production")

	// Check both legacy sources: EXE-relative and CWD-relative.
	for _, oldRoot := range []string{oldExeDataDir(), oldWdsDataDir()} {
		if oldRoot == "" {
			continue
		}
		if _, err := os.Stat(oldRoot); os.IsNotExist(err) {
			continue
		}

		// ── Directories to migrate ──
		dirMigrations := []struct{ oldSub, newPath string }{
			{"models", filepath.Join(appRoot, "models")},
			{"cache", filepath.Join(appRoot, "cache")},
			{"plugins", filepath.Join(appRoot, "plugins")},
			{"plugin-tmp", filepath.Join(appRoot, "plugin-tmp")},
			{"guides", filepath.Join(appRoot, "guides")},
			{"skills", filepath.Join(zoneDir, "skills")},
			{"uploads", filepath.Join(appRoot, "uploads")},
			{"memory", filepath.Join(zoneDir, "memory")},
		}
		for _, m := range dirMigrations {
			oldPath := filepath.Join(oldRoot, m.oldSub)
			if st, e := os.Stat(oldPath); e == nil && st.IsDir() {
				if err := migrateDir(oldPath, m.newPath); err != nil {
					log.Printf("[migrate] 目录迁移失败 %s: %v", m.oldSub, err)
				}
			}
		}

		// ── Files to migrate (from data/ root) ──
		fileMigrations := []struct{ oldName, newPath string }{
			{"mcp_servers.json", filepath.Join(zoneDir, "mcp_servers.json")},
			{"skills.json", filepath.Join(zoneDir, "skills.json")},
		}
		for _, m := range fileMigrations {
			oldPath := filepath.Join(oldRoot, m.oldName)
			if _, e := os.Stat(oldPath); e == nil {
				if err := migrateFile(oldPath, m.newPath); err != nil {
					log.Printf("[migrate] 文件迁移失败 %s: %v", m.oldName, err)
				}
			}
		}
	}

	// ── Migrate a2a_agents.json from EXE/data/ ──
	for _, oldRoot := range []string{oldExeDataDir(), oldWdsDataDir()} {
		oldPath := filepath.Join(oldRoot, "a2a_agents.json")
		newPath := filepath.Join(zoneDir, "a2a_agents.json")
		if _, e := os.Stat(oldPath); e == nil {
			_ = migrateFile(oldPath, newPath)
		}
	}
}

// ─── Directory creation ────────────────────────────────────────────

// EnsureDataDir creates the canonical APPDATA data directories.
func EnsureDataDir() error {
	appRoot, err := rootAppData()
	if err != nil {
		return err
	}
	zoneDir := filepath.Join(appRoot, "zones", "production")

	for _, p := range []string{
		filepath.Join(appRoot, "models"),
		filepath.Join(appRoot, "cache"),
		filepath.Join(appRoot, "plugins"),
		filepath.Join(appRoot, "plugin-tmp"),
		filepath.Join(appRoot, "guides"),
		filepath.Join(appRoot, "uploads"),
		zoneDir,
		filepath.Join(zoneDir, "knowledge"),
		filepath.Join(zoneDir, "knowledge", "chromem"),
		filepath.Join(zoneDir, "memory"),
		filepath.Join(zoneDir, "wiki"),
		filepath.Join(zoneDir, "wiki", "chromem"),
		filepath.Join(zoneDir, "workflows"),
		filepath.Join(zoneDir, "skills"),
	} {
		if err := os.MkdirAll(p, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", p, err)
		}
	}
	return nil
}

// ─── Model discovery ───────────────────────────────────────────────

// DiscoverModels 遍历给定目录，返回找到的模型文件路径。
func DiscoverModels(dirs []string) []string {
	var found []string
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			for _, known := range KnownModelExtensions {
				if ext == known {
					found = append(found, path)
					break
				}
			}
			return nil
		})
	}
	return found
}
