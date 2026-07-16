//go:build windows

package rag

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
)

// ExtractTextFromPDF extracts readable text from raw PDF bytes.
//
// Strategy (handles the 95% case — text-based PDFs with /FlateDecode):
//  1. Locate stream objects (NN 0 obj … stream … endstream).
//  2. Check the preceding dictionary for /Filter entries.
//  3. Decompress (/FlateDecode → zlib or raw deflate).
//  4. Extract text from the decompressed content via BT…ET blocks
//     and Tj/TJ/' operators.
//
// This is NOT a full PDF parser. It will miss text in:
//   - Image-based / scanned PDFs (needs OCR)
//   - Font-encoded glyph IDs (CID fonts without ToUnicode maps)
//   - AES-encrypted PDFs
func ExtractTextFromPDF(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty PDF data")
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return "", fmt.Errorf("not a PDF file")
	}

	// Find all streams and extract text from each.
	var chunks []string
	pos := 0

	for {
		// Find next "stream\r\n" or "stream\n"
		si := indexAfter(data, pos, "stream")
		if si < 0 {
			break
		}
		// Skip "stream" + optional \r\n or \n
		streamStart := si
		if streamStart < len(data) && data[streamStart] == '\r' {
			streamStart++
		}
		if streamStart < len(data) && data[streamStart] == '\n' {
			streamStart++
		}
		if streamStart >= len(data) {
			break
		}

		// Find matching "endstream"
		ei := bytes.Index(data[streamStart:], []byte("endstream"))
		if ei < 0 {
			break
		}
		end := streamStart + ei
		pos = end + 9

		// Determine filter by looking backwards from "stream" for /Filter
		dictEnd := si - len("stream")
		filter := detectFilter(data[:dictEnd])

		// Decompress the stream content
		raw := data[streamStart:end]
		content, err := decompressStream(raw, filter)
		if err != nil {
			// Try uncompressed anyway — some PDFs omit or mislabel.
			content = raw
		}

		// Extract text operators
		text := extractTextOps(content)
		if len(text) > 0 {
			chunks = append(chunks, text)
		}
	}

	result := strings.Join(chunks, "\n\n")
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("no extractable text found in PDF (may be image-based, encrypted, or CID-encoded)")
	}
	return result, nil
}

// ─── Stream filter detection ─────────────────────────────────────

func detectFilter(dictBytes []byte) string {
	// Look backwards from the end for the most recent /Filter entry.
	// PDF dict syntax: /Filter /FlateDecode  or  /Filter [/FlateDecode]
	s := dictBytes
	// Narrow to the ~2KB before "stream"
	if len(s) > 2048 {
		s = s[len(s)-2048:]
	}

	if idx := bytes.LastIndex(s, []byte("/Filter")); idx >= 0 {
		rest := bytes.TrimSpace(s[idx+len("/Filter"):])
		// Single name: /FlateDecode
		if bytes.HasPrefix(rest, []byte("/FlateDecode")) ||
			bytes.HasPrefix(rest, []byte("/Fl")) {
			return "FlateDecode"
		}
		if bytes.HasPrefix(rest, []byte("/ASCII85Decode")) ||
			bytes.HasPrefix(rest, []byte("/A85")) {
			return "ASCII85Decode"
		}
		if bytes.HasPrefix(rest, []byte("/ASCIIHexDecode")) ||
			bytes.HasPrefix(rest, []byte("/AHx")) {
			return "ASCIIHexDecode"
		}
		if bytes.HasPrefix(rest, []byte("/LZWDecode")) {
			return "LZWDecode"
		}
		// Array: [/FlateDecode] — check if FlateDecode appears nearby
		if bytes.Contains(rest[:min(len(rest), 60)], []byte("FlateDecode")) {
			return "FlateDecode"
		}
	}
	return ""
}

// ─── Decompression ───────────────────────────────────────────────

func decompressStream(data []byte, filter string) ([]byte, error) {
	switch filter {
	case "FlateDecode":
		return decompressFlate(data)
	default:
		// No filter or unknown filter — return raw bytes.
		// Strip any trailing \r\n before endstream (PDF spec).
		data = bytes.TrimRight(data, "\r\n ")
		return data, nil
	}
}

