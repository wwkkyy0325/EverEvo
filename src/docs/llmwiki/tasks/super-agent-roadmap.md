# Super Agent Roadmap

> 让 EverEvo 从"手工作坊"进化为"超级智能体"——接入 MCP 生态，获得无限扩展能力。

---

## 现状分析

| 模块 | 现状 | 瓶颈 |
|---|---|---|
| 工具系统 | 44 个硬编码 Go 函数，`internal/tools/registry.go` 全局注册 | 新增能力 = 写 Go → 编译 → 发布 |
| MCP | 仅有 Server 端（`internal/mcp/server.go`），向外暴露能力 | 不能消费外部 MCP |
| Skill | JSON 分组过滤器，`tools` 字段只能写内部工具名 | 无法引用外部能力 |
| 工具分发 | `app.go` 巨型 switch-case（~260 行） | 每次加工具改核心文件 |
| UI | `AICapability.vue` 管理 Skill，无 MCP Server 连接界面 | 无外部 Server 管理 |

---

## 总体路线

```
Phase 1: MCP Client          Phase 2: 系统级能力        Phase 3: Skill 市场
┌──────────────────┐       ┌──────────────────┐       ┌──────────────────┐
│ 连接外部 MCP     │  ──→  │ Shell / FS /     │  ──→  │ Skill 商店       │
│ 动态工具注册     │       │ Browser 控制     │       │ 一键安装         │
│ Server 管理 UI   │       │ 沙箱安全层       │       │ 社区生态         │
└──────────────────┘       └──────────────────┘       └──────────────────┘
   基础：打通外部           底层：操作电脑              生态：人人贡献
```

---

## Phase 1: MCP Client — 连通外部生态

> **目标：** EverEvo 能连接任意 MCP Server（本地/远程），将其工具动态注册到内部工具表，在 Chat + Skill 中直接使用。

### 1.1 MCP Client 连接管理层

**新文件：** `internal/mcp/client/manager.go`

```
Manager 职责：
- 管理多个 MCP Server 连接（增删改查）
- 每个连接维护一个 JSON-RPC 会话（stdio 或 HTTP）
- 启动时自动重连已配置的 Server
- 定期心跳检测连接健康状态
```

**数据结构：**

```go
// MCPConnection 表示一个已连接的外部 MCP Server
type MCPConnection struct {
    ID        string       `json:"id"`
    Name      string       `json:"name"`
    Transport string       `json:"transport"` // "stdio" | "http"
    Command   string       `json:"command,omitempty"`   // stdio: 可执行文件路径
    Args      []string     `json:"args,omitempty"`      // stdio: 启动参数
    URL       string       `json:"url,omitempty"`       // http: 端点地址
    Status    string       `json:"status"` // "connected" | "disconnected" | "error"
    Error     string       `json:"error,omitempty"`

    // 运行时字段（不持久化）
    client    *rpc.Client  // JSON-RPC 客户端
    tools     []*tools.ToolDef // 从该 Server 获取的工具列表
}
```

**传输层实现：**

| Transport | 实现方式 | 适用场景 |
|---|---|---|
| `stdio` | `os/exec` 启动子进程，stdin/stdout JSON-RPC | 本地 MCP Server（如 Filesystem、GitHub） |
| `http` | `net/http` Streamable HTTP | 远程 MCP Server |

**关键操作：**
- `Connect(id string) error` — 建立连接，发送 `initialize` + `tools/list`
- `Disconnect(id string) error` — 关闭连接
- `ListConnections() []MCPConnection`
- `AddConnection(cfg MCPConnection) error` — 新增配置
- `RemoveConnection(id string) error` — 删除配置
- `RefreshTools(id string) error` — 重新获取工具列表（支持 `tools/listChanged`）

**持久化：** `<exe>/data/mcp_servers.json`

### 1.2 动态工具注册

**修改文件：** `internal/tools/registry.go`

当前 `registry` 是静态 `map[string]*ToolDef`，所有工具在 `RegisterAll()` 时注册。需要改造为支持动态添加/移除：

```go
// 新增：动态注册接口
func RegisterExternal(t *ToolDef, source string)  // source = connection ID
func UnregisterExternal(source string)             // 断开时清理该 Server 的所有工具
func ListBySource(source string) []*ToolDef       // 按来源列出工具
```

**工具命名规则：** 外部工具以 `mcp.<connection_id>.<tool_name>` 格式注册，避免与内部工具冲突。在 LLM 调用时展示为 `[Server名称] 工具名`。

