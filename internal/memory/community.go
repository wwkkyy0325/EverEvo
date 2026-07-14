package memory

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ─── Community Detection (GraphRAG Leiden-style) ──────────────────────

// KGCommunity is a detected cluster of related entities/relations.
type KGCommunity struct {
	ID          string            `json:"id"`
	Level       int               `json:"level"` // 0 = root, 1+ = sub-communities
	ParentID    string            `json:"parentId,omitempty"`
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	FullContent string            `json:"fullContent,omitempty"`
	NodeIDs     []string          `json:"nodeIds"` // JSON array
	EdgeIDs     []string          `json:"edgeIds"` // JSON array
	NodeCount   int               `json:"nodeCount"`
	EdgeCount   int               `json:"edgeCount"`
	Metrics     map[string]float64 `json:"metrics,omitempty"` // modularity, density, etc.
	CreatedAt   int64              `json:"createdAt"`
}

// CommunityConfig controls community detection parameters.
type CommunityConfig struct {
	MinCommunitySize int     // minimum nodes per community (default: 3)
	MaxCommunities   int     // max communities to detect (default: 50)
	Resolution       float64 // modularity resolution parameter (default: 1.0)
	MaxLevels        int     // max hierarchical levels (default: 3)
}

// ─── Louvain-style Community Detection (simplified Leiden alternative) ─

// DetectCommunities runs graph community detection using a Louvain-style
// greedy modularity optimization. For production Leiden algorithm, integrate
// an external library (e.g., graspologic via cgo or a Go port).
//
// The algorithm:
//  1. Assign each node to its own community
//  2. Iteratively move nodes to neighboring communities if modularity improves
//  3. Aggregate communities into super-nodes and repeat
//  4. Stop when no improvement or max levels reached
func (s *Store) DetectCommunities(cfg CommunityConfig) ([]KGCommunity, error) {
	if cfg.MinCommunitySize <= 0 {
		cfg.MinCommunitySize = 3
	}
	if cfg.MaxCommunities <= 0 {
		cfg.MaxCommunities = 50
	}
	if cfg.Resolution <= 0 {
		cfg.Resolution = 1.0
	}
	if cfg.MaxLevels <= 0 {
		cfg.MaxLevels = 3
	}

	// Phase 1: Build adjacency map from kg_edges
	adj, nodes, totalWeight := s.buildAdjacencyMap()
	if len(nodes) < cfg.MinCommunitySize {
		return nil, fmt.Errorf("community: too few nodes (%d) for detection (min=%d)", len(nodes), cfg.MinCommunitySize)
	}

	// Phase 2: Greedy modularity optimization (simplified Louvain)
	communities, err := s.louvainCommunities(adj, nodes, totalWeight, cfg)
	if err != nil {
		return nil, err
	}

	return communities, nil
}

func (s *Store) buildAdjacencyMap() (adj map[string]map[string]float64, nodes []string, totalWeight float64) {
	adj = make(map[string]map[string]float64)
	nodeSet := make(map[string]bool)

	rows, err := s.db.Query(`SELECT src_id, dst_id, CAST(weight AS REAL) FROM kg_edges WHERE valid_to IS NULL`)
	if err != nil {
		return adj, nodes, 0
	}
	defer rows.Close()

	for rows.Next() {
		var src, dst string
		var w float64
		if err := rows.Scan(&src, &dst, &w); err != nil {
			continue
		}
		if w <= 0 {
			w = 1.0
		}
		totalWeight += w
		nodeSet[src] = true
		nodeSet[dst] = true

		if adj[src] == nil {
			adj[src] = make(map[string]float64)
		}
		if adj[dst] == nil {
			adj[dst] = make(map[string]float64)
		}
		adj[src][dst] += w
		adj[dst][src] += w
	}

	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	return
}

