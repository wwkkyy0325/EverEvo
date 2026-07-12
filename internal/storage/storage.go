package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// KnownModelExtensions lists known model file extensions.
var KnownModelExtensions = []string{".gguf", ".onnx", ".safetensors", ".bin", ".pt", ".pth"}

// ─── Path resolution — portable or AppData ───────────────────────
//
// Project root: the directory containing go.mod, found by walking up
// from the EXE. This works for EXEs anywhere in the tree:
//   - root/everevo.exe         → root
//   - root/build/bin/everevo.exe → root (walk up 2 levels)
//   - root/sandbox/alpha/everevo.exe → root (walk up 2 levels)
//
// Portable mode (go.mod found):
//
//	{projectRoot}/
//	├── data/           ← protected: zones, memory.db, chromem, wiki, knowledge
//	├── runtime/        ← rebuildable: models, plugins, downloads, guides
//	├── build/bin/      ← wails build output
//	└── sandbox/        ← evolution sandbox instances
//
// User mode (no go.mod found = standalone EXE):
//
//	%APPDATA%/EverEvo/
//	├── data/
//	└── runtime/

// ExeDir returns the directory containing the running executable.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

// ProjectRoot walks up from the EXE directory until it finds go.mod.
// Falls back to the working directory (wails dev compiles EXE to temp dir).
// Returns "" if not found anywhere (standalone EXE, not in a dev tree).
func ProjectRoot() string {
	if root := walkUpToGoMod(ExeDir()); root != "" {
		return root
	}
	// wails dev: EXE is compiled to a temp dir, but CWD is the project root.
	if wd, err := os.Getwd(); err == nil {
		return walkUpToGoMod(wd)
	}
	return ""
}

func walkUpToGoMod(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// IsPortable returns true when the EXE lives inside a source tree
// (go.mod found anywhere up the directory chain).
func IsPortable() bool {
	return ProjectRoot() != ""
}

// DataDir returns the root of protected (non-rebuildable) data.
// This is the directory that holds zones/, memory.db, wiki/, knowledge/, etc.
//
//	Portable:  {projectRoot}/data
//	User mode: %APPDATA%/EverEvo  (no extra /data — AppData IS the data dir)
func DataDir() string {
	if root := ProjectRoot(); root != "" {
		return filepath.Join(root, "data")
	}
	return appDataRoot()
}

// RuntimeDir returns the root of rebuildable runtime data.
//
//	Portable:  {projectRoot}/runtime
//	User mode: %APPDATA%/EverEvo/runtime
func RuntimeDir() string {
	if root := ProjectRoot(); root != "" {
		return filepath.Join(root, "runtime")
	}
	return filepath.Join(appDataRoot(), "runtime")
}

// AppDataDir returns the zone-scoped data directory.
//
//	EVEREVO_ZONE=""       → data/zones/production
//	EVEREVO_ZONE=alpha    → data/zones/alpha
func AppDataDir() (string, error) {
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	return filepath.Join(DataDir(), "zones", zone), nil
}

// RootAppDataDir returns the shared data root (for multi-zone access).
func RootAppDataDir() (string, error) {
	return DataDir(), nil
}

// ─── Sub-directory helpers ────────────────────────────────────────

// ModelsDir returns the model storage directory.
func ModelsDir() string {
	dir := filepath.Join(RuntimeDir(), "models")
	// Check legacy locations for existing models.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		for _, legacy := range []string{
			filepath.Join(appDataRoot(), "models"),
			filepath.Join(ExeDir(), "data", "models"),
		} {
			if st, e := os.Stat(legacy); e == nil && st.IsDir() {
				return legacy
			}
		}
	}
	return dir
}

// PluginsDir returns the plugin installation directory.
func PluginsDir() string { return filepath.Join(RuntimeDir(), "plugins") }

// DownloadsDir returns the download cache directory.
func DownloadsDir() string { return filepath.Join(RuntimeDir(), "downloads") }

// GuidesDir returns the synced guides directory.
func GuidesDir() string { return filepath.Join(RuntimeDir(), "guides") }

// CacheDir returns the general cache directory.
func CacheDir() string { return filepath.Join(RuntimeDir(), "cache") }

