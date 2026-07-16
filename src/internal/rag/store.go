//go:build windows

package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	chromem "github.com/philippgille/chromem-go"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// DocEntry is a lightweight record of a stored document for listing purposes.
type DocEntry struct {
	ID        string            `json:"id"`
	Preview   string            `json:"preview"` // first 200 chars
	Metadata  map[string]string `json:"metadata"`
	AddedAt   string            `json:"addedAt"`
}

// Store wraps a chromem-go persistent database for knowledge base operations.
// KB metadata and document manifests are persisted as JSON — chromem-go's
// Collection.metadata and document map are unexported. The bm25 field holds
// per-collection BM25 sparse indices for hybrid vector+keyword retrieval.
type Store struct {
	db        *chromem.DB
	meta      map[string]*KnowledgeBase // kbID → metadata
	docs      map[string][]DocEntry     // collectionName → document manifest
	bm25      map[string]*Bm25Index     // collectionName → BM25 sparse index
	registrar ChunkRegistrar            // optional: called after AddDocuments for bidirectional indexing
	mu        sync.RWMutex
}

// NewStore opens or creates the persistent chromem-go DB and loads metadata.
func NewStore() (*Store, error) {
	dir, err := KnowDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "chromem")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("创建 chromem 目录失败: %w", err)
	}
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("打开知识库数据库失败: %w", err)
	}

	s := &Store{
		db:   db,
		meta: make(map[string]*KnowledgeBase),
		docs: make(map[string][]DocEntry),
		bm25: make(map[string]*Bm25Index),
	}
	s.loadMeta()
	s.loadDocs()
	s.rebuildBm25()
	return s, nil
}

// SetChunkRegistrar sets an optional callback invoked after documents are added
// to register chunk→source mappings for bidirectional indexing.
func (s *Store) SetChunkRegistrar(r ChunkRegistrar) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrar = r
}

// ─── JSON 持久化 ────────────────────────────────────────────────

func (s *Store) dataDir() string {
	dir, _ := KnowDir()
	return dir
}

func (s *Store) metaPath() string { return filepath.Join(s.dataDir(), "meta.json") }
func (s *Store) docsPath() string { return filepath.Join(s.dataDir(), "docs.json") }

func (s *Store) loadMeta() {
	data, err := os.ReadFile(s.metaPath())
	if err != nil {
		return
	}
	var list []*KnowledgeBase
	if json.Unmarshal(data, &list) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kb := range list {
		s.meta[kb.ID] = kb
	}
}

func (s *Store) saveMetaLocked() {
	var list []*KnowledgeBase
	for _, kb := range s.meta {
		list = append(list, kb)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("[rag] 保存元数据 JSON 失败: %v", err)
		return
	}
	if err := atomic.WriteFile(s.metaPath(), data, 0644); err != nil {
		log.Printf("[rag] 保存元数据失败: %v", err)
	}
}

func (s *Store) loadDocs() {
	data, err := os.ReadFile(s.docsPath())
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	json.Unmarshal(data, &s.docs)
	if s.docs == nil {
		s.docs = make(map[string][]DocEntry)
	}
}

func (s *Store) saveDocsLocked() {
	data, err := json.MarshalIndent(s.docs, "", "  ")
	if err != nil {
		log.Printf("[rag] 保存文档 JSON 失败: %v", err)
		return
	}
	if err := atomic.WriteFile(s.docsPath(), data, 0644); err != nil {
		log.Printf("[rag] 保存文档失败: %v", err)
	}
}

// ─── BM25 helpers ────────────────────────────────────────────────

func (s *Store) ensureBm25Locked(collectionName string) *Bm25Index {
	idx, ok := s.bm25[collectionName]
	if !ok {
		idx = NewBm25Index()
		s.bm25[collectionName] = idx
	}
	return idx
}

// rebuildBm25 rebuilds all BM25 indices from the document manifests at startup.
func (s *Store) rebuildBm25() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for col, entries := range s.docs {
		idx := s.ensureBm25Locked(col)
		colRef := s.db.GetCollection(col, nil)
		for _, e := range entries {
			if colRef != nil {
				if d, err := colRef.GetByID(context.Background(), e.ID); err == nil {
					idx.Add(e.ID, d.Content)
				}
			}
		}
	}
}

// ─── Collection 操作 ─────────────────────────────────────────────

