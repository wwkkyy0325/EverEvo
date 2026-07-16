package memory

import (
	"context"
	"fmt"

	"everevo/internal/core"
)

// EmbedFunc converts a text query to a float32 embedding vector.
// The app layer provides this (typically via ONNX-based embedding).
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// MemoryPlugin wraps Store to implement core.MemoryPlugin, making the memory
// subsystem visible to the Engine's plugin registry. The embedder function is
// injected by the app layer so the memory package stays free of ONNX coupling.
type MemoryPlugin struct {
	store   *Store
	embedFn EmbedFunc
}

// NewMemoryPlugin creates the plugin wrapper. Call SetEmbedder before
// VectorSearch is used (typically right after construction).
func NewMemoryPlugin(s *Store, embedFn EmbedFunc) *MemoryPlugin {
	return &MemoryPlugin{store: s, embedFn: embedFn}
}

// Manifest satisfies core.Plugin.
func (mp *MemoryPlugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID:          "memory",
		Name:        "Chat Memory",
		Version:     "1.0",
		Description: "SQLite-backed chat persistence with Chromem vector semantic memory",
		Author:      "EverEvo",
		Type:        "memory",
	}
}

// Store satisfies core.MemoryPlugin, returning a key-value view of the
// underlying persistence layer.
func (mp *MemoryPlugin) Store() core.Store {
	return &kvAdapter{store: mp.store}
}

// VectorSearch satisfies core.MemoryPlugin. It delegates to the embedder for
// text-to-vector conversion, then performs semantic search across turns and
// facts. Returns nil if the embedder is not yet configured.
func (mp *MemoryPlugin) VectorSearch(ctx context.Context, query string, k int) ([]core.VectorResult, error) {
	if mp.embedFn == nil {
		return nil, nil // degraded mode — embedder not configured yet
	}
	emb, err := mp.embedFn(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory: embed query: %w", err)
	}
	turns, facts, err := mp.store.QueryMemory(emb, k, "")
	if err != nil {
		return nil, fmt.Errorf("memory: vector search: %w", err)
	}
	out := make([]core.VectorResult, 0, len(turns)+len(facts))
	for _, t := range turns {
		out = append(out, core.VectorResult{
			ID:      t.ItemID,
			Content: t.Content,
			Score:   float64(t.Similarity),
			Metadata: map[string]string{
				"kind":  "turn",
				"reply": t.Reply,
			},
		})
	}
	for _, f := range facts {
		out = append(out, core.VectorResult{
			ID:      f.ItemID,
			Content: f.Content,
			Score:   float64(f.Similarity),
			Metadata: map[string]string{
				"kind":     "fact",
				"category": f.Category,
			},
		})
	}
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}
