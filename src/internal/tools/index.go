package tools

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ToolIndexEntry is a lightweight descriptor for tool discovery — ~30 tokens
// instead of the ~250-400 of a full ToolDef.
type ToolIndexEntry struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description"`           // one-line summary
	Category    string   `json:"category"`
	Keywords    []string `json:"keywords,omitempty"`    // search aids
	ReadOnly    bool     `json:"readOnly,omitempty"`    // doesn't modify state
}

// ToolIndex is the full lightweight registry loaded into every request.
type ToolIndex struct {
	Categories map[string][]ToolIndexEntry `json:"categories"`
	Core       []ToolIndexEntry            `json:"core"` // always-loaded tools
}

// schemaCache caches full ToolDef schemas fetched via tool_search.
// Keyed per-turn; purged after each assistant response.
var (
	schemaCache   = map[string]*ToolDef{}
	schemaCacheMu sync.RWMutex
)

// BuildToolIndex constructs the lightweight index from the full registry.
func BuildToolIndex() ToolIndex {
	regMu.RLock()
	defer regMu.RUnlock()

	idx := ToolIndex{
		Categories: map[string][]ToolIndexEntry{},
	}

	// Collect all internal tools
	for _, t := range registry {
		entry := ToolIndexEntry{
			Name:        t.Name,
			Title:       t.Title,
			Description: firstSentence(t.Description),
			Category:    t.Category,
			Keywords:    extractKeywords(t.Name, t.Description),
		}
		if t.Annotations != nil && t.Annotations.ReadOnlyHint {
			entry.ReadOnly = true
		}
		idx.Categories[t.Category] = append(idx.Categories[t.Category], entry)
	}

	// Collect external MCP tools under "external" category
	for _, t := range externalRegistry {
		entry := ToolIndexEntry{
			Name:        t.Name,
			Title:       t.Title,
			Description: firstSentence(t.Description),
			Category:    "external",
			Keywords:    extractKeywords(t.Name, t.Description),
		}
		idx.Categories["external"] = append(idx.Categories["external"], entry)
	}

	// Sort each category
	for cat := range idx.Categories {
		sort.Slice(idx.Categories[cat], func(i, j int) bool {
			return idx.Categories[cat][i].Name < idx.Categories[cat][j].Name
		})
	}

	// Populate core tools
	for _, name := range CoreToolNames() {
		if t := lookupNoLock(name); t != nil {
			idx.Core = append(idx.Core, ToolIndexEntry{
				Name:        t.Name,
				Title:       t.Title,
				Description: t.Description, // core tools get full description
				Category:    t.Category,
			})
		}
	}

	return idx
}

// lookupNoLock is Lookup without locking (caller must hold regMu.RLock).
func lookupNoLock(name string) *ToolDef {
	if t, ok := registry[name]; ok {
		return t
	}
	return externalRegistry[name]
}

// SearchTools searches the tool index by query and optional category filter.
// Returns matching ToolDef full schemas (loaded from registry, not cached).
func SearchTools(query string, category string) []*ToolDef {
	regMu.RLock()
	defer regMu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))

	var results []*ToolDef

	for _, t := range registry {
		if category != "" && t.Category != category {
			continue
		}
		if query == "" || query == "*" || matchTool(t, query) {
			results = append(results, t)
		}
	}
	for _, t := range externalRegistry {
		if category != "" && category != "external" {
			continue
		}
		if query == "" || query == "*" || matchTool(t, query) {
			results = append(results, t)
		}
	}

	// Sort by relevance: exact name match first, then partial, then description
	sort.Slice(results, func(i, j int) bool {
		ai := matchScore(results[i], query)
		aj := matchScore(results[j], query)
		return ai > aj
	})

	// Cap at 15 results to avoid flooding context
	if len(results) > 15 {
		results = results[:15]
	}

	return results
}

func matchTool(t *ToolDef, query string) bool {
	if strings.Contains(strings.ToLower(t.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(t.Description), query) {
		return true
	}
	if strings.Contains(strings.ToLower(t.Title), query) {
		return true
	}
	return false
}

func matchScore(t *ToolDef, query string) int {
	score := 0
	name := strings.ToLower(t.Name)
	if name == query {
		score += 100
	} else if strings.Contains(name, query) {
		score += 50
	}
	if strings.Contains(strings.ToLower(t.Description), query) {
		score += 20
	}
	if strings.Contains(strings.ToLower(t.Title), query) {
		score += 10
	}
	return score
}

// CacheSchema stores a fetched schema in the per-turn cache.
func CacheSchema(t *ToolDef) {
	schemaCacheMu.Lock()
	defer schemaCacheMu.Unlock()
	schemaCache[t.Name] = t
}

// GetCachedSchema retrieves a schema from the per-turn cache.
func GetCachedSchema(name string) *ToolDef {
	schemaCacheMu.RLock()
	defer schemaCacheMu.RUnlock()
	return schemaCache[name]
}

// ClearSchemaCache purges the per-turn schema cache.
func ClearSchemaCache() {
	schemaCacheMu.Lock()
	defer schemaCacheMu.Unlock()
	schemaCache = map[string]*ToolDef{}
}

// ToolIndexJSON returns the JSON-encoded tool index for injection into system prompts.
func ToolIndexJSON() json.RawMessage {
	idx := BuildToolIndex()
	data, _ := json.Marshal(idx)
	return data
}

// CoreToolsDef returns the ToolDef list for always-loaded core tools.
// These bypass the tool_search lazy-loading mechanism.
func CoreToolsDef() []*ToolDef {
	regMu.RLock()
	defer regMu.RUnlock()
	var out []*ToolDef
	for _, name := range CoreToolNames() {
		if t := registry[name]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// firstSentence returns up to the first period or 120 chars of text.
func firstSentence(text string) string {
	if idx := strings.IndexByte(text, '.'); idx > 0 && idx < 120 {
		return text[:idx+1]
	}
	if len(text) > 120 {
		return text[:120] + "..."
	}
	return text
}

// extractKeywords pulls search-friendly keywords from tool name + description.
func extractKeywords(name, desc string) []string {
	words := strings.Fields(strings.ToLower(name + " " + desc))
	seen := map[string]bool{}
	var kw []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:()[]{}!?\"'")
		if len(w) >= 3 && !seen[w] && !isStopWord(w) {
			seen[w] = true
			kw = append(kw, w)
		}
	}
	if len(kw) > 8 {
		kw = kw[:8]
	}
	return kw
}

func isStopWord(w string) bool {
	switch w {
	case "the", "and", "for", "that", "this", "with", "from", "are", "was",
		"all", "not", "has", "can", "its", "but", "have", "been", "will",
		"when", "were", "they", "what", "which", "their", "about", "into",
		"each", "more", "some", "such", "than", "then", "them", "also",
		"only", "over", "very", "your", "just", "how", "use", "used",
		"using", "one", "two", "any", "new", "set", "get", "out", "now",
		"may", "see", "way", "who", "our", "too", "did", "per", "top":
		return true
	}
	return false
}