// CreateCollection creates a chromem-go collection and saves KB metadata.
func (s *Store) CreateCollection(id, name, modelDir, libraryID, createdAt string) error {
	_, err := s.db.CreateCollection(id, nil, nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.meta[id] = &KnowledgeBase{
		ID:        id,
		Name:      name,
		ModelDir:  modelDir,
		LibraryID: libraryID,
		CreatedAt: createdAt,
	}
	s.docs[id] = []DocEntry{}
	s.ensureBm25Locked(id)
	s.saveMetaLocked()
	s.saveDocsLocked()
	s.mu.Unlock()
	return nil
}

// AddDocuments inserts pre-embedded documents and tracks them in the manifest.
func (s *Store) AddDocuments(collectionName string, docs []chromem.Document, concurrency int) (int, error) {
	col := s.db.GetCollection(collectionName, nil)
	if col == nil {
		return 0, fmt.Errorf("知识库不存在: %s", collectionName)
	}
	if err := col.AddDocuments(context.Background(), docs, concurrency); err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	// 确保 legacy KB（meta 有但 docs 无）也能正常跟踪
	if _, ok := s.docs[collectionName]; !ok {
		s.docs[collectionName] = []DocEntry{}
	}
	bm := s.ensureBm25Locked(collectionName)
	for _, d := range docs {
		preview := d.Content
		if len([]rune(preview)) > 200 {
			preview = string([]rune(preview)[:200]) + "…"
		}
		s.docs[collectionName] = append(s.docs[collectionName], DocEntry{
			ID:       d.ID,
			Preview:  preview,
			Metadata: d.Metadata,
			AddedAt:  now,
		})
		bm.Add(d.ID, d.Content)
	}
	s.saveDocsLocked()
	// snapshot registrar before releasing lock
	reg := s.registrar
	s.mu.Unlock()
	// Call registrar outside lock to avoid deadlock with memory store
	if reg != nil {
		docIDs := make([]string, len(docs))
		contents := make([]string, len(docs))
		for i, d := range docs {
			docIDs[i] = d.ID
			contents[i] = d.Content
		}
		// Best-effort registration (non-blocking)
		_ = reg("rag_kb", collectionName, docIDs, 0, contents)
	}
	return len(docs), nil
}

// Query searches a collection by embedding vector (dense-only, legacy path).
// For hybrid retrieval use HybridSearch.
func (s *Store) Query(collectionName string, embedding []float32, k int, filter map[string]string) ([]chromem.Result, error) {
	col := s.db.GetCollection(collectionName, nil)
	if col == nil {
		return nil, fmt.Errorf("知识库不存在: %s", collectionName)
	}
	n := col.Count()
	if n == 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	return col.QueryEmbedding(context.Background(), embedding, k, filter, nil)
}

// Count returns the number of documents in a collection.
func (s *Store) Count(collectionName string) int {
	col := s.db.GetCollection(collectionName, nil)
	if col == nil {
		return 0
	}
	return col.Count()
}

// ─── Hybrid search ───────────────────────────────────────────────

// rrfK is the rank-smoothing constant for Reciprocal Rank Fusion.
const rrfK = 60.0

// HybridSearch performs dense vector + sparse BM25 retrieval and fuses results
// via Reciprocal Rank Fusion. queryEmb is the dense query embedding; queryStr is
// the raw query text for BM25 keyword matching.
func (s *Store) HybridSearch(collectionName string, queryEmb []float32, queryStr string, k int, filter map[string]string) ([]chromem.Result, error) {
	col := s.db.GetCollection(collectionName, nil)
	if col == nil {
		return nil, fmt.Errorf("知识库不存在: %s", collectionName)
	}

	n := col.Count()
	if n == 0 {
		return nil, nil
	}

	// 1. Dense vector search — always runs.
	denseK := k * 3
	if denseK > n {
		denseK = n
	}
	denseResults, err := col.QueryEmbedding(context.Background(), queryEmb, denseK, filter, nil)
	if err != nil {
		return nil, err
	}

	// 2. BM25 sparse search — best-effort.
	s.mu.RLock()
	bm, hasBm := s.bm25[collectionName]
	s.mu.RUnlock()

	var bm25Results []Bm25Result
	if hasBm {
		bm25Results = bm.Search(queryStr, denseK)
	}

	// 3. RRF fusion.
	scores := make(map[string]float64)
	idToResult := make(map[string]chromem.Result)

	set := func(id string, r chromem.Result, score float64) {
		if existing, ok := scores[id]; ok {
			scores[id] = existing + score
		} else {
			scores[id] = score
			idToResult[id] = r
		}
	}

	for rank, r := range denseResults {
		set(r.ID, r, 1.0/(rrfK+float64(rank+1)))
	}
	for rank, r := range bm25Results {
		// If the doc wasn't in dense results, we need its content.
		if _, ok := idToResult[r.DocID]; !ok {
			if d, err := col.GetByID(context.Background(), r.DocID); err == nil {
				idToResult[r.DocID] = chromem.Result{ID: r.DocID, Content: d.Content, Metadata: d.Metadata}
			}
		}
		set(r.DocID, idToResult[r.DocID], 1.0/(rrfK+float64(rank+1)))
	}

	// Sort by RRF score descending.
	type scored struct {
		id    string
		score float64
	}
	var sorted []scored
	for id, s := range scores {
		sorted = append(sorted, scored{id: id, score: s})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })

	results := make([]chromem.Result, 0, k)
	for _, sc := range sorted {
		r := idToResult[sc.id]
		r.Similarity = float32(math.Min(sc.score*float64(rrfK), 1.0)) // normalize to ~[0,1]
		results = append(results, r)
		if len(results) >= k {
			break
		}
	}
	return results, nil
}

