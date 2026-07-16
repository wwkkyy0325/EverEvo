// Package marketplace provides a built-in skill marketplace so users
// can discover and install pre-built skills with one click.
package marketplace

// SkillPackage defines a distributable skill package (.mbskill.json format).
type SkillPackage struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Icon        string   `json:"icon,omitempty"`
	Category    string   `json:"category"`

	MCPServers  []MCPDependency `json:"mcpServers,omitempty"`
	Tools       []string        `json:"tools"`
	MCPTools    []string        `json:"mcpTools,omitempty"`
	SystemPrompt string         `json:"systemPrompt,omitempty"`

	Installed bool `json:"installed"` // runtime: whether user has it installed
}

// MCPDependency describes a required MCP server.
type MCPDependency struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	URL       string   `json:"url,omitempty"`
}

// BuiltinMarket returns the built-in skill marketplace.
func BuiltinMarket() []SkillPackage {
	return []SkillPackage{
		{
			Schema: "EverEvo-skill-v1", Name: "code-reviewer", Title: "代码审查助手",
			Description: "自动审查代码，发现安全漏洞、性能问题和代码风格问题",
			Author: "EverEvo", Version: "1.0.0", Icon: "◈", Category: "dev",
			Tools:       []string{"model_list"},
			SystemPrompt: "你是一个资深代码审查员。审查代码时关注：安全漏洞、性能问题、代码风格、潜在bug。每条建议附带具体行号和修改方案。",
		},
		{
			Schema: "EverEvo-skill-v1", Name: "file-manager", Title: "文件管理助手",
			Description: "管理本地文件：搜索、读取、写入、整理文件",
			Author: "EverEvo", Version: "1.0.0", Icon: "◫", Category: "filesystem",
			MCPServers: []MCPDependency{{
				Name: "Filesystem", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
			}},
			MCPTools:     []string{"mcp__srv_fs__read_file", "mcp__srv_fs__write_file", "mcp__srv_fs__list_directory", "mcp__srv_fs__search_files"},
			SystemPrompt: "你是一个文件管理助手。帮助用户查找、读取、编辑和管理本地文件。操作前确认文件路径。",
		},
		{
			Schema: "EverEvo-skill-v1", Name: "web-browser", Title: "网页浏览助手",
			Description: "浏览网页、截图、提取内容、填写表单",
			Author: "EverEvo", Version: "1.0.0", Icon: "◎", Category: "browser",
			MCPServers: []MCPDependency{{
				Name: "Puppeteer", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@anthropic/mcp-server-puppeteer"},
			}},
			SystemPrompt: "你是一个网页浏览助手。帮助用户浏览网页、提取信息、截图。",
		},
		{
			Schema: "EverEvo-skill-v1", Name: "data-analyst", Title: "数据分析师",
			Description: "连接 SQLite 数据库，执行 SQL 查询，生成报表",
			Author: "EverEvo", Version: "1.0.0", Icon: "≡", Category: "data",
			MCPServers: []MCPDependency{{
				Name: "SQLite", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@anthropic/mcp-server-sqlite"},
			}},
			SystemPrompt: "你是一个数据分析师。帮助用户查询数据库、分析数据、生成报表。使用 SQL 查询时注意注入风险。",
		},
		{
			Schema: "EverEvo-skill-v1", Name: "github-ci", Title: "GitHub 运维助手",
			Description: "管理 GitHub Issue/PR，搜索代码，查看 CI 状态",
			Author: "EverEvo", Version: "1.0.0", Icon: "□", Category: "dev",
			MCPServers: []MCPDependency{{
				Name: "GitHub", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"},
			}},
			SystemPrompt: "你是一个 GitHub 运维助手。帮助用户管理仓库、Issue 和 Pull Request。",
		},
		{
			Schema: "EverEvo-skill-v1", Name: "internet-search", Title: "互联网搜索助手",
			Description: "使用 Brave Search 搜索互联网获取最新信息",
			Author: "EverEvo", Version: "1.0.0", Icon: "☰", Category: "data",
			MCPServers: []MCPDependency{{
				Name: "Brave Search", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@anthropic/mcp-server-brave-search"},
			}},
			SystemPrompt: "你是一个搜索助手。帮助用户搜索互联网获取最新信息。引用来源。",
		},
	}
}

// ─── Install / Uninstall helpers ────────────────────────────────

// InstallResult describes what happened during a skill installation.
type InstallResult struct {
	SkillName   string   `json:"skillName"`
	MCPServers  []string `json:"mcpServersAdded"` // server names that were auto-added
	Existing    bool     `json:"existing"`         // skill already existed (updated)
}
