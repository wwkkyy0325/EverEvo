// Package guides manages the guide/tutorial document system.
// Guides are synced from remote sources (Gitee, GitHub repos, raw URLs)
// and stored locally as markdown files. The LLM can read guides via
// MCP tools and resources.
package guides

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Source defines a remote source of guide documents.
type Source struct {
	Name    string `json:"name"`    // unique key, e.g. "my-gitee-repo"
	Title   string `json:"title"`   // display name
	URL     string `json:"url"`     // git repo URL or raw file URL
	Branch  string `json:"branch"`  // git branch, default "main"
	Type    string `json:"type"`    // "git" or "url"
	Enabled bool   `json:"enabled"`
}

// Guide represents a single guide document.
type Guide struct {
	ID        string `json:"id"`        // source-name/relative-path (URL-safe)
	Title     string `json:"title"`     // derived from filename or first heading
	Source    string `json:"source"`    // source name
	Path      string `json:"path"`      // relative path within source dir
	Size      int64  `json:"size"`      // file size in bytes
	UpdatedAt string `json:"updatedAt"` // last modified (ISO 8601)
}

// Manager manages guide sources and the local guide index.
type Manager struct {
	mu      sync.RWMutex
	sources []Source
}

// NewManager creates a guide manager and loads persisted sources. On first run
// (no sources.json yet) it seeds the bundled EverEvo usage-guide source and
// persists it. The returned seeded flag lets the caller trigger an initial sync
// so the Guide Center is populated immediately.
func NewManager() (*Manager, bool) {
	m := &Manager{}
	m.loadSources()
	if len(m.sources) == 0 {
		m.sources = []Source{defaultEverEvoSource()}
		_ = m.saveSources()
		return m, true
	}
	return m, false
}

// defaultEverEvoSource is the built-in local source that ships the app's own
// usage guides (embedded markdown, synced from the binary — no network).
func defaultEverEvoSource() Source {
	return Source{
		Name:    "everevo",
		Title:   "EverEvo 使用指南",
		Type:    "local",
		Enabled: true,
	}
}

// ─── Sources ──────────────────────────────────────────────────────

func (m *Manager) sourcesPath() string {
	dir := storage.DataDir()
	return filepath.Join(dir, "guides", "sources.json")
}

func guidesDir() string {
	dir := storage.DataDir()
	return filepath.Join(dir, "guides")
}

func (m *Manager) sourceDir(name string) string {
	return filepath.Join(guidesDir(), name)
}

// loadSources reads sources.json from disk.
func (m *Manager) loadSources() {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.sourcesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		m.sources = []Source{}
		return
	}
	if err := json.Unmarshal(data, &m.sources); err != nil {
		m.sources = []Source{}
	}
}

// saveSources persists sources to disk.
func (m *Manager) saveSources() error {
	dir := filepath.Dir(m.sourcesPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.sources, "", "  ")
	if err != nil {
		return err
	}
	return atomic.WriteFile(m.sourcesPath(), data, 0644)
}

// ListSources returns all configured sources.
func (m *Manager) ListSources() []Source {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Source, len(m.sources))
	copy(out, m.sources)
	return out
}

// AddSource adds or updates a source and persists.
func (m *Manager) AddSource(s Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.sources {
		if m.sources[i].Name == s.Name {
			m.sources[i] = s
			return m.saveSources()
		}
	}
	m.sources = append(m.sources, s)
	return m.saveSources()
}

// RemoveSource removes a source by name.
func (m *Manager) RemoveSource(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.sources {
		if m.sources[i].Name == name {
			m.sources = append(m.sources[:i], m.sources[i+1:]...)
			return m.saveSources()
		}
	}
	return fmt.Errorf("source %q not found", name)
}

// GetSource returns a source by name.
func (m *Manager) GetSource(name string) *Source {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.sources {
		if m.sources[i].Name == name {
			return &m.sources[i]
		}
	}
	return nil
}

// ─── Guides Index ──────────────────────────────────────────────────