### 1.3 工具分发改造

**修改文件：** `app.go` — `CallTool()` 方法（~2027-2281 行）

当前的巨型 switch-case 需要增加一个 fallback 分支：

```go
func (a *App) CallTool(name string, params map[string]any) tools.ToolResult {
    // 1. 先查内部工具（现有 switch）
    // 2. 若无匹配，查 MCP Client 工具表
    // 3. 若找到，转发到对应 MCP Server 执行（tools/call）
    // 4. 返回结果
}
```

`internal/mcp/client/` 需要新增 `CallTool(serverID, toolName string, params map[string]any) (*tools.ToolResult, error)` 方法。

### 1.4 MCP Server 管理 UI

**修改文件：** `frontend/src/components/AICapability.vue`

在现有 Tab 栏新增 "MCP Server" 标签页：

```
◎ MCP 服务 | ⊞ 能力清单 | ⚙ 模型配置 | ⇌ MCP Server
```

**页面内容：**

```
┌ MCP Server ─────────────────────────────────────┐
│                                                  │
│  ┌ Filesystem ──────────────────────────────┐   │
│  │ ● 已连接  12 工具  本地 stdio            │   │
│  │ [断开] [刷新] [查看工具]                 │   │
│  └──────────────────────────────────────────┘   │
│                                                  │
│  ┌ GitHub ──────────────────────────────────┐   │
│  │ ○ 已断开  HTTP 超时                      │   │
│  │ [连接] [编辑] [删除]                     │   │
│  └──────────────────────────────────────────┘   │
│                                                  │
│  [+ 添加 MCP Server]                            │
│                                                  │
│  添加方式:                                       │
│  ○ 命令行:  [npx -y @modelcontextprotocol/...] │
│  ○ HTTP:    [https://mcp.example.com]           │
│  ○ 推荐列表: [Filesystem] [GitHub] [Puppeteer]  │
│                                                  │
└──────────────────────────────────────────────────┘
```

**添加 Server 对话框：**
- 名称（自定义）
- 传输方式（stdio / HTTP）
- 命令行（stdio 模式）
- URL（HTTP 模式）
- 环境变量（可选）
- 预设推荐列表（一键添加社区热门 MCP Server）

### 1.5 Chat 集成

**修改文件：** `frontend/src/store/chatStore.js`

当前 `loadSkills()` → `GetEnabledToolNames()` 只返回内部工具名。需要改为：

```
loadSkillsAndMCPTools():
  1. 获取启用的 Skill → 得到内部工具名列表
  2. 获取所有已连接的 MCP Server 的工具列表
  3. 合并 → 传给 LLM
```

工具在 LLM 上下文中展示时加来源标注：`[Filesystem] read_file`、`[GitHub] search_code`。

### 1.6 Skill 扩展

**修改文件：** `internal/skills/skill.go`

Skill 结构新增字段：

```go
type Skill struct {
    // ... 现有字段 ...
    MCPServers []string `json:"mcpServers,omitempty"` // 依赖的 MCP Server ID 列表
    MCPTools   []string `json:"mcpTools,omitempty"`   // 引用的外部工具名
}
```

**前端修改：** `AICapability.vue` Skill 编辑对话框增加 "外部工具" 选择区——从已连接的 MCP Server 拉取工具列表，勾选需要的工具。

### 1.7 Go API 层

**修改文件：** `app.go`

新增以下 Wails 绑定方法：

```go
// MCP Server 管理
func (a *App) ListMCPServers() []mcpclient.MCPConnection
func (a *App) AddMCPServer(cfg mcpclient.MCPConnection) error
func (a *App) RemoveMCPServer(id string) error
func (a *App) ConnectMCPServer(id string) error
func (a *App) DisconnectMCPServer(id string) error
func (a *App) GetMCPServerTools(id string) ([]*tools.ToolDef, error)
func (a *App) ListMCPRecommends() []mcpclient.RecommendInfo // 预设推荐列表
```

### Phase 1 验证清单

- [ ] 能通过 stdio 连接本地 MCP Server（如 `npx @modelcontextprotocol/server-filesystem`）
- [ ] 能通过 HTTP 连接远程 MCP Server
- [ ] 外部工具出现在 Chat 的 LLM 工具列表中
- [ ] LLM 调用外部工具后返回正确结果
- [ ] 断开 Server 后工具自动移除
- [ ] 重启应用后自动重连已配置的 Server
- [ ] UI 可增删改查 MCP Server
- [ ] Skill 中可引用外部工具

