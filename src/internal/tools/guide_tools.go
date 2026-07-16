package tools

func registerGuideTools() {
	Register(&ToolDef{
		Name:        "guide_list",
		Title:       "列出所有攻略",
		Description: "列出所有已同步的攻略文档，返回 ID、标题、来源、大小、更新时间。可按关键词搜索过滤",
		Category:    "guide",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"query": {Type: "string", Description: "可选的搜索关键词，匹配标题和文件名"},
			},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "guide_read",
		Title:       "阅读攻略内容",
		Description: "读取指定攻略文档的完整 Markdown 内容。传入 guide_list 返回的 ID 即可获取全文",
		Category:    "guide",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "攻略文档 ID（来自 guide_list）"},
			},
			Required: []string{"id"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "guide_search",
		Title:       "搜索攻略",
		Description: "在攻略文档的标题和文件内容中搜索关键词，返回匹配的文档列表及摘要摘录",
		Category:    "guide",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"query": {Type: "string", Description: "搜索关键词"},
			},
			Required: []string{"query"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "guide_sync",
		Title:       "同步攻略",
		Description: "从配置的远程来源（Gitee/GitHub 仓库或 URL）同步最新的攻略文档到本地",
		Category:    "guide",
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
		Annotations: &ToolAnnotations{IdempotentHint: true},
	})

	Register(&ToolDef{
		Name:        "guide_sources",
		Title:       "查看攻略来源",
		Description: "列出所有已配置的攻略文档来源（Gitee 仓库、GitHub 仓库、URL 等）及启用状态",
		Category:    "guide",
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})
}
