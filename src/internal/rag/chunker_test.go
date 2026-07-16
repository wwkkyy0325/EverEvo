//go:build windows

package rag

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkText_Empty(t *testing.T) {
	if got := ChunkText(""); len(got) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(got))
	}
}

func TestChunkText_SingleParagraph(t *testing.T) {
	chunks := ChunkText("hello world")
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Fatalf("unexpected: %v", chunks)
	}
}

func TestChunkText_MultipleParagraphs(t *testing.T) {
	text := "first paragraph\n\nsecond paragraph\n\nthird paragraph"
	chunks := ChunkText(text)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestChunkText_ChineseParagraphs(t *testing.T) {
	text := "这是第一段。\n\n这是第二段。\n\n这是第三段。"
	chunks := ChunkText(text)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestChunkText_LongParagraph(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("这是一段测试文本。")
	}
	chunks := ChunkText(sb.String())
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for long text, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) == 0 {
			t.Fatal("got empty chunk")
		}
	}
}

func TestChunkText_ShortParagraphPreserved(t *testing.T) {
	// Build a paragraph just under shortParaMax — it should stay as one chunk.
	var sb strings.Builder
	for sb.Len() < shortParaMax-50 {
		sb.WriteString("短段落文本。")
	}
	para := sb.String()
	rlen := utf8.RuneCountInString(para)
	if rlen > shortParaMax {
		t.Skipf("test paragraph too long: %d runes", rlen)
	}
	chunks := ChunkText(para)
	if len(chunks) != 1 {
		t.Fatalf("short paragraph should stay as 1 chunk, got %d", len(chunks))
	}
}

func TestChunkText_OverlapBetweenChunks(t *testing.T) {
	// A very long sentence-less paragraph must be split with overlap.
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("abcdefghij") // 10 runes each, no sentence boundaries
	}
	chunks := ChunkText(sb.String())
	if len(chunks) < 3 {
		t.Fatalf("expected >=3 chunks for long text without sentence boundaries, got %d", len(chunks))
	}
	// Verify adjacent chunks share overlap content (last runes of chunk N
	// appear at the start of chunk N+1).
	for i := 0; i < len(chunks)-1; i++ {
		a := []rune(chunks[i])
		b := []rune(chunks[i+1])
		if len(a) < overlapRunes || len(b) < overlapRunes {
			continue // edge case for final chunk
		}
		overlapA := string(a[len(a)-overlapRunes:])
		overlapB := string(b[:overlapRunes])
		if overlapA != overlapB {
			t.Fatalf("chunk %d→%d missing overlap:\n  tail[%d]: %q\n  head[%d]: %q",
				i, i+1, len(a), overlapA, len(b), overlapB)
		}
	}
}

func TestChunkText_NoSentenceBoundary(t *testing.T) {
	// Text with no sentence terminators — should still be chunked by hard split.
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("abc_def_ghi_") // no .!? etc.
	}
	chunks := ChunkText(sb.String())
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for long unsplittable text, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) == 0 {
			t.Fatal("got empty chunk")
		}
	}
}

func TestChunkText_WhitespaceOnly(t *testing.T) {
	chunks := ChunkText("   \n\n  \n  ")
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for whitespace-only, got %d", len(chunks))
	}
}

func TestChunkText_MixedNewlines(t *testing.T) {
	text := "a\r\n\r\nb\n\nc"
	chunks := ChunkText(text)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
}
