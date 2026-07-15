package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	chromem "github.com/philippgille/chromem-go"

	"everevo/internal/storage"
)

// memCollection holds conversation memory as embeddings, kept separate from the
// RAG knowledge-base collections (which live under data/knowledge/chromem).
// Documents are tagged with metadata.kind = "turn" | "fact" | "entity" and
// metadata.ws = <libraryID> for per-domain isolation.
//
// chromem's filter is equality-AND only (no $or). Per-domain isolation is
// therefore enforced in Go after retrieval: results whose "ws" matches the
// target library, OR whose "ws" is empty (legacy pre-isolation data), are kept.
const memCollection = "mem_longterm"

// VectorStore is the semantic memory layer. It stores pre-computed embeddings
// only — the ONNX embedding itself is done by the app layer (which owns the
// model), so this package stays free of ONNX/toolbox coupling.
type VectorStore struct {
	db  *chromem.DB
	col *chromem.Collection
	mu  sync.RWMutex
}

// NewVectorStore opens (or creates) the chromem DB under data/memory/chromem
// and ensures the mem_longterm collection exists.
func NewVectorStore() (*VectorStore, error) {
	base, err := storage.AppDataDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(base, "memory", "chromem")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("创建记忆向量目录失败: %w", err)
	}
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("打开记忆向量库失败: %w", err)
	}
	col := db.GetCollection(memCollection, nil)
	if col == nil {
		col, err = db.CreateCollection(memCollection, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("创建记忆集合失败: %w", err)
		}
	}
	return &VectorStore{db: db, col: col}, nil
}

// AddWithEmbedding stores a pre-embedded document with arbitrary metadata.
func (v *VectorStore) AddWithEmbedding(id, content string, embedding []float32, metadata map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	doc := chromem.Document{ID: id, Content: content, Embedding: embedding, Metadata: metadata}
	return v.col.AddDocuments(context.Background(), []chromem.Document{doc}, 1)
}

// AddTurn stores a user-question turn vector, tagged with its domain library.
// The assistant reply is carried in metadata so a single question-vector match
// returns the whole Q&A pair.
func (v *VectorStore) AddTurn(id, userText, reply, sessionID, itemID, workspaceID string, emb []float32) error {
	return v.AddWithEmbedding(id, userText, emb, map[string]string{
		"kind": "turn", "reply": reply, "sessionId": sessionID, "itemId": itemID, "ws": workspaceID,
	})
}

// AddFact stores an extracted fact vector, tagged with its domain library.
func (v *VectorStore) AddFact(id, content, category, itemID, workspaceID string, emb []float32) error {
	return v.AddWithEmbedding(id, content, emb, map[string]string{
		"kind": "fact", "category": category, "itemId": itemID, "ws": workspaceID,
	})
}

// AddEntity stores a graph-entity vector (kind=entity) so the retriever can
// seed graph expansion by semantic similarity to the query.
func (v *VectorStore) AddEntity(nodeID, name, entityType, workspaceID string, emb []float32) error {
	return v.AddWithEmbedding(nodeID, name, emb, map[string]string{
		"kind": "entity", "type": entityType, "name": name, "itemId": nodeID, "ws": workspaceID,
	})
}

// AddCoreFact stores a high-importance core-fact vector (kind=core_fact).
// Core facts are identity/preference/constraint items that are permanent
// and never decayed. They live alongside turn/fact/entity in the same
// chromem collection, distinguished by kind metadata.
func (v *VectorStore) AddCoreFact(id, content, category, workspaceID string, emb []float32) error {
	return v.AddWithEmbedding(id, content, emb, map[string]string{
		"kind": "core_fact", "category": category, "itemId": id, "ws": workspaceID,
	})
}

// AddExperience stores a reflection/distilled insight vector (kind=experience).
// These are reusable lessons from the dream/reflection pipeline.
func (v *VectorStore) AddExperience(id, content, expKind, workspaceID string, emb []float32) error {
	return v.AddWithEmbedding(id, content, emb, map[string]string{
		"kind": "experience", "expKind": expKind, "itemId": id, "ws": workspaceID,
	})
}

// Delete removes documents by ID (orphan cleanup on memory/node deletion).
func (v *VectorStore) Delete(ids ...string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	return v.col.Delete(context.Background(), nil, nil, ids...)
}

// QueryWithEmbedding returns up to k nearest docs matching the metadata filter.
// Pass nil filter to match all. Returns nil (no error) when empty.
func (v *VectorStore) QueryWithEmbedding(embedding []float32, k int, filter map[string]string) ([]chromem.Result, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	n := v.col.Count()
	if n == 0 || k <= 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	return v.col.QueryEmbedding(context.Background(), embedding, k, filter, nil)
}