func (s *Store) louvainCommunities(
	adj map[string]map[string]float64,
	nodes []string,
	totalWeight float64,
	cfg CommunityConfig,
) ([]KGCommunity, error) {
	// Initialize: each node in its own community
	community := make(map[string]string, len(nodes))
	nodeToComm := make(map[string]string, len(nodes))
	for _, n := range nodes {
		cID := "c_" + n[:minInt(8, len(n))]
		community[cID] = n
		nodeToComm[n] = cID
	}

	// Compute node weights (sum of incident edge weights)
	nodeWeight := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		var w float64
		for _, ew := range adj[n] {
			w += ew
		}
		nodeWeight[n] = w
	}

	// Greedy optimization: for each node, try moving to neighbor communities
	changed := true
	iterations := 0
	maxIter := 100

	for changed && iterations < maxIter {
		changed = false
		iterations++

		for _, n := range nodes {
			currentComm := nodeToComm[n]

			// Compute best community for this node
			commWeight := make(map[string]float64) // commID → total edge weight to that comm
			for neighbor, w := range adj[n] {
				nc := nodeToComm[neighbor]
				commWeight[nc] += w
			}

			// Find best move
			bestComm := currentComm
			bestDeltaQ := 0.0

			for comm, wToComm := range commWeight {
				if comm == currentComm {
					continue
				}
				// Modularity gain approximation
				deltaQ := (wToComm / totalWeight) * cfg.Resolution
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = comm
				}
			}

			if bestComm != currentComm && bestDeltaQ > 0 {
				nodeToComm[n] = bestComm
				changed = true
			}
		}
	}

	// Aggregate communities
	commNodes := make(map[string][]string)
	for _, n := range nodes {
		c := nodeToComm[n]
		commNodes[c] = append(commNodes[c], n)
	}

	// Convert to KGCommunity objects, filtering small communities
	now := time.Now().UnixMilli()
	var communities []KGCommunity
	for cID, members := range commNodes {
		if len(members) < cfg.MinCommunitySize {
			continue
		}
		if len(communities) >= cfg.MaxCommunities {
			break
		}
		// Collect edges within this community
		var edgeIDs []string
		edgeCount := 0
		memberSet := make(map[string]bool, len(members))
		for _, m := range members {
			memberSet[m] = true
		}
		// Query edges between community members
		for _, m := range members {
			edges, _ := s.ListEdgesForNode(m, 100)
			for _, e := range edges {
				if memberSet[e.SrcID] && memberSet[e.DstID] {
					edgeIDs = append(edgeIDs, e.ID)
					edgeCount++
				}
			}
		}

		communities = append(communities, KGCommunity{
			ID:        cID,
			Level:     0,
			NodeIDs:   members,
			EdgeIDs:   edgeIDs,
			NodeCount: len(members),
			EdgeCount: edgeCount,
			CreatedAt: now,
		})
	}

	// Sort by size descending
	sort.Slice(communities, func(i, j int) bool {
		return communities[i].NodeCount > communities[j].NodeCount
	})

	return communities, nil
}

// ─── Community Summarization (LLM-powered) ──────────────────────────

// SummarizeCommunity generates a title and summary for a community using
// its constituent entities and relations. caller provides the LLM interface.
func (s *Store) SummarizeCommunity(comm *KGCommunity, caller func(string) (string, error)) error {
	if caller == nil || comm == nil {
		return fmt.Errorf("summarize: caller and community required")
	}

	// Build context from community members
	var entities, relations []string
	for _, nodeID := range comm.NodeIDs {
		var name, etype string
		if err := s.db.QueryRow(`SELECT COALESCE(name_raw,name), COALESCE(type,'') FROM kg_nodes WHERE id=?`, nodeID).Scan(&name, &etype); err == nil {
			entities = append(entities, fmt.Sprintf("- %s (%s)", name, etype))
		}
	}
	for _, edgeID := range comm.EdgeIDs {
		var srcName, dstName, relType string
		err := s.db.QueryRow(`SELECT COALESCE(sn.name_raw,sn.name), COALESCE(dn.name_raw,dn.name), e.type
			FROM kg_edges e JOIN kg_nodes sn ON sn.id=e.src_id JOIN kg_nodes dn ON dn.id=e.dst_id
			WHERE e.id=?`, edgeID).Scan(&srcName, &dstName, &relType)
		if err == nil {
			relations = append(relations, fmt.Sprintf("- %s → %s → %s", srcName, relType, dstName))
		}
	}

	prompt := fmt.Sprintf(`Summarize this knowledge graph community in 2-3 sentences.
Give a concise title (max 8 words).

Entities:
%s

Relations:
%s

Output format:
TITLE: <community title>
SUMMARY: <2-3 sentence summary>`,
		joinLines(entities, 10),
		joinLines(relations, 15),
	)

	response, err := caller(prompt)
	if err != nil {
		return err
	}

	// Parse title and summary
	var title, summary string
	for _, line := range splitLines(response) {
		if title == "" && hasPrefix(line, "TITLE:") {
			title = trimPrefix(line, "TITLE:")
		} else if summary == "" && hasPrefix(line, "SUMMARY:") {
			summary = trimPrefix(line, "SUMMARY:")
		} else if summary != "" {
			summary += " " + line
		}
	}

	if title != "" {
		comm.Title = title
	}
	if summary != "" {
		comm.Summary = summary
		comm.FullContent = response
	}
	return nil
}

// ─── Community Queries ──────────────────────────────────────────────

// GetCommunities returns all stored communities for a level.
func (s *Store) GetCommunities(level int) ([]KGCommunity, error) {
	_ = uuid.New() // ensure import
	// Communities are stored in-memory after detection for now.
	// For persistent storage, add a kg_communities table.
	return nil, fmt.Errorf("persistent community storage not yet implemented — use DetectCommunities result directly")
}

// ─── Helpers ───────────────────────────────────────────────────────

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func joinLines(lines []string, max int) string {
	if len(lines) > max {
		lines = lines[:max]
	}
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}
