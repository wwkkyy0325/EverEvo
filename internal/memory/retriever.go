package memory

import (
	"fmt"
	"sort"
	"strings"
)

// RetrieveGraph is the graph half of hybrid retrieval: take the query embedding
// → vector-seed entities → expand 2 hops along currently-valid edges → assemble a
// compact "subject —predicate→ object" context block.
//
// It accepts the embedding already computed by the caller (MemoryRecall computes
// it once for turns/facts/graph), avoiding double-embedding. Returns "" (no error)
// when there are no seeds or no edges, so the caller can no-op cleanly — matching
// MemoryRecall's zero-impact degradation contract.
func (s *Store) RetrieveGraph(emb []float32, k int, libraryID string) (string, error) {
	if len(emb) == 0 {
		return "", nil
	}
	if k <= 0 {
		k = 3
	}
	seeds, err := s.EntitySearch(emb, k, libraryID)
	if err != nil || len(seeds) == 0 {
		return "", nil
	}
	seedIDs := make([]string, 0, len(seeds))
	for _, e := range seeds {
		seedIDs = append(seedIDs, e.ID)
	}
	// Adaptive hops: sparse seeds → explore one more level.
	hops := 2
	if len(seedIDs) <= 2 {
		hops = 3
	}
	edges, err := s.QueryGraph(seedIDs, hops, libraryID)
	if err != nil || len(edges) == 0 {
		return "", nil
	}
	// Freshness: most-recently-recorded facts first, then cap context size.
	sort.Slice(edges, func(i, j int) bool { return edges[i].ValidFrom > edges[j].ValidFrom })
	const maxLines = 12
	var lines []string
	seen := make(map[string]bool, len(edges))
	for _, e := range edges {
		line := fmt.Sprintf("%s —%s→ %s", e.SrcName, e.Type, e.DstName)
		if seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
		if len(lines) >= maxLines {
			break
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n"), nil
}

// GraphTrace is the seed/edge ids a graph retrieval used — lets the UI highlight
// which part of the graph the last recall touched.
type GraphTrace struct {
	SeedIDs []string `json:"seedIds"`
	EdgeIDs []string `json:"edgeIds"`
}

// RetrieveGraphTrace runs the same seed+expand as RetrieveGraph but returns the
// ids used (for viewer highlight) instead of a text block.
func (s *Store) RetrieveGraphTrace(emb []float32, k int, libraryID string) *GraphTrace {
	t := &GraphTrace{SeedIDs: []string{}, EdgeIDs: []string{}}
	if len(emb) == 0 {
		return t
	}
	if k <= 0 {
		k = 3
	}
	seeds, err := s.EntitySearch(emb, k, libraryID)
	if err != nil || len(seeds) == 0 {
		return t
	}
	for _, e := range seeds {
		t.SeedIDs = append(t.SeedIDs, e.ID)
	}
	hops := 2
	if len(t.SeedIDs) <= 2 {
		hops = 3
	}
	edges, err := s.QueryGraph(t.SeedIDs, hops, libraryID)
	if err != nil {
		return t
	}
	for _, e := range edges {
		t.EdgeIDs = append(t.EdgeIDs, e.ID)
	}
	return t
}
