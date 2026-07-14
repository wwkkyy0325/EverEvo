package memory

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ConceptNode is a row in kg_concept_tree — a term/concept in a domain taxonomy.
type ConceptNode struct {
	ID            string   `json:"id"`
	Concept       string   `json:"concept"`
	ParentID      string   `json:"parentId,omitempty"`
	TreeType      string   `json:"treeType"` // "IS_A"|"PART_OF"|"broader/narrower"
	Level         int      `json:"level"`
	Definition    string   `json:"definition,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	Synonyms      []string `json:"synonyms,omitempty"` // SKOS altLabels
	SourceChunkID string   `json:"sourceChunkId,omitempty"`
	CreatedAt     int64    `json:"createdAt"`
}

// UpsertConcept inserts or replaces a concept node in the taxonomy tree.
func (s *Store) UpsertConcept(c ConceptNode) (string, error) {
	if c.Concept == "" {
		return "", Err("concept name is required")
	}
	if c.ID == "" {
		c.ID = "cpt_" + uuid.NewString()
	}
	if c.TreeType == "" {
		c.TreeType = "IS_A"
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().UnixMilli()
	}

	// Compute level from parent if not set.
	if c.Level == 0 && c.ParentID != "" {
		var parentLevel int
		if err := s.db.QueryRow(`SELECT level FROM kg_concept_tree WHERE id = ?`, c.ParentID).Scan(&parentLevel); err == nil {
			c.Level = parentLevel + 1
		}
	}

	synJSON, _ := json.Marshal(c.Synonyms)
	if len(c.Synonyms) == 0 {
		synJSON = []byte("[]")
	}

	_, err := s.db.Exec(`INSERT OR REPLACE INTO kg_concept_tree
		(id, concept, parent_id, tree_type, level, definition, domain, synonyms, source_chunk_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Concept, nullIfStr(c.ParentID), c.TreeType, c.Level,
		nullIfStr(c.Definition), nullIfStr(c.Domain), string(synJSON),
		nullIfStr(c.SourceChunkID), c.CreatedAt)
	return c.ID, err
}

// GetConceptHierarchy returns the full ancestor path for a concept (root → concept).
func (s *Store) GetConceptHierarchy(conceptID string) ([]ConceptNode, error) {
	var nodes []ConceptNode
	cur := conceptID
	const maxDepth = 20 // safety limit
	for i := 0; i < maxDepth; i++ {
		var c ConceptNode
		var synStr string
		var parentID string
		err := s.db.QueryRow(`SELECT id, concept, COALESCE(parent_id,''), tree_type, level,
			COALESCE(definition,''), COALESCE(domain,''), COALESCE(synonyms,'[]'),
			COALESCE(source_chunk_id,''), created_at
			FROM kg_concept_tree WHERE id = ?`, cur).Scan(
			&c.ID, &c.Concept, &parentID, &c.TreeType, &c.Level,
			&c.Definition, &c.Domain, &synStr, &c.SourceChunkID, &c.CreatedAt)
		if err != nil {
			break
		}
		c.ParentID = parentID
		json.Unmarshal([]byte(synStr), &c.Synonyms)
		nodes = append([]ConceptNode{c}, nodes...) // prepend for root-first order
		cur = parentID
		if cur == "" {
			break
		}
	}
	return nodes, nil
}

// GetConceptChildren returns direct children of a concept.
func (s *Store) GetConceptChildren(parentID string) ([]ConceptNode, error) {
	rows, err := s.db.Query(`SELECT id, concept, COALESCE(parent_id,''), tree_type, level,
		COALESCE(definition,''), COALESCE(domain,''), COALESCE(synonyms,'[]'),
		COALESCE(source_chunk_id,''), created_at
		FROM kg_concept_tree WHERE parent_id = ? ORDER BY level, concept`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConcepts(rows)
}

// SearchConcepts searches concepts by name (LIKE) or synonym match.
func (s *Store) SearchConcepts(query string, limit int) ([]ConceptNode, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, concept, COALESCE(parent_id,''), tree_type, level,
		COALESCE(definition,''), COALESCE(domain,''), COALESCE(synonyms,'[]'),
		COALESCE(source_chunk_id,''), created_at
		FROM kg_concept_tree
		WHERE concept LIKE ? OR synonyms LIKE ?
		ORDER BY level LIMIT ?`,
		"%"+query+"%", "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConcepts(rows)
}

// GetConceptsByDomain returns all concepts in a domain taxonomy.
func (s *Store) GetConceptsByDomain(domain string) ([]ConceptNode, error) {
	rows, err := s.db.Query(`SELECT id, concept, COALESCE(parent_id,''), tree_type, level,
		COALESCE(definition,''), COALESCE(domain,''), COALESCE(synonyms,'[]'),
		COALESCE(source_chunk_id,''), created_at
		FROM kg_concept_tree WHERE domain = ? ORDER BY level, concept`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConcepts(rows)
}

func scanConcepts(rows *sql.Rows) ([]ConceptNode, error) {
	var out []ConceptNode
	for rows.Next() {
		var c ConceptNode
		var synStr string
		if err := rows.Scan(&c.ID, &c.Concept, &c.ParentID, &c.TreeType, &c.Level,
			&c.Definition, &c.Domain, &synStr, &c.SourceChunkID, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(synStr), &c.Synonyms)
		out = append(out, c)
	}
	return out, nil
}
