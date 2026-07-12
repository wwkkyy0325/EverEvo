package rag

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/storage"
)

// ─── KB source backup ──────────────────────────────────────────────
//
// Every chunk added to a KB is synchronously dumped as a plain-text file
// under data/knowledge/source/{kbId}/. This provides:
//   - Migration safety: rescan source/ to rebuild all KBs from scratch
//   - Version independence: plain text, no binary format dependency
//   - Manual editability: users can open and edit the files
//
// File layout:
//
//	data/knowledge/source/{kbId}/
//	  ├── manifest.json          # { chunkID → { file, metadata, time } }
//	  ├── chunk_0001.txt
//	  ├── chunk_0002.txt
//	  └── ...

// ChunkRecord is one entry in the source manifest.
type ChunkRecord struct {
	ChunkID  string            `json:"chunkId"`
	File     string            `json:"file"`     // chunk_0001.txt
	Metadata map[string]string `json:"metadata,omitempty"`
	AddedAt  string            `json:"addedAt"`
}

// SourceManifest is the JSON index file.
type SourceManifest struct {
	KBID    string        `json:"kbId"`
	Updated string        `json:"updated"`
	Chunks  []ChunkRecord `json:"chunks"`
}

func sourceDir(kbID string) string {
	return filepath.Join(storage.DataDir(), "knowledge", "source", kbID)
}

func manifestPath(kbID string) string {
	return filepath.Join(sourceDir(kbID), "manifest.json")
}

// SaveChunksToSource writes a set of chunks to the source backup directory.
// Call AFTER the chunks have been successfully added to chromem.
func SaveChunksToSource(kbID string, chunks []string, ids []string, metadata map[string]string) error {
	dir := sourceDir(kbID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("source dir: %w", err)
	}

	// Load existing manifest.
	mf := loadManifest(kbID)
	mf.Updated = time.Now().Format(time.RFC3339)

	// Determine next file index.
	maxIdx := 0
	for _, c := range mf.Chunks {
		var idx int
		fmt.Sscanf(c.File, "chunk_%d.txt", &idx)
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	for i, chunk := range chunks {
		idx := maxIdx + i + 1
		filename := fmt.Sprintf("chunk_%04d.txt", idx)
		filePath := filepath.Join(dir, filename)

		if err := os.WriteFile(filePath, []byte(chunk), 0644); err != nil {
			log.Printf("[kb-source] write %s: %v", filename, err)
			continue
		}

		mf.Chunks = append(mf.Chunks, ChunkRecord{
			ChunkID:  ids[i],
			File:     filename,
			Metadata: metadata,
			AddedAt:  time.Now().Format(time.RFC3339),
		})
	}

	return saveManifest(kbID, &mf)
}

// DeleteChunksFromSource removes source files for the given chunk IDs.
func DeleteChunksFromSource(kbID string, chunkIDs []string) {
	mf := loadManifest(kbID)
	dir := sourceDir(kbID)
	target := make(map[string]bool, len(chunkIDs))
	for _, id := range chunkIDs {
		target[id] = true
	}

	var kept []ChunkRecord
	for _, c := range mf.Chunks {
		if target[c.ChunkID] {
			os.Remove(filepath.Join(dir, c.File))
			continue
		}
		kept = append(kept, c)
	}
	mf.Chunks = kept
	mf.Updated = time.Now().Format(time.RFC3339)
	_ = saveManifest(kbID, &mf)
}

// ClearSource removes all source files for a KB.
func ClearSource(kbID string) {
	dir := sourceDir(kbID)
	os.RemoveAll(dir)
	log.Printf("[kb-source] 已清除 %s", dir)
}

// HasSource returns true if the KB has any source backup files.
func HasSource(kbID string) bool {
	_, err := os.Stat(manifestPath(kbID))
	return err == nil
}

// SourceChunkCount returns the number of backed-up chunks.
func SourceChunkCount(kbID string) int {
	mf := loadManifest(kbID)
	return len(mf.Chunks)
}

// RebuildKBFromSource re-adds all backed-up chunks to the KB store.
// Returns the number of chunks re-added.
func RebuildKBFromSource(kbID string, embedFn func(texts []string) ([][]float32, error), addFn func(chunks []string, ids []string, metas []map[string]string) error) (int, error) {
	mf := loadManifest(kbID)
	if len(mf.Chunks) == 0 {
		return 0, nil
	}

	dir := sourceDir(kbID)
	var chunks []string
	var metas []map[string]string

	for _, c := range mf.Chunks {
		data, err := os.ReadFile(filepath.Join(dir, c.File))
		if err != nil {
			log.Printf("[kb-source] rebuild skip %s: %v", c.File, err)
			continue
		}
		chunks = append(chunks, string(data))
		metas = append(metas, c.Metadata)
	}

	if len(chunks) == 0 {
		return 0, fmt.Errorf("no readable source files")
	}

	log.Printf("[kb-source] 从 %d 个源文件重建 KB %s...", len(chunks), kbID)

	// The caller provides embedding and addition functions to avoid
	// creating a circular dependency on the App layer.
	ids := make([]string, len(chunks))
	for i := range chunks {
		ids[i] = fmt.Sprintf("rebuild_%s_%d", kbID[:8], i)
	}

	return len(chunks), addFn(chunks, ids, metas)
}

// ─── Internal helpers ──────────────────────────────────────────────

func loadManifest(kbID string) SourceManifest {
	var mf SourceManifest
	mf.KBID = kbID
	data, err := os.ReadFile(manifestPath(kbID))
	if err != nil {
		return mf
	}
	json.Unmarshal(data, &mf)
	return mf
}

func saveManifest(kbID string, mf *SourceManifest) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(kbID), data, 0644)
}

// SanitizeKBID replaces path-dangerous characters.
func SanitizeKBID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, id)
}
