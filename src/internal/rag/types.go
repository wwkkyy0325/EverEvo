//go:build windows

package rag

// KnowledgeBase represents a named collection of embedded text chunks.
type KnowledgeBase struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ModelDir   string `json:"modelDir"`
	LibraryID  string `json:"libraryId,omitempty"`
	ChunkCount int    `json:"chunkCount"`
	CreatedAt  string `json:"createdAt"`
}

// SearchResult is a single hit from a semantic search.
type SearchResult struct {
	ID         string            `json:"id"`
	Content    string            `json:"content"`
	Source     string            `json:"source"`   // document title / file name
	Position   int               `json:"position"` // zero-based chunk index within source
	Metadata   map[string]string `json:"metadata"`
	Similarity float32           `json:"similarity"`
}

// ChunkRegistrar is an optional callback called after documents are added to
// chromem. Implementations should store chunk→source mappings for bidirectional
// lookup and hierarchical retrieval.
type ChunkRegistrar func(sourceType, sourceID string, docIDs []string, chunkStartIndex int, contents []string) error

// DocumentProfile describes how a document should be processed.
type DocumentProfile struct {
	Format    string  `json:"format"`    // "markdown"|"pdf"|"txt"|"html"
	Structure string  `json:"structure"` // "highly_structured"|"semi_structured"|"unstructured"
	Domain    string  `json:"domain"`    // "legal"|"technical"|"narrative"|"general"
	Confidence float64 `json:"confidence"`
}

// SectionNode is one node in the document section hierarchy tree.
type SectionNode struct {
	ID        string         `json:"id"`
	Level     int            `json:"level"`
	Numbering string         `json:"numbering"` // original numbering text
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	ByteStart int            `json:"byteStart"`
	ByteEnd   int            `json:"byteEnd"`
	Children  []*SectionNode `json:"children,omitempty"`
}

// ChunkInfo holds metadata for a structure-preserving chunk.
type ChunkInfo struct {
	ChunkID      string   `json:"chunkId"`
	Content      string   `json:"content"`
	SectionPath  []string `json:"sectionPath"`  // hierarchical path ["Part I", "Chapter 1", "Section 2"]
	ParentID     string   `json:"parentId,omitempty"`
	CrossRefs    []string `json:"crossRefs,omitempty"`    // referenced section numberings
	DefinesTerms []string `json:"definesTerms,omitempty"` // terms defined in this chunk
}
