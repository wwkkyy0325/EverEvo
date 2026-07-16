//go:build windows

package rag

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxChunkRunes = 480
	overlapRunes  = 80 // adjacent chunk overlap for continuity
	shortParaMax  = maxChunkRunes + maxChunkRunes/2 // 720: paragraphs ≤ this stay intact
)

// ChunkText splits text into segments suitable for embedding.
// Strategy: split on blank-line paragraph boundaries first.
// Short paragraphs (≤ shortParaMax) remain one chunk.
// Oversized paragraphs are split with sliding-window overlap on sentence boundaries.
func ChunkText(text string) []string {
	paragraphs := splitParagraphs(text)
	var chunks []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		rlen := utf8.RuneCountInString(p)
		if rlen <= shortParaMax {
			chunks = append(chunks, p)
			continue
		}
		subChunks := splitLongParagraph(p)
		for _, sc := range subChunks {
			sc = strings.TrimSpace(sc)
			if sc != "" {
				chunks = append(chunks, sc)
			}
		}
	}
	return chunks
}

// splitParagraphs splits text on one or more blank lines.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	raw := strings.Split(text, "\n\n")
	var out []string
	for _, r := range raw {
		trimmed := strings.TrimSpace(r)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// splitLongParagraph splits a paragraph that exceeds shortParaMax using
// sentence-boundary-guided sliding windows with overlap.
func splitLongParagraph(para string) []string {
	runes := []rune(para)

	// Collect sentence boundary positions
	var positions []int
	for i, r := range runes {
		if isSentenceEnd(r) {
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) || isSentenceEnd(runes[i+1]) {
				positions = append(positions, i+1) // split AFTER the terminator
			}
		}
	}

	if len(positions) == 0 {
		// No sentence boundaries — use sliding window with overlap
		return splitWithOverlap(runes, maxChunkRunes, overlapRunes)
	}

	// Build natural sentence-grouped chunks
	sentenceChunks := buildSentenceChunks(runes, positions)

	// Merge sentence chunks into final chunks with overlap where possible
	return mergeSentenceChunks(sentenceChunks)
}

// buildSentenceChunks splits runes into sentence-bounded segments.
func buildSentenceChunks(runes []rune, positions []int) [][]rune {
	var chunks [][]rune
	start := 0
	for _, pos := range positions {
		chunk := runes[start:pos]
		if len(bytesTrim(chunk)) > 0 {
			chunks = append(chunks, chunk)
		}
		start = pos
	}
	if start < len(runes) {
		chunk := runes[start:]
		if len(bytesTrim(chunk)) > 0 {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

// mergeSentenceChunks merges sentence-level chunks into final chunks.
// Adjacent chunks that together fit within maxChunkRunes stay together.
// Oversized single-sentence chunks are hard-split with overlap.
func mergeSentenceChunks(sentences [][]rune) []string {
	var out []string
	var buf []rune

	flush := func() {
		if len(buf) > 0 {
			out = append(out, string(buf))
			// Keep last overlapRunes runes as carry-over for next chunk
			if len(buf) > overlapRunes {
				buf = buf[len(buf)-overlapRunes:]
			} else {
				buf = nil
			}
		}
	}

	for _, s := range sentences {
		if len(s) > maxChunkRunes {
			// Flush accumulated buffer first
			flush()
			buf = nil
			// Split this oversized sentence with overlap
			for _, sc := range splitWithOverlap(s, maxChunkRunes, overlapRunes) {
				out = append(out, sc)
			}
			continue
		}
		if len(buf)+len(s) > maxChunkRunes {
			flush()
		}
		buf = append(buf, s...)
	}
	// Final buffer (no overlap carry-over needed for last chunk)
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

// splitWithOverlap splits runes into chunks of maxLen with overlap between adjacent chunks.
func splitWithOverlap(runes []rune, maxLen, overlap int) []string {
	if len(runes) <= maxLen {
		return []string{string(runes)}
	}
	var chunks []string
	step := maxLen - overlap
	for start := 0; start < len(runes); start += step {
		end := start + maxLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// isSentenceEnd reports whether r is a sentence-terminating rune.
func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '.', '!', '?':
		return true
	}
	return false
}

// bytesTrim trims whitespace from a rune slice, returning the trimmed slice.
// Unlike the unicode-based version, this keeps the original rune backing array.
func bytesTrim(runes []rune) []rune {
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	end := len(runes) - 1
	for end >= start && unicode.IsSpace(runes[end]) {
		end--
	}
	if start > end {
		return nil
	}
	return runes[start : end+1]
}
