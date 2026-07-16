package tools

func registerAppControlTools() {
	// ── Memory ──
	Register(&ToolDef{
		Name: "memory_list", Category: "memory",
		Description: "列出长期记忆条目（问答对和提取的事实），支持分页",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"limit": {Type: "integer", Description: "返回数量，默认 20"},
		}},
	})
	Register(&ToolDef{
		Name: "memory_search", Category: "memory",
		Description: "在长期记忆中语义搜索相关条目（需要嵌入模型）",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"query": {Type: "string", Description: "搜索查询"},
			"k":     {Type: "integer", Description: "返回数量，默认 5"},
		}, Required: []string{"query"}},
	})
	Register(&ToolDef{
		Name: "memory_delete", Category: "memory",
		Description: "删除指定的长期记忆条目",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "记忆条目 ID"},
		}, Required: []string{"id"}},
	})
	Register(&ToolDef{
		Name: "memory_add_fact", Category: "memory",
		Description: "手动添加一条事实到长期记忆（用户偏好、身份信息等）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"content":  {Type: "string", Description: "事实内容"},
			"category": {Type: "string", Description: "分类: preference|fact|event|relationship"},
		}, Required: []string{"content", "category"}},
	})
	Register(&ToolDef{
		Name: "memory_clear", Category: "memory",
		Description: "清空所有长期记忆（不可恢复）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})

	// ── Core Facts ──
	Register(&ToolDef{
		Name: "core_list", Category: "memory",
		Description: "列出核心记忆（永久身份/偏好，不衰减）",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "core_add", Category: "memory",
		Description: "添加一条核心记忆",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"key":      {Type: "string", Description: "键（如 偏好/身份）"},
			"value":    {Type: "string", Description: "值"},
			"category": {Type: "string", Description: "分类标签"},
		}, Required: []string{"value"}},
	})
	Register(&ToolDef{
		Name: "core_delete", Category: "memory",
		Description: "删除一条核心记忆",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "核心记忆 ID"},
		}, Required: []string{"id"}},
	})

	// ── Sessions ──
	Register(&ToolDef{
		Name: "session_list", Category: "memory",
		Description: "列出所有对话会话",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "session_delete", Category: "memory",
		Description: "删除指定对话会话及其所有消息",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "会话 ID"},
		}, Required: []string{"id"}},
	})

	// ── Knowledge Graph ──
	Register(&ToolDef{
		Name: "graph_migrate", Category: "memory",
		Description: "将历史图谱数据从默认领域迁移到对应领域（基于 Wiki 页面标题、KB 名称匹配）。在创建新领域后调用，让已有节点关联到正确领域",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "graph_rebuild_from_domain", Category: "memory",
		Description: "从指定领域的 KB 文档和 Wiki 页面重建知识图谱（含向量嵌入）。无需重新导入文件——在修复图谱逻辑后运行，一步补齐所有缺失的实体和关系",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"libraryId": {Type: "string", Description: "目标领域库 ID"},
		}, Required: []string{"libraryId"}},
	})
	Register(&ToolDef{
		Name: "graph_list", Category: "memory",
		Description: "列出知识图谱的实体节点和关系",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"search":    {Type: "string", Description: "搜索关键词（可选）"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}},
	})
	Register(&ToolDef{
		Name: "graph_add_edge", Category: "memory",
		Description: "在知识图谱中添加一条关系（自动创建不存在的实体）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"srcName":   {Type: "string", Description: "源实体名"},
			"dstName":   {Type: "string", Description: "目标实体名"},
			"type":      {Type: "string", Description: "关系类型（如 likes/uses/owns）"},
			"replaces":  {Type: "boolean", Description: "是否取代旧关系（true=改用/false=共存）"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}, Required: []string{"srcName", "dstName", "type"}},
	})
	Register(&ToolDef{
		Name: "graph_delete_node", Category: "memory",
		Description: "删除知识图谱中的一个实体及其所有关系",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id":        {Type: "string", Description: "实体 ID"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选）"},
		}, Required: []string{"id"}},
	})
	Register(&ToolDef{
		Name: "graph_rename_node", Category: "memory",
		Description: "重命名知识图谱中的一个实体",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id":        {Type: "string", Description: "实体 ID"},
			"name":      {Type: "string", Description: "新名称"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选）"},
		}, Required: []string{"id", "name"}},
	})

	// ── Wiki ──
	Register(&ToolDef{
		Name: "wiki_list", Category: "kb",
		Description: "列出指定领域的 wiki 页面",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}},
	})
	Register(&ToolDef{
		Name: "wiki_read", Category: "kb",
		Description: "读取 wiki 页面的完整内容",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"pageId":    {Type: "string", Description: "页面 ID"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}, Required: []string{"pageId"}},
	})
	Register(&ToolDef{
		Name: "wiki_save", Category: "kb",
		Description: "创建或更新 wiki 页面（Markdown 格式）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"pageId":    {Type: "string", Description: "页面 ID（新页面会自动生成）"},
			"title":     {Type: "string", Description: "页面标题"},
			"content":   {Type: "string", Description: "Markdown 内容"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}, Required: []string{"title", "content"}},
	})
	Register(&ToolDef{
		Name: "wiki_delete", Category: "kb",
		Description: "删除 wiki 页面",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"pageId":    {Type: "string", Description: "页面 ID"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}, Required: []string{"pageId"}},
	})
	Register(&ToolDef{
		Name: "wiki_search", Category: "kb",
		Description: "语义搜索 wiki 文档",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"query":     {Type: "string", Description: "搜索查询"},
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}, Required: []string{"query"}},
	})
	Register(&ToolDef{
		Name: "wiki_move", Category: "kb",
		Description: "将 wiki 页面从一个领域库移动到另一个领域库（读取源→写入目标→可选清理源）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"fromLibraryId": {Type: "string", Description: "源领域库 ID"},
			"toLibraryId":   {Type: "string", Description: "目标领域库 ID"},
			"pageId":        {Type: "string", Description: "页面 ID"},
			"deleteSource":  {Type: "boolean", Description: "是否删除源页面（默认 false）"},
		}, Required: []string{"fromLibraryId", "toLibraryId", "pageId"}},
	})
	Register(&ToolDef{
		Name: "wiki_reindex", Category: "kb",
		Description: "重建 wiki 索引（重新嵌入所有页面）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"libraryId": {Type: "string", Description: "领域库 ID（可选，不传则使用默认领域）"},
		}},
	})

	// ── Experience ──
	Register(&ToolDef{
		Name: "experience_list", Category: "memory",
		Description: "列出经验教训（反思蒸馏的洞察）",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"limit": {Type: "integer", Description: "返回数量，默认 10"},
		}},
	})
	Register(&ToolDef{
		Name: "experience_delete", Category: "memory",
		Description: "删除一条经验教训",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "经验 ID"},
		}, Required: []string{"id"}},
	})

	// ── Domain Libraries ──
	Register(&ToolDef{
		Name: "library_list", Category: "memory",
		Description: "列出所有领域库",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "library_create", Category: "memory",
		Description: "创建新的领域库",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"name":        {Type: "string", Description: "领域名称"},
			"description": {Type: "string", Description: "领域描述"},
		}, Required: []string{"name"}},
	})
	Register(&ToolDef{
		Name: "library_delete", Category: "memory",
		Description: "删除领域库",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "领域 ID"},
		}, Required: []string{"id"}},
	})

	// ── File write ──
	Register(&ToolDef{
		Name: "write_file", Category: "kb",
		Description: "写入内容到文件（创建或覆盖）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"path":    {Type: "string", Description: "文件绝对路径"},
			"content": {Type: "string", Description: "文件内容"},
		}, Required: []string{"path", "content"}},
	})
	Register(&ToolDef{
		Name: "list_directory", Category: "kb",
		Description: "列出目录中的文件和子目录",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"path": {Type: "string", Description: "目录绝对路径"},
		}, Required: []string{"path"}},
	})
}
