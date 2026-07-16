//go:build windows

package ingest

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const defaultChunkRunes = 3000
const chunkOverlapRunes = 100

// ChunkInfo holds metadata for a structure-preserving chunk, ready for
// embedding and chunk_registry registration.
type ChunkInfo struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	SectionPath  []string `json:"sectionPath"`          // hierarchical path
	ParentID     string   `json:"parentId,omitempty"`   // parent chunk ID (for hierarchical retrieval)
	PrevID       string   `json:"prevId,omitempty"`     // previous sibling chunk ID
	NextID       string   `json:"nextId,omitempty"`     // next sibling chunk ID
	CrossRefs    []string `json:"crossRefs,omitempty"`   // referenced section numberings
	DefinesTerms []string `json:"definesTerms,omitempty"` // terms defined in this chunk
	TableIDs     []string `json:"tableIds,omitempty"`    // table IDs contained
	SourceType   string   `json:"sourceType"`            // "rag_kb" | "wiki" | "ingest"
	SourceID     string   `json:"sourceId"`              // KB ID or page ID
}

// StructurePreservingChunk converts a parsed section tree into chunks,
// preserving section boundaries: NEVER cut mid-section.
//
// Rules:
//  1. Each leaf section is an atomic, indivisible unit.
//  2. Multiple short leaf sections are merged up to maxRunes.
//  3. A single oversized leaf is split at natural paragraph boundaries,
//     retaining the parent section ID.
//  4. Every chunk carries its full hierarchical section_path.
//  5. Chunks that were split from the same parent are linked via prev_id/next_id.
func StructurePreservingChunk(doc *DocumentStructure, sourceType, sourceID string, maxRunes int) []ChunkInfo {
	if maxRunes <= 0 {
		maxRunes = defaultChunkRunes
	}

	leaves := LeafSections(doc.SectionTree)
	if len(leaves) == 0 {
		return nil
	}

	// Build definition lookup by section_id
	defBySection := make(map[string][]DefinitionEntry)
	for _, d := range doc.Definitions {
		defBySection[d.DefinedIn] = append(defBySection[d.DefinedIn], d)
	}
	refBySection := make(map[string][]CrossRefEntry)
	for _, r := range doc.CrossReferences {
		refBySection[r.SourceID] = append(refBySection[r.SourceID], r)
	}

	var chunks []ChunkInfo
	chunkIdx := 0

		// Phase 1: merge short adjacent leaves into larger chunks
	var buf chunkSegment

	flush := func() {
		if buf.content == "" {
			return
		}
		parentID := ""
		if len(buf.sectionIDs) > 1 {
			// Multiple leaves merged → create synthetic parent reference
			parentID = fmt.Sprintf("%s_parent_%d", sourceID, chunkIdx)
		} else if len(buf.sectionIDs) == 1 {
			parentID = findParentID(doc.SectionTree, buf.sectionIDs[0])
		}
		chunks = append(chunks, ChunkInfo{
			ID:           fmt.Sprintf("%s_%d", sourceID, chunkIdx),
			Content:      strings.TrimSpace(buf.content),
			SectionPath:  buf.sectionPath,
			ParentID:     parentID,
			CrossRefs:    dedupStrings(buf.crossRefs),
			DefinesTerms: dedupStrings(buf.definesTerms),
			SourceType:   sourceType,
			SourceID:     sourceID,
		})
		chunkIdx++
		buf = chunkSegment{}
	}

	for _, leaf := range leaves {
		text := leaf.Content
		if text == "" && leaf.Title != "" {
			text = leaf.Title
		}
		if text == "" {
			continue
		}

		// Build rich context
		var defTerms []string
		for _, d := range defBySection[leaf.ID] {
			defTerms = append(defTerms, d.Term)
		}
		var xrefs []string
		for _, r := range refBySection[leaf.ID] {
			xrefs = append(xrefs, r.TargetNumbering)
		}

		rlen := utf8.RuneCountInString(text)

		// If this single leaf is already oversized, split it.
		if rlen > maxRunes && buf.content != "" {
			flush()
		}
		if rlen > maxRunes {
			// Split at paragraph boundaries.
			subs := splitParagraphs(text)
			var subSegments []chunkSegment
			for _, sub := range subs {
				subSegments = append(subSegments, chunkSegment{
					content:      sub,
					sectionIDs:   []string{leaf.ID},
					sectionPath:  sectionPathForLeaf(doc.SectionTree, leaf.ID),
					definesTerms: defTerms,
					crossRefs:    xrefs,
				})
			}
			emitLinkedLeafChunks(&chunks, &chunkIdx, subSegments, sourceType, sourceID, maxRunes)
			continue
		}

		// Try to merge with buffer
		if utf8.RuneCountInString(buf.content)+rlen > maxRunes && buf.content != "" {
			flush()
		}
		buf.content += "\n\n" + text
		buf.sectionIDs = append(buf.sectionIDs, leaf.ID)
		// Use the deepest (most specific) section path
		buf.sectionPath = sectionPathForLeaf(doc.SectionTree, leaf.ID)
		buf.definesTerms = append(buf.definesTerms, defTerms...)
		buf.crossRefs = append(buf.crossRefs, xrefs...)
	}
	flush()

	// Link siblings (prev/next)
	for i := range chunks {
		if i > 0 {
			chunks[i].PrevID = chunks[i-1].ID
		}
		if i < len(chunks)-1 {
			chunks[i].NextID = chunks[i+1].ID
		}
	}

	return chunks
}