// ─── 文档 CRUD ───────────────────────────────────────────────────

// ListDocuments returns the document manifest for a collection.
// Returns an empty list (not an error) if the KB exists but has no tracked docs.
func (s *Store) ListDocuments(collectionName string) ([]DocEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, ok := s.docs[collectionName]
	if !ok {
		// KB 可能是旧版创建的，docs.json 尚未注册。初始化空清单。
		if _, metaOK := s.meta[collectionName]; metaOK {
			return []DocEntry{}, nil
		}
		return nil, fmt.Errorf("知识库不存在: %s", collectionName)
	}
	return entries, nil
}

// DeleteDocuments removes specific documents from a collection by ID.
// Returns the number of deleted documents.
func (s *Store) DeleteDocuments(collectionName string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	col := s.db.GetCollection(collectionName, nil)
	if col == nil {
		return 0, fmt.Errorf("知识库不存在: %s", collectionName)
	}
	if err := col.Delete(context.Background(), nil, nil, ids...); err != nil {
		return 0, err
	}

	// Remove from manifest and BM25 index.
	s.mu.Lock()
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	filtered := make([]DocEntry, 0, len(s.docs[collectionName]))
	for _, e := range s.docs[collectionName] {
		if !idSet[e.ID] {
			filtered = append(filtered, e)
		}
	}
	s.docs[collectionName] = filtered
	if bm, ok := s.bm25[collectionName]; ok {
		for _, id := range ids {
			bm.Remove(id)
		}
	}
	s.saveDocsLocked()
	s.mu.Unlock()
	return len(ids), nil
}

// ClearCollection wipes all documents from a collection while preserving KB metadata.
func (s *Store) ClearCollection(collectionName string) error {
	// chromem-go has no "delete all" without IDs, so drop and recreate.
	if err := s.db.DeleteCollection(collectionName); err != nil {
		return err
	}
	if _, err := s.db.CreateCollection(collectionName, nil, nil); err != nil {
		return fmt.Errorf("重建集合失败: %w", err)
	}
	s.mu.Lock()
	s.docs[collectionName] = []DocEntry{}
	// Rebuild empty BM25 index
	s.bm25[collectionName] = NewBm25Index()
	s.saveDocsLocked()
	s.mu.Unlock()
	return nil
}

// ─── KB 元数据查询 ───────────────────────────────────────────────

// GetKB returns KB metadata by ID.
func (s *Store) GetKB(kbID string) (*KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kb, ok := s.meta[kbID]
	if !ok {
		return nil, fmt.Errorf("知识库不存在: %s", kbID)
	}
	kb.ChunkCount = s.Count(kbID)
	return kb, nil
}

// ListKBs returns all knowledge bases, optionally filtered by libraryID.
func (s *Store) ListKBs(libraryID string) []*KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*KnowledgeBase
	for _, kb := range s.meta {
		if libraryID != "" && kb.LibraryID != libraryID && kb.LibraryID != "" {
			continue
		}
		kb.ChunkCount = s.Count(kb.ID)
		list = append(list, kb)
	}
	return list
}

