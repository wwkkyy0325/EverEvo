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

// ─── Path resolution — always portable ───────────────────────────────
//
// EverEvo is always distributed with source code (required for self-evolution).
// All data lives under {projectRoot}/data/ and {projectRoot}/runtime/.
//
//	{projectRoot}/
//	├── data/zones/{zone}/   ← protected (config, memory, KB, wiki, workflows, skills)
//	├── runtime/             ← rebuildable (models, plugins, downloads, guides, cache, python)
//	└── build/bin/           ← wails build output

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
// Returns "" if not found anywhere.
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

// resolveRoot returns the project root, falling back to CWD when go.mod
// cannot be found. This is the single choke-point for all path resolution.
func resolveRoot() string {
	if root := ProjectRoot(); root != "" {
		return root
	}
	// Last resort: use working directory. This handles edge cases like
	// running the EXE from an unusual location during development.
	wd, _ := os.Getwd()
	log.Printf("[storage] ⚠ go.mod not found, using CWD as root: %s", wd)
	return wd
}

// DataDir returns the root of protected (non-rebuildable) data.
//
//	{projectRoot}/data
func DataDir() string {
	return filepath.Join(resolveRoot(), "data")
}

// RuntimeDir returns the root of rebuildable runtime data.
//
//	{projectRoot}/runtime
func RuntimeDir() string {
	return filepath.Join(resolveRoot(), "runtime")
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

// ─── Sub-directory helpers ────────────────────────────────────────

// ModelsDir returns the model storage directory.
func ModelsDir() string {
	return filepath.Join(RuntimeDir(), "models")
}

// PluginsDir returns the plugin installation directory.
func PluginsDir() string { return filepath.Join(RuntimeDir(), "plugins") }

// DownloadsDir returns the download cache directory.
func DownloadsDir() string { return filepath.Join(RuntimeDir(), "downloads") }

// GuidesDir returns the synced guides directory.
func GuidesDir() string { return filepath.Join(RuntimeDir(), "guides") }

// CacheDir returns the general cache directory.
func CacheDir() string { return filepath.Join(RuntimeDir(), "cache") }

// PythonDir returns the portable Python installation directory.
func PythonDir() string { return filepath.Join(RuntimeDir(), "python") }

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
		PythonDir(),
	}
	for _, p := range dirs {
		if err := os.MkdirAll(p, 0755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
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
