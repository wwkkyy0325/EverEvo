//go:build windows

package tools

// registerParadigmTools registers 思维范式 (thinking paradigm) tools for
// the SELECT-ADAPT-EXECUTE pipeline. The LLM can: list paradigms, match a
// paradigm to a task, select/load a specific paradigm's methodology, and
// submit feedback on paradigm effectiveness.
func registerParadigmTools() {
	// ── paradigm_match ──
	Register(&ToolDef{
		Name:        "paradigm_match",
		Title:       "匹配思维范式",
		Description: "根据当前任务描述匹配最合适的思维范式（思考方法）。返回按成功率排序的推荐范式列表。应在开始复杂任务前调用此工具以选择最佳思考策略。",
		Category:    "system",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"task": {Type: "string", Description: "当前任务描述（一句话概括你要做什么）"},
				"count": {Type: "number", Description: "返回前 N 个推荐范式，默认 3"},
			},
			Required: []string{"task"},
		},
	})

	// ── paradigm_select ──
	Register(&ToolDef{
		Name:        "paradigm_select",
		Title:       "选择思维范式",
		Description: "加载指定思维范式的完整方法论（详细步骤指南）。调用后，LLM 应严格按照方法论的步骤执行任务，并在回复末尾附加 @paradigm 标记。",
		Category:    "system",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "范式 ID（从 paradigm_list 或 paradigm_match 获取）"},
			},
			Required: []string{"id"},
		},
	})

	// ── paradigm_list ──
	Register(&ToolDef{
		Name:        "paradigm_list",
		Title:       "列出思维范式",
		Description: "列出所有可用的思维范式及其分类、描述和成功率。用于浏览可用的思考方法库。",
		Category:    "system",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"category": {Type: "string", Description: "按分类筛选：analysis/decision/creative/debug/planning，空字符串返回全部"},
			},
		},
	})

	// ── paradigm_feedback ──
	Register(&ToolDef{
		Name:        "paradigm_feedback",
		Title:       "思维范式反馈",
		Description: "提交对已使用范式的三维度反馈（匹配度/执行度/结果度），帮助系统学习哪种范式在什么场景下最有效。在每个使用范式的任务完成后调用。",
		Category:    "system",
		Annotations: &ToolAnnotations{},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id":            {Type: "string", Description: "使用的范式 ID"},
				"match_quality": {Type: "number", Description: "范式与任务的匹配度，0.0-1.0"},
				"exec_quality":  {Type: "number", Description: "范式执行顺利度，0.0-1.0"},
				"outcome":       {Type: "number", Description: "最终结果质量，0.0-1.0"},
				"reason":        {Type: "string", Description: "简短反馈原因（为什么好/不好）"},
			},
			Required: []string{"id", "match_quality", "exec_quality", "outcome"},
		},
	})
}
