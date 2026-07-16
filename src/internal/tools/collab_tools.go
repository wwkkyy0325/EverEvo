package tools

// registerCollabTools exposes the multi-agent collaboration primitives to the
// LLM. The orchestrator agent (Evo) uses these to build a team, dispatch work,
// share state via the blackboard, and gather results.
func registerCollabTools() {
	// ── Collaboration sessions ──
	Register(&ToolDef{
		Name: "collab_create", Category: "system",
		Description: "创建一个多 agent 协同会话：设定目标、指定主控 agent、挂载成员 agent，并分配共享黑板。返回会话 ID",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"goal":           {Type: "string", Description: "协同目标（要解决的问题）"},
			"orchestratorId": {Type: "string", Description: "主控 agent 的 ID（通常是自己 Evo）"},
			"members": {Type: "array", Description: "成员 agent 列表", Items: &ToolProp{Type: "object"}},
		}, Required: []string{"goal", "orchestratorId"}},
	})
	Register(&ToolDef{
		Name: "collab_dispatch", Category: "system",
		Description: "派任务给指定 agent 执行（同步阻塞，返回 agent 的输出）。异步派发用 collab_dispatch_async",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"targetAgentId": {Type: "string", Description: "目标 agent ID"},
			"task":          {Type: "string", Description: "要执行的任务描述"},
		}, Required: []string{"targetAgentId", "task"}},
	})
	Register(&ToolDef{
		Name: "collab_dispatch_async", Category: "system",
		Description: "异步派任务给 agent，立即返回 runId。配合 collab_wait 收集结果，实现并发派发",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"sessionId":     {Type: "string", Description: "协同会话 ID"},
			"targetAgentId": {Type: "string", Description: "目标 agent ID"},
			"task":          {Type: "string", Description: "任务描述"},
		}, Required: []string{"targetAgentId", "task"}},
	})
	Register(&ToolDef{
		Name: "collab_wait", Category: "system",
		Description: "等待一个或多个异步派发的 agent 任务完成，返回各自结果。用于 gather 阶段",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"runIds": {Type: "array", Description: "collab_dispatch_async 返回的 runId 列表", Items: &ToolProp{Type: "string"}},
		}, Required: []string{"runIds"}},
	})

	// ── Blackboard (shared scratchpad) ──
	Register(&ToolDef{
		Name: "blackboard_set", Category: "system",
		Description: "向协同会话的共享黑板写入/更新一个键。写入会通知所有订阅的 agent",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"sessionId": {Type: "string", Description: "协同会话 ID"},
			"key":       {Type: "string", Description: "键名"},
			"value":     {Type: "string", Description: "值"},
			"kind":      {Type: "string", Description: "类型: text | artifact | decision | todo"},
		}, Required: []string{"sessionId", "key", "value"}},
	})
	Register(&ToolDef{
		Name: "blackboard_get", Category: "system",
		Description: "从共享黑板读取一个键",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"sessionId": {Type: "string", Description: "协同会话 ID"},
			"key":       {Type: "string", Description: "键名"},
		}, Required: []string{"sessionId", "key"}},
	})
	Register(&ToolDef{
		Name: "blackboard_list", Category: "system",
		Description: "列出协同会话共享黑板的所有条目",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"sessionId": {Type: "string", Description: "协同会话 ID"},
		}, Required: []string{"sessionId"}},
	})

	// ── Session listing ──
	Register(&ToolDef{
		Name: "collab_list_sessions", Category: "system",
		Description: "列出所有活跃的协同会话及其状态和成员",
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
		Parameters:   &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name: "collab_complete", Category: "system",
		Description: "结束一个协同会话（标记完成并清除黑板）",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"sessionId": {Type: "string", Description: "协同会话 ID"},
		}, Required: []string{"sessionId"}},
	})

	// ── Agent messaging (local A2A) ──
	Register(&ToolDef{
		Name: "agent_message", Category: "system",
		Description: "向另一个 agent 发送消息（本地走进程内通道，远程走 A2A）。等对方回复后返回",
		Parameters: &ToolParams{Type: "object", Properties: map[string]ToolProp{
			"targetAgentId": {Type: "string", Description: "目标 agent ID 或名称"},
			"message":       {Type: "string", Description: "消息内容"},
		}, Required: []string{"targetAgentId", "message"}},
	})
}
