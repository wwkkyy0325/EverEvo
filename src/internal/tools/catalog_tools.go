package tools

func registerCatalogTools() {
	Register(&ToolDef{
		Name:        "catalog_search",
		Description: "在 HuggingFace 和 ModelScope 模型市场中搜索模型。返回匹配的模型列表，包含名称、任务类型、下载量、作者等信息",
		Category:    "catalog",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"query": {Type: "string", Description: "搜索关键词（如 'bert', 'whisper', 'llama' 等）"},
			},
			Required: []string{"query"},
		},
	})

	Register(&ToolDef{
		Name:        "catalog_get_detail",
		Description: "获取指定模型的详细信息：名称、描述、作者、任务类型、文件列表、文件树结构",
		Category:    "catalog",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"source": {Type: "string", Description: "模型来源平台", Enum: []string{"huggingface", "modelscope"}},
				"repoId": {Type: "string", Description: "仓库 ID（如 'sentence-transformers/all-MiniLM-L6-v2'）"},
			},
			Required: []string{"source", "repoId"},
		},
	})

	Register(&ToolDef{
		Name:        "catalog_list_files",
		Description: "列出模型仓库中的所有文件及大小信息",
		Category:    "catalog",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"source": {Type: "string", Description: "模型来源平台", Enum: []string{"huggingface", "modelscope"}},
				"repoId": {Type: "string", Description: "仓库 ID"},
			},
			Required: []string{"source", "repoId"},
		},
	})

}
