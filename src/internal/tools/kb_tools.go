package tools

func registerKBTools() {
	Register(&ToolDef{
		Name:        "kb_list",
		Description: "列出所有知识库，返回 ID、名称、文档数量、创建时间等信息",
		Category:    "kb",
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
	})

	Register(&ToolDef{
		Name:        "kb_create",
		Description: "创建一个新的知识库，绑定指定的嵌入模型（需要在工具箱中可用）。创建成功后即可添加文档",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":      {Type: "string", Description: "知识库名称"},
				"modelDir":  {Type: "string", Description: "嵌入模型的本地目录路径（需包含 ONNX 模型文件）"},
				"libraryId": {Type: "string", Description: "目标领域库 ID（来自 library_list）。不传则使用默认领域"},
			},
			Required: []string{"name", "modelDir"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_set_library",
		Description: "将知识库移动到指定领域库。用于修正知识库归属领域，或在领域间迁移知识库",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId":      {Type: "string", Description: "知识库 ID"},
				"libraryId": {Type: "string", Description: "目标领域库 ID（来自 library_list）。传空字符串则移回默认领域"},
			},
			Required: []string{"kbId", "libraryId"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_add_texts",
		Description: "向知识库中添加文本，会自动分块、嵌入并存储。可附加元数据（键值对）用于后续过滤",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId":     {Type: "string", Description: "知识库 ID"},
				"texts":    {Type: "array", Description: "要添加的文本列表", Items: &ToolProp{Type: "string"}},
				"metadata": {Type: "object", Description: "附加元数据键值对（可选，如 {\"source\": \"文档A\"}）"},
			},
			Required: []string{"kbId", "texts"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_search",
		Description: "在知识库中执行语义搜索，返回按相似度排序的结果，每个结果包含内容片段和相似度分数",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId":   {Type: "string", Description: "知识库 ID"},
				"query":  {Type: "string", Description: "搜索查询文本"},
				"k":      {Type: "integer", Description: "返回结果数量（默认 5）"},
				"filter": {Type: "object", Description: "元数据过滤条件（可选，如 {\"source\": \"文档A\"}）"},
			},
			Required: []string{"kbId", "query"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_delete",
		Description: "删除整个知识库及其所有数据，不可恢复",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId": {Type: "string", Description: "知识库 ID"},
			},
			Required: []string{"kbId"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_clear",
		Description: "清空知识库中的所有文档，但保留知识库本身的配置和模型绑定",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId": {Type: "string", Description: "知识库 ID"},
			},
			Required: []string{"kbId"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_list_docs",
		Description: "列出知识库中所有文档的摘要信息（ID、内容预览、元数据等）",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId": {Type: "string", Description: "知识库 ID"},
			},
			Required: []string{"kbId"},
		},
	})

	Register(&ToolDef{
		Name:        "kb_delete_chunks",
		Description: "按文档 ID 列表删除知识库中的指定文档",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"kbId": {Type: "string", Description: "知识库 ID"},
				"ids":  {Type: "array", Description: "要删除的文档 ID 列表", Items: &ToolProp{Type: "string"}},
			},
			Required: []string{"kbId", "ids"},
		},
	})

	// read_file — read a saved chat upload file from disk.
	Register(&ToolDef{
		Name:        "read_file",
		Description: "读取用户上传到聊天中的文件内容。支持 PDF（文本提取）、TXT、MD、CSV、JSON、XML 等格式。对于扫描件/图片型 PDF，会返回 isScanned=true（此时请使用 read_media_file 以图片形式查看）。",
		Category:    "kb",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"path":   {Type: "string", Description: "文件的绝对路径"},
					"offset": {Type: "integer", Description: "起始行号（0=文件开头），用于分段读取"},
					"limit":  {Type: "integer", Description: "最大读取行数（默认2000），用于分段读取"},
			},
			Required: []string{"path"},
		},
	})

	// read_media_file — read an image or render a scanned PDF page as base64.
	Register(&ToolDef{
		Name:        "read_media_file",
		Description: "读取用户上传的图片文件（PNG、JPG、GIF、BMP、WebP、SVG）或扫描件 PDF，返回 base64 编码的图片数据。用于视觉模型分析图片内容。当用户上传截图、照片、扫描件时，优先使用此工具查看其内容。",
		Category:    "kb",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"path": {Type: "string", Description: "图片文件的绝对路径，或扫描件 PDF 的路径"},
			},
			Required: []string{"path"},
		},
	})
}
