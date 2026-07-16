package tools

func registerDownloadTools() {
	Register(&ToolDef{
		Name:        "download_file",
		Description: "从模型市场下载单个文件到本地模型库。返回下载任务 ID，可通过 download-progress 事件追踪进度",
		Category:    "download",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"source":   {Type: "string", Description: "模型来源平台", Enum: []string{"huggingface", "modelscope"}},
				"repoId":   {Type: "string", Description: "仓库 ID"},
				"filename": {Type: "string", Description: "具体文件名（如 'model.onnx' 或 'onnx/model.onnx'）"},
			},
			Required: []string{"source", "repoId", "filename"},
		},
	})

	Register(&ToolDef{
		Name:        "download_package",
		Description: "一键下载模型仓库中的所有文件到本地模型库",
		Category:    "download",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"source": {Type: "string", Description: "模型来源平台", Enum: []string{"huggingface", "modelscope"}},
				"repoId": {Type: "string", Description: "仓库 ID"},
			},
			Required: []string{"source", "repoId"},
		},
	})

	Register(&ToolDef{
		Name:        "download_selected",
		Description: "下载用户指定的文件列表到本地模型库",
		Category:    "download",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"source": {Type: "string", Description: "模型来源平台", Enum: []string{"huggingface", "modelscope"}},
				"repoId": {Type: "string", Description: "仓库 ID"},
				"files":  {Type: "array", Description: "要下载的文件路径列表", Items: &ToolProp{Type: "string"}},
			},
			Required: []string{"source", "repoId", "files"},
		},
	})

	Register(&ToolDef{
		Name:        "download_delete_file",
		Description: "删除一个已下载的模型文件",
		Category:    "download",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"relPath": {Type: "string", Description: "要删除的文件相对路径（相对于模型目录）"},
			},
			Required: []string{"relPath"},
		},
	})

	Register(&ToolDef{
		Name:        "download_delete_dir",
		Description: "删除整个已下载的模型目录及其所有内容",
		Category:    "download",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"dirName": {Type: "string", Description: "要删除的目录名（模型目录下的子目录名）"},
			},
			Required: []string{"dirName"},
		},
	})
}
