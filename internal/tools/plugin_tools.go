package tools

func registerPluginTools() {
	Register(&ToolDef{
		Name:        "plugin_list",
		Description: "列出所有已安装的插件，包括名称、版本、类型、描述、运行时、暴露的方法列表等",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type:       "object",
			Properties: map[string]ToolProp{},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "plugin_status",
		Description: "查询指定插件的运行状态：是否运行中、PID、启动时间、错误信息",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "插件名称"},
			},
			Required: []string{"name"},
		},
		Annotations: &ToolAnnotations{ReadOnlyHint: true},
	})

	Register(&ToolDef{
		Name:        "plugin_start",
		Description: "启动指定插件进程，使其进入运行状态并可以接收 RPC 调用",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "插件名称"},
			},
			Required: []string{"name"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_stop",
		Description: "停止指定插件的运行进程（优雅关闭，3 秒超时后强制终止）",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "插件名称"},
			},
			Required: []string{"name"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_restart",
		Description: "重启指定插件（先停止再启动）",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "插件名称"},
			},
			Required: []string{"name"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_run",
		Description: "调用插件的指定方法，传入 JSON 参数并获取返回结果。可用方法见插件列表中的 methods 字段（常见: echo/uppercase/count/health/info）",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":   {Type: "string", Description: "插件名称"},
				"method": {Type: "string", Description: "要调用的方法名"},
				"params": {Type: "object", Description: "方法参数，JSON 对象格式"},
			},
			Required: []string{"name", "method"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_install",
		Description: "从 .zip 压缩包或目录路径安装一个新插件",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"path": {Type: "string", Description: "插件 .zip 文件或目录的绝对路径"},
			},
			Required: []string{"path"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_delete",
		Description: "卸载指定插件（如果正在运行会先停止，然后删除整个插件目录）",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "要删除的插件名称"},
			},
			Required: []string{"name"},
		},
	})

	Register(&ToolDef{
		Name:        "plugin_logs",
		Description: "获取指定插件最近的 stderr 日志输出（环形缓冲区，最多 64KB）",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name": {Type: "string", Description: "插件名称"},
			},
			Required: []string{"name"},
		},
	})

	// ── Agent self-writes plugins (multi-runtime) ──
	Register(&ToolDef{
		Name:        "plugin_create",
		Description: "用代码创建新插件并热加载。支持三种运行时：python（默认，自动 venv 隔离）、go（编译为 EXE）、node（Node.js/TypeScript）。Agent 提供名称+代码+运行时，系统自动生成模板、安装、热启动",
		Category:    "plugin",
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"name":         {Type: "string", Description: "插件名称（小写字母+连字符）"},
				"runtime":      {Type: "string", Description: "运行时: python（默认）| go | node"},
				"description":  {Type: "string", Description: "插件功能描述"},
				"code":         {Type: "string", Description: "插件源码。python: def handle(method,params) 函数体; go: package main + JSON-RPC I/O; node: async function handle(method,params)"},
				"methods":      {Type: "string", Description: "逗号分隔的方法名，默认 health,info"},
				"dependencies": {Type: "string", Description: "python: pip 包名（空格分隔）; node: npm 包名; go: 忽略"},
				"autoStart":    {Type: "boolean", Description: "安装后是否立即热启动（默认 true）"},
			},
			Required: []string{"name", "code"},
		},
	})
}