---

## Phase 2: 系统级能力 — AI 操作电脑

> **目标：** 通过接入社区 MCP Server 获得 Shell 执行、文件读写、浏览器控制能力，并加上必要的安全层。

### 2.1 Shell 执行

**推荐方案：** 直接接入社区 MCP Server，不自研。

推荐列表：
- `@anthropic/mcp-server-commands` — Anthropic 官方 Shell MCP
- `@modelcontextprotocol/server-everything` — 社区通用执行器

**安全层（必须）：**

新增 `internal/security/sandbox.go`：

```go
// CommandPolicy 定义 Shell 执行的安全策略
type CommandPolicy struct {
    AllowList    []string // 允许的命令路径（空 = 全部允许）
    DenyList     []string // 禁止的命令（如 rm -rf, format）
    WorkingDir   string   // 限制工作目录
    MaxTimeout   int      // 最大执行时间（秒）
    ConfirmFirst bool     // 是否需先确认
}
```

**默认策略：**
- 任何 `rm -rf`、`format`、`shutdown`、写入系统目录等操作需用户确认
- 设置 60 秒超时
- 工作目录默认为 EXE 所在目录

**UI 确认流程：**
```
┌ ⚠ 危险操作确认 ─────────────────────────┐
│                                          │
│  AI 想要执行:                             │
│  $ rm -rf /some/path                     │
│                                          │
│  [拒绝]  [仅此一次]  [始终允许此命令]     │
│                                          │
└──────────────────────────────────────────┘
```

### 2.2 文件系统

**推荐方案：** 接入 `@modelcontextprotocol/server-filesystem`

这是最成熟的 MCP Server 之一，提供 `read_file`、`write_file`、`list_directory`、`search_files` 等完整文件操作。

**安全层：**
- 默认限制在 EXE 目录 + 用户文档目录
- 系统目录（`C:\Windows`、`C:\Program Files`）默认只读
- 写入前显示 diff/preview

### 2.3 浏览器控制

**推荐方案：** 接入 `@anthropic/mcp-server-puppeteer` 或 `@modelcontextprotocol/server-playwright`

能力：
- 导航到 URL
- 截图
- 点击、输入
- 执行 JS
- 提取页面内容

安全注意事项：
- 默认不访问 localhost / 内网地址
- 不保存 cookie / session 到磁盘（可选）

### 2.4 安全框架

**新文件：** `internal/security/`

```
internal/security/
├── policy.go      — 策略定义和加载
├── sandbox.go     — Shell 执行沙箱
├── confirm.go     — 用户确认模型（always/ask/never）
└── audit.go       — 操作审计日志
```

**策略持久化：** `<exe>/data/security_policy.json`

**审计日志：** `<exe>/data/audit.log` — 记录所有 Shell 执行和文件写入操作。

### Phase 2 验证清单

- [ ] 能通过 MCP Server 执行 Shell 命令并获取输出
- [ ] 危险命令触发确认对话框
- [ ] 文件读写受路径白名单限制
- [ ] 浏览器能打开网页、截图、点击
- [ ] 审计日志记录所有敏感操作
- [ ] 安全策略可通过 UI 配置

---

## Phase 3: Skill 市场 — 生态化分发

> **目标：** 用户可以分享和安装 Skill 包，形成社区生态。

### 3.1 Skill 包格式

**文件扩展名：** `.mbskill.json`

```json
{
  "schema": "EverEvo-skill-v1",
  "name": "code-reviewer",
  "title": "代码审查助手",
  "description": "自动审查 PR 并给出改进建议",
  "author": "username",
  "version": "1.0.0",
  "icon": "◈",
  "category": "dev",

  "mcpServers": [
    {
      "name": "github",
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "{{ask}}" }
    }
  ],

  "tools": [
    "mcp.github.search_code",
    "mcp.github.create_pull_request_review"
  ],

  "prompt": "你是一个资深代码审查员。审查代码时关注：安全漏洞、性能问题、代码风格、潜在 bug。每条建议附带具体行号和修改方案。",

  "knowledge": {
    "rules": ["检查 SQL 注入", "检查 XSS", "检查敏感信息泄露"]
  }
}
```

**关键设计：**
- `mcpServers` — 声明依赖哪些 MCP Server，安装时自动添加和连接
- `{{ask}}` 占位符 — 安装时提示用户输入（如 API Key）
- `prompt` — 内置 system prompt，定义 Agent 角色行为
- `knowledge.rules` — 审查规则列表，注入到 LLM 上下文

