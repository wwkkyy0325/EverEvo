package ingest

import (
	"strings"

	"everevo/internal/rag"
)

// SmartChunk picks a chunk strategy based on file extension.
func SmartChunk(text string, ext string) []string {
	profile := QuickClassify(text, ext)
	return smartChunkWithProfile(text, ext, profile)
}

// SmartChunkWithProfile picks a chunk strategy based on an explicit document profile.
func SmartChunkWithProfile(text string, ext string, profile DocumentProfile) []string {
	return smartChunkWithProfile(text, ext, profile)
}

func smartChunkWithProfile(text string, ext string, profile DocumentProfile) []string {
	switch profile.Structure {
	case "highly_structured":
		// For structured documents, prefer structure-preserving chunking.
		// If no LLM parsed structure is available, fall through to code/markdown strategies.
		// The caller should have already run LLM parsing before chunking.
		return rag.ChunkText(text) // fallback: caller overrides with StructurePreservingChunk after LLM parse
	case "unstructured":
		// For narrative/unstructured documents, use semantic chunking (future).
		// Currently falls through to default.
		return rag.ChunkText(text)
	default:
		// semi_structured: dispatch by file type
	}
	switch ext {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".c", ".cpp", ".h":
		return chunkCode(text)
	case ".md", ".mdx", ".rst":
		return chunkMarkdown(text)
	case ".json", ".yaml", ".yml", ".toml", ".xml":
		return chunkConfig(text)
	default:
		return rag.ChunkText(text)
	}
}

// chunkCode splits source code at function/method/class boundaries.
func chunkCode(text string) []string {
	lines := strings.Split(text, "\n")
	var boundaries []int
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") ||
			strings.HasPrefix(trimmed, "def ") ||
			strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "export function ") ||
			strings.HasPrefix(trimmed, "export class ") ||
			strings.HasPrefix(trimmed, "public ") ||
			strings.HasPrefix(trimmed, "private ") ||
			strings.HasPrefix(trimmed, "async function ") ||
			strings.HasPrefix(trimmed, "fn ") {
			boundaries = append(boundaries, i)
		}
	}
	if len(boundaries) < 2 {
		return rag.ChunkText(text)
	}

	var chunks []string
	var current []string
	for i := 0; i < len(boundaries); i++ {
		start := boundaries[i]
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}
		block := strings.Join(lines[start:end], "\n")
		if len([]rune(block)) > 3000 {
			subs := rag.ChunkText(block)
			chunks = append(chunks, subs...)
		} else {
			if len([]rune(strings.Join(current, "\n")))+len([]rune(block)) > 3000 && len(current) > 0 {
				chunks = append(chunks, strings.Join(current, "\n"))
				current = nil
			}
			current = append(current, block)
		}
	}
	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n"))
	}
	if len(chunks) == 0 {
		return rag.ChunkText(text)
	}
	return chunks
}

// chunkMarkdown splits at heading boundaries.
func chunkMarkdown(text string) []string {
	lines := strings.Split(text, "\n")
	var boundaries []int
	for i, line := range lines {
		if strings.HasPrefix(line, "##") || strings.HasPrefix(line, "###") {
			boundaries = append(boundaries, i)
		}
	}
	if len(boundaries) < 2 {
		return rag.ChunkText(text)
	}
	var chunks []string
	for i := 0; i < len(boundaries); i++ {
		start := boundaries[i]
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}
		block := strings.Join(lines[start:end], "\n")
		if len([]rune(block)) > 3000 {
			chunks = append(chunks, rag.ChunkText(block)...)
		} else {
			chunks = append(chunks, block)
		}
	}
	return chunks
}

// chunkConfig keeps small config files intact.
func chunkConfig(text string) []string {
	if len([]rune(text)) <= 2000 {
		return []string{text}
	}
	return rag.ChunkText(text)
}
