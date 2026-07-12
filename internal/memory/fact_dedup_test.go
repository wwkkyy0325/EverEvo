package memory

import (
	"testing"

	chromem "github.com/philippgille/chromem-go"
)

// newTestStoreWithVector is like newTestStore but wires an in-memory chromem
// vector store so the semantic-dedup path in AddFactMemory can be exercised.
func newTestStoreWithVector(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	db, err := chromem.NewPersistentDB(t.TempDir(), false)
	if err != nil {
		t.Fatalf("create chromem db: %v", err)
	}
	col, err := db.CreateCollection(memCollection, nil, nil)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	s.vector = &VectorStore{db: db, col: col}
	return s
}

// TestAddFactMemoryExactDedup: storing the same fact twice must yield one row.
// Guards the LLM re-extracting "用户喜欢 Go" every N turns into 5 duplicate rows.
func TestAddFactMemoryExactDedup(t *testing.T) {
	s := newTestStore(t) // no vector layer needed for the exact path
	emb := []float32{1, 0, 0}
	if err := s.AddFactMemory("f1", "用户喜欢 Go", "preference", "normal", "default", "[]", emb); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFactMemory("f2", "用户喜欢 Go", "preference", "normal", "default", "[]", emb); err != nil {
		t.Fatal(err)
	}
	_, facts := s.CountMemory("default")
	if facts != 1 {
		t.Errorf("exact dup: want 1 fact, got %d", facts)
	}
}

// TestAddFactMemorySemanticDedup: a paraphrase whose embedding is near-identical
// to an existing fact is dropped; a genuinely different fact is kept.
func TestAddFactMemorySemanticDedup(t *testing.T) {
	s := newTestStoreWithVector(t)

	// fact A stored
	if err := s.AddFactMemory("f1", "用户喜欢 Go 语言", "preference", "normal", "default", "[]", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	// paraphrase A' with near-identical embedding (cosine ≈ 0.9999) → deduped
	if err := s.AddFactMemory("f2", "用户喜爱的编程语言是 Go", "preference", "normal", "default", "[]", []float32{0.99, 0.01, 0}); err != nil {
		t.Fatal(err)
	}
	// distinct fact B (orthogonal embedding) → stored
	if err := s.AddFactMemory("f3", "用户住在北京", "profile", "normal", "default", "[]", []float32{0, 0, 1}); err != nil {
		t.Fatal(err)
	}

	_, facts := s.CountMemory("default")
	if facts != 2 {
		t.Errorf("semantic dedup: want 2 facts (A + B; paraphrase deduped), got %d", facts)
	}
}
