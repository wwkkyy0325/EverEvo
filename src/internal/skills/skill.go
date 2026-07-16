// Package skills defines the EverEvo Skill abstraction — a group of
// related MCP Tools, Resources, and Prompts that form a coherent capability
// domain. Skills can be enabled/disabled, imported/exported as JSON files,
// and are compatible with the MCP ecosystem.
package skills

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Skill is a named group of MCP capabilities.
type Skill struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Package     string   `json:"package"` // which package this skill belongs to (drag-to-move)
	Icon        string   `json:"icon,omitempty"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools"`
	Resources   []string `json:"resources"`
	Prompts     []string `json:"prompts"`
	MCPServers  []string `json:"mcpServers,omitempty"` // dependent MCP server IDs
	MCPTools    []string `json:"mcpTools,omitempty"`   // external tool names to include
	SystemPrompt string  `json:"systemPrompt,omitempty"` // custom system prompt for this skill
	LibraryID   string  `json:"libraryId,omitempty"`   // domain library this skill belongs to
}

// builtinSkills defines the default skills bundled with EverEvo.
var builtinSkills = []Skill{
	{
		Name: "paradigm-thinking", Title: "思维范式", Icon: "🧠",
		Description: "选择并应用思维范式（MECE、根因分析、SWOT等）来结构化思考和解决问题",
		Category:    "thinking", Package: "everevo", Enabled: true,
		Tools:     []string{"paradigm_match", "paradigm_select", "paradigm_list", "paradigm_feedback", "paradigm_refine", "paradigm_distill"},
		SystemPrompt: "你是思维范式专家。面对复杂任务时，先用 paradigm_match 根据任务描述匹配最适合的思维范式，然后用 paradigm_select 加载完整方法论，严格按其步骤执行。完成后用 paradigm_feedback 提交效果反馈。常用范式包括：MECE分解、根因分析、系统思维、SWOT分析、第一性原理、权衡矩阵、事前验尸、逆向思维、类比推理、SCAMPER头脑风暴、二分排除法、假设检验驱动调试、分治法、SMART目标、ReAct标准、结构化诊断分析、对比决策、逐步执行、反思优化。",
	},
	{
		Name: "model-management", Title: "模型管理", Icon: "□",
		Description: "管理已下载和已加载的 AI 模型：加载、卸载、运行推理、查看已下载模型列表",
		Category:    "model", Package: "everevo", Enabled: true,
		Tools:     []string{"model_list", "model_load", "model_unload", "model_run", "model_list_downloaded", "model_list_tool"},
		Resources: []string{"everevo://models/list", "everevo://models/downloaded", "everevo://models/tool"},
		SystemPrompt: "你是模型管理专家。帮助用户加载、卸载和运行 AI 模型。当用户询问模型相关问题或需要执行推理时，使用模型管理工具。建议合适的模型并解释其用途。",
	},
	{
		Name: "plugin-system", Title: "插件系统", Icon: "⊕",
		Description: "管理子进程插件：安装、启动、停止、重启、调用插件方法、查看日志",
		Category:    "plugin", Package: "everevo", Enabled: true,
		Tools:     []string{"plugin_list", "plugin_status", "plugin_start", "plugin_stop", "plugin_restart", "plugin_run", "plugin_install", "plugin_create", "plugin_delete", "plugin_logs"},
		Resources: []string{"everevo://plugins/list"},
		SystemPrompt: "你是插件系统管理员。帮助用户安装、配置和调试插件。当插件运行异常时，首先查看日志定位问题。",
	},
	{
		Name: "knowledge-base", Title: "知识库", Icon: "≡",
		Description: "管理知识库：创建、添加文档、语义搜索、列出/删除文档",
		Category:    "kb", Package: "everevo", Enabled: true,
		Tools:     []string{"kb_list", "kb_create", "kb_add_texts", "kb_search", "kb_delete", "kb_clear", "kb_set_library", "kb_list_docs", "kb_delete_chunks", "read_file", "read_media_file", "wiki_list", "wiki_read", "wiki_save", "wiki_delete", "wiki_move", "wiki_search", "wiki_reindex", "write_file", "list_directory"},
		Resources: []string{"everevo://kb/list"},
		SystemPrompt: "你是知识库管理专家。帮助用户构建和维护知识库，使用语义搜索查找相关信息。建议合适的文档切分策略以优化检索效果。",
	},
	{
		Name: "model-catalog", Title: "模型市场", Icon: "◇",
		Description: "搜索 HuggingFace 和 ModelScope：搜索模型、查看详情、管理 API Token",
		Category:    "catalog", Package: "everevo", Enabled: true,
		Tools:     []string{"catalog_search", "catalog_get_detail", "catalog_list_files"},
		SystemPrompt: "你是模型市场导购。帮助用户在 HuggingFace 和 ModelScope 上搜索和发现合适的 AI 模型。根据用户需求推荐模型，解释模型的特点和适用场景。",
	},
	{
		Name: "download-manager", Title: "下载管理", Icon: "↓",
		Description: "从模型市场下载模型文件：单文件下载、批量下载、删除已下载模型",
		Category:    "download", Package: "everevo", Enabled: true,
		Tools:     []string{"download_file", "download_package", "download_selected", "download_delete_file", "download_delete_dir"},
		SystemPrompt: "你是下载管理助手。帮助用户从模型市场下载模型文件，管理下载队列和磁盘空间。下载大文件时提醒用户注意磁盘空间。",
	},
	{
		Name: "system-monitor", Title: "系统信息", Icon: "◎",
		Description: "查看系统和硬件信息：CPU/GPU/内存状态、推理后端可用性、应用配置",
		Category:    "system", Package: "everevo", Enabled: true,
		Tools:     []string{"system_info", "system_dynamic", "system_backends", "system_config", "proxy_get", "proxy_set", "proxy_test", "proxy_toggle", "download_engine", "shell_exec", "web_search", "web_fetch"},
		Resources: []string{"everevo://system/info", "everevo://system/dynamic", "everevo://system/backends"},
		SystemPrompt: "你是系统监控专家。帮助用户查看硬件状态、推理引擎可用性和应用配置。当用户遇到性能问题时，分析系统资源瓶颈。你可以使用 shell_exec 执行命令来检查文件、git 状态、npm/pip 操作等，就像在终端操作一样（也支持 playwright-cli 浏览器自动化）。使用 web_search 搜索互联网信息，web_fetch 抓取网页内容。",
	},
	{
		Name: "workflow-engine", Title: "工作流引擎", Icon: "⇢",
		Description: "编排和执行自动化工作流：创建、编辑、运行 LLM+工具调用流程，支持条件分支和循环",
		Category:    "workflow", Package: "everevo", Enabled: true,
		Tools:     []string{"workflow_list", "workflow_get", "workflow_create", "workflow_update", "workflow_delete", "workflow_execute", "workflow_status", "workflow_validate"},
		SystemPrompt: "你是工作流编排专家。帮助用户设计、创建和执行自动化工作流。工作流由节点（输入、LLM调用、工具调用、条件分支、代码变换、循环、输出）通过连线组成。当用户需要多步骤 AI 任务时，建议使用工作流。创建新工作流前先用 workflow_validate 验证，执行后用 workflow_status 跟踪进度。",
	},
	{
		Name: "guide-system", Title: "攻略中心", Icon: "☰",
		Description: "阅读已同步的攻略和学习文档：列出、搜索、阅读攻略内容，同步远程仓库",
		Category:    "guide", Package: "everevo", Enabled: true,
		Tools:     []string{"guide_list", "guide_read", "guide_search", "guide_sync", "guide_sources"},
		Resources: []string{"everevo://guides/list", "everevo://guides/content"},
		SystemPrompt: "你是学习向导。帮助用户查找和阅读攻略文档，推荐相关的学习资源。当用户有具体问题时，搜索攻略内容提供最佳实践。",
	},
	{
		Name: "mcp-management", Title: "MCP 连接管理", Icon: "⇌",
		Description: "管理外部 MCP 服务器：添加、删除、连接、断开、查看工具列表、查看 MCP 状态",
		Category:    "mcp", Package: "everevo", Enabled: true,
		Tools:     []string{"mcp_list_servers", "mcp_add_server", "mcp_remove_server", "mcp_connect_server", "mcp_disconnect_server", "mcp_get_server_tools", "mcp_status"},
		Resources: []string{"everevo://mcp/servers", "everevo://mcp/status"},
		SystemPrompt: "你是 MCP 连接管理员。帮助用户配置和管理外部 MCP 服务器。添加新服务器时，先确认传输协议（stdio 还是 http）。连接失败时，检查启动命令、端口或 URL 是否正确。",
	},
	{
		Name: "llm-config", Title: "LLM 配置管理", Icon: "◎",
		Description: "管理 LLM 供应商：列出、切换活动供应商、测试连接、查看预设",
		Category:    "provider", Package: "everevo", Enabled: true,
		Tools:     []string{"provider_list", "provider_get_active", "provider_set_active", "provider_test", "provider_list_presets"},
		Resources: []string{"everevo://providers/list", "everevo://providers/active"},
		SystemPrompt: "你是 LLM 配置管理员。帮助用户管理和切换大语言模型供应商。当用户需要换模型或测试 API 连接时，使用供应商管理工具。推荐合适的供应商预设。",
	},
	{
		Name: "agent-orchestration", Title: "智能体编排", Icon: "◉",
		Description: "管理本地 Agent（智能体人格）：列出、按需创建专门角色、把子任务委派给指定 Agent 执行",
		Category:    "agent", Package: "everevo", Enabled: true,
		Tools:     []string{"agent_list", "agent_create", "agent_set_library", "agent_run", "agent_run_async", "agent_delegate_to_domain", "agent_delegate_multi_domain", "agent_synthesize_tool", "memory_list", "memory_search", "memory_delete", "memory_add_fact", "memory_clear", "core_list", "core_add", "core_delete", "session_list", "session_delete", "graph_list", "graph_add_edge", "graph_delete_node", "graph_rename_node", "graph_migrate", "graph_rebuild_from_domain", "experience_list", "experience_delete", "library_list", "library_create", "library_delete", "a2a_list_agents", "a2a_connect_agent", "a2a_disconnect_agent", "a2a_send_to_agent", "a2a_agent_status"},
		SystemPrompt: "你是智能体编排中枢。当需要专门角色的子任务时：先用 agent_list 查看可用 Agent；多独立子任务用 agent_run_async（非阻塞）同时启动多个 Agent 并行执行；需要等待结果的用 agent_run（阻塞）。若没有合适的 Agent，用 agent_create 按需创建。也可用 a2a_list_agents 查看远端 Agent，a2a_send_to_agent 向远端派发。委派后整合子 Agent 的回复，不要原样转述内部思考。",
		},
		{
			Name: "taskboard", Title: "任务板", Icon: "📋",
			Description: "跨对话追踪任务进度：列出、添加、更新、管理任务步骤",
			Category: "taskboard", Package: "everevo", Enabled: true,
			Tools:     []string{"taskboard_list", "taskboard_add", "taskboard_update", "taskboard_steps"},
			SystemPrompt: "你是任务板管理员。当前任务板中的任务会跨对话追踪。用 taskboard_list 查看进度，taskboard_add 添加新任务，完成后用 taskboard_update status=done 标记。在每次回复末尾简要提醒未完成的任务。",
		},
		{
			Name: "collab", Title: "多智能体协同", Icon: "👥",
			Description: "多 Agent 协同工作：创建会话、派发任务、异步并发、黑板共享、Agent 间通信",
			Category: "collab", Package: "everevo", Enabled: true,
			Tools:     []string{"collab_create", "collab_dispatch", "collab_dispatch_async", "collab_wait", "collab_complete", "collab_list_sessions", "blackboard_set", "blackboard_get", "blackboard_list", "agent_message"},
			SystemPrompt: "你是多智能体协同编排师。当任务复杂需要多个 Agent 分工协作时，用 collab_create 创建协同会话，collab_dispatch_async 并发派发子任务给不同 Agent，collab_wait 收集结果，黑板（blackboard）共享中间状态。同步任务用 collab_dispatch，Agent 间直接通信用 agent_message。完成后用 collab_complete 结束会话。",
		},
		{
			Name: "zone", Title: "运行区管理", Icon: "🔬",
			Description: "管理多运行区：创建实验区、启动/停止、合并/丢弃、备份恢复",
			Category: "zone", Package: "everevo", Enabled: true,
			Tools:     []string{"zone_list", "zone_create_experiment", "zone_launch", "zone_stop", "zone_merge", "zone_discard", "backup_list", "backup_create", "backup_restore"},
			SystemPrompt: "你是运行区管理员。生产区稳定运行，实验区用于测试新功能。用 zone_create_experiment 从生产区 fork 实验区，zone_launch/zone_stop 启停，验证通过后 zone_merge 合并回生产，zone_discard 丢弃失败的实验。定期用 backup_create 备份生产区。",
		},
		{
			Name: "evolve", Title: "自进化引擎", Icon: "🧬",
			Description: "项目自我编译、替换、重启：检测能力、重新构建、热替换 EXE、查看进化历史",
			Category: "evolve", Package: "everevo", Enabled: true,
			Tools:     []string{"evolve_capability", "evolve_build", "evolve_swap", "evolve_tasks"},
			SystemPrompt: "你是自进化引擎。当用户要求修改代码并生效时，先 evolve_capability 检测编译环境是否就绪，然后 evolve_build 重新编译项目，编译成功后 evolve_swap 替换当前 EXE 并重启。用 evolve_tasks 查看构建/替换历史。⚠ 这些操作会影响运行中的应用，执行前确认用户意图。",
		},
		{
			Name: "plan", Title: "任务计划", Icon: "📝",
			Description: "AI 任务计划：把目标拆解为有序步骤，逐步推进，面板实时显示进度",
			Category: "plan", Package: "everevo", Enabled: true,
			Tools:     []string{"plan_create", "plan_step_update", "plan_list"},
			SystemPrompt: "你是任务规划师。面对复杂任务时，用 plan_create 把目标拆解成有序步骤清单。执行过程中用 plan_step_update 标记每步进度（in_progress/done/skipped），用户可在面板看到实时进度。用 plan_list 随时查看当前所有计划的状态。",
		},
		{
			Name: "ingest", Title: "知识导入", Icon: "📥",
			Description: "批量导入文件夹到知识库：快速导入、深度 LLM 分析导入、预览、后置审查",
			Category: "ingest", Package: "everevo", Enabled: true,
			Tools:     []string{"ingest_folder", "ingest_analyze", "ingest_commit", "ingest_cancel", "ingest_deep", "ingest_deep_analyze", "ingest_deep_commit", "ingest_deep_review"},
			SystemPrompt: "你是知识导入专家。快速导入用 ingest_analyze 预览 → ingest_commit 执行；深度导入用 ingest_deep_analyze 让 LLM 逐文件分析提取结构化元数据 → ingest_deep_commit 执行 → ingest_deep_review 后置审查。大批量高质量导入推荐深度模式。ingest_cancel 可取消进行中的导入。",
		},
		{
			Name: "scripting", Title: "脚本编程", Icon: "💻",
			Description: "编写和运行脚本（Python/Node.js/Go/Playwright）：包管理、脚本执行、项目搭建",
			Category: "scripting", Package: "everevo", Enabled: true,
			Tools:     []string{"shell_exec", "write_file", "list_directory", "read_file"},
			SystemPrompt: `你是多语言脚本专家。代码写在 workspace/ 下，禁止在项目根目录创建文件。

Python: python --version → python -m venv workspace/.venv → 激活 workspace/.venv/Scripts/activate → pip install <pkg>。脚本: workspace/scripts/py_YYYY-MM-DD_name.py，首行 #!/usr/bin/env python3，必须有 if __name__ == '__main__':。

Node.js: node --version && npm --version → npm init -y → npm install <pkg>。JS: workspace/scripts/js_YYYY-MM-DD_name.js。TS: workspace/scripts/ts_YYYY-MM-DD_name.ts，需 npm install -D typescript ts-node @types/node。

Go: go version → go run workspace/scripts/go_YYYY-MM-DD_name.go。独立工具: workspace/plugins/<name>/ 下 go mod init + main.go，可 import everevo/internal/xxx。

Playwright: npm install -g @playwright/cli，然后用 shell_exec 调用 playwright-cli 控制浏览器。

文件目录: workspace/scripts/, workspace/plugins/<name>/, workspace/scratch/, workspace/output/。写完后用 list_directory 确认。`,
		},
}

