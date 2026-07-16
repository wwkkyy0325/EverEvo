//go:build windows

package plugin

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"everevo/internal/atomic"
)

// Spec is the parsed manifest.json of a single plugin.
type Spec struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Type        string   `json:"type"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Entry       string   `json:"entry"`
	Runtime     string   `json:"runtime"`
	Env         string   `json:"env"`
	Dir         string   `json:"dir"`
	Methods     []string `json:"methods"`
	GPU         struct {
		Required bool   `json:"required"`
		MinVRAM  string `json:"minVRAM"`
	} `json:"gpu"`
	// Viewer declares the frontend viewer type (e.g. "text-io", "image-classifier").
	Viewer string `json:"viewer,omitempty"`
	// InputSchema maps method name → input field definitions for dynamic forms.
	InputSchema map[string]map[string]InputField `json:"inputSchema,omitempty"`
	// OutputSchema maps method name → output field definitions for result rendering.
	OutputSchema map[string]map[string]OutputField `json:"outputSchema,omitempty"`
}

// InputField describes a single input control for a plugin method.
type InputField struct {
	Type        string   `json:"type"`        // "text", "textarea", "number", "file", "select"
	Label       string   `json:"label"`       // Display label
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []string `json:"options,omitempty"` // For select type
}

// OutputField describes a single output display area.
type OutputField struct {
	Type  string `json:"type"`  // "text", "json", "image-base64", "number"
	Label string `json:"label"`
}

// ScanPlugins walks data/plugins/ and parses every subdirectory containing a valid manifest.json.
func ScanPlugins(pluginsDir string) ([]Spec, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Spec
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		manifestPath := filepath.Join(dir, "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // skip directories without manifest
		}
		var s Spec
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		// Validate required fields
		if s.Name == "" || s.Type == "" || s.Entry == "" || s.Runtime == "" {
			continue
		}
		s.Dir = dir
		// Default methods
		if len(s.Methods) == 0 {
			s.Methods = []string{"info", "health"}
		}
		out = append(out, s)
	}
	return out, nil
}

// Lookup finds a plugin spec by name (case-insensitive prefix match).
func Lookup(specs []Spec, name string) (*Spec, error) {
	for i := range specs {
		if strings.EqualFold(specs[i].Name, name) {
			return &specs[i], nil
		}
	}
	return nil, fmt.Errorf("插件不存在: %s", name)
}

// PluginsDir returns the data/plugins/ directory path.
func PluginsDir(dataDir string) string {
	return filepath.Join(dataDir, "plugins")
}

// TmpDir returns the data/plugin-tmp/ directory path.
func TmpDir(dataDir string) string {
	return filepath.Join(dataDir, "plugin-tmp")
}

// ValidateManifest reads and validates manifest.json in the given directory.
func ValidateManifest(dir string) (*Spec, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 manifest.json 失败: %w", err)
	}
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("manifest.json 格式错误: %w", err)
	}
	if s.Name == "" || s.Type == "" || s.Entry == "" || s.Runtime == "" {
		return nil, fmt.Errorf("manifest.json 缺少必填字段 (name/type/entry/runtime)")
	}
	return &s, nil
}

// InstallFromDir copies a plugin directory into pluginsDir.
func InstallFromDir(srcDir, pluginsDir string) (*Spec, error) {
	spec, err := ValidateManifest(srcDir)
	if err != nil {
		return nil, err
	}
	dstDir := filepath.Join(pluginsDir, spec.Name)
	if _, err := os.Stat(dstDir); err == nil {
		return nil, fmt.Errorf("插件 %s 已存在，请先卸载", spec.Name)
	}
	if err := copyDir(srcDir, dstDir); err != nil {
		return nil, fmt.Errorf("复制插件目录失败: %w", err)
	}
	spec.Dir = dstDir
	if len(spec.Methods) == 0 {
		spec.Methods = []string{"info", "health"}
	}
	return spec, nil
}

// InstallFromZip extracts a plugin zip into pluginsDir. The zip must contain a
// single plugin with manifest.json at root or one directory deep.
func InstallFromZip(zipPath, pluginsDir, tmpDir string) (*Spec, error) {
	// Extract to temp dir
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.RemoveAll(extractDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, err
	}

	// Security: validate paths before extraction
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipFile(f, extractDir); err != nil {
			return nil, err
		}
	}

	// Find the plugin root (directory containing manifest.json)
	pluginRoot, err := findPluginRoot(extractDir)
	if err != nil {
		os.RemoveAll(extractDir)
		return nil, err
	}

	spec, err := ValidateManifest(pluginRoot)
	if err != nil {
		os.RemoveAll(extractDir)
		return nil, err
	}

	dstDir := filepath.Join(pluginsDir, spec.Name)
	if _, err := os.Stat(dstDir); err == nil {
		os.RemoveAll(extractDir)
		return nil, fmt.Errorf("插件 %s 已存在，请先卸载", spec.Name)
	}

	if err := os.Rename(pluginRoot, dstDir); err != nil {
		// Fallback: copy + remove
		if err := copyDir(pluginRoot, dstDir); err != nil {
			os.RemoveAll(extractDir)
			return nil, fmt.Errorf("移动插件目录失败: %w", err)
		}
	}

	os.RemoveAll(extractDir)

	spec.Dir = dstDir
	if len(spec.Methods) == 0 {
		spec.Methods = []string{"info", "health"}
	}
	return spec, nil
}

// DeletePlugin removes a plugin directory. The caller should stop the plugin first.
func DeletePlugin(pluginsDir, name string) error {
	target := filepath.Join(pluginsDir, name)
	// Security: ensure target is inside pluginsDir
	absPlugins, _ := filepath.Abs(pluginsDir)
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absPlugins+string(filepath.Separator)) && absTarget != absPlugins {
		return fmt.Errorf("路径不安全: %s", name)
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return fmt.Errorf("插件不存在: %s", name)
	}
	return os.RemoveAll(target)
}

// extractZipFile extracts a single zip file entry, blocking path traversal.
func extractZipFile(f *zip.File, dest string) error {
	// Normalize: forward slash → OS separator, then clean
	name := filepath.Clean(filepath.FromSlash(f.Name))

	// Block absolute paths and parent-directory traversal
	if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
		return fmt.Errorf("危险的 zip 路径: %s", f.Name)
	}

	path := filepath.Join(dest, name)
	// Defense in depth: verify resolved absolute path is under dest
	absDest, _ := filepath.Abs(dest)
	absPath, _ := filepath.Abs(path)
	if !strings.HasPrefix(absPath, absDest+string(filepath.Separator)) && absPath != absDest {
		return fmt.Errorf("危险的路径: %s", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(path, 0755)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	_, err = io.Copy(dst, src)
	return err
}

// findPluginRoot walks dir and returns the first directory containing manifest.json.
func findPluginRoot(dir string) (string, error) {
	// First check root
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil {
		return dir, nil
	}
	// Check one level deep
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			sub := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(sub, "manifest.json")); err == nil {
				return sub, nil
			}
		}
	}
	// Walk deeper
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.Name() == "manifest.json" {
			found = filepath.Dir(path)
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("未在压缩包中找到 manifest.json")
	}
	return found, nil
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return os.MkdirAll(dst, info.Mode())
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return atomic.WriteFile(target, data, info.Mode())
	})
}
