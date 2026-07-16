package tools

func registerWorkflowTools() {
	Register(&ToolDef{
		Name:        "workflow_list",
		Title:       "列出所有工作流",
		Description: "列出所有已保存的工作流，返回 ID、名称、描述、节点数量和更新时间",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "workflow_get",
		Title:       "获取工作流详情",
		Description: "获取指定工作流的完整定义，包括所有节点、连线、配置和变量",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "工作流 ID（来自 workflow_list）"},
			},
			Required: []string{"id"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "workflow_create",
		Title:       "创建工作流",
		Description: "创建一个新的工作流。工作流由节点（LLM调用、工具调用、条件分支、循环等）和连线组成，按拓扑顺序执行",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":        {Type: "string", Description: "工作流名称"},
				"description": {Type: "string", Description: "工作流描述（可选）"},
			},
			Required: []string{"name"},
		},
	})

	Register(&ToolDef{
		Name:        "workflow_update",
		Title:       "更新工作流",
		Description: "更新一个已有工作流的定义。传入完整的工作流 JSON（包含 nodes、edges 等字段）",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id":         {Type: "string", Description: "工作流 ID"},
				"name":       {Type: "string", Description: "工作流名称"},
				"nodes":      {Type: "array", Description: "节点数组，每个节点包含 id、type、title、config"},
				"edges":      {Type: "array", Description: "连线数组，每条连线包含 source、target、sourceHandle"},
				"description": {Type: "string", Description: "描述（可选）"},
			},
			Required: []string{"id"},
		},
	})

	Register(&ToolDef{
		Name:        "workflow_delete",
		Title:       "删除工作流",
		Description: "删除指定 ID 的工作流，此操作不可撤销",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "要删除的工作流 ID"},
			},
			Required: []string{"id"},
		},
		Annotations: &ToolAnnotations{DestructiveHint: true},
	})

	Register(&ToolDef{
		Name:        "workflow_execute",
		Title:       "执行工作流",
		Description: "运行一个工作流并等待执行完成，返回最终结果（status/outputs/error）。工作流按拓扑顺序执行各节点；长工作流会阻塞直到完成或超时",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id":      {Type: "string", Description: "要执行的工作流 ID"},
				"inputs":  {Type: "object", Description: "输入参数（JSON 对象），对应工作流的输入节点字段"},
				"timeout": {Type: "number", Description: "最长等待秒数（可选，默认 600）"},
			},
			Required: []string{"id"},
		},
	})

	Register(&ToolDef{
		Name:        "workflow_status",
		Title:       "查看执行状态",
		Description: "查询工作流执行的当前状态和结果。返回各节点的运行状态（pending/running/done/error/skipped）和输出数据",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"execId": {Type: "string", Description: "执行 ID（来自 workflow_execute 的返回值）"},
			},
			Required: []string{"execId"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "workflow_validate",
		Title:       "验证工作流",
		Description: "检查工作流定义是否有效，返回检查报告（是否缺少输入/输出节点、连线是否完整、是否有循环引用等）",
		Category:    "workflow",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "要验证的工作流 ID"},
			},
			Required: []string{"id"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})
}
