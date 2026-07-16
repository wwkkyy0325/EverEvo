package memory

import (
	"encoding/json"
	"testing"
)

// TestListNodesEmptyMarshalsToArray guards the graph_list contract: an empty node
// set must serialize to JSON [] (not null). The graph_list tool handler relies
// on this — an earlier bug asserted nodes to []any (which always fails for a
// []GraphNode) and surfaced as "nodes: null" to the LLM.
func TestListNodesEmptyMarshalsToArray(t *testing.T) {
	s := newTestStore(t)
	nodes, err := s.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	if nodes == nil {
		t.Fatal("ListNodes must return a non-nil empty slice so JSON is [] not null")
	}
	b, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Errorf("empty nodes JSON: want [], got %s", b)
	}
}
