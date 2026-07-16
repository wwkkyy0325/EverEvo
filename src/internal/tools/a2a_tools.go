package tools

func registerA2ATools() {
	Register(&ToolDef{
		Name:        "a2a_list_agents",
		Description: "列出所有已配置的 A2A 远端 Agent 及其连接状态",
		Category:    "a2a",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "a2a_connect_agent",
		Description: "连接到指定的远端 A2A Agent",
		Category:    "a2a",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "Agent ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "a2a_disconnect_agent",
		Description: "断开指定的远端 A2A Agent 连接",
		Category:    "a2a",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "Agent ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "a2a_send_to_agent",
		Description: "向已连接的远端 A2A Agent 发送消息并获取回复。可用于调用远端 Agent 的能力",
		Category:    "a2a",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"agentId":   {Type: "string", Description: "Agent ID（来自 a2a_list_agents）"},
				"message":   {Type: "string", Description: "要发送的消息文本"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "a2a_agent_status",
		Description: "获取本机 A2A Agent 服务状态",
		Category:    "a2a",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
}
