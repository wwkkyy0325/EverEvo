package tools

func registerMCPTools() {
	Register(&ToolDef{
		Name:        "mcp_list_servers",
		Description: "列出所有 MCP 服务器配置及其连接状态",
		Category:    "mcp",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "mcp_add_server",
		Description: "添加一个新的 MCP 服务器。name 为显示名称，transport 为 stdio 或 http，command/args/url 根据 transport 填写",
		Category:    "mcp",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":      {Type: "string", Description: "服务器显示名称"},
				"transport": {Type: "string", Description: "传输协议: stdio 或 http"},
				"command":   {Type: "string", Description: "stdio 模式的启动命令"},
				"args":      {Type: "array", Description: "stdio 模式的启动参数"},
				"url":       {Type: "string", Description: "http 模式的服务器 URL"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "mcp_remove_server",
		Description: "删除指定 ID 的 MCP 服务器配置",
		Category:    "mcp",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "服务器 ID（来自 mcp_list_servers）"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "mcp_connect_server",
		Description: "连接到指定 MCP 服务器，获取其提供的工具和资源列表",
		Category:    "mcp",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "服务器 ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "mcp_disconnect_server",
		Description: "断开指定 MCP 服务器连接",
		Category:    "mcp",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "服务器 ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "mcp_get_server_tools",
		Description: "获取指定已连接 MCP 服务器提供的工具列表",
		Category:    "mcp",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "服务器 ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "mcp_status",
		Description: "获取 EverEvo 自身 MCP 服务器状态、端口、工具数量",
		Category:    "mcp",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
}
