package tools

func registerTaskBoardTools() {
	// ── Task Board ──
	Register(&ToolDef{
		Name: "taskboard_list", Category: "taskboard",
		Description: "列出任务板中的所有任务（跨对话进度追踪）",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters:   &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "taskboard_add", Category: "taskboard",
		Description: "向任务板添加一个新任务，用于跨对话追踪进度",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"title":       {Type: "string", Description: "任务标题"},
			"description": {Type: "string", Description: "任务描述（可选）"},
			"priority":    {Type: "string", Description: "优先级: P0/P1/P2/P3", Enum: []string{"P0", "P1", "P2", "P3"}},
			"steps":       {Type: "array", Description: "子步骤列表（可选）", Items: &ToolProp{Type: "string"}},
			"dependsOn":   {Type: "array", Description: "依赖任务 ID 列表（可选）", Items: &ToolProp{Type: "string"}},
		}, Required: []string{"title", "priority"}},
	})
	Register(&ToolDef{
		Name: "taskboard_update", Category: "taskboard",
		Description: "更新任务状态、进度或添加备注。完成后使用 status=done",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id":       {Type: "string", Description: "任务 ID"},
			"status":   {Type: "string", Description: "新状态", Enum: []string{"pending", "in_progress", "done", "blocked"}},
			"progress": {Type: "integer", Description: "进度百分比 0-100（可选）"},
			"notes":    {Type: "string", Description: "备注，会追加到已有备注后（可选）"},
		}, Required: []string{"id", "status"}},
	})
	Register(&ToolDef{
		Name: "taskboard_steps", Category: "taskboard",
		Description: "更新任务的子步骤列表",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"id": {Type: "string", Description: "任务 ID"},
			"steps": {Type: "array", Description: "子步骤列表，每项 {text, done}",
				Items: &ToolProp{Type: "object"}},
		}, Required: []string{"id", "steps"}},
	})
}
