// Package wiki provides wiki knowledge base tools as a self-registering ToolPlugin.
//
// Tools: wiki_search, wiki_recall, wiki_save_page, wiki_read_page, wiki_list_pages.
package wiki

import (
	"context"
	"fmt"

	"everevo/internal/core"
)

const pluginID = "wiki"

// ── Delegate interface ──────────────────────────────────────────────────

// WikiDelegate is implemented by the App to provide wiki operations.
type WikiDelegate interface {
	// WikiSearch returns raw chunks for a query within a domain library.
	WikiSearch(libraryID, query string) ([]Chunk, error)
	// WikiRecall returns a formatted text block of the top wiki hits.
	WikiRecall(libraryID, query string) (string, error)
	// WikiSavePage creates or updates a user wiki page.
	WikiSavePage(libraryID, pageID, title, content string) error
	// WikiReadPage reads a wiki page by ID.
	WikiReadPage(libraryID, pageID string) (map[string]any, error)
	// WikiListPages returns all indexed wiki pages for browsing.
	WikiListPages(libraryID string) ([]WikiPageInfo, error)
}

// Chunk is a wiki search result chunk.
type Chunk struct {
	Page    string  `json:"page"`
	Heading string  `json:"heading,omitempty"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
}

// WikiPageInfo is a lightweight page descriptor.
type WikiPageInfo struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Path  string `json:"path,omitempty"`
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin implements core.ToolPlugin for wiki operations.
type Plugin struct {
	delegate WikiDelegate
}

var _ core.ToolPlugin = (*Plugin)(nil)

// SetWikiDelegate wires the App-level delegate. Called once at startup.
func SetWikiDelegate(d WikiDelegate) {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return
	}
	if plug, ok := p.(*Plugin); ok {
		plug.delegate = d
	}
}

func init() {
	core.GlobalTools.Register(pluginID, &Plugin{}, core.PluginManifest{
		ID: pluginID, Name: "Wiki 知识库", Version: "1.0",
		Description: "wiki_search, wiki_recall, wiki_save_page, wiki_read_page, wiki_list_pages",
		Author: "EverEvo", Type: "toolset",
	})
}

func (p *Plugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID: pluginID, Name: "Wiki 知识库", Version: "1.0",
		Description: "Wiki knowledge base operations",
		Author: "EverEvo", Type: "toolset",
	}
}

func (p *Plugin) ToolDefs() []core.ToolDef {
	return []core.ToolDef{
		{
			Name:        "wiki_search",
			Description: "在 Wiki 知识库中搜索相关文档块。返回匹配的页面、标题和内容片段",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "搜索查询"},
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选，默认使用当前领域库）"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "wiki_recall",
			Description: "从 Wiki 知识库中召回与查询最相关的文档摘要（格式化文本），适合直接放入对话上下文",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "搜索查询"},
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选）"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "wiki_save_page",
			Description: "创建或更新 Wiki 页面。会自动分块和索引内容以提高搜索质量",
			ReadOnly:    false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pageId":    map[string]any{"type": "string", "description": "页面 ID（可选，不填则使用 title 作为 ID）"},
					"title":     map[string]any{"type": "string", "description": "页面标题"},
					"content":   map[string]any{"type": "string", "description": "页面内容（Markdown 格式）"},
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选）"},
				},
				"required": []string{"title", "content"},
			},
		},
		{
			Name:        "wiki_read_page",
			Description: "读取 Wiki 页面的完整内容",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pageId":    map[string]any{"type": "string", "description": "页面 ID"},
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选）"},
				},
				"required": []string{"pageId"},
			},
		},
		{
			Name:        "wiki_list_pages",
			Description: "列出 Wiki 中所有已索引的页面",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选）"},
				},
			},
		},
	}
}

// CallTool dispatches a wiki tool call to the delegate.
func (p *Plugin) CallTool(_ context.Context, name string, args map[string]any) (core.ToolResult, error) {
	if p.delegate == nil {
		return core.ToolResult{Success: false, Error: fmt.Sprintf("wiki/%s: delegate not wired", name)}, nil
	}

	switch name {
	case "wiki_search":
		return p.callWikiSearch(args)
	case "wiki_recall":
		return p.callWikiRecall(args)
	case "wiki_save_page":
		return p.callWikiSavePage(args)
	case "wiki_read_page":
		return p.callWikiReadPage(args)
	case "wiki_list_pages":
		return p.callWikiListPages(args)
	default:
		return core.ToolResult{Success: false, Error: fmt.Sprintf("wiki: unknown tool %q", name)}, nil
	}
}

// ── Tool implementations ────────────────────────────────────────────────

func (p *Plugin) callWikiSearch(args map[string]any) (core.ToolResult, error) {
	query := getStr(args, "query")
	if query == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: query"}, nil
	}
	libraryID := getStr(args, "libraryId")
	hits, err := p.delegate.WikiSearch(libraryID, query)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	if hits == nil {
		hits = []Chunk{}
	}
	return core.ToolResult{Success: true, Data: hits}, nil
}

func (p *Plugin) callWikiRecall(args map[string]any) (core.ToolResult, error) {
	query := getStr(args, "query")
	if query == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: query"}, nil
	}
	libraryID := getStr(args, "libraryId")
	text, err := p.delegate.WikiRecall(libraryID, query)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: map[string]string{"text": text}}, nil
}

func (p *Plugin) callWikiSavePage(args map[string]any) (core.ToolResult, error) {
	title := getStr(args, "title")
	content := getStr(args, "content")
	if title == "" || content == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: title, content"}, nil
	}
	pageID := getStr(args, "pageId")
	if pageID == "" {
		pageID = title
	}
	libraryID := getStr(args, "libraryId")
	if err := p.delegate.WikiSavePage(libraryID, pageID, title, content); err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: map[string]string{"pageId": pageID, "status": "saved"}}, nil
}

func (p *Plugin) callWikiReadPage(args map[string]any) (core.ToolResult, error) {
	pageID := getStr(args, "pageId")
	if pageID == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: pageId"}, nil
	}
	libraryID := getStr(args, "libraryId")
	content, err := p.delegate.WikiReadPage(libraryID, pageID)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: content}, nil
}

func (p *Plugin) callWikiListPages(args map[string]any) (core.ToolResult, error) {
	libraryID := getStr(args, "libraryId")
	pages, err := p.delegate.WikiListPages(libraryID)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	if pages == nil {
		pages = []WikiPageInfo{}
	}
	return core.ToolResult{Success: true, Data: pages}, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func getStr(args map[string]any, key string) string {
	v, _ := args[key]
	s, _ := v.(string)
	return s
}
