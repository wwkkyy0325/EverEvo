package tools

// registerAgentTools registers the local-agent orchestration surface: listing
// personas, creating new ones at runtime, and delegating a task to a sub-agent.
func registerAgentTools() {
	Register(&ToolDef{
		Name:        "agent_list",
		Description: "列出所有本地 Agent（智能体人格），包括名称、描述和是否为默认主 Agent。用于了解可委派的 Agent",
		Category:    "agent",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "agent_create",
		Description: "创建一个新的本地 Agent（智能体人格）并保存到列表。用于按需定义专门角色（如翻译专家、代码审查员等）供后续委派",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":         {Type: "string", Description: "Agent 名称（唯一标识，如「翻译专家」）"},
				"description":  {Type: "string", Description: "Agent 用途的简短描述"},
				"systemPrompt": {Type: "string", Description: "Agent 的完整系统提示词，定义其人格、能力和行为准则"},
				"skills": {Type: "array", Description: "授予该 Agent 的能力域名称列表（可选，留空则不授予任何能力）",
					Items: &ToolProp{Type: "string"}},
				"libraryId": {Type: "string", Description: "所属领域库 ID（来自 library_list）。不传则使用默认领域"},
			},
			Required: []string{"name", "systemPrompt"},
		},
	})
	Register(&ToolDef{
		Name:        "agent_run",
		Description: "将一个任务委派给指定的本地 Agent 后台执行（非阻塞，立即返回任务 ID）。Agent 完成后结果自动注入后续对话。可同一轮多次调用来并行启动多个 Agent。如需阻塞等待结果，使用 agent_run_async 设置 wait=true",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"agentId": {Type: "string", Description: "目标 Agent 的 ID（来自 agent_list）。与 name 二选一"},
				"name":    {Type: "string", Description: "目标 Agent 的名称（agentId 的替代，便于按名称委派）"},
				"task":    {Type: "string", Description: "要交给该 Agent 执行的任务描述"},
			},
			Required: []string{"task"},
		},
	})
	Register(&ToolDef{
		Name:        "agent_run_async",
		Description: "将一个任务委派给指定的本地 Agent 后台执行（非阻塞，立即返回任务 ID）。Agent 完成后结果自动注入下一轮对话。可同时调用多次来并行启动多个 Agent，各自独立执行，互不阻塞",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"agentId": {Type: "string", Description: "目标 Agent 的 ID（来自 agent_list）。与 name 二选一"},
				"name":    {Type: "string", Description: "目标 Agent 的名称（agentId 的替代）"},
				"task":    {Type: "string", Description: "要交给该 Agent 后台执行的任务描述"},
			},
			Required: []string{"task"},
		},
	})
	Register(&ToolDef{
		Name:        "library_list",
		Description: "列出所有领域库及其拥有的 Agent，用于了解可委派的领域和专家 Agent",
		Category:    "agent",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "agent_delegate_to_domain",
		Description: "将任务委派给指定领域库的 Agent 执行。该 Agent 会获得其所属领域的知识库和知识图谱上下文。适合把专业领域的子任务交给对应的领域专家处理",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"libraryId":   {Type: "string", Description: "目标领域库 ID（来自 library_list）。与 libraryName 二选一"},
				"libraryName": {Type: "string", Description: "目标领域库名称（libraryId 的替代）"},
				"agentId":     {Type: "string", Description: "目标 Agent ID（可选；不指定则使用该领域的默认 Agent）"},
				"task":        {Type: "string", Description: "要委派的任务描述"},
			},
			Required: []string{"task"},
		},
	})
	Register(&ToolDef{
		Name:        "agent_delegate_multi_domain",
		Description: "将任务并发委派给多个领域库的 Agent，聚合各领域专家的回复。适合跨领域问题（如'法律+技术'、'医学+营养'等）",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"domains": {Type: "array", Description: "领域库名称列表，每个领域并发委派",
					Items: &ToolProp{Type: "string"}},
				"task": {Type: "string", Description: "要委派的任务描述"},
			},
			Required: []string{"domains", "task"},
		},
	})
	Register(&ToolDef{
		Name:        "agent_synthesize_tool",
		Description: "动态创建一个新工具（Python 函数）来扩展 Agent 的能力。适用场景：现有工具无法满足需求时，生成临时工具完成任务",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":        {Type: "string", Description: "新工具的名称"},
				"description": {Type: "string", Description: "工具用途描述"},
				"code":        {Type: "string", Description: "Python 函数代码。函数签名: def tool(args: dict) -> dict:。返回 {'result': ...} 或 {'error': '...'}"},
			},
			Required: []string{"name", "code"},
		},
	})

	Register(&ToolDef{
		Name:        "agent_set_library",
		Description: "将 Agent 移动到指定领域库。用于修正 Agent 归属领域，使其能被 agent_delegate_to_domain 正确委派",
		Category:    "agent",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"agentId":   {Type: "string", Description: "Agent ID（来自 agent_list）"},
				"libraryId": {Type: "string", Description: "目标领域库 ID（来自 library_list）。传空字符串则移回默认领域"},
			},
			Required: []string{"agentId", "libraryId"},
		},
	})
}

func registerCodebaseTools() {
	Register(&ToolDef{
		Name:        "codebase_import",
		Description: "扫描项目 internal/ 目录的所有 Go 源码，生成 Wiki 页面（每个包）、知识图谱节点（导出类型）、图谱边（import 依赖）、RAG 向量块（源码分块）。一次运行完成全量代码库导入。libraryId 为空则使用默认领域",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"libraryId": {Type: "string", Description: "目标领域库 ID（可选，默认核心领域）"},
			},
		},
	})
}

func registerDomainTools() {
	Register(&ToolDef{
		Name:        "domain_search",
		Description: "跨领域只读检索。在指定的目标领域库中搜索知识库文档、语义记忆和知识图谱，返回合并结果。不会修改目标领域的任何数据。用于 Agent 需要引用其他领域知识时",
		Category:    "kb",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"query":    {Type: "string", Description: "搜索查询"},
				"libraryId": {Type: "string", Description: "目标领域库 ID（来自 library_list）"},
			},
			Required: []string{"query", "libraryId"},
		},
	})
}
