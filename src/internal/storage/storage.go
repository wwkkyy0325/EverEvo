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

// ─── Path resolution — single data/ root ─────────────────────────────
//
// EverEvo stores ALL runtime state under a single {projectRoot}/data/ tree:
//
//	{projectRoot}/
//	├── data/
//	│   ├── zones/{zone}/   ← persistent (config, memory.db, KB, wiki, workflows, skills)
//	│   ├── models/         ← model storage
//	│   ├── downloads/      ← downloaded files
//	│   ├── plugins/        ← installed plugin runtimes
//	│   ├── guides/         ← synced guides
//	│   ├── cache/          ← general cache
//	│   └── python/         ← portable python
//	└── dist/bin/            ← wails build output

// ExeDir returns the directory containing the running executable.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

// moduleRoot finds the directory containing go.mod (the Go module root, src/).
func moduleRoot() string {
	if root := walkUpToGoMod(ExeDir()); root != "" {
		return root
	}
	if wd, err := os.Getwd(); err == nil {
		return walkUpToGoMod(wd)
	}
	return ""
}

// ProjectRoot returns the git repository root — the parent of the go.mod
// directory. go.mod lives in src/, so the project root is one level above.
func ProjectRoot() string {
	if mr := moduleRoot(); mr != "" {
		return filepath.Dir(mr)
	}
	wd, _ := os.Getwd()
	log.Printf("[storage] go.mod not found, using CWD as root: %s", wd)
	return wd
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

func resolveRoot() string {
	return ProjectRoot()
}

// DataDir returns the unified data root.
//
//	{projectRoot}/data
func DataDir() string {
	return filepath.Join(resolveRoot(), "data")
}

// RuntimeDir is an alias for DataDir (backward compatibility).
//
// Deprecated: use DataDir() instead.
func RuntimeDir() string {
	return DataDir()
}

// AppDataDir returns the zone-scoped persistent data directory.
//
//	EVEREVO_ZONE=""    → data/zones/production
//	EVEREVO_ZONE=alpha → data/zones/alpha
func AppDataDir() (string, error) {
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	return filepath.Join(DataDir(), "zones", zone), nil
}

// ─── Sub-directory helpers ────────────────────────────────────────

func ModelsDir() string    { return filepath.Join(DataDir(), "models") }
func DownloadsDir() string { return filepath.Join(DataDir(), "downloads") }
func PluginsDir() string   { return filepath.Join(DataDir(), "plugins") }
func GuidesDir() string    { return filepath.Join(DataDir(), "guides") }
func CacheDir() string     { return filepath.Join(DataDir(), "cache") }

// ─── Portable runtimes (data/envs/) ──────────────────────────────

func EnvsDir() string  { return filepath.Join(DataDir(), "envs") }
func PythonDir() string { return filepath.Join(EnvsDir(), "python") }
func GoDir() string     { return filepath.Join(EnvsDir(), "go") }
func NodeDir() string   { return filepath.Join(EnvsDir(), "node") }

// ─── Session workspaces (data/sessions/) ─────────────────────────

func SessionsDir() string { return filepath.Join(DataDir(), "sessions") }

// WorkspaceDir returns the working directory for a session.
func WorkspaceDir(sessionID string) string {
	return filepath.Join(SessionsDir(), sessionID, "workspace")
}

// ─── Sandbox instances (data/sandbox/) ───────────────────────────

func SandboxDir() string { return filepath.Join(DataDir(), "sandbox") }

// ─── Legacy migration ─────────────────────────────────────────────
//
// migrateRuntime moves old runtime/ subdirs into data/. Safe to call
// on every startup — if runtime/ doesn't exist, it's a no-op.

func migrateRuntime() {
	root := resolveRoot()
	oldRuntime := filepath.Join(root, "runtime")
	if _, err := os.Stat(oldRuntime); os.IsNotExist(err) {
		return
	}
	log.Printf("[storage] migrating legacy runtime/ → data/")
	mappings := []struct{ old, new string }{
		{"models", ModelsDir()},
		{"downloads", DownloadsDir()},
		{"plugins", PluginsDir()},
		{"guides", GuidesDir()},
		{"cache", CacheDir()},
		{"python", PythonDir()},
	}
	for _, m := range mappings {
		src := filepath.Join(oldRuntime, m.old)
		if _, err := os.Stat(src); err == nil {
			if _, err := os.Stat(m.new); os.IsNotExist(err) {
				log.Printf("[storage]   %s → %s", src, m.new)
				_ = os.Rename(src, m.new)
			}
		}
	}
	// Remove empty runtime dir after migration.
	_ = os.Remove(oldRuntime)
}

// ─── Directory initialization ─────────────────────────────────────

// EnsureDirs creates all required data directories.
func EnsureDirs() error {
	zone := os.Getenv("EVEREVO_ZONE")
	if zone == "" {
		zone = "production"
	}
	zoneDir := filepath.Join(DataDir(), "zones", zone)

	dirs := []string{
		// Zone-scoped persistent data
		zoneDir,
		filepath.Join(zoneDir, "memory"),
		filepath.Join(zoneDir, "knowledge"),
		filepath.Join(zoneDir, "knowledge", "chromem"),
		filepath.Join(zoneDir, "wiki"),
		filepath.Join(zoneDir, "wiki", "chromem"),
		filepath.Join(zoneDir, "workflows"),
		filepath.Join(zoneDir, "skills"),
		// Shared runtime data
		ModelsDir(),
		DownloadsDir(),
		PluginsDir(),
		GuidesDir(),
		CacheDir(),
		// Portable runtimes
		EnvsDir(),
		PythonDir(),
		GoDir(),
		NodeDir(),
		// Session workspaces
		SessionsDir(),
		// Sandbox instances
		SandboxDir(),
	}
	for _, p := range dirs {
		if err := os.MkdirAll(p, 0755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}

	// One-time migration of legacy runtime/ (safe no-op if already done).
	migrateRuntime()
	return nil
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
