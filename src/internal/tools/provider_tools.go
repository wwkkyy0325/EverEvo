package tools

func registerProviderTools() {
	Register(&ToolDef{
		Name:        "provider_list",
		Description: "列出所有已配置的 LLM 供应商及其状态（活跃/禁用/模型名/端点）",
		Category:    "provider",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "provider_get_active",
		Description: "获取当前活跃的 LLM 供应商配置详情",
		Category:    "provider",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
	Register(&ToolDef{
		Name:        "provider_set_active",
		Description: "切换当前活跃的 LLM 供应商（传入 provider ID）",
		Category:    "provider",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "供应商 ID（来自 provider_list）"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "provider_test",
		Description: "测试指定供应商的 API 连接是否正常，返回测试结果",
		Category:    "provider",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"id": {Type: "string", Description: "供应商 ID"},
			},
		},
	})
	Register(&ToolDef{
		Name:        "provider_list_presets",
		Description: "列出所有内置 LLM 供应商预设（OpenAI / DeepSeek / 通义千问 / Ollama 等）",
		Category:    "provider",
		Parameters:  &ToolParams{Type: "object", Properties: map[string]ToolProp{}},
	})
}
