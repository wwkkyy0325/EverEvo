package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GraphEdge is a relation between two entities, with both endpoints' names.
type GraphEdge struct {
	ID            string  `json:"id"`
	SrcID         string  `json:"srcId"`
	DstID         string  `json:"dstId"`
	Type          string  `json:"type"`
	SrcName       string  `json:"srcName"`
	DstName       string  `json:"dstName"`
	ValidFrom     int64   `json:"validFrom"`
	ValidTo       int64   `json:"validTo"` // 0 = currently valid
	RecordedAt    int64   `json:"recordedAt"`
	CrossTags     string  `json:"crossTags"`     // JSON array of library IDs (P7 cross-library)
	Weight        int     `json:"weight"`        // consolidated count of identical relations
	Polarity      string  `json:"polarity,omitempty"`   // "positive"|"negative"|"neutral"
	Intensity     float64 `json:"intensity,omitempty"`  // 0.0-1.0
	Level         string  `json:"level,omitempty"`      // "parent"|"child"|"peer"
	Confidence    float64 `json:"confidence,omitempty"` // 0.0-1.0
	SourceChunkID string  `json:"sourceChunkId,omitempty"`
	Evidence      string  `json:"evidence,omitempty"`
}

// GraphNode is an entity row for the UI viewer.
type GraphNode struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"` // display form (name_raw, falling back to name)
	CreatedAt int64  `json:"createdAt"`
}

