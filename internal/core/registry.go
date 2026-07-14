package core

import "sync"

// RegistryEntry pairs a plugin with its manifest.
type RegistryEntry[P Plugin] struct {
	Plugin   P
	Manifest PluginManifest
}

// Registry is a type-safe plugin registry.
// It stores plugin instances indexed by ID, with support for enabling/disabling.
type Registry[P Plugin] struct {
	kind     string
	mu       sync.RWMutex
	entries  map[string]*RegistryEntry[P]
	enabled  map[string]bool // tracks which plugins are enabled (default: all)
}

// NewRegistry creates an empty registry of the given kind.
func NewRegistry[P Plugin](kind string) *Registry[P] {
	return &Registry[P]{
		kind:    kind,
		entries: make(map[string]*RegistryEntry[P]),
		enabled: make(map[string]bool),
	}
}

// Register adds a plugin to the registry. If already registered, it is replaced.
func (r *Registry[P]) Register(id string, p P, m PluginManifest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = &RegistryEntry[P]{Plugin: p, Manifest: m}
	if _, exists := r.enabled[id]; !exists {
		r.enabled[id] = true // registered plugins are enabled by default
	}
}

// Get returns a plugin by ID. Returns nil, false if not found.
func (r *Registry[P]) Get(id string) (P, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[id]
	if !ok {
		var zero P
		return zero, false
	}
	return entry.Plugin, true
}

// List returns all registered entries.
func (r *Registry[P]) List() []RegistryEntry[P] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistryEntry[P], 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, *entry)
	}
	return out
}

// ListEnabled returns entries for plugins that are currently enabled.
func (r *Registry[P]) ListEnabled() []RegistryEntry[P] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistryEntry[P], 0, len(r.entries))
	for id, entry := range r.entries {
		if r.enabled[id] {
			out = append(out, *entry)
		}
	}
	return out
}

// Len returns the number of registered plugins.
func (r *Registry[P]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
