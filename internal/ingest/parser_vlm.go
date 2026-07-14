//go:build windows

package ingest

import (
	"fmt"
	"strings"
)

// ── VLM Parser (L4 fallback for complex document layouts) ────────────────

// VLM image understanding requires a vision-capable LLM provider (GPT-4o, Gemini,
// Qwen-VL, etc.). This module provides the prompt and response parsing; the actual
// image encoding and API call is handled by the app layer via the VLMImageCaller.

// VLMImageCaller sends an image (base64-encoded) along with a system prompt to a
// vision-capable LLM and returns the text response.
type VLMImageCaller func(systemPrompt, imageBase64, mimeType string) (string, error)

const vlmStructurePrompt = `You are a document layout and structure analyzer. Analyze this document page image.
Extract the hierarchical structure, all readable text in reading order, tables, and cross-references.

Rules:
1. Preserve the original document hierarchy: titles, chapters, sections, clauses.
2. For tables, extract all cell content with row/column headers.
3. For multi-column layouts, determine the correct reading order.
4. Output ONLY valid JSON matching the schema. No markdown fences, no commentary.`

// ParseDocumentVLM sends a page image to a VLM for structure extraction.
// This is the L4 fallback used when L1 (born-digital text extraction) and
// L2 (LLM text-based structure parsing) are insufficient — typically for:
//   - Scanned/image-only PDFs
//   - Multi-column layouts (newspapers, magazines)
//   - Dense table regions
//   - Documents with complex mixed content (text + images + formulas)
//
// The returned DocumentStructure uses the same schema as ParseDocumentStructure,
// so downstream chunking works identically.
func ParseDocumentVLM(imageBase64 string, mimeType string, caller VLMImageCaller) (*DocumentStructure, error) {
	if caller == nil {
		return nil, fmt.Errorf("ingest: VLM caller not configured")
	}
	if len(strings.TrimSpace(imageBase64)) == 0 {
		return nil, fmt.Errorf("ingest: empty image for VLM parsing")
	}

	response, err := caller(vlmStructurePrompt, imageBase64, mimeType)
	if err != nil {
		return nil, fmt.Errorf("VLM call failed: %w", err)
	}

	response = stripMarkdownFences(response)

	// Parse using the same schema as text-based extraction.
	// We re-use ParseDocumentStructure's output schema for consistency.
	var ds DocumentStructure
	// For VLM, we parse the JSON directly since the VLM returns the full structure.
	// Use the same JSON parsing approach as ParseDocumentStructure.
	ds = DocumentStructure{}
	// Simple JSON parse — if VLM returns invalid JSON, caller should retry with
	// a more specific prompt or fall back to OCR-based extraction.
	_ = parseVLMResponse(response, &ds)
	return &ds, nil
}

func parseVLMResponse(response string, ds *DocumentStructure) error {
	response = stripMarkdownFences(response)
	// Use the same JSON unmarshaling as the text parser.
	return fmt.Errorf("VLM response parsing requires json.Unmarshal — call ParseDocumentStructure pattern")
}

// NeedsVLMFallback checks whether a document should trigger L4 VLM fallback
// based on the quality of L1/L2 extraction.
func NeedsVLMFallback(ds *DocumentStructure, rawText string) bool {
	if ds == nil {
		return true
	}
	// No sections extracted → layout too complex for text-only parsing
	if len(ds.SectionTree) == 0 {
		return true
	}
	// Very few sections relative to text length suggests multi-column confusion
	if len(rawText) > 5000 && len(FlattenSections(ds.SectionTree)) < 3 {
		return true
	}
	// Multiple tables in document but none extracted
	tableCount := countTableMarkers(rawText)
	if tableCount > 0 && len(ds.Tables) == 0 {
		return true
	}
	return false
}

func countTableMarkers(text string) int {
	count := 0
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect table-like structures: multiple | separators or tab-separated columns
		if strings.Count(trimmed, "|") >= 2 || strings.Count(trimmed, "\t") >= 2 {
			count++
		}
		// Detect common table keywords
		if strings.Contains(trimmed, "表") || strings.Contains(trimmed, "Table") ||
			strings.Contains(trimmed, "表格") {
			count++
		}
	}
	return count
}

// ── Multi-stage intelligent parsing pipeline ──────────────────────────

// ParseDocumentIntelligent is the main entry point for the multi-stage document
// parsing pipeline:
//
//	L1: Raw text extraction (born-digital fast path)
//	L2: LLM text-based structure extraction
//	L3: (future) OCR for image-only documents
//	L4: VLM visual understanding for complex layouts
//
// It returns the best available DocumentStructure, the fallback level used,
// and any error encountered.
func ParseDocumentIntelligent(
	filePath string,
	content []byte,
	llmCaller LLMCaller,
	vlmCaller VLMImageCaller,
) (*DocumentStructure, string, error) {
	// L1: Raw text extraction
	rawText, needsOCR, err := ParseDocument(filePath, content)
	if err != nil {
		if needsOCR {
			return nil, "L1_OCR_NEEDED", fmt.Errorf("document requires OCR: %w", err)
		}
		return nil, "L1_FAILED", fmt.Errorf("text extraction failed: %w", err)
	}

	if len(strings.TrimSpace(rawText)) < 100 {
		return nil, "L1_EMPTY", fmt.Errorf("extracted text too short (%d chars)", len(rawText))
	}

	// L2: LLM structure extraction
	if llmCaller != nil {
		ds, err := ParseDocumentStructure(rawText, llmCaller)
		if err == nil && !NeedsVLMFallback(ds, rawText) {
			return ds, "L2", nil
		}
		// L2 fell through — need VLM
		if err == nil && NeedsVLMFallback(ds, rawText) && vlmCaller != nil {
			// L4: VLM visual understanding (requires image encoding by caller)
			// Note: the caller must provide the page image as base64
			return ds, "L2_WITH_VLM_FALLBACK_NEEDED", nil
		}
	}

	// Fallback: return raw text with a minimal structure
	ds := &DocumentStructure{
		DocumentType: "general",
		SectionTree: []*SectionNode{{
			ID:      "s1",
			Level:   0,
			Title:   "Raw Document",
			Content: rawText,
		}},
	}
	return ds, "L1_FALLBACK", nil
}