### 3.2 本地 Skill 市场

**新文件：** `internal/marketplace/market.go`

```
市场后端职责：
- 维护一个 skill 注册表（先本地 JSON，后对接 GitHub）
- 搜索 / 浏览 / 分类
- 安装（解析依赖 → 添加 MCP Server → 导入 Skill → 启用）
- 卸载（清理依赖）
- 版本检查
```

**内置市场数据：** `<exe>/data/marketplace.json`

预置 10+ 常用 Skill 包：
- 代码审查
- 数据分析（接入 SQLite MCP）
- 文件管理
- 网页抓取
- 自动化测试
- ...

### 3.3 市场 UI

**修改文件：** `frontend/src/components/AICapability.vue`

"能力清单" Tab 顶部增加 "市场" 按钮：

```
能力清单   [浏览市场]   [导入 Skill]

┌ 市场 ───────────────────────────────────────────┐
│  🔍 搜索...                                      │
│                                                  │
│  ┌ 代码审查助手 ────────────────────────────┐   │
│  │ ◈ 自动审查 PR，给出改进建议              │   │
│  │ 依赖: GitHub MCP                        │   │
│  │ 作者: xxx  ★ 4.8  已装 1200+           │   │
│  │ [安装]                                   │   │
│  └──────────────────────────────────────────┘   │
│                                                  │
│  ┌ 数据分析师 ──────────────────────────────┐   │
│  │ ≡ 连接数据库，SQL 查询，生成报表          │   │
│  │ 依赖: SQLite MCP, Filesystem MCP         │   │
│  │ [安装]                                   │   │
│  └──────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

### 3.4 在线市场（远期）

当本地市场稳定后：
- GitHub Repo 作为官方市场源：`EverEvo-marketplace/skills`
- `marketplace.json` 从 GitHub 拉取，定期同步
- 用户可提交 PR 贡献 Skill
- 版本更新通知

### Phase 3 验证清单

- [ ] `.mbskill.json` 可导出/导入
- [ ] 安装 Skill 自动添加依赖的 MCP Server
- [ ] 安装时 `{{ask}}` 占位符提示用户输入
- [ ] Skill market 页面可浏览/搜索/安装
- [ ] 卸载 Skill 清理关联资源
- [ ] 预置 Skill 包可正常使用

---

## 实施顺序与时间估算

| 阶段 | 子任务 | 预估工作量 | 依赖 |
|---|---|---|---|
| **P1.1** | MCP Client 连接管理 | 3-4 天 | — |
| **P1.2** | 动态工具注册 | 1 天 | P1.1 |
| **P1.3** | 工具分发改造 | 1 天 | P1.2 |
| **P1.4** | MCP Server 管理 UI | 2 天 | P1.1 |
| **P1.5** | Chat 集成 | 1 天 | P1.2 |
| **P1.6** | Skill 扩展 | 1 天 | P1.2 |
| **P1.7** | Go API 层 | 1 天 | P1.1 |
| **P2.1** | Shell 执行（接入社区 MCP） | 1 天 | P1 |
| **P2.2** | 文件系统（接入社区 MCP） | 0.5 天 | P1 |
| **P2.3** | 浏览器控制（接入社区 MCP） | 0.5 天 | P1 |
| **P2.4** | 安全框架 | 2 天 | P2.1 |
| **P3.1** | Skill 包格式 | 1 天 | P1 |
| **P3.2** | 本地 Skill 市场后端 | 2 天 | P3.1 |
| **P3.3** | 市场 UI | 2 天 | P3.2 |
| **P3.4** | 在线市场 | 后期 | P3.3 |

**总计：Phase 1 约 10 天，Phase 2 约 4 天，Phase 3 约 5 天。**

---

## 关键设计决策

1. **不自研 Shell/FS/Browser → 接入社区 MCP Server。** 社区已有成熟实现，重复造轮子没有价值。EverEvo 的价值在于"连接和编排"这些能力。

2. **Skill = MCP Server 依赖 + 工具筛选 + System Prompt。** 当 Skill 可以声明依赖的 MCP Server 并带自定义 prompt 后，它从"工具开关"升级为"智能体角色定义"。

3. **安全层必须第一天就做。** MCP Client 导入外部能力的同时也导入了风险。Shell 执行 + 文件写入的安全策略是底线。