// scopeByWorkspace keeps results that belong to the target library, plus any
// legacy results that carry no "ws" tag (pre-isolation data). This implements
// per-domain isolation without re-embedding old vectors.
func scopeByWorkspace(res []chromem.Result, libraryID string) []chromem.Result {
	if libraryID == "" {
		return res
	}
	out := res[:0]
	for _, r := range res {
		ws := r.Metadata["ws"]
		if ws == libraryID || ws == "" {
			out = append(out, r)
		}
	}
	return out
}

// QueryTurns returns up to k turn hits scoped to libraryID (legacy data included).
func (v *VectorStore) QueryTurns(emb []float32, k int, libraryID string) ([]TurnHit, error) {
	// Fetch a wider candidate set (3x) so post-filtering still yields ~k hits.
	fetchK := k * 3
	res, err := v.QueryWithEmbedding(emb, fetchK, map[string]string{"kind": "turn"})
	if err != nil {
		return nil, err
	}
	res = scopeByWorkspace(res, libraryID)
	if len(res) > k {
		res = res[:k]
	}
	out := make([]TurnHit, 0, len(res))
	for _, r := range res {
		out = append(out, TurnHit{Content: r.Content, Reply: r.Metadata["reply"], ItemID: r.Metadata["itemId"], Similarity: r.Similarity})
	}
	return out, nil
}

// QueryFacts returns up to k fact hits scoped to libraryID.
func (v *VectorStore) QueryFacts(emb []float32, k int, libraryID string) ([]FactHit, error) {
	fetchK := k * 3
	res, err := v.QueryWithEmbedding(emb, fetchK, map[string]string{"kind": "fact"})
	if err != nil {
		return nil, err
	}
	res = scopeByWorkspace(res, libraryID)
	if len(res) > k {
		res = res[:k]
	}
	out := make([]FactHit, 0, len(res))
	for _, r := range res {
		out = append(out, FactHit{Content: r.Content, Category: r.Metadata["category"], ItemID: r.Metadata["itemId"], Similarity: r.Similarity})
	}
	return out, nil
}

// QueryCoreFacts returns up to k core-fact hits scoped to libraryID.
// Same fetchK*3 + scopeByWorkspace pattern as QueryFacts.
func (v *VectorStore) QueryCoreFacts(emb []float32, k int, libraryID string) ([]FactHit, error) {
	fetchK := k * 3
	res, err := v.QueryWithEmbedding(emb, fetchK, map[string]string{"kind": "core_fact"})
	if err != nil {
		return nil, err
	}
	res = scopeByWorkspace(res, libraryID)
	if len(res) > k {
		res = res[:k]
	}
	out := make([]FactHit, 0, len(res))
	for _, r := range res {
		out = append(out, FactHit{Content: r.Content, Category: r.Metadata["category"], ItemID: r.Metadata["itemId"], Similarity: r.Similarity})
	}
	return out, nil
}

// QueryExperience returns up to k experience/insight hits scoped to libraryID.
func (v *VectorStore) QueryExperience(emb []float32, k int, libraryID string) ([]FactHit, error) {
	fetchK := k * 3
	res, err := v.QueryWithEmbedding(emb, fetchK, map[string]string{"kind": "experience"})
	if err != nil {
		return nil, err
	}
	res = scopeByWorkspace(res, libraryID)
	if len(res) > k {
		res = res[:k]
	}
	out := make([]FactHit, 0, len(res))
	for _, r := range res {
		out = append(out, FactHit{Content: r.Content, Category: r.Metadata["expKind"], ItemID: r.Metadata["itemId"], Similarity: r.Similarity})
	}
	return out, nil
}

// QueryEntities returns up to k entity hits scoped to libraryID.
func (v *VectorStore) QueryEntities(emb []float32, k int, libraryID string) ([]EntityHit, error) {
	fetchK := k * 3
	res, err := v.QueryWithEmbedding(emb, fetchK, map[string]string{"kind": "entity"})
	if err != nil {
		return nil, err
	}
	res = scopeByWorkspace(res, libraryID)
	if len(res) > k {
		res = res[:k]
	}
	out := make([]EntityHit, 0, len(res))
	for _, r := range res {
		out = append(out, EntityHit{ID: r.Metadata["itemId"], Name: r.Metadata["name"], Type: r.Metadata["type"], Similarity: r.Similarity})
	}
	return out, nil
}

// Clear drops and recreates the collection, wiping all memory vectors.
func (v *VectorStore) Clear() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.db.DeleteCollection(memCollection); err != nil {
		return err
	}
	col, err := v.db.CreateCollection(memCollection, nil, nil)
	if err != nil {
		return err
	}
	v.col = col
	return nil
}