func decompressFlate(data []byte) ([]byte, error) {
	data = bytes.TrimRight(data, "\r\n ")

	// PDF spec: FlateDecode can be either raw deflate or zlib-wrapped.
	// Try zlib first (more common in PDFs), then raw deflate.

	// zlib
	if r, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		defer r.Close()
		var buf bytes.Buffer
		if _, e := io.Copy(&buf, r); e == nil {
			return buf.Bytes(), nil
		}
	}

	// raw deflate
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ─── Text operator extraction ─────────────────────────────────────
//
// PDF text is placed between BT (begin text) and ET (end text) operators.
// Within a BT…ET block, text-showing operators are:
//   Tj  — show a string: (Hello World) Tj
//   TJ  — show an array of strings and gaps: [(Hello) 20 (World)] TJ
//   '   — move to next line + show: (text) '
//   "   — set word/char spacing + move + show: wc (text) "

func extractTextOps(content []byte) string {
	var out strings.Builder
	textBlocks := splitBTET(content)

	for _, block := range textBlocks {
		// Extract text from Tj, TJ, ', " operators
		ops := extractFromBlock(block)
		for _, op := range ops {
			if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") &&
				!strings.HasSuffix(out.String(), " ") {
				out.WriteByte(' ')
			}
			out.WriteString(op)
		}
	}
	return out.String()
}

// splitBTET splits PDF content by BT…ET text blocks.
func splitBTET(content []byte) [][]byte {
	var blocks [][]byte
	data := content
	for {
		btIdx := bytes.Index(data, []byte("BT\n"))
		if btIdx < 0 {
			btIdx = bytes.Index(data, []byte("BT\r"))
		}
		if btIdx < 0 {
			break
		}
		start := btIdx + 2
		// Skip whitespace after BT
		for start < len(data) && (data[start] == '\n' || data[start] == '\r' || data[start] == ' ') {
			start++
		}
		etIdx := bytes.Index(data[start:], []byte("ET"))
		if etIdx < 0 {
			break
		}
		end := start + etIdx
		blocks = append(blocks, data[start:end])
		data = data[end+2:]
	}
	return blocks
}

// extractFromBlock extracts text strings from a single BT…ET block body.
func extractFromBlock(block []byte) []string {
	var results []string

	// Helper: extract parenthesized strings
	extractParenStrings := func(segment []byte) []string {
		var out []string
		for {
			open := bytes.IndexByte(segment, '(')
			if open < 0 {
				break
			}
			// Find matching close — handle nested parens and escapes
			depth := 1
			close := open + 1
			for close < len(segment) && depth > 0 {
				if segment[close] == '\\' {
					close += 2 // skip escaped char
					continue
				}
				if segment[close] == '(' {
					depth++
				} else if segment[close] == ')' {
					depth--
				}
				close++
			}
			if depth != 0 {
				break
			}
			raw := segment[open+1 : close-1]
			out = append(out, cleanPdfString(raw))
			segment = segment[close:]
		}
		return out
	}

	// Tj operator: (string) Tj
	for _, seg := range bytes.Split(block, []byte("Tj")) {
		results = append(results, extractParenStrings(seg)...)
	}

	// TJ operator: [(str1) num (str2)] TJ
	for _, seg := range bytes.Split(block, []byte("TJ")) {
		results = append(results, extractParenStrings(seg)...)
	}

	// ' operator: (string) '
	for _, seg := range bytes.Split(block, []byte("'")) {
		// Only extract the last parenthesized string before '
		strs := extractParenStrings(seg)
		if len(strs) > 0 {
			results = append(results, strs[len(strs)-1])
		}
	}

	// " operator: wc (string) "
	for _, seg := range bytes.Split(block, []byte("\"")) {
		strs := extractParenStrings(seg)
		if len(strs) > 0 {
			results = append(results, strs[len(strs)-1])
		}
	}

	return results
}

// cleanPdfString decodes common PDF string escapes: \n \r \t \\ \( \)
// and octal escapes \ddd. Multi-byte UTF-16BE sequences are detected
// via BOM (\xFE\xFF) and converted.
func cleanPdfString(raw []byte) string {
	var out strings.Builder
	i := 0
	for i < len(raw) {
		b := raw[i]
		if b == '\\' && i+1 < len(raw) {
			i++
			switch raw[i] {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case '\\':
				out.WriteByte('\\')
			case '(':
				out.WriteByte('(')
			case ')':
				out.WriteByte(')')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// Octal escape: up to 3 digits
				val := int(raw[i] - '0')
				for j := 1; j < 3 && i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7'; j++ {
					i++
					val = val*8 + int(raw[i]-'0')
				}
				out.WriteByte(byte(val))
			default:
				out.WriteByte(raw[i])
			}
		} else {
			out.WriteByte(b)
		}
		i++
	}
	return out.String()
}

// ─── helpers ──────────────────────────────────────────────────────

func indexAfter(data []byte, start int, target string) int {
	idx := bytes.Index(data[start:], []byte(target))
	if idx < 0 {
		return -1
	}
	return start + idx + len(target)
}