// Manager holds the skill state and handles import/export/persistence.
type Manager struct {
	Skills []Skill `json:"skills"`
}

// skillsPath returns the path to the persisted skills file.
func skillsPath() string {
	dataDir, err := storage.AppDataDir()
	if err != nil {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "skills.json")
}

// NewManager creates a skill manager, loading from disk if available,
// and merging any new built-in skills that don't exist yet.
func NewManager() *Manager {
	m := &Manager{}
	loaded := loadFromDisk()

	if loaded != nil {
		m.Skills = loaded
		// Merge new built-in skills that the user doesn't have yet.
		existing := map[string]bool{}
		for _, s := range m.Skills {
			existing[s.Name] = true
		}
		added := 0
		for _, bs := range builtinSkills {
			if !existing[bs.Name] {
				m.Skills = append(m.Skills, bs)
				added++
			}
		}
		// Also merge new tools into existing skills — builtin definitions may
		// have gained tools (e.g. shell_exec) that the user's disk copy lacks.
		toolsMerged := 0
		builtinByName := map[string]Skill{}
		for _, bs := range builtinSkills {
			builtinByName[bs.Name] = bs
		}
		for i := range m.Skills {
			b, ok := builtinByName[m.Skills[i].Name]
			if !ok {
				continue
			}
			// Tools — union: disk tools + builtins not yet on disk.
			diskTools := map[string]bool{}
			for _, t := range m.Skills[i].Tools {
				diskTools[t] = true
			}
			for _, bt := range b.Tools {
				if !diskTools[bt] {
					m.Skills[i].Tools = append(m.Skills[i].Tools, bt)
					toolsMerged++
				}
			}
			// MCPTools — same union logic.
			diskMCP := map[string]bool{}
			for _, t := range m.Skills[i].MCPTools {
				diskMCP[t] = true
			}
			for _, bt := range b.MCPTools {
				if !diskMCP[bt] {
					m.Skills[i].MCPTools = append(m.Skills[i].MCPTools, bt)
					toolsMerged++
				}
			}
		}
		if added > 0 || toolsMerged > 0 {
			log.Printf("[skills] 从磁盘加载 %d 个 + 新增 %d 个能力 + 合并 %d 个工具", len(loaded), added, toolsMerged)
			_ = m.Save()
		} else {
			log.Printf("[skills] 从磁盘加载 %d 个能力域", len(m.Skills))
		}
		return m
	}

	// No disk file — use built-in defaults
	skills := make([]Skill, len(builtinSkills))
	copy(skills, builtinSkills)
	m.Skills = skills
	log.Printf("[skills] 使用内置默认 %d 个能力域", len(m.Skills))
	return m
}

