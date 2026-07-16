package mcp

// builtinPrompts defines all available prompt templates.
var builtinPrompts = []PromptDef{
	{
		Name:        "search-models",
		Title:       "搜索模型",
		Description: "在 HuggingFace 和 ModelScope 模型市场中搜索模型",
		Arguments: []PromptArgument{
			{Name: "query", Title: "搜索关键词", Description: "要搜索的模型名称或关键词（如 bert, whisper, llama）", Required: true},
		},
	},
	{
		Name:        "download-model",
		Title:       "下载模型",
		Description: "从模型市场下载模型到本地",
		Arguments: []PromptArgument{
			{Name: "source", Title: "平台", Description: "模型来源: huggingface 或 modelscope", Required: true},
			{Name: "repoId", Title: "仓库 ID", Description: "模型仓库 ID，如 sentence-transformers/all-MiniLM-L6-v2", Required: true},
		},
	},
	{
		Name:        "analyze-image",
		Title:       "图像分类分析",
		Description: "使用已下载的图像分类模型分析图片内容",
		Arguments: []PromptArgument{
			{Name: "modelPath", Title: "模型路径", Description: "图像分类模型的本地目录路径", Required: true},
		},
	},
	{
		Name:        "create-kb",
		Title:       "创建知识库",
		Description: "创建一个新的知识库，绑定句向量模型用于语义搜索",
		Arguments: []PromptArgument{
			{Name: "name", Title: "知识库名称", Description: "新知识库的名称", Required: true},
			{Name: "modelDir", Title: "模型目录", Description: "句向量模型的本地目录路径", Required: true},
		},
	},
	{
		Name:        "semantic-search",
		Title:       "语义搜索知识库",
		Description: "在知识库中执行语义搜索，返回相关文档片段",
		Arguments: []PromptArgument{
			{Name: "kbId", Title: "知识库 ID", Description: "目标知识库的 ID", Required: true},
			{Name: "query", Title: "查询", Description: "搜索查询文本", Required: true},
		},
	},
	{
		Name:        "manage-plugin",
		Title:       "管理插件",
		Description: "启动、停止、查看或调用插件",
		Arguments: []PromptArgument{
			{Name: "name", Title: "插件名称", Description: "要管理的插件名称", Required: true},
		},
	},
}

// HandlePromptsList returns all available prompts.
func HandlePromptsList() (*ListPromptsResult, error) {
	return &ListPromptsResult{Prompts: builtinPrompts}, nil
}

// HandlePromptsGet returns a specific prompt rendered with arguments.
func HandlePromptsGet(name string, args map[string]string) (*GetPromptResult, error) {
	var prompt *PromptDef
	for i := range builtinPrompts {
		if builtinPrompts[i].Name == name {
			prompt = &builtinPrompts[i]
			break
		}
	}
	if prompt == nil {
		return nil, nil // will be returned as protocol error
	}

	var messages []PromptMessage
	switch name {
	case "search-models":
		query := args["query"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("请帮我在模型市场搜索「" + query + "」，列出找到的模型及其简要信息。然后我可以帮你下载感兴趣的模型。")},
		}
	case "download-model":
		source := args["source"]
		repoID := args["repoId"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("请帮我从 " + source + " 下载模型 " + repoID + "。先查看模型详情了解文件结构，然后选择主要的模型文件进行下载。")},
		}
	case "analyze-image":
		modelPath := args["modelPath"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("我有一个图像分类模型在 " + modelPath + "。请帮我加载这个模型，然后我会上传一张图片让你分析。")},
		}
	case "create-kb":
		name := args["name"]
		modelDir := args["modelDir"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("请帮我创建一个名为「" + name + "」的知识库，使用 " + modelDir + " 中的嵌入模型。创建成功后告诉我知识库 ID。")},
		}
	case "semantic-search":
		kbID := args["kbId"]
		query := args["query"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("请在知识库 " + kbID + " 中搜索：「" + query + "」。返回最相关的 5 个结果，并解释它们与查询的关联。")},
		}
	case "manage-plugin":
		name := args["name"]
		messages = []PromptMessage{
			{Role: "user", Content: TextContent("请帮我管理插件「" + name + "」。先查看它的状态，如果没运行就启动它，然后列出它支持的方法。")},
		}
	}

	return &GetPromptResult{
		Messages:    messages,
		Description: prompt.Description,
	}, nil
}