// chunkSegment is a temporary accumulator for building chunks.
type chunkSegment struct {
	content      string
	sectionIDs   []string
	sectionPath  []string
	definesTerms []string
	crossRefs    []string
}

// emitLinkedLeafChunks splits oversized sub-chunkSegments into chunks, linking them.
func emitLinkedLeafChunks(chunks *[]ChunkInfo, idx *int, segs []chunkSegment, sourceType, sourceID string, maxRunes int) {
	var batch []ChunkInfo
	localIdx := 0
	var buf chunkSegment

	flushLocal := func() {
		if buf.content == "" {
			return
		}
		batch = append(batch, ChunkInfo{
			ID:           fmt.Sprintf("%s_%d", sourceID, *idx),
			Content:      strings.TrimSpace(buf.content),
			SectionPath:  buf.sectionPath,
			DefinesTerms: dedupStrings(buf.definesTerms),
			CrossRefs:    dedupStrings(buf.crossRefs),
			SourceType:   sourceType,
			SourceID:     sourceID,
		})
		*idx++
		localIdx++
		buf = chunkSegment{}
	}

	for _, s := range segs {
		rlen := utf8.RuneCountInString(s.content)
		if utf8.RuneCountInString(buf.content)+rlen > maxRunes && buf.content != "" {
			flushLocal()
		}
		if buf.content == "" {
			buf = s
		} else {
			buf.content += "\n\n" + s.content
			buf.sectionIDs = append(buf.sectionIDs, s.sectionIDs...)
			buf.definesTerms = append(buf.definesTerms, s.definesTerms...)
			buf.crossRefs = append(buf.crossRefs, s.crossRefs...)
		}
	}
	flushLocal()

	// Link siblings within this batch
	for i := range batch {
		if i > 0 {
			batch[i].PrevID = batch[i-1].ID
		}
		if i < len(batch)-1 {
			batch[i].NextID = batch[i+1].ID
		}
	}
	*chunks = append(*chunks, batch...)
}

// splitParagraphs splits text at blank-line boundaries.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n\n")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// findParentID returns the parent section ID for a given leaf section.
func findParentID(nodes []*SectionNode, targetID string) string {
	var find func([]*SectionNode) string
	find = func(ns []*SectionNode) string {
		for _, n := range ns {
			for _, c := range n.Children {
				if c.ID == targetID {
					return n.ID
				}
				if result := find([]*SectionNode{c}); result != "" {
					return result
				}
			}
		}
		return ""
	}
	return find(nodes)
}

// findNodePath returns the node and its ancestors for a leaf ID.
func findNodePath(nodes []*SectionNode, targetID string) (path []*SectionNode) {
	var walk func([]*SectionNode) bool
	walk = func(ns []*SectionNode) bool {
		for _, n := range ns {
			path = append(path, n)
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
	return
}

func sectionPathForLeaf(nodes []*SectionNode, targetID string) []string {
	nodePath := findNodePath(nodes, targetID)
	var parts []string
	for _, n := range nodePath {
		label := strings.TrimSpace(n.Numbering + " " + n.Title)
		if label == "" {
			label = n.ID
		}
		parts = append(parts, label)
	}
	return parts
}

func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
