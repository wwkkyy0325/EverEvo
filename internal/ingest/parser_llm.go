//go:build windows

package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ── Structured output types (match the LLM output schema) ────────────────

// DocumentStructure is the parsed hierarchical structure of a document.
type DocumentStructure struct {
	DocumentType     string            `json:"document_type"` // "statute"|"contract"|"judgment"|"manual"|"novel"|"article"|"code"|"general"
	SectionTree      []*SectionNode    `json:"section_tree"`
	Definitions      []DefinitionEntry `json:"definitions,omitempty"`
	CrossReferences  []CrossRefEntry   `json:"cross_references,omitempty"`
	Tables           []TableEntry      `json:"tables,omitempty"`
	EntitiesMentioned []EntityMention   `json:"entities_mentioned,omitempty"`
}

// SectionNode is one node in the document hierarchy tree.
type SectionNode struct {
	ID        string         `json:"id"`
	Level     int            `json:"level"`
	Numbering string         `json:"numbering"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	ByteStart int            `json:"byte_start"`
	ByteEnd   int            `json:"byte_end"`
	Children  []*SectionNode `json:"children,omitempty"`
}

// DefinitionEntry is a term→definition pair extracted from the document.
type DefinitionEntry struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
	DefinedIn  string `json:"defined_in"` // section tree node id
	Context    string `json:"context"`
}

// CrossRefEntry is an internal cross-reference within the document.
type CrossRefEntry struct {
	SourceID         string `json:"source_id"`
	TargetNumbering  string `json:"target_numbering"`
	TargetText       string `json:"target_text"`
	Relation         string `json:"relation"` // "supplements"|"overrides"|"exceptions"|"see_also"
}

// TableEntry represents a table extracted from the document.
type TableEntry struct {
	ID          string     `json:"id"`
	Caption     string     `json:"caption"`
	Headers     [][]string `json:"headers"`
	Rows        [][]string `json:"rows"`
	ContainedIn string     `json:"contained_in"` // section tree node id
}

// EntityMention is an entity name mentioned in the document.
type EntityMention struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // "person"|"organization"|"location"|"event"|...
	MentionsAt []string `json:"mentions_at"` // section tree node ids
}

// ── LLM caller type ─────────────────────────────────────────────────────

// LLMCaller is a function that calls the configured LLM with a system prompt
// and user content, returning the raw text response. The implementation is
// wired by the app layer.
type LLMCaller func(systemPrompt, userContent string) (string, error)

// ── Prompt templates ────────────────────────────────────────────────────

const structureExtractionSystem = `You are a document structure parser. Extract the hierarchical structure,
definitions, cross-references, tables, and mentioned entities from the text.

Rules:
1. Preserve ALL original text — do NOT summarize, rephrase, or omit content.
2. For each section, capture its exact numbering, title, and full content.
3. Build a proper tree: parent sections contain child subsections.
4. The content of a parent node should NOT duplicate its children's content.
5. Detect document type from these categories: statute|contract|judgment|manual|novel|article|code|general
6. If a section has subsections, its content field can be empty (the subsections contain the actual content).
7. byte_start/byte_end are approximate character offsets in the input text (use 0 if uncertain).
8. Output ONLY valid JSON matching the schema. No markdown fences, no commentary.`

const structureExtractionSchema = `
Output JSON schema:
{
  "document_type": "statute|contract|judgment|manual|novel|article|code|general",
  "section_tree": [
    {
      "id": "s1",
      "level": 1,
      "numbering": "第一章",
      "title": "总则",
      "content": "section text here...",
      "byte_start": 0,
      "byte_end": 1500,
      "children": [
        {
          "id": "s1_1",
          "level": 2,
          "numbering": "第一条",
          "title": "",
          "content": "article text here...",
          "byte_start": 120,
          "byte_end": 380,
          "children": []
        }
      ]
    }
  ],
  "definitions": [
    {
      "term": "违约金",
      "definition": "当事人约定的，一方违约时应当向对方支付的一定数额的金钱",
      "defined_in": "s5_3",
      "context": "本法所称违约金，是指..."
    }
  ],
  "cross_references": [
    {
      "source_id": "s8_1",
      "target_numbering": "第七条",
      "target_text": "依照本法第七条的规定",
      "relation": "supplements"
    }
  ],
  "tables": [
    {
      "id": "t1",
      "caption": "违约金计算标准",
      "headers": [["违约天数", "计算比例"]],
      "rows": [["1-30天", "0.1%/日"]],
      "contained_in": "s5_3"
    }
  ],
  "entities_mentioned": [
    {
      "name": "国务院",
      "type": "organization",
      "mentions_at": ["s2_1"]
    }
  ]
}`

// ── Parser ──────────────────────────────────────────────────────────────

const maxRetries = 2

// ParseDocumentStructure extracts document structure using an LLM.
// caller is the LLM invocation function (wired by app layer).
// Returns the parsed structure or an error after exhausting retries.
func ParseDocumentStructure(text string, caller LLMCaller) (*DocumentStructure, error) {
	if caller == nil {
		return nil, fmt.Errorf("ingest: LLM caller not configured")
	}
	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("ingest: empty document text")
	}

	userPrompt := fmt.Sprintf("Parse this document structure:\n\n```\n%s\n```", truncateForLLM(text, 32000))

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		response, err := caller(structureExtractionSystem, userPrompt)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed (attempt %d): %w", attempt+1, err)
			continue
		}

		// Strip markdown code fences if present.
		response = stripMarkdownFences(response)

		var ds DocumentStructure
		if err := json.Unmarshal([]byte(response), &ds); err != nil {
			lastErr = fmt.Errorf("JSON parse failed (attempt %d): %w\nResponse: %.500s", attempt+1, err, response)
			// Retry with error feedback.
			userPrompt = fmt.Sprintf("Parse this document structure.\nPrevious attempt returned invalid JSON: %v\n\n```\n%s\n```", err, truncateForLLM(text, 32000))
			continue
		}

		// Quick validation
		if len(ds.SectionTree) == 0 && len(ds.Definitions) == 0 && len(ds.Tables) == 0 {
			lastErr = fmt.Errorf("LLM returned empty structure (attempt %d)", attempt+1)
			userPrompt = fmt.Sprintf("Parse this document structure. The previous response was empty — ensure you extract the full hierarchy.\n\n```\n%s\n```", truncateForLLM(text, 32000))
			continue
		}

		return &ds, nil
	}

	return nil, fmt.Errorf("structure extraction failed after %d attempts: %w", maxRetries+1, lastErr)
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	// Remove ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		// Find first newline
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		} else {
			s = strings.TrimPrefix(s, "```")
			s = strings.TrimPrefix(s, "json")
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

func truncateForLLM(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n\n[...truncated]"
}

// FlattenSections returns all section nodes in depth-first pre-order.
func FlattenSections(nodes []*SectionNode) []*SectionNode {
	var out []*SectionNode
	var walk func([]*SectionNode)
	walk = func(ns []*SectionNode) {
		for _, n := range ns {
			out = append(out, n)
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

// LeafSections returns only leaf section nodes (nodes with no children).
func LeafSections(nodes []*SectionNode) []*SectionNode {
	var out []*SectionNode
	for _, n := range FlattenSections(nodes) {
		if len(n.Children) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// SectionPath returns the hierarchical path from root to the given section ID.
func SectionPath(nodes []*SectionNode, targetID string) []string {
	var path []string
	var found bool
	var walk func([]*SectionNode) bool
	walk = func(ns []*SectionNode) bool {
		for _, n := range ns {
			path = append(path, n.Numbering+" "+n.Title)
			if n.ID == targetID {
				return true
			}
			if len(n.Children) > 0 && walk(n.Children) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	walk(nodes)
	if !found {
		return nil
	}
	return path
}