// ListGuides scans all enabled source directories and returns all .md files.
func (m *Manager) ListGuides() []Guide {
	m.mu.RLock()
	srcs := make([]Source, len(m.sources))
	copy(srcs, m.sources)
	m.mu.RUnlock()

	var guides []Guide
	for _, src := range srcs {
		if !src.Enabled {
			continue
		}
		dir := m.sourceDir(src.Name)
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				// Skip .git directory
				if info.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			title := titleFromFile(info.Name(), path)
			guides = append(guides, Guide{
				ID:        src.Name + "/" + filepath.ToSlash(rel),
				Title:     title,
				Source:    src.Name,
				Path:      rel,
				Size:      info.Size(),
				UpdatedAt: info.ModTime().Format(time.RFC3339),
			})
			return nil
		})
	}
	sort.Slice(guides, func(i, j int) bool {
		return guides[i].UpdatedAt > guides[j].UpdatedAt
	})
	return guides
}

// ReadGuide reads the full content of a guide by its ID.
func (m *Manager) ReadGuide(id string) (string, error) {
	// ID format: "source-name/path/to/file.md"
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid guide ID: %s", id)
	}
	srcName := parts[0]
	relPath := parts[1]

	src := m.GetSource(srcName)
	if src == nil || !src.Enabled {
		return "", fmt.Errorf("source %q not found or disabled", srcName)
	}

	fullPath := filepath.Join(m.sourceDir(srcName), filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read guide: %w", err)
	}
	return string(data), nil
}

// SearchGuides searches guide titles, filenames, and content for a keyword.
// Chinese text is matched character-by-character (substring) — no word segmentation
// needed since strings.Contains handles CJK natively.
func (m *Manager) SearchGuides(query string) []Guide {
	all := m.ListGuides()
	if query == "" {
		return all
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var results []Guide
	for _, g := range all {
		// Title and path match (fast).
		if strings.Contains(strings.ToLower(g.Title), q) ||
			strings.Contains(strings.ToLower(g.Path), q) {
			results = append(results, g)
			continue
		}
		// Content match — reconstruct full path from source dir + relative path.
		fullPath := filepath.Join(m.sourceDir(g.Source), filepath.FromSlash(g.Path))
		if data, err := os.ReadFile(fullPath); err == nil {
			if strings.Contains(strings.ToLower(string(data)), q) {
				results = append(results, g)
			}
		}
	}
	return results
}

// titleFromFile derives a guide title from the first markdown heading or filename.
func titleFromFile(filename, fullPath string) string {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return filenameToTitle(filename)
	}
	content := string(data)
	// Look for first "# " heading
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimPrefix(line, "# ")
			if title != "" {
				return title
			}
		}
	}
	return filenameToTitle(filename)
}

// filenameToTitle converts a filename like "getting-started.md" to "Getting started".
func filenameToTitle(name string) string {
	name = strings.TrimSuffix(name, ".md")
	name = strings.TrimSuffix(name, ".MD")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.TrimSpace(name)
}

// syncSource synchronizes a single source.
func (m *Manager) syncSource(src Source) error {
	switch src.Type {
	case "git":
		return m.syncGit(src)
	case "url":
		return m.syncURL(src)
	case "local":
		return m.syncLocal(src)
	default:
		return fmt.Errorf("unknown source type: %s", src.Type)
	}
}

// SyncAll synchronizes all enabled sources.
func (m *Manager) SyncAll() []string {
	var results []string
	srcs := m.ListSources()
	for _, src := range srcs {
		if !src.Enabled {
			continue
		}
		log.Printf("[guides] syncing %s (%s)...", src.Name, src.URL)
		if err := m.syncSource(src); err != nil {
			msg := fmt.Sprintf("✗ %s: %v", src.Title, err)
			log.Printf("[guides] %s", msg)
			results = append(results, msg)
		} else {
			msg := fmt.Sprintf("✓ %s: 同步完成", src.Title)
			log.Printf("[guides] %s", msg)
			results = append(results, msg)
		}
	}
	return results
}

// SyncOne synchronizes a single source by name.
func (m *Manager) SyncOne(name string) (string, error) {
	src := m.GetSource(name)
	if src == nil {
		return "", fmt.Errorf("source %q not found", name)
	}
	if err := m.syncSource(*src); err != nil {
		return fmt.Sprintf("✗ %s: %v", src.Title, err), err
	}
	return fmt.Sprintf("✓ %s: 同步完成", src.Title), nil
}

// GuidesDir returns the guides data directory path.
func GuidesDir() string { return guidesDir() }

// SourceDir returns the local directory for a source.
func SourceDir(name string) string { return filepath.Join(guidesDir(), name) }
