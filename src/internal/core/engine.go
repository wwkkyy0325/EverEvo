package core

import (
	"context"
	"log"
	"sync"
)

// Engine is the central dependency-injection container and lifecycle manager.
// It owns the registries (extension points), but does NOT import any specific
// plugin implementations. Plugins self-register via init().
//
// Engine replaces the monolithic App struct as the single source of truth for
// all subsystem references. The Wails App struct becomes a thin wrapper that
// delegates to Engine.
type Engine struct {
	// ── Infrastructure ──
	store Store     // unified persistence (storage.Store)
	bus   *EventBus // in-process pub/sub

	// ── Extension points (registries) ──
	Providers *Registry[ProviderPlugin]
	Tools     *Registry[ToolPlugin]
	Channels  *Registry[ChannelPlugin]
	Memories  *Registry[MemoryPlugin]

	// ── Lifecycle ──
	ctx context.Context
	cancel context.CancelFunc
	mu       sync.RWMutex
	started  bool
}

// Store is the minimal persistence interface Engine needs.
// Implementations: storage.SQLiteStore, or memory-backed for tests.
type Store interface {
	Get(ctx context.Context, bucket, key string) ([]byte, error)
	Set(ctx context.Context, bucket, key string, value []byte) error
	Delete(ctx context.Context, bucket, key string) error
	List(ctx context.Context, bucket, prefix string) ([]string, error)
}

// EngineOption configures an Engine at construction time.
type EngineOption func(*Engine)

// WithStore sets the persistence backend.
func WithStore(s Store) EngineOption {
	return func(e *Engine) { e.store = s }
}

// NewEngine creates an Engine with default (empty) registries.
// Use WithStore() and other options to configure, then call Start().
func NewEngine(opts ...EngineOption) *Engine {
	e := &Engine{
		bus:       NewEventBus(),
		Providers: NewRegistry[ProviderPlugin]("providers"),
		Tools:     NewRegistry[ToolPlugin]("tools"),
		Channels:  NewRegistry[ChannelPlugin]("channels"),
		Memories:  NewRegistry[MemoryPlugin]("memories"),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Start initializes all registered plugins and infrastructure.
// Calls each plugin's Init() if it implements LifecyclePlugin.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return nil
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.started = true
	e.MergeGlobals()

	log.Println("[core] engine starting")
	log.Printf("[core] providers=%d tools=%d channels=%d memories=%d",
		e.Providers.Len(), e.Tools.Len(), e.Channels.Len(), e.Memories.Len())

	// Initialize plugins that support the LifecyclePlugin interface.
	for _, entry := range e.Providers.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			if err := lp.Init(ctx); err != nil {
				log.Printf("[core] provider %q init failed: %v", entry.Manifest.ID, err)
			}
		}
	}
	for _, entry := range e.Tools.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			if err := lp.Init(ctx); err != nil {
				log.Printf("[core] tool %q init failed: %v", entry.Manifest.ID, err)
			}
		}
	}
	for _, entry := range e.Channels.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			if err := lp.Init(ctx); err != nil {
				log.Printf("[core] channel %q init failed: %v", entry.Manifest.ID, err)
			}
		}
	}
		// Initialize memory backends.
		for _, entry := range e.Memories.List() {
			if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
				if err := lp.Init(ctx); err != nil {
					log.Printf("[core] memory %q init failed: %v", entry.Manifest.ID, err)
				}
			}
		}


	log.Println("[core] engine started")
	return nil
}

// Stop shuts down all plugins and releases resources.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return nil
	}

	log.Println("[core] engine stopping")

	// Stop tools.
	for _, entry := range e.Tools.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			_ = lp.Stop()
		}
	}
	for _, entry := range e.Providers.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			_ = lp.Stop()
		}
	}
	// Stop channels last (connections, listeners).
	for _, entry := range e.Channels.List() {
		if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
			if err := lp.Stop(); err != nil {
				log.Printf("[core] channel %q stop: %v", entry.Manifest.ID, err)
			}
		}
	}
		// Stop memory backends.
		for _, entry := range e.Memories.List() {
			if lp, ok := any(entry.Plugin).(LifecyclePlugin); ok {
				_ = lp.Stop()
			}
		}


	e.cancel()
	e.started = false
	log.Println("[core] engine stopped")
	return nil
}

// Context returns the engine's lifecycle context (cancelled on Stop).
func (e *Engine) Context() context.Context {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// Bus returns the in-process event bus for pub/sub communication.
func (e *Engine) Bus() *EventBus { return e.bus }

// GetStore returns the persistence backend (nil if not configured).
func (e *Engine) GetStore() Store {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.store
}

// LifecyclePlugin is an optional interface for plugins that need
// initialization and cleanup beyond manifest registration.
type LifecyclePlugin interface {
	Init(ctx context.Context) error
	Stop() error
}

// ─── Global registries ────────────────────────────────────────────────
// Plugin packages call Register on these during init().
// Engine.Start() then discovers all registered plugins.

var (
	// GlobalProviders is the global registry for AI provider plugins.
	GlobalProviders = NewRegistry[ProviderPlugin]("providers")

	// GlobalTools is the global registry for tool plugins.
	GlobalTools = NewRegistry[ToolPlugin]("tools")

	// GlobalChannels is the global registry for channel plugins.
	GlobalChannels = NewRegistry[ChannelPlugin]("channels")

	// GlobalMemories is the global registry for memory plugins.
	GlobalMemories = NewRegistry[MemoryPlugin]("memories")
)

// MergeGlobals copies all entries from the global registries into the
// engine's registries. Called once during Engine.Start().
func (e *Engine) MergeGlobals() {
	for _, entry := range GlobalProviders.List() {
		e.Providers.Register(entry.Manifest.ID, entry.Plugin, entry.Manifest)
	}
	for _, entry := range GlobalTools.List() {
		e.Tools.Register(entry.Manifest.ID, entry.Plugin, entry.Manifest)
	}
	for _, entry := range GlobalChannels.List() {
		e.Channels.Register(entry.Manifest.ID, entry.Plugin, entry.Manifest)
	}
	for _, entry := range GlobalMemories.List() {
		e.Memories.Register(entry.Manifest.ID, entry.Plugin, entry.Manifest)
	}
}

