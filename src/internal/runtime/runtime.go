// Package runtime manages portable language runtimes installed under data/envs/.
//
// Each runtime is a self-contained, no-install distribution extracted from an
// official portable archive. The app detects missing runtimes on startup and
// offers to install them on demand. No system-wide installation, registry
// writes, or PATH pollution.
package runtime

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/httpclient"
	"everevo/internal/storage"
)

// Kind identifies a portable runtime.
type Kind string

const (
	Python Kind = "python"
	Go     Kind = "go"
	Node   Kind = "node"
)

// Info describes an installed runtime.
type Info struct {
	Kind    Kind   `json:"kind"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Exe     string `json:"exe"`
	Ready   bool   `json:"ready"`
}

// ─── Pinned versions & download URLs ────────────────────────────

type runtimeSpec struct {
	version string
	urls    []string // primary first, fallbacks after; {version} will be substituted
	exeRel  string   // relative exe path after extraction
}

var specs = map[Kind]runtimeSpec{
	Python: {
		version: "3.12.8",
		urls: []string{
			"https://www.python.org/ftp/python/{version}/python-{version}-embed-amd64.zip",
			"https://registry.npmmirror.com/-/binary/python/{version}/python-{version}-embed-amd64.zip",
		},
		exeRel: "python.exe",
	},
	Go: {
		version: "1.24.2",
		urls: []string{
			"https://go.dev/dl/go{version}.windows-amd64.zip",
			"https://golang.google.cn/dl/go{version}.windows-amd64.zip",
		},
		exeRel: "go/bin/go.exe",
	},
	Node: {
		version: "22.14.0",
		urls: []string{
			"https://nodejs.org/dist/v{version}/node-v{version}-win-x64.zip",
			"https://registry.npmmirror.com/-/binary/node/v{version}/node-v{version}-win-x64.zip",
		},
		exeRel: "node.exe",
	},
}

// ─── Public API ──────────────────────────────────────────────────

// All returns the status of every managed runtime.
func All() []Info {
	return []Info{probe(Python), probe(Go), probe(Node)}
}

// Get returns info for a specific runtime kind.
func Get(k Kind) Info { return probe(k) }

// Ensure makes sure the given runtime is installed and ready.
// If missing, it downloads and extracts the portable archive (blocking).
func Ensure(k Kind) error {
	info := probe(k)
	if info.Ready {
		return nil
	}
	log.Printf("[runtime] %s not found — downloading %s...", k, specs[k].version)
	if err := downloadAndExtract(k); err != nil {
		return fmt.Errorf("install %s: %w", k, err)
	}
	return nil
}

// EnsureAll installs all missing runtimes. Returns the count of newly installed runtimes.
func EnsureAll() (installed int, errs []error) {
	for _, k := range []Kind{Python, Go, Node} {
		if err := Ensure(k); err != nil {
			errs = append(errs, err)
		} else {
			installed++
		}
	}
	return
}

// ExePath returns the path to the runtime executable.
func ExePath(k Kind) string { return probe(k).Exe }

// EnvPrepend returns PATH-style env adjustments.
func EnvPrepend(k Kind) []string {
	info := probe(k)
	if !info.Ready {
		return nil
	}
	bin := filepath.Join(info.Path, "bin")
	switch k {
	case Python:
		return []string{
			"PATH=" + info.Path + string(os.PathListSeparator) + "$PATH",
			"PYTHONHOME=" + info.Path,
		}
	case Go:
		return []string{
			"GOROOT=" + info.Path,
			"PATH=" + bin + string(os.PathListSeparator) + "$PATH",
		}
	case Node:
		return []string{
			"PATH=" + info.Path + string(os.PathListSeparator) + "$PATH",
		}
	}
	return nil
}

// ─── Internal ─────────────────────────────────────────────────────

// downloadHTTP uses the proxy-aware client with a generous timeout for large zips.
var downloadHTTP = httpclient.New(10 * time.Minute)

func probe(k Kind) Info {
	spec := specs[k]
	dir := storage.EnvsDir()
	exe := filepath.Join(dir, string(k), spec.exeRel)
	info := Info{Kind: k, Path: filepath.Join(dir, string(k)), Exe: exe}
	if _, err := os.Stat(exe); err == nil {
		info.Ready = true
		info.Version = detectVersion(k)
	}
	return info
}

func detectVersion(k Kind) string {
	exe := probe(k).Exe
	if exe == "" {
		return ""
	}
	args := []string{"--version"}
	switch k {
	case Go:
		args = []string{"version"}
	case Python:
		args = []string{"--version"}
	}
	out, err := exec.Command(exe, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func downloadAndExtract(k Kind) error {
	spec := specs[k]
	targetDir := filepath.Join(storage.EnvsDir(), string(k))
	version := spec.version

	// Try each URL until one works.
	tmpZip := filepath.Join(os.TempDir(), fmt.Sprintf("everevo-%s-%s.zip", k, version))
	var lastErr error
	for _, urlTemplate := range spec.urls {
		url := strings.ReplaceAll(urlTemplate, "{version}", version)
		log.Printf("[runtime] downloading %s from %s", k, url)
		if err := downloadFile(url, tmpZip); err != nil {
			lastErr = err
			log.Printf("[runtime] %s download failed from %s: %v", k, url, err)
			os.Remove(tmpZip)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return fmt.Errorf("download %s: all mirrors failed, last error: %w", k, lastErr)
	}
	defer os.Remove(tmpZip)

	// Extract.
	log.Printf("[runtime] extracting %s to %s", k, targetDir)
	if err := extractZip(tmpZip, targetDir, k); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Post-install: Python embeddable needs _pth fix.
	if k == Python {
		fixPythonEmbed(targetDir)
	}

	// Verify.
	if _, err := os.Stat(probe(k).Exe); err != nil {
		return fmt.Errorf("verify: exe not found after extraction at %s", probe(k).Exe)
	}

	log.Printf("[runtime] %s %s installed successfully", k, spec.version)
	return nil
}

func downloadFile(url, dest string) error {
	os.MkdirAll(filepath.Dir(dest), 0755)
	resp, err := downloadHTTP.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractZip(zipPath, destDir string, k Kind) error {
	os.MkdirAll(destDir, 0755)
	os.RemoveAll(destDir) // clean slate
	os.MkdirAll(destDir, 0755)

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// Go and Node archives have a top-level directory (go/, node-v{version}-win-x64/).
	// We strip that prefix so the runtime root is data/envs/{kind}/.
	stripPrefix := findStripPrefix(&r.Reader, k)

	for _, f := range r.File {
		rel := f.Name
		if stripPrefix != "" && strings.HasPrefix(rel, stripPrefix) {
			rel = rel[len(stripPrefix):]
			if rel == "" {
				continue
			}
		}
		dest := filepath.Join(destDir, filepath.FromSlash(rel))

		if f.FileInfo().IsDir() {
			os.MkdirAll(dest, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(dest), 0755)

		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		rc.Close()
		w.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findStripPrefix detects the common top-level directory prefix in a zip.
// Go archives: go/...
// Node archives: node-v22.14.0-win-x64/...
// Python embeddable: no prefix (files at root).
func findStripPrefix(r *zip.Reader, k Kind) string {
	if k == Python {
		return "" // Python embeddable has no top-level dir
	}
	if len(r.File) > 0 {
		parts := strings.SplitN(r.File[0].Name, "/", 2)
		if len(parts) > 1 {
			return parts[0] + "/"
		}
	}
	return ""
}

// fixPythonEmbed unlocks pip/site-packages in the embeddable distribution:
//  1. Create Lib/site-packages/
//  2. Uncomment "import site" in python3xx._pth
func fixPythonEmbed(dir string) {
	// Create site-packages.
	sitePkg := filepath.Join(dir, "Lib", "site-packages")
	os.MkdirAll(sitePkg, 0755)

	// Fix _pth file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "._pth") {
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)
			// Uncomment or add "import site" line.
			if !strings.Contains(content, "import site") {
				content = strings.Replace(content, "#import site", "import site", 1)
				if !strings.Contains(content, "import site") {
					content += "\nimport site\n"
				}
				os.WriteFile(path, []byte(content), 0644)
			}
			// Ensure Lib/site-packages is in the path.
			if !strings.Contains(content, "Lib\\site-packages") && !strings.Contains(content, "Lib/site-packages") {
				content = strings.TrimRight(content, "\n") + "\nLib\\site-packages\n"
				os.WriteFile(path, []byte(content), 0644)
			}
			break
		}
	}
}
