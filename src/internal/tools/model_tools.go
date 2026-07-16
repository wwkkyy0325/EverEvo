package tools

func registerModelTools() {
	Register(&ToolDef{
		Name: "model_list", Title: "列出已加载模型",
		Description: "列出当前所有已加载到内存中的模型，返回模型 ID、名称、类型、状态（idle/ready/running/error）等信息",
		Category:    "model",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
	})

	Register(&ToolDef{
		Name: "model_load", Title: "加载模型",
		Description: "加载一个模型文件到内存中，自动识别模型类型（ONNX/GGUF/SafeTensors/PyTorch）。加载后可以进行推理",
		Category:    "model",
		Annotations: &ToolAnnotations{IdempotentHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id":        {Type: "string", Description: "模型的唯一标识符，用于后续引用"},
				"name":      {Type: "string", Description: "模型的显示名称"},
				"modelPath": {Type: "string", Description: "模型文件在磁盘上的绝对路径"},
			},
			Required: []string{"id", "name", "modelPath"},
		},
	})

	Register(&ToolDef{
		Name: "model_unload", Title: "卸载模型",
		Description: "从内存中卸载一个已加载的模型，释放资源",
		Category:    "model",
		Annotations: &ToolAnnotations{DestructiveHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "要卸载的模型 ID"},
			},
			Required: []string{"id"},
		},
	})

	Register(&ToolDef{
		Name: "model_run", Title: "运行模型推理",
		Description: "对已加载的模型执行推理。输入可以是文本（句向量模型）或 base64 编码的图片（图像分类模型）。返回模型输出的原始字符串",
		Category:    "model",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id":    {Type: "string", Description: "已加载的模型 ID"},
				"input": {Type: "string", Description: "推理输入：文本字符串或 base64 编码的图片数据"},
			},
			Required: []string{"id", "input"},
		},
	})

	Register(&ToolDef{
		Name: "model_list_downloaded", Title: "列出已下载模型",
		Description: "列出所有已下载到本地的模型文件，包括文件名、路径、大小和扩展名",
		Category:    "model",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
	})

	Register(&ToolDef{
		Name: "model_list_tool", Title: "列出可探测模型",
		Description: "列出已下载且可自动探测类型的模型（句向量等），返回模型名称、类型和路径",
		Category:    "model",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
	})

	Register(&ToolDef{
		Name: "model_embed_texts", Title: "文本嵌入",
		Description: "用句向量（sentence-embedding）模型将多段文本编码为嵌入向量。可用于计算文本相似度或语义搜索",
		Category:    "model",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"modelDir": {Type: "string", Description: "句向量模型的本地目录路径"},
				"texts":    {Type: "array", Description: "要编码的文本列表", Items: &ToolProp{Type: "string"}},
			},
			Required: []string{"modelDir", "texts"},
		},
	})
}
