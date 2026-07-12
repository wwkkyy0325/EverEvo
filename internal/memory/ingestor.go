package memory

import "strings"

// ExtractedRelation is a subject-predicate-object triple. Replaces=true means the
// new fact supersedes an existing same-(subject,predicate) fact (a switch); false
// means it coexists (an addition).
type ExtractedRelation struct {
	Subject   string   `json:"subject"`
	Predicate string   `json:"predicate"`
	Object    string   `json:"object"`
	Replaces  bool     `json:"replaces"`
	Domains   []string `json:"domains"` // domain library names (P7 auto-x)
}

type ExtractedEntity struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Domains []string `json:"domains"`
}

// typeSynonyms maps variant entity types to a canonical form so coloring/filtering
// stay stable when the LLM emits person/人物/人 interchangeably.
var typeSynonyms = map[string]string{
	"人物": "person", "人": "person", "用户": "person", "person": "person", "user": "person",
	"语言": "language", "编程语言": "language", "language": "language",
	"公司": "company", "企业": "company", "组织": "company", "company": "company",
	"项目": "project", "project": "project",
	"地点": "place", "地方": "place", "位置": "place", "place": "place",
	"工具": "tool", "tool": "tool",
	"模型": "model", "model": "model",
}

// normalizeType canonicalizes an entity type (trim + synonym lookup).
func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if c, ok := typeSynonyms[strings.ToLower(t)]; ok {
		return c
	}
	return t
}

// IngestGraph writes extracted entities and relations into the temporal knowledge
// graph. Each entity is upserted (disambiguated by normalized name, vectorized via
// the embed callback); each relation becomes a bi-temporal edge. Entity
// coreference: if an existing entity is embedding-similar (cosine ≥ 0.92) it is
// reused, merging near-duplicates like 用户/User/我. Best-effort.
func (s *Store) IngestGraph(entities []ExtractedEntity, relations []ExtractedRelation, sessionID, workspaceID string, embed func(string) ([]float32, error)) error {
	nameToID := make(map[string]string, len(entities)+len(relations)*2)
	upsert := func(name string) (string, bool) {
		norm := normalizeName(name)
		if norm == "" {
			return "", false
		}
		if id, ok := nameToID[norm]; ok {
			return id, true
		}
		// Coreference: reuse an embedding-similar existing entity instead of
		// creating a near-duplicate node (e.g. 用户/User/我 collapse to one).
		if embed != nil {
			if vec, eErr := embed(name); eErr == nil && len(vec) > 0 {
				if hits, _ := s.EntitySearch(vec, 1, workspaceID); len(hits) > 0 && hits[0].Similarity >= 0.92 {
					nameToID[norm] = hits[0].ID
					return hits[0].ID, true
				}
			}
		}
		id, err := s.UpsertNode("", name, workspaceID, embed)
		if err != nil {
			return "", false
		}
		nameToID[norm] = id
		return id, true
	}

	for _, e := range entities {
		t := normalizeType(e.Type)
		if id, ok := upsert(e.Name); ok && t != "" {
			_, _ = s.db.Exec(`UPDATE kg_nodes SET type = ? WHERE id = ? AND type = ''`, t, id)
		}
	}
	for _, r := range relations {
		srcID, ok1 := upsert(r.Subject)
		dstID, ok2 := upsert(r.Object)
		if !ok1 || !ok2 {
			continue
		}
		_ = s.AddEdge(srcID, dstID, normalizePredicate(r.Predicate), "{}", sessionID, "[]", r.Replaces)
	}
	return nil
}
