// Package ingest implements folder ingestion pipelines: file scanning,
// smart chunking, LLM-based categorization, domain synthesis, and KB creation.
package ingest

// FileInfo describes a scanned file with hash and preview.
type FileInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Ext     string `json:"ext"`
	Size    int64  `json:"size"`
	Preview string `json:"preview"`
	Hash    string `json:"hash"`
	IsNew   bool   `json:"isNew"`
}

// Domain groups related files under a category name.
type Domain struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Files       []FileInfo `json:"files"`
	FileCount   int        `json:"fileCount"`
	TotalSize   int64      `json:"totalSize"`
}

// Analysis is the output of an ingest dry-run (no embedding).
type Analysis struct {
	TotalFiles   int      `json:"totalFiles"`
	TotalSize    int64    `json:"totalSize"`
	SkippedFiles []string `json:"skippedFiles"`
	Domains      []Domain `json:"domains"`
	Suggested    string   `json:"suggested"`
}

// Result summarizes a completed ingestion run.
type Result struct {
	TotalFiles  int      `json:"totalFiles"`
	TotalChunks int      `json:"totalChunks"`
	NewFiles    int      `json:"newFiles"`
	Domains     int      `json:"domains"`
	Agents      []string `json:"agents"`
	Duration    string   `json:"duration"`
}

// RawFile is an internal scan result before content hashing.
type RawFile struct {
	Path string
	Size int64
	Ext  string
	Name string
}

// FileMeta is the LLM-extracted metadata for a single file (deep pipeline).
type FileMeta struct {
	Path       string   `json:"path"`
	Summary    string   `json:"summary"`
	DomainHint string   `json:"domainHint"`
	Importance string   `json:"importance"` // high | medium | low
	Topics     []string `json:"topics"`
	Entities   []string `json:"entities"`
}

// DeepDomain is a synthesized domain from the deep analysis pipeline.
type DeepDomain struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

// DeepAnalysis is the output of a deep ingestion dry-run.
type DeepAnalysis struct {
	Domains       []DeepDomain `json:"domains"`
	TotalFiles    int          `json:"totalFiles"`
	Metas         []FileMeta   `json:"metas"`
	DesignDoc     string       `json:"designDoc"`
	FileDomainMap map[string]string `json:"fileDomainMap"`
}

// Manifest tracks which files have been ingested, for incremental re-ingestion.
type Manifest struct {
	Files map[string]string `json:"files"` // path → hash
}