// ─── Legacy AppData helpers (for migration) ───────────────────────

// appDataRoot returns the old %APPDATA%/EverEvo path.
func appDataRoot() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(dir, "EverEvo")
}

// OldAppDataDir returns the legacy zone-scoped AppData path (for migration).
func OldAppDataDir() string {
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	return filepath.Join(appDataRoot(), "zones", zone)
}

// ─── Directory initialization ─────────────────────────────────────

// EnsureDirs creates all required data and runtime directories.
func EnsureDirs() error {
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	zoneDir := filepath.Join(DataDir(), "zones", zone)

	dirs := []string{
		// Protected data
		zoneDir,
		filepath.Join(zoneDir, "memory"),
		filepath.Join(zoneDir, "knowledge"),
		filepath.Join(zoneDir, "knowledge", "chromem"),
		filepath.Join(zoneDir, "wiki"),
		filepath.Join(zoneDir, "wiki", "chromem"),
		filepath.Join(zoneDir, "workflows"),
		filepath.Join(zoneDir, "skills"),
		// Rebuildable runtime
		ModelsDir(),
		PluginsDir(),
		DownloadsDir(),
		GuidesDir(),
		CacheDir(),
	}
	for _, p := range dirs {
		if err := os.MkdirAll(p, 0755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	return nil
}

// ─── Migration ─────────────────────────────────────────────────────

// MigrateLegacyData moves data from the old %APPDATA%/EverEvo/ tree into
// the new portable layout. Safe to call at every startup — existing data
// in the new location is never overwritten.
//
// Migration is only relevant in portable mode. In user mode the AppData
// paths are already correct.
func MigrateLegacyData() {
	if !IsPortable() {
		return // user mode — AppData paths unchanged
	}

	oldRoot := appDataRoot()
	if _, err := os.Stat(oldRoot); os.IsNotExist(err) {
		return // nothing to migrate
	}

	newData := DataDir()
	newRuntime := RuntimeDir()
	oldZoneDir := filepath.Join(oldRoot, "zones", "production")
	newZoneDir := filepath.Join(newData, "zones", "production")

	log.Printf("[migrate] 检测到旧数据目录 %s，迁移中...", oldRoot)

	// Protected data: zone configs, memory, wiki, knowledge
	zoneSubs := []string{"memory", "knowledge", "wiki", "workflows", "skills"}
	for _, sub := range zoneSubs {
		oldPath := filepath.Join(oldZoneDir, sub)
		newPath := filepath.Join(newZoneDir, sub)
		_ = migrateDir(oldPath, newPath)
	}

	// Zone config files
	zoneFiles := []string{"config.json", "agents.json", "skills.json",
		"mcp_servers.json", "a2a_agents.json", "taskboard.json", "evolve_tasks.json"}
	for _, f := range zoneFiles {
		_ = migrateFile(filepath.Join(oldZoneDir, f), filepath.Join(newZoneDir, f))
	}

	// Rebuildable data: models, plugins, downloads, guides
	runtimeSubs := []string{"models", "plugins", "cache", "guides"}
	for _, sub := range runtimeSubs {
		_ = migrateDir(filepath.Join(oldRoot, sub), filepath.Join(newRuntime, sub))
	}

	// Also migrate plugin-tmp and uploads if they exist
	_ = migrateDir(filepath.Join(oldRoot, "plugin-tmp"), filepath.Join(newRuntime, "plugins"))
	_ = migrateDir(filepath.Join(oldRoot, "uploads"), filepath.Join(newRuntime, "downloads"))

	log.Printf("[migrate] 数据迁移完成 → %s", newData)
}

func migrateDir(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil // already exists
	}
	log.Printf("[migrate] %s → %s", oldPath, newPath)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return nil
	}
	// Cross-volume fallback
	if err := copyDir(oldPath, newPath); err != nil {
		return fmt.Errorf("copy %s → %s: %w", oldPath, newPath, err)
	}
	_ = os.Rename(oldPath, oldPath+".migrated")
	return nil
}

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

// ─── Model discovery ───────────────────────────────────────────────

// DiscoverModels walks the given dirs and returns found model file paths.
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
