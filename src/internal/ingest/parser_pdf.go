//go:build windows

package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"everevo/internal/rag"
)

// ParseDocument extracts text from a document file using the appropriate
// strategy for its format. Returns the raw text content ready for LLM
// structure extraction (L2).
//
// Pipeline levels:
//
//	L1 (born-digital): direct text extraction — always tried first
//	L2 (LLM structure): parse text → structured JSON — core pipeline
//	L3 (OCR): fallback for image-only PDFs — not implemented yet
//	L4 (VLM): visual understanding for complex layouts — not implemented yet
func ParseDocument(filePath string, content []byte) (text string, needsOCR bool, err error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".pdf":
		return parsePDF(filePath, content)
	case ".md", ".mdx", ".rst", ".txt":
		return string(content), false, nil
	case ".html", ".htm":
		return parseHTML(string(content)), false, nil
	default:
		// Try as plain text
		return string(content), false, nil
	}
}

// parsePDF extracts text from a PDF using the lightweight BT/ET parser.
// If the parser extracts meaningful text, it's a born-digital PDF.
// If the result is near-empty, the caller should fall through to OCR (L3).
func parsePDF(filePath string, content []byte) (string, bool, error) {
	// Try the lightweight parser first (handles FlateDecode, BT/ET, Tj/TJ).
	text, pdfErr := rag.ExtractTextFromPDF(content)
	if pdfErr != nil {
		text = ""
	}
	text = strings.TrimSpace(text)

	// Heuristic: if we got fewer than 50 meaningful characters, the PDF is
	// likely image-only and needs OCR.
	meaningful := 0
	for _, r := range text {
		if r > ' ' && r != '\n' && r != '\r' {
			meaningful++
		}
	}
	needsOCR := meaningful < 50

	if needsOCR {
		return text, true, fmt.Errorf("PDF appears to be image-only (extracted %d meaningful chars), OCR not yet implemented", meaningful)
	}

	return text, false, nil
}

// parseHTML strips HTML tags to extract plain text. This is a best-effort
// fallback; for production, use a proper HTML-to-text converter.
func parseHTML(html string) string {
	var buf strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			buf.WriteByte(' ')
			continue
		}
		if !inTag {
			buf.WriteRune(r)
		}
	}
	return strings.TrimSpace(buf.String())
}

// ReadFileContent reads the full content of a file for document parsing.
func ReadFileContent(filePath string) (data []byte, err error) {
	return os.ReadFile(filePath)
}
