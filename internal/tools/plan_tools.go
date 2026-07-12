package tools

// registerPlanTools exposes the AI task-planning primitives. An agent
// decomposes a goal into ordered steps, marks each in_progress/done as it
// works, and the UI reflects progress in real time via the event bus.
func registerPlanTools() {
	Register(&ToolDef{
		Name: "plan_create", Category: "system",
		Description: "创建一个任务计划：把目标拆解为有序步骤清单。创建后面板实时显示进度，用 plan_step_update 逐步推进",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"goal":  {Type: "string", Description: "计划目标（要完成什么）"},
			"steps": {Type: "array", Description: "有序步骤标题列表", Items: &ToolProp{Type: "string"}},
		}, Required: []string{"goal", "steps"}},
	})
	Register(&ToolDef{
		Name: "plan_step_update", Category: "system",
		Description: "更新计划中某一步的状态：pending | in_progress | done | skipped。完成一步就标记 done，面板会打勾",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"planId":    {Type: "string", Description: "计划 ID（plan_create 返回）"},
			"stepIndex": {Type: "integer", Description: "步骤序号（从 0 开始）"},
			"status":    {Type: "string", Description: "pending | in_progress | done | skipped"},
			"note":      {Type: "string", Description: "可选备注（结果/说明）"},
		}, Required: []string{"planId", "stepIndex", "status"}},
	})
	Register(&ToolDef{
		Name: "plan_list", Category: "system",
		Description: "列出当前所有计划及其步骤状态",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters:   &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
}