// loadFromDisk tries to read persisted skills from data/skills.json.
func loadFromDisk() []Skill {
	path := skillsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var skills []Skill
	if err := json.Unmarshal(data, &skills); err != nil {
		log.Printf("[skills] 解析 %s 失败: %v", path, err)
		return nil
	}
	if len(skills) == 0 {
		return nil
	}
	return skills
}

// Save persists the current skill list to disk.
func (m *Manager) Save() error {
	path := skillsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建 skills 目录失败: %w", err)
	}
	data, err := json.MarshalIndent(m.Skills, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 skills 失败: %w", err)
	}
	if err := atomic.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入 skills.json 失败: %w", err)
	}
	log.Printf("[skills] 已保存 %d 个能力域到 %s", len(m.Skills), path)
	return nil
}

// List returns all skills.
func (m *Manager) List() []Skill { return m.Skills }

// ListByLibrary returns skills filtered by library ID. Empty libraryID returns
// all skills (backward-compatible).
func (m *Manager) ListByLibrary(libraryID string) []Skill {
	if libraryID == "" {
		return m.Skills
	}
	var out []Skill
	for _, s := range m.Skills {
		if s.LibraryID == libraryID {
			out = append(out, s)
		}
	}
	return out
}

// ListEnabled returns only enabled skills.
func (m *Manager) ListEnabled() []Skill {
	var out []Skill
	for _, s := range m.Skills {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

// ListEnabledByLibrary returns enabled skills filtered by library ID.
// When libraryID is empty, returns only global skills (empty LibraryID) —
// domain-scoped skills are the domain path, global skills the global path.
func (m *Manager) ListEnabledByLibrary(libraryID string) []Skill {
	var out []Skill
	for _, s := range m.Skills {
		if !s.Enabled {
			continue
		}
		if libraryID == "" {
			// Global-only: skills with no library binding.
			if s.LibraryID == "" {
				out = append(out, s)
			}
		} else if s.LibraryID == libraryID || s.LibraryID == "" {
			// Domain-scoped: skills matching this domain + global skills.
			out = append(out, s)
		}
	}
	return out
}

// SetEnabled enables or disables a skill by name.
func (m *Manager) SetEnabled(name string, enabled bool) bool {
	for i := range m.Skills {
		if m.Skills[i].Name == name {
			m.Skills[i].Enabled = enabled
			return true
		}
	}
	return false
}

// MoveSkill changes the package of a skill and saves.
func (m *Manager) MoveSkill(name string, newPackage string) error {
	for i := range m.Skills {
		if m.Skills[i].Name == name {
			m.Skills[i].Package = newPackage
			return m.Save()
		}
	}
	return fmt.Errorf("技能 %q 不存在", name)
}

// Create adds a new skill to the manager.
func (m *Manager) Create(s Skill) error {
	// Check for duplicate name
	for _, existing := range m.Skills {
		if existing.Name == s.Name {
			return fmt.Errorf("技能名称 %q 已存在", s.Name)
		}
	}
	m.Skills = append(m.Skills, s)
	return m.Save()
}

// Update modifies an existing skill by name.
func (m *Manager) Update(name string, s Skill) error {
	for i := range m.Skills {
		if m.Skills[i].Name == name {
			// Preserve original name if not changed (or allow rename)
			if s.Name == "" {
				s.Name = name
			}
			m.Skills[i] = s
			return m.Save()
		}
	}
	return fmt.Errorf("技能 %q 不存在", name)
}

// Delete removes a skill by name.
func (m *Manager) Delete(name string) error {
	for i := range m.Skills {
		if m.Skills[i].Name == name {
			m.Skills = append(m.Skills[:i], m.Skills[i+1:]...)
			return m.Save()
		}
	}
	return fmt.Errorf("技能 %q 不存在", name)
}

// Reset restores skills to built-in defaults and saves.
func (m *Manager) Reset() error {
	skills := make([]Skill, len(builtinSkills))
	copy(skills, builtinSkills)
	m.Skills = skills
	return m.Save()
}

// Export returns the full skill list as JSON.
func (m *Manager) Export() (json.RawMessage, error) {
	return json.MarshalIndent(m.Skills, "", "  ")
}

// Import merges skills from JSON data. Existing skills with the same name are updated.
func (m *Manager) Import(data json.RawMessage) error {
	var incoming []Skill
	if err := json.Unmarshal(data, &incoming); err != nil {
		return err
	}
	for _, s := range incoming {
		found := false
		for i := range m.Skills {
			if m.Skills[i].Name == s.Name {
				m.Skills[i] = s
				found = true
				break
			}
		}
		if !found {
			m.Skills = append(m.Skills, s)
		}
	}
	return m.Save()
}

// EnsureLibraryIDs backfills empty or invalid LibraryID fields with the given
// default ID and saves. Safe to call at startup after the memory store is ready.
// validIDs is the set of current domain library IDs from the memory store.
func (m *Manager) EnsureLibraryIDs(defaultLibraryID string, validIDs []string) error {
	valid := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}
	changed := false
	for i := range m.Skills {
		if m.Skills[i].LibraryID == "" || !valid[m.Skills[i].LibraryID] {
			if m.Skills[i].LibraryID != "" {
				log.Printf("[skills] Skill %q 的 libraryId %q 无效，回填为默认领域", m.Skills[i].Name, m.Skills[i].LibraryID)
			}
			m.Skills[i].LibraryID = defaultLibraryID
			changed = true
		}
	}
	if changed {
		return m.Save()
	}
	return nil
}

// GetEnabledTools returns the union of all tools from enabled skills.
func (m *Manager) GetEnabledTools() []string {
	seen := map[string]bool{}
	for _, s := range m.ListEnabled() {
		for _, t := range s.Tools {
			seen[t] = true
		}
		for _, t := range s.MCPTools {
			seen[t] = true
		}
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	return out
}
