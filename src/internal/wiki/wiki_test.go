package wiki

import "testing"

func TestResolveLink(t *testing.T) {
	cases := map[string]string{
		"tasks/memory-graph.md":    "memory-graph",
		"design.md":                "design",
		"internal/rag/store.go":    "",
		"frontend/src/chatStore.ts":"",
		"app_workflow.go":           "",
		"https://example.com":      "",
		"readme.md":                 "readme",
	}
	for in, want := range cases {
		got := resolveLink(in)
		if got != want {
			t.Errorf("resolveLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMarkdown(t *testing.T) {
	src := "# 标题\n\n一段文字。\n\n## 子标题\n\n另一段 [链接](tasks/memory-graph.md)。\n"
	chunks, links := ParseMarkdown("test", src)
	if len(chunks) != 2 {
		t.Errorf("chunks: want 2, got %d", len(chunks))
	}
	if len(links) != 1 || links[0].To != "memory-graph" {
		t.Errorf("links: want [{test, memory-graph}], got %v", links)
	}
	if chunks[0].Heading != "标题" && chunks[0].Heading != "" {
		t.Errorf("chunk[0] heading: want 标题 or \"\", got %q", chunks[0].Heading)
	}
	if chunks[1].Heading != "子标题" {
		t.Errorf("chunk[1] heading: want 子标题, got %q", chunks[1].Heading)
	}
}
