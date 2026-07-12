package memory

import (
	"database/sql"
	"testing"
)

// newTestStore opens an in-memory SQLite store (no vector layer) for graph tests.
// Graph operations are SQLite-only; the embed callback is nil so no vectors are needed.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func TestUpsertNodeDisambiguates(t *testing.T) {
	s := newTestStore(t)
	id1, err := s.UpsertNode("language", "Go", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.UpsertNode("language", "  go ", "default", nil) // normalized → same node
	if err != nil {
		t.Fatal(err)
	}
	id3, err := s.UpsertNode("language", "Rust", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("expected 'Go' and 'go' to merge into one node; got %q vs %q", id1, id2)
	}
	if id1 == id3 {
		t.Error("expected 'Rust' to be a distinct node from 'Go'")
	}
	if got := s.NodeCount(); got != 2 {
		t.Errorf("node count: want 2, got %d", got)
	}
}

func TestAddEdgeBiTemporalClose(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.UpsertNode("person", "用户", "default", nil)
	goNode, _ := s.UpsertNode("language", "Go", "default", nil)
	rust, _ := s.UpsertNode("language", "Rust", "default", nil)

	if err := s.AddEdge(user, goNode, "likes", "{}", "s1", "[]", true); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEdge(user, rust, "likes", "{}", "s2", "[]", true); err != nil { // supersedes "likes Go"
		t.Fatal(err)
	}

	// Current belief: only "用户 likes Rust".
	if got := s.CurrentEdgeCount(); got != 1 {
		t.Errorf("current edge count: want 1, got %d", got)
	}
	// History: both edges persist (the old one is closed, not deleted).
	var total int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM kg_edges WHERE src_id = ? AND type = ?`, user, "likes").Scan(&total)
	if total != 2 {
		t.Errorf("total likes edges (incl. history): want 2, got %d", total)
	}
	// The closed edge's valid_to must be set.
	var closedValidTo sql.NullInt64
	_ = s.db.QueryRow(`SELECT valid_to FROM kg_edges WHERE dst_id = ?`, goNode).Scan(&closedValidTo)
	if !closedValidTo.Valid {
		t.Error("expected the old 'likes Go' edge to have valid_to closed")
	}
}

func TestQueryGraphHops(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.UpsertNode("", "A", "default", nil)
	b, _ := s.UpsertNode("", "B", "default", nil)
	c, _ := s.UpsertNode("", "C", "default", nil)
	_ = s.AddEdge(a, b, "knows", "{}", "", "[]", false)
	_ = s.AddEdge(b, c, "knows", "{}", "", "[]", false)

	// 1 hop from A reaches {A, B} → only the A–B edge has both ends in reach.
	edges1, err := s.QueryGraph([]string{a}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges1) != 1 {
		t.Errorf("1-hop from A: want 1 edge, got %d (%v)", len(edges1), edges1)
	}
	// 2 hops reaches {A, B, C} → A–B and B–C.
	edges2, err := s.QueryGraph([]string{a}, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges2) != 2 {
		t.Errorf("2-hop from A: want 2 edges, got %d (%v)", len(edges2), edges2)
	}
	// 0 hops → nothing.
	if edges0, _ := s.QueryGraph([]string{a}, 0, ""); len(edges0) != 0 {
		t.Errorf("0-hop: want 0 edges, got %d", len(edges0))
	}
}

func TestNormalizePredicate(t *testing.T) {
	cases := map[string]string{
		"用":     "使用",
		"采用":   "使用",
		"使用":   "使用",
		"喜爱":   "喜欢",
		"偏好":   "喜欢",
		"喜欢":   "喜欢",
		"工作于": "就职于",
		"  使用 ": "使用",
		"独立谓词": "独立谓词",
	}
	for in, want := range cases {
		if got := normalizePredicate(in); got != want {
			t.Errorf("normalizePredicate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddEdgeReplacesFalseCoexists(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.UpsertNode("person", "用户", "default", nil)
	goNode, _ := s.UpsertNode("language", "Go", "default", nil)
	py, _ := s.UpsertNode("language", "Python", "default", nil)
	// Two coexisting likes (replaces=false) → both stay current.
	if err := s.AddEdge(user, goNode, "喜欢", "{}", "", "[]", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEdge(user, py, "喜欢", "{}", "", "[]", false); err != nil {
		t.Fatal(err)
	}
	if got := s.CurrentEdgeCount(); got != 2 {
		t.Errorf("coexisting likes: want 2 current edges, got %d", got)
	}
	// A replaces=true like supersedes the current same-(src,type) edges, then inserts one.
	rust, _ := s.UpsertNode("language", "Rust", "default", nil)
	if err := s.AddEdge(user, rust, "喜欢", "{}", "", "[]", true); err != nil {
		t.Fatal(err)
	}
	if got := s.CurrentEdgeCount(); got != 1 {
		t.Errorf("after replaces: want 1 current edge, got %d", got)
	}
}