// BackfillLibraryIDs sets LibraryID on all KBs that don't have one (or have
// an invalid/dangling one). Called at startup. validIDs is the set of current
// domain library IDs from the memory store.
func (s *Store) BackfillLibraryIDs(defaultLibraryID string, validIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	valid := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}
	changed := false
	for _, kb := range s.meta {
		if kb.LibraryID == "" || !valid[kb.LibraryID] {
			if kb.LibraryID != "" {
				log.Printf("[rag] KB %q 的 libraryId %q 无效，回填为默认领域", kb.Name, kb.LibraryID)
			}
			kb.LibraryID = defaultLibraryID
			changed = true
		}
	}
	if changed {
		s.saveMetaLocked()
	}
}

// DeleteCollection removes a collection, its KB metadata, and its doc manifest.
func (s *Store) DeleteCollection(name string) error {
	if err := s.db.DeleteCollection(name); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.meta, name)
	delete(s.docs, name)
	delete(s.bm25, name)
	s.saveMetaLocked()
	s.saveDocsLocked()
	s.mu.Unlock()
	return nil
}

// SetKBLibrary changes a KB's library/domain association and persists metadata.
func (s *Store) SetKBLibrary(kbID, libraryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kb, ok := s.meta[kbID]
	if !ok {
		return fmt.Errorf("知识库不存在: %s", kbID)
	}
	kb.LibraryID = libraryID
	s.saveMetaLocked()
	return nil
}

// UpdateKBModelDir changes a KB's bound embedding model dir. Only safe when the
// KB has no documents; use MigrateKBModel to re-embed existing docs.
func (s *Store) UpdateKBModelDir(kbID, newDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.meta[kbID]; !ok {
		return fmt.Errorf("知识库不存在: %s", kbID)
	}
	s.meta[kbID].ModelDir = newDir
	s.saveMetaLocked()
	return nil
}

// MigrateKBModel re-embeds all KB docs with a new model and rebinds. Reads full
// content via GetByID (manifest holds IDs), re-embeds in bulk, then swaps the
// collection. Aborts (without mutation) if manifest is incomplete vs count.
func (s *Store) MigrateKBModel(kbID, newDir string, embedBatch func([]string) ([][]float32, error)) error {
	col := s.db.GetCollection(kbID, nil)
	if col == nil {
		return fmt.Errorf("知识库不存在: %s", kbID)
	}
	s.mu.RLock()
	entries := append([]DocEntry(nil), s.docs[kbID]...)
	s.mu.RUnlock()

	if col.Count() == 0 {
		return s.UpdateKBModelDir(kbID, newDir) // empty KB — free rebind
	}
	if len(entries) != col.Count() {
		return fmt.Errorf("文档清单不完整（清单 %d / 实际 %d），无法安全迁移；请先重建知识库", len(entries), col.Count())
	}

	contents := make([]string, len(entries))
	metas := make([]map[string]string, len(entries))
	ids := make([]string, len(entries))
	for i, e := range entries {
		d, err := col.GetByID(context.Background(), e.ID)
		if err != nil {
			return fmt.Errorf("读取文档 %s 失败: %w", e.ID, err)
		}
		contents[i] = d.Content
		metas[i] = d.Metadata
		ids[i] = d.ID
	}
	vecs, err := embedBatch(contents)
	if err != nil {
		return fmt.Errorf("重新嵌入失败: %w", err)
	}

	docs := make([]chromem.Document, len(entries))
	for i := range entries {
		docs[i] = chromem.Document{ID: ids[i], Content: contents[i], Embedding: vecs[i], Metadata: metas[i]}
	}
	// Swap: drop + recreate + reinsert, then rebind.
	if err := s.db.DeleteCollection(kbID); err != nil {
		return err
	}
	col2, err := s.db.CreateCollection(kbID, nil, nil)
	if err != nil {
		return err
	}
	if err := col2.AddDocuments(context.Background(), docs, 4); err != nil {
		return err
	}
	// Rebuild BM25 index with new IDs (same content).
	s.mu.Lock()
	s.bm25[kbID] = NewBm25Index()
	for _, d := range docs {
		s.bm25[kbID].Add(d.ID, d.Content)
	}
	s.mu.Unlock()
	return s.UpdateKBModelDir(kbID, newDir)
}

// KnowDir returns the data/knowledge/ directory path.
func KnowDir() (string, error) {
	base, err := storage.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "knowledge"), nil
}