// normalizeName is the disambiguation key: case-insensitive, trimmed. Good enough
// for single-user memory (LLM coreference resolution is a P3 follow-up).
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func placeholders(n int) string {
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

// predicateSynonyms maps variant predicates to a canonical form so semantically
// identical relations merge into one edge type — and bi-temporal close keys on
// the canonical form. Extend as patterns emerge from real usage.
var predicateSynonyms = map[string]string{
	"用":       "使用",
	"采用":     "使用",
	"用着":     "使用",
	"使用着":   "使用",
	"喜爱":     "喜欢",
	"偏好":     "喜欢",
	"爱用":     "喜欢",
	"工作于":   "就职于",
	"任职于":   "就职于",
	"在...工作": "就职于",
	"处于":     "位于",
}

// normalizePredicate canonicalizes a relation predicate (trim + synonym lookup).
func normalizePredicate(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if canon, ok := predicateSynonyms[p]; ok {
		return canon
	}
	return p
}

// UpsertNode returns the id of the entity with the given name, creating it if
// absent. Disambiguation is by normalized name (ToLower+TrimSpace), so "Go",
// "go ", "GO" all merge into one node. The embed callback vectorizes the name so
// the node can be found as a graph seed; if it fails the node is still stored
// (graph degrades to SQLite-only — no seed-by-similarity for that entity).
func (s *Store) UpsertNode(nodeType, name, workspaceID string, embed func(string) ([]float32, error)) (string, error) {
	norm := normalizeName(name)
	if norm == "" {
		return "", fmt.Errorf("empty entity name")
	}

	var id string
	err := s.db.QueryRow(`SELECT id FROM kg_nodes WHERE name = ?`, norm).Scan(&id)
	if err == nil {
		// Existing entity — refresh surface form / type only if they were blank.
		_, _ = s.db.Exec(`UPDATE kg_nodes
			SET name_raw = COALESCE(NULLIF(name_raw, ''), ?),
			    type = COALESCE(NULLIF(type, ''), ?)
			WHERE id = ?`, name, nodeType, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	id = "n_" + uuid.NewString()
	now := time.Now().UnixMilli()
	embeddingID := ""
	if embed != nil {
		if vec, eErr := embed(name); eErr == nil && len(vec) > 0 {
			ws := workspaceID
			if ws == "" {
				ws = "default"
			}
			if aErr := s.AddEntity(id, name, nodeType, ws, vec); aErr == nil {
				embeddingID = id
			}
		}
	}
	if workspaceID == "" {
		workspaceID = "default"
	}
	_, err = s.db.Exec(`INSERT INTO kg_nodes(id, type, name, name_raw, props, embedding_id, workspace_id, created_at)
		VALUES(?, ?, ?, ?, '{}', ?, ?, ?)`, id, nodeType, norm, name, embeddingID, workspaceID, now)
	if err != nil {
		return "", err
	}
	return id, nil
}

// AddEdge records a relation. When replaces is true, the new fact supersedes the
// currently-valid edge(s) of the same subject + relation type (e.g. "使用 Go" →
// "使用 Rust" closes "使用 Go", keeping it queryable as history). When false, the
// new edge coexists (e.g. "也喜欢 Python" alongside an existing like). The new
// edge is inserted with valid_from = recorded_at = now. crossTags is a JSON array
// of library IDs the edge belongs to (P7 cross-library).
func (s *Store) AddEdge(srcID, dstID, relType, props, sessionID, crossTags string, replaces bool) error {
	if props == "" {
		props = "{}"
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if replaces {
		if _, err := tx.Exec(`UPDATE kg_edges SET valid_to = ?
			WHERE src_id = ? AND type = ? AND valid_to IS NULL`, now, srcID, relType); err != nil {
			_ = tx.Rollback()
			return err
		}
	} else {
		// Dedup: bump weight on identical valid edge instead of inserting duplicate.
		var existingID string
		if err := tx.QueryRow(`SELECT id FROM kg_edges
			WHERE src_id=? AND dst_id=? AND type=? AND valid_to IS NULL
			LIMIT 1`, srcID, dstID, relType).Scan(&existingID); err == nil && existingID != "" {
			if _, err := tx.Exec(`UPDATE kg_edges SET weight = weight + 1, recorded_at = ?
				WHERE id = ?`, now, existingID); err != nil {
				_ = tx.Rollback()
				return err
			}
			return tx.Commit()
		}
	}
	if crossTags == "" {
		crossTags = "[]"
	}
	// Auto-detect cross-domain: if src and dst are in different workspaces, tag it.
	var srcWS, dstWS string
	if row := s.db.QueryRow(`SELECT workspace_id FROM kg_nodes WHERE id=?`, srcID); row.Scan(&srcWS) == nil {
		if row2 := s.db.QueryRow(`SELECT workspace_id FROM kg_nodes WHERE id=?`, dstID); row2.Scan(&dstWS) == nil {
			if srcWS != "" && dstWS != "" && srcWS != dstWS {
				var tags []string
				json.Unmarshal([]byte(crossTags), &tags)
				tags = append(tags, "cross-domain")
				if b, err := json.Marshal(tags); err == nil {
					crossTags = string(b)
				}
			}
		}
	}
	id := "e_" + uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO kg_edges(id, src_id, dst_id, type, props, valid_from, recorded_at, session_id, cross_tags, weight)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`, id, srcID, dstID, relType, props, now, now, sessionID, crossTags); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// QueryGraph expands from the seed nodes up to `hops` away along currently-valid
// edges (bidirectional), returning the edges among the reachable sub-graph. This
// is the graph half of LightRAG-style hybrid retrieval.
func (s *Store) QueryGraph(seedIDs []string, hops int, libraryID string) ([]GraphEdge, error) {
	if len(seedIDs) == 0 || hops <= 0 {
		return nil, nil
	}
	// libraryID scopes the traversal: only edges whose reached endpoint belongs
	// to the given library (or legacy 'default') are followed, preventing
	// cross-domain leakage. Empty → global.
	scopeClause := ""
	var scopeArgs []any
	if libraryID != "" {
		pool := s.defaultWorkspacePool()
		scopeClause = ` AND (n.workspace_id = ? OR n.workspace_id IN (` + placeholders(len(pool)) + `))`
		scopeArgs = append([]any{libraryID}, strsToAnys(pool)...)
	}
	q := `
WITH RECURSIVE reach(id, depth) AS (
	SELECT id, 0 FROM kg_nodes WHERE id IN (` + placeholders(len(seedIDs)) + `)
	UNION
	SELECT e.dst_id, r.depth + 1 FROM reach r
		JOIN kg_edges e ON e.src_id = r.id
		JOIN kg_nodes n ON n.id = e.dst_id
		WHERE e.valid_to IS NULL AND r.depth < ?` + scopeClause + `
	UNION
	SELECT e.src_id, r.depth + 1 FROM reach r
		JOIN kg_edges e ON e.dst_id = r.id
		JOIN kg_nodes n ON n.id = e.src_id
		WHERE e.valid_to IS NULL AND r.depth < ?` + scopeClause + `
)
SELECT e.id, e.src_id, e.dst_id, e.type, e.valid_from, COALESCE(e.valid_to, 0), e.recorded_at, e.cross_tags, e.weight,
       sn.name_raw, dn.name_raw
FROM kg_edges e
JOIN (SELECT DISTINCT id FROM reach) rs ON rs.id = e.src_id
JOIN (SELECT DISTINCT id FROM reach) rd ON rd.id = e.dst_id
JOIN kg_nodes sn ON sn.id = e.src_id
JOIN kg_nodes dn ON dn.id = e.dst_id
WHERE e.valid_to IS NULL`
	args := make([]any, 0, len(seedIDs)+2+len(scopeArgs)*2)
	for _, id := range seedIDs {
		args = append(args, id)
	}
	args = append(args, hops)
	args = append(args, scopeArgs...)
	args = append(args, hops)
	args = append(args, scopeArgs...)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphEdge
	for rows.Next() {
		var ge GraphEdge
		if err := rows.Scan(&ge.ID, &ge.SrcID, &ge.DstID, &ge.Type, &ge.ValidFrom, &ge.ValidTo, &ge.RecordedAt, &ge.CrossTags, &ge.Weight, &ge.SrcName, &ge.DstName); err != nil {
			return nil, err
		}
		out = append(out, ge)
	}
	return out, rows.Err()
}

// ListNodes returns all graph nodes (newest first), for the UI viewer.
func (s *Store) ListNodes() ([]GraphNode, error) { return s.ListNodesByLibrary("") }

// ListNodesByLibrary returns KG nodes filtered by workspace_id. Empty libraryID = all.
func (s *Store) ListNodesByLibrary(libraryID string) ([]GraphNode, error) {
	var rows *sql.Rows
	var err error
	if libraryID == "" {
		rows, err = s.db.Query(`SELECT id, type, COALESCE(name_raw, name), created_at FROM kg_nodes ORDER BY created_at DESC`)
	} else {
		defID, _ := s.DefaultLibrary()
		rows, err = s.db.Query(`SELECT id, type, COALESCE(name_raw, name), created_at FROM kg_nodes
			WHERE (workspace_id=? OR workspace_id=? OR workspace_id='default' OR workspace_id='')
			ORDER BY created_at DESC`, libraryID, defID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GraphNode, 0) // empty slice, not nil — so JSON is [] not null
	for rows.Next() {
		var n GraphNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Name = displayNodeName(n.ID, n.Type, n.Name)
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListAllEdges returns currently-valid edges with endpoint names (newest recorded first).
func (s *Store) ListAllEdges() ([]GraphEdge, error) { return s.ListAllEdgesByLibrary("") }

// ListAllEdgesByLibrary returns currently-valid edges, optionally filtered by library.
func (s *Store) ListAllEdgesByLibrary(libraryID string) ([]GraphEdge, error) {
	var rows *sql.Rows
	var err error
	query := `SELECT e.id, e.src_id, e.dst_id, e.type, e.valid_from, COALESCE(e.valid_to, 0), e.recorded_at, e.cross_tags, e.weight,
		COALESCE(sn.name_raw, sn.name), COALESCE(dn.name_raw, dn.name)
		FROM kg_edges e
		JOIN kg_nodes sn ON sn.id = e.src_id
		JOIN kg_nodes dn ON dn.id = e.dst_id
		WHERE e.valid_to IS NULL`
	if libraryID != "" {
		defID, _ := s.DefaultLibrary()
		query += ` AND (sn.workspace_id IN (?,?,'default','') OR dn.workspace_id IN (?,?,'default','') OR e.cross_tags LIKE ?)`
		rows, err = s.db.Query(query+" ORDER BY e.recorded_at DESC", libraryID, defID, libraryID, defID, "%"+libraryID+"%")
	} else {
		rows, err = s.db.Query(query + " ORDER BY e.recorded_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphEdge
	for rows.Next() {
		var ge GraphEdge
		if err := rows.Scan(&ge.ID, &ge.SrcID, &ge.DstID, &ge.Type, &ge.ValidFrom, &ge.ValidTo, &ge.RecordedAt, &ge.CrossTags, &ge.Weight, &ge.SrcName, &ge.DstName); err != nil {
			return nil, err
		}
		ge.SrcName = displayNodeName(ge.SrcID, "", ge.SrcName)
		ge.DstName = displayNodeName(ge.DstID, "", ge.DstName)
		out = append(out, ge)
	}
	return out, rows.Err()
}

// ListAllEdgesIncludeHistory is like ListAllEdges but also returns closed edges
// (valid_to set), so the UI can show how beliefs changed over time.
func (s *Store) ListAllEdgesIncludeHistory() ([]GraphEdge, error) { return s.ListAllEdgesIncludeHistoryByLibrary("") }

func (s *Store) ListAllEdgesIncludeHistoryByLibrary(libraryID string) ([]GraphEdge, error) {
	query := `SELECT e.id, e.src_id, e.dst_id, e.type, e.valid_from, COALESCE(e.valid_to, 0), e.recorded_at, e.cross_tags, e.weight,
		COALESCE(sn.name_raw, sn.name), COALESCE(dn.name_raw, dn.name)
		FROM kg_edges e
		JOIN kg_nodes sn ON sn.id = e.src_id
		JOIN kg_nodes dn ON dn.id = e.dst_id`
	var rows *sql.Rows
	var err error
	if libraryID != "" {
		defID, _ := s.DefaultLibrary()
		query += ` WHERE (sn.workspace_id IN (?,?,'default','') OR dn.workspace_id IN (?,?,'default','') OR e.cross_tags LIKE ?)`
		rows, err = s.db.Query(query+" ORDER BY e.recorded_at DESC", libraryID, defID, libraryID, defID, "%"+libraryID+"%")
	} else {
		rows, err = s.db.Query(query + " ORDER BY e.recorded_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphEdge
	for rows.Next() {
		var ge GraphEdge
		if err := rows.Scan(&ge.ID, &ge.SrcID, &ge.DstID, &ge.Type, &ge.ValidFrom, &ge.ValidTo, &ge.RecordedAt, &ge.CrossTags, &ge.Weight, &ge.SrcName, &ge.DstName); err != nil {
			return nil, err
		}
		ge.SrcName = displayNodeName(ge.SrcID, "", ge.SrcName)
		ge.DstName = displayNodeName(ge.DstID, "", ge.DstName)
		out = append(out, ge)
	}
	return out, rows.Err()
}

// DeleteNode removes a node, cascades its edges, and deletes the entity's
// chromem doc so EntitySearch no longer returns a dead seed.
func (s *Store) DeleteNode(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_edges WHERE src_id = ? OR dst_id = ?`, id, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_nodes WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.vector != nil {
		_ = s.vector.Delete(id) // best-effort orphan cleanup
	}
	return nil
}

// DeleteNodesByLibrary removes all graph nodes and their edges in the
// given workspace/library. Used before GraphRebuildFromDomain to start fresh.
func (s *Store) DeleteNodesByLibrary(libraryID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Delete edges first, then nodes.
	if _, err := tx.Exec(`DELETE FROM kg_edges WHERE src_id IN (SELECT id FROM kg_nodes WHERE workspace_id = ?) OR dst_id IN (SELECT id FROM kg_nodes WHERE workspace_id = ?)`, libraryID, libraryID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_nodes WHERE workspace_id = ?`, libraryID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DeleteEdge removes a single edge.
func (s *Store) DeleteEdge(id string) error {
	_, err := s.db.Exec(`DELETE FROM kg_edges WHERE id = ?`, id)
	return err
}

// NodeCount returns the total entity count (for status/UI).
func (s *Store) NodeCount() int { return s.NodeCountByLibrary("") }
func (s *Store) NodeCountByLibrary(libraryID string) int {
	var n int
	if libraryID == "" {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM kg_nodes`).Scan(&n)
	} else {
		defID, _ := s.DefaultLibrary()
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM kg_nodes WHERE (workspace_id=? OR workspace_id=? OR workspace_id='default' OR workspace_id='')`, libraryID, defID).Scan(&n)
	}
	return n
}

// RenameNode changes an entity's display name and its normalized disambiguation
// key. The chromem entity doc is left as-is (orphan vector; harmless — recall
// joins SQLite by id).
func (s *Store) RenameNode(id, name string) error {
	norm := normalizeName(name)
	if norm == "" {
		return fmt.Errorf("empty name")
	}
	_, err := s.db.Exec(`UPDATE kg_nodes SET name = ?, name_raw = ? WHERE id = ?`, norm, name, id)
	return err
}

// MergeNodes folds dropID into keepID: re-points its edges to keepID, removes the
// self-loops and duplicate (src,dst,type) edges the merge introduces, then deletes
// the dropID node.
func (s *Store) MergeNodes(keepID, dropID string) error {
	if keepID == dropID {
		return fmt.Errorf("cannot merge a node into itself")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE kg_edges SET src_id = ? WHERE src_id = ?`, keepID, dropID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE kg_edges SET dst_id = ? WHERE dst_id = ?`, keepID, dropID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_edges WHERE src_id = dst_id`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_edges WHERE id NOT IN (SELECT MIN(id) FROM kg_edges GROUP BY src_id, dst_id, type)`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM kg_nodes WHERE id = ?`, dropID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// GraphStats holds aggregate stats for the UI header.
type GraphStats struct {
	EdgesPerType map[string]int `json:"edgesPerType"`
	TopHubs     []GraphHub      `json:"topHubs"`
}

// GraphHub is a top-degree entity.
type GraphHub struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Degree int    `json:"degree"`
}

// Stats returns edge counts per type (top 10) and top-degree hub nodes (current edges only).
func (s *Store) Stats() (*GraphStats, error) {
	out := &GraphStats{EdgesPerType: map[string]int{}}
	rows, err := s.db.Query(`SELECT type, COUNT(*) FROM kg_edges WHERE valid_to IS NULL GROUP BY type ORDER BY COUNT(*) DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return nil, err
		}
		out.EdgesPerType[t] = c
	}
	rows.Close()
	hrows, err := s.db.Query(`SELECT n.id, COALESCE(n.name_raw, n.name), COUNT(e.id) AS deg
		FROM kg_nodes n
		LEFT JOIN kg_edges e ON (e.src_id = n.id OR e.dst_id = n.id) AND e.valid_to IS NULL
		GROUP BY n.id ORDER BY deg DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	for hrows.Next() {
		var h GraphHub
		if err := hrows.Scan(&h.ID, &h.Name, &h.Degree); err != nil {
			hrows.Close()
			return nil, err
		}
		out.TopHubs = append(out.TopHubs, h)
	}
	return out, hrows.Close()
}

// CurrentEdgeCount returns the number of currently-valid edges.
func (s *Store) CurrentEdgeCount() int { return s.CurrentEdgeCountByLibrary("") }
func (s *Store) CurrentEdgeCountByLibrary(libraryID string) int {
	var n int
	if libraryID == "" {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM kg_edges WHERE valid_to IS NULL`).Scan(&n)
	} else {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM kg_edges WHERE valid_to IS NULL AND (workspace_id=? OR cross_tags LIKE ?)`, libraryID, "%"+libraryID+"%").Scan(&n)
	}
	return n
}

// dedupEdges consolidates duplicate valid edges (same src+dst+type) into a
// single row by summing weights. One-shot migration; safe to call after the
// weight column has been added.
func (s *Store) dedupEdges() error {
	rows, err := s.db.Query(`SELECT src_id, dst_id, type, COUNT(*) AS cnt, SUM(weight) AS total
		FROM kg_edges WHERE valid_to IS NULL
		GROUP BY src_id, dst_id, type
		HAVING COUNT(*) > 1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type dup struct{ src, dst, typ string; cnt, total int }
	var dups []dup
	for rows.Next() {
		var d dup
		if err := rows.Scan(&d.src, &d.dst, &d.typ, &d.cnt, &d.total); err != nil {
			return err
		}
		dups = append(dups, d)
	}
	for _, d := range dups {
		// Keep one row and set its weight to the total.
		var keeperID string
		if err := s.db.QueryRow(`SELECT MIN(id) FROM kg_edges
			WHERE src_id=? AND dst_id=? AND type=? AND valid_to IS NULL`,
			d.src, d.dst, d.typ).Scan(&keeperID); err != nil {
			continue
		}
		_, _ = s.db.Exec(`UPDATE kg_edges SET weight = ? WHERE id = ?`, d.total, keeperID)
		_, _ = s.db.Exec(`DELETE FROM kg_edges
			WHERE src_id=? AND dst_id=? AND type=? AND valid_to IS NULL AND id != ?`,
			d.src, d.dst, d.typ, keeperID)
	}
	return nil
}

// SearchNodesByKeyword returns nodes whose name contains the query (LIKE search).
func (s *Store) SearchNodesByKeyword(query string, limit int) ([]GraphNode, error) {
	if limit <= 0 { limit = 10 }
	rows, err := s.db.Query(`SELECT id, type, name, created_at FROM kg_nodes
		WHERE name LIKE ? OR name_raw LIKE ? ORDER BY name LIMIT ?`,
		"%"+query+"%", "%"+query+"%", limit)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []GraphNode
	for rows.Next() {
		var n GraphNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.CreatedAt); err != nil { return nil, err }
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListEdgesForNode returns currently-valid edges connected to a node (both directions).
func (s *Store) ListEdgesForNode(nodeID string, limit int) ([]GraphEdge, error) {
	if limit <= 0 { limit = 10 }
	rows, err := s.db.Query(`SELECT e.id, e.src_id, e.dst_id, e.type, e.valid_from, COALESCE(e.valid_to,0), e.recorded_at, e.cross_tags, e.weight,
		COALESCE(sn.name_raw, sn.name), COALESCE(dn.name_raw, dn.name)
		FROM kg_edges e
		JOIN kg_nodes sn ON sn.id = e.src_id
		JOIN kg_nodes dn ON dn.id = e.dst_id
		WHERE (e.src_id=? OR e.dst_id=?) AND e.valid_to IS NULL
		ORDER BY e.weight DESC LIMIT ?`, nodeID, nodeID, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []GraphEdge
	for rows.Next() {
		var ge GraphEdge
		if err := rows.Scan(&ge.ID, &ge.SrcID, &ge.DstID, &ge.Type, &ge.ValidFrom, &ge.ValidTo, &ge.RecordedAt, &ge.CrossTags, &ge.Weight, &ge.SrcName, &ge.DstName); err != nil { return nil, err }
		out = append(out, ge)
	}
	return out, rows.Err()
}

// displayNodeName returns a human-readable label. If the stored name looks like
// a UUID or auto-generated ID (e.g., from a KB document chunk with no metadata),
// it generates a compact display label from the type + short ID suffix.
func displayNodeName(id, etype, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return shortLabel(id, etype)
	}
	// UUID-like: 36 chars, 4 hyphens, hex segments (e.g. "a1b2c3d4-...")
	if looksLikeUUID(name) {
		return shortLabel(id, etype)
	}
	return name
}

func looksLikeUUID(s string) bool {
	return len(s) >= 32 && strings.Count(s, "-") >= 4
}

func shortLabel(id, etype string) string {
	tag := etype
	if tag == "" {
		tag = "entity"
	}
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	return tag + "-" + short
}

// FindNodeByName returns the full node row (id, workspace_id) for a normalized
// name, or ("", "") if not found. Used for domain inference when creating edges.
func (s *Store) FindNodeByName(name string) (id, workspaceID string) {
	norm := normalizeName(name)
	if norm == "" {
		return "", ""
	}
	_ = s.db.QueryRow(`SELECT id, workspace_id FROM kg_nodes WHERE name = ? LIMIT 1`, norm).Scan(&id, &workspaceID)
	return
}

// defaultWorkspacePool returns the set of workspace_id values that are considered
// "unassigned": the literal 'default', empty string, and the actual default library
// UUID. All queries that scope or propagate domain assignment MUST use this pool
// instead of hardcoding 'default'.
func (s *Store) defaultWorkspacePool() []string {
	defID, _ := s.DefaultLibrary()
	pool := []string{"default", ""}
	if defID != "" && defID != "default" {
		pool = append(pool, defID)
	}
	return pool
}

// IsDefaultWS returns true if the given workspace_id is in the default pool.
func (s *Store) IsDefaultWS(ws string) bool {
	if ws == "" || ws == "default" {
		return true
	}
	defID, _ := s.DefaultLibrary()
	return ws == defID
}

// SetNodeWorkspace updates the workspace_id on an existing node. Best-effort —
// silently no-ops if the node doesn't exist or is already in a different domain.
func (s *Store) SetNodeWorkspace(nodeID, workspaceID string) {
	defID, _ := s.DefaultLibrary()
	// Match both the literal 'default' and the actual default library UUID.
	_, _ = s.db.Exec(`UPDATE kg_nodes SET workspace_id = ?
		WHERE id = ? AND (workspace_id = 'default' OR workspace_id = ? OR workspace_id = '')`,
		workspaceID, nodeID, defID)
}

// RenameEdge updates the relation type (predicate) of an edge.
func (s *Store) RenameEdge(id, newType string) error {
	_, err := s.db.Exec(`UPDATE kg_edges SET type = ? WHERE id = ?`, newType, id)
	return err
}

// PropagateGraphWorkspace starts from seed nodes (name→libraryID) and propagates
// domain assignment along edges: if node A belongs to domain X and A→B has an edge,
// and B is still in the default pool, then B is reassigned to X. Runs multiple
// rounds until no more nodes are reassigned (fixed-point iteration).
//
// Returns the number of nodes reassigned.
func (s *Store) PropagateGraphWorkspace(seedHints map[string]string) (int, error) {
	if len(seedHints) == 0 {
		return 0, nil
	}

	// Compute the default-pool set: 'default' string + actual default library UUID.
	defID, _ := s.DefaultLibrary()
	defPool := []string{"default", ""}
	if defID != "" && defID != "default" {
		defPool = append(defPool, defID)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// ── Round 0: apply seed hints to matching nodes ──
	total := 0
	for normName, libID := range seedHints {
		res, err := tx.Exec(`UPDATE kg_nodes SET workspace_id = ?
			WHERE (name = ? OR name_raw = ?) AND workspace_id IN (`+
			placeholders(len(defPool))+`)`,
			append([]any{libID, normName, normName}, strsToAnys(defPool)...)...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}

	// ── Rounds 1..N: propagate along edges until convergence ──
	defPlaceholders := placeholders(len(defPool))
	for round := 1; round <= 5; round++ {
		// src has domain, dst is in default pool → propagate src domain to dst.
		query1 := `UPDATE kg_nodes SET workspace_id = (
			SELECT DISTINCT sn.workspace_id FROM kg_edges e
			JOIN kg_nodes sn ON sn.id = e.src_id
			WHERE e.dst_id = kg_nodes.id
			  AND sn.workspace_id NOT IN (` + defPlaceholders + `)
			LIMIT 1
		) WHERE kg_nodes.id IN (
			SELECT e.dst_id FROM kg_edges e
			JOIN kg_nodes sn ON sn.id = e.src_id
			WHERE sn.workspace_id NOT IN (` + defPlaceholders + `)
			  AND EXISTS (SELECT 1 FROM kg_nodes dn WHERE dn.id = e.dst_id AND dn.workspace_id IN (` + defPlaceholders + `))
		)`
		args1 := append([]any{}, strsToAnys(defPool)...)
		args1 = append(args1, strsToAnys(defPool)...)
		args1 = append(args1, strsToAnys(defPool)...)
		res, err := tx.Exec(query1, args1...)
		if err != nil {
			return 0, err
		}
		n1, _ := res.RowsAffected()

		// Same in reverse: dst has domain, propagate to src.
		query2 := `UPDATE kg_nodes SET workspace_id = (
			SELECT DISTINCT dn.workspace_id FROM kg_edges e
			JOIN kg_nodes dn ON dn.id = e.dst_id
			WHERE e.src_id = kg_nodes.id
			  AND dn.workspace_id NOT IN (` + defPlaceholders + `)
			LIMIT 1
		) WHERE kg_nodes.id IN (
			SELECT e.src_id FROM kg_edges e
			JOIN kg_nodes dn ON dn.id = e.dst_id
			WHERE dn.workspace_id NOT IN (` + defPlaceholders + `)
			  AND EXISTS (SELECT 1 FROM kg_nodes sn WHERE sn.id = e.src_id AND sn.workspace_id IN (` + defPlaceholders + `))
		)`
		args2 := append([]any{}, strsToAnys(defPool)...)
		args2 = append(args2, strsToAnys(defPool)...)
		args2 = append(args2, strsToAnys(defPool)...)
		res2, err := tx.Exec(query2, args2...)
		if err != nil {
			return 0, err
		}
		n2, _ := res2.RowsAffected()

		roundN := int(n1) + int(n2)
		total += roundN
		if roundN == 0 {
			break // converged
		}
	}

	err = tx.Commit()
	tx = nil
	return total, err
}

// strsToAnys converts []string to []any for SQL argument lists.
func strsToAnys(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// MigrateGraphNodes reassigns workspace_id on graph nodes whose name (or
// normalized name) appears in the hint set for a given library. Only reassigns
// nodes currently on 'default' (never steals from another explicit domain).
// Deprecated: prefer PropagateGraphWorkspace which also propagates along edges.
func (s *Store) MigrateGraphNodes(nameHints map[string]string) (int, error) {
	if len(nameHints) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	total := 0
	for normName, libID := range nameHints {
		res, err := tx.Exec(`UPDATE kg_nodes SET workspace_id = ?
			WHERE (name = ? OR name_raw = ?) AND (workspace_id = 'default')`,
			libID, normName, normName)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, tx.Commit()
}

// NodeWorkspace returns the workspace_id of a single graph node, or "" if not found.
func (s *Store) NodeWorkspace(nodeID string) string {
	var ws string
	if err := s.db.QueryRow(`SELECT workspace_id FROM kg_nodes WHERE id = ?`, nodeID).Scan(&ws); err != nil {
		return ""
	}
	return ws
}

// ─── Entity Timeline Queries ──────────────────────────────────────────

// EntitySnapshot holds the complete state of an entity at a point in time.
type EntitySnapshot struct {
	EntityID   string            `json:"entityId"`
	EntityName string            `json:"entityName"`
	Properties []EntityProperty   `json:"properties"`
	Edges      []GraphEdge       `json:"edges"`
	Events     []KGEvent          `json:"events,omitempty"`
	Spatial    []SpatialRecord    `json:"spatial,omitempty"`
}

// QueryEntityTimeline returns the full snapshot of an entity within a time range,
// including its properties, relations, and events at that time.
func (s *Store) QueryEntityTimeline(entityID string, from, to int64) (*EntitySnapshot, error) {
	if entityID == "" {
		return nil, fmt.Errorf("entity_id required")
	}
	es := &EntitySnapshot{EntityID: entityID}
	_ = s.db.QueryRow(`SELECT COALESCE(name_raw, name) FROM kg_nodes WHERE id = ?`, entityID).Scan(&es.EntityName)

	// Properties valid in [from, to] range
	props, err := s.GetEntityProperties(entityID)
	if err == nil {
		for _, p := range props {
			if from > 0 && p.ValidTo > 0 && p.ValidTo < from {
				continue // property ended before range start
			}
			if to > 0 && p.ValidFrom > 0 && p.ValidFrom > to {
				continue // property started after range end
			}
			es.Properties = append(es.Properties, p)
		}
	}

	// Currently-valid edges
	edges, err := s.ListEdgesForNode(entityID, 20)
	if err == nil {
		es.Edges = edges
	}

	// Events the entity participated in
	events, err := s.GetEventsForEntity(entityID, 10)
	if err == nil {
		for _, ev := range events {
			if from > 0 && ev.TimeEnd > 0 && ev.TimeEnd < from {
				continue
			}
			if to > 0 && ev.TimeStart > 0 && ev.TimeStart > to {
				continue
			}
			es.Events = append(es.Events, ev)
		}
	}

	// Spatial records
	spatial, err := s.GetSpatialForEntity(entityID)
	if err == nil {
		es.Spatial = spatial
	}

	return es, nil
}

// ─── Causal Chain Queries ────────────────────────────────────────────

// CausalLink is a step in a causal chain.
type CausalLink struct {
	FromEvent KGEvent  `json:"fromEvent"`
	ToEvent   KGEvent  `json:"toEvent"`
	Edge      GraphEdge `json:"edge"`
}

// QueryCausalChain follows "causes" / "precedes" edges from a starting event
// for up to `depth` steps. direction: "forward" (causes→) | "backward" (←caused_by)
// | "both".
func (s *Store) QueryCausalChain(eventID string, direction string, depth int) ([]CausalLink, error) {
	if depth <= 0 {
		depth = 3
	}
	if depth > 10 {
		depth = 10 // safety cap
	}

	var links []CausalLink
	visited := map[string]bool{eventID: true}

	causalQuery := `SELECT e.id, e.src_id, e.dst_id, e.type,
		COALESCE(e.polarity,''), COALESCE(e.intensity,0.5), COALESCE(e.confidence,1.0),
		COALESCE(e.evidence,'')
		FROM kg_edges e WHERE e.valid_to IS NULL AND e.type IN ('causes','precedes','leads_to')`

	var expand func(currentID string, dir string, remaining int) error
	expand = func(currentID string, dir string, remaining int) error {
		if remaining <= 0 {
			return nil
		}
		if dir == "forward" || dir == "both" {
			r, err := s.db.Query(causalQuery+" AND e.src_id = ?", currentID)
			if err == nil {
				defer r.Close()
				for r.Next() {
					var id, srcID, dstID, typ, pol, evid string
					var intens, conf float64
					if err := r.Scan(&id, &srcID, &dstID, &typ, &pol, &intens, &conf, &evid); err != nil {
						continue
					}
					if visited[dstID] {
						continue
					}
					visited[dstID] = true
					fromEv, _ := s.GetEvent(currentID)
					toEv, _ := s.GetEvent(dstID)
					if fromEv != nil && toEv != nil {
						links = append(links, CausalLink{
							FromEvent: *fromEv,
							ToEvent:   *toEv,
							Edge:      GraphEdge{ID: id, SrcID: srcID, DstID: dstID, Type: typ, Polarity: pol, Intensity: intens, Confidence: conf, Evidence: evid},
						})
					}
					expand(dstID, dir, remaining-1)
				}
			}
		}
		if dir == "backward" || dir == "both" {
			r, err := s.db.Query(causalQuery+" AND e.dst_id = ?", currentID)
			if err == nil {
				defer r.Close()
				for r.Next() {
					var id, srcID, dstID, typ, pol, evid string
					var intens, conf float64
					if err := r.Scan(&id, &srcID, &dstID, &typ, &pol, &intens, &conf, &evid); err != nil {
						continue
					}
					if visited[srcID] {
						continue
					}
					visited[srcID] = true
					fromEv, _ := s.GetEvent(srcID)
					toEv, _ := s.GetEvent(currentID)
					if fromEv != nil && toEv != nil {
						links = append(links, CausalLink{
							FromEvent: *fromEv,
							ToEvent:   *toEv,
							Edge:      GraphEdge{ID: id, SrcID: srcID, DstID: dstID, Type: typ, Polarity: pol, Intensity: intens, Confidence: conf, Evidence: evid},
						})
					}
					expand(srcID, dir, remaining-1)
				}
			}
		}
		return nil
	}

	if err := expand(eventID, direction, depth); err != nil {
		return links, err
	}
	return links, nil
}

// ─── Spatial Context Query ──────────────────────────────────────────

// SpatialResult is an entity or event with a spatial record matching a query.
type SpatialResult struct {
	SpatialRecord
	EntityName  string `json:"entityName,omitempty"`
	EntityType  string `json:"entityType,omitempty"`
	EventTitle  string `json:"eventTitle,omitempty"`
}

// QuerySpatialContext returns entities and events matching a spatial query
// (region name LIKE search).
func (s *Store) QuerySpatialContext(region string, limit int) ([]SpatialResult, error) {
	if region == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT sp.id, COALESCE(sp.entity_id,''), COALESCE(sp.event_id,''),
		sp.spatial_type, COALESCE(sp.coordinates,''), COALESCE(sp.address,''),
		COALESCE(sp.region,''), COALESCE(sp.named_location,''),
		COALESCE(sp.valid_from,0), COALESCE(sp.valid_to,0),
		sp.confidence, COALESCE(sp.source_chunk_id,''), sp.recorded_at
		FROM kg_spatial sp
		WHERE sp.region LIKE ? OR sp.named_location LIKE ? OR sp.address LIKE ?
		ORDER BY sp.confidence DESC LIMIT ?`,
		"%"+region+"%", "%"+region+"%", "%"+region+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpatialResult
	for rows.Next() {
		var sr SpatialResult
		if err := rows.Scan(&sr.ID, &sr.EntityID, &sr.EventID,
			&sr.SpatialType, &sr.Coordinates, &sr.Address,
			&sr.Region, &sr.NamedLocation,
			&sr.ValidFrom, &sr.ValidTo,
			&sr.Confidence, &sr.SourceChunkID, &sr.RecordedAt); err != nil {
			return nil, err
		}
		// Enrich with entity/event names
		if sr.EntityID != "" {
			_ = s.db.QueryRow(`SELECT COALESCE(name_raw, name), COALESCE(type, '') FROM kg_nodes WHERE id = ?`, sr.EntityID).Scan(&sr.EntityName, &sr.EntityType)
		}
		if sr.EventID != "" {
			_ = s.db.QueryRow(`SELECT title FROM kg_events WHERE id = ?`, sr.EventID).Scan(&sr.EventTitle)
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// ListNodeWorkspaces returns the distinct (workspace_id, count) pairs for graph
// nodes, for UI domain assignment inspection.
func (s *Store) ListNodeWorkspaces() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT workspace_id, COUNT(*) FROM kg_nodes GROUP BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var ws string
		var c int
		if err := rows.Scan(&ws, &c); err != nil {
			return nil, err
		}
		out[ws] = c
	}
	return out, rows.Err()
}
