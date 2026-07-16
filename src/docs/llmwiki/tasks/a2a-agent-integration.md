# A2A Agent Integration Plan

> A2A (Agent-to-Agent) 协议完整接入方案：本地 Agent 通信为主，保留客户端和服务端双模能力。
> **Status: IMPLEMENTED (2026-07-07) — Steps 1-8 complete. See verification results below.**

---

## 1. 背景与目标

### 1.1 当前状态

- `internal/a2a/types.go` 已有 Google A2A 协议完整类型定义（AgentCard、Message、Task、JSON-RPC 等）
- **无服务端实现、无客户端实现、无管理层、无 UI** — 纯骨架

### 1.2 目标

给 EverEvo 接入完整的 A2A 协议栈：

| 层级 | 内容 | 说明 |
|------|------|------|
| **A2A Server** | 启动本地 HTTP JSON-RPC 端点 | 暴露自身能力，供其他 Agent 调用 |
| **A2A Client** | 连接远程 A2A Agent | 发现对方能力，发送任务 |
| **A2A Manager** | 统一管理 Server + 多个 Client 连接 | 启动/停止、连接/断开、任务追踪 |
| **Agent Config UI** | 大语言模型页新增「Agent」Tab | 配置本机 Agent Card、管理远端 Agent 连接 |
| **Config Persistence** | Agent 配置持久化到 user_config.json | 重启后自动恢复 |

### 1.3 核心定位

- **主要场景**：本地 Agent 通信（loopback `127.0.0.1`），同一台机器上多个 EverEvo 实例或 Agent 进程互相调用
- **保留能力**：同时可作为客户端连接远端 Agent，或作为服务端接受远端调用
- **优先级**：Server 能力优先（让别人调我），Client 能力紧随（我调别人）

---

## 2. 架构设计

```
┌──────────────────────────────────────────────────────┐
│  Frontend (LLMConfig.vue → Agent Tab)                │
│  ┌────────────────────────────────────────────────┐  │
│  │  LLMAgent.vue                                   │  │
│  │  ├─ Local Agent Card (name, description, port)  │  │
│  │  ├─ Server Start/Stop toggle                    │  │
│  │  ├─ Remote Agent list (connect/disconnect)      │  │
│  │  └─ AgentDialog.vue (add/edit remote agent)     │  │
│  └────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────┤
│  API Layer (api/agent.ts)                             │
│  ├─ getAgentConfig()                                  │
│  ├─ updateAgentCard()                                 │
│  ├─ startAgentServer()                                │
│  ├─ stopAgentServer()                                 │
│  ├─ listRemoteAgents()                                │
│  ├─ addRemoteAgent()                                  │
│  ├─ removeRemoteAgent()                               │
│  ├─ connectRemoteAgent()                              │
│  ├─ disconnectRemoteAgent()                           │
│  └─ sendTask() / sendTaskStream()                     │
└──────────────────────────────────────────────────────┘
         │ Wails bridge
┌──────────────────────────────────────────────────────┐
│  Backend (app_agent.go)                               │
│  ├─ a2aManager *a2a.Manager                          │
│  └─ all public methods bound to frontend             │
├──────────────────────────────────────────────────────┤
│  internal/a2a/                                        │
│  ├─ types.go          (existing — type defs)         │
│  ├─ server.go         (NEW — HTTP JSON-RPC server)   │
│  ├─ client.go         (NEW — HTTP JSON-RPC client)   │
│  ├─ manager.go        (NEW — orchestration layer)    │
│  └─ store.go          (NEW — connection persistence) │
├──────────────────────────────────────────────────────┤
│  internal/config/                                     │
│  └─ config.go         (MODIFY — add AgentConfig)     │
└──────────────────────────────────────────────────────┘
```

---

## 3. 数据结构设计

### 3.1 配置模型（`internal/config/config.go`）

```go
// A2AAgentConfig holds the local agent identity and server settings.
type A2AAgentConfig struct {
    Enabled     bool   `json:"enabled"`     // auto-start agent server
    Name        string `json:"name"`        // agent card name
    Description string `json:"description"` // agent card description
    Version     string `json:"version"`     // agent card version
    Port        int    `json:"port"`        // A2A server listen port (default 19801)
}

// A2ARemoteAgent holds connection info for a remote A2A agent.
type A2ARemoteAgent struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    URL         string `json:"url"`         // e.g. "http://127.0.0.1:19801"
    Status      string `json:"status"`      // "connected" | "disconnected" | "error"
    Error       string `json:"error,omitempty"`
    Card        *a2a.AgentCard `json:"card,omitempty"` // fetched from /.well-known/agent-card.json
    ConnectedAt int64  `json:"connectedAt,omitempty"`
}
```

`LLMConfig` 新增字段：
```go
type LLMConfig struct {
    // ... existing ...
    A2AConfig      A2AAgentConfig  `json:"a2aConfig"`
    RemoteAgents   []A2ARemoteAgent `json:"remoteAgents,omitempty"`
}
```

### 3.2 Manager 模型（`internal/a2a/manager.go`）

```go
type Manager struct {
    mu      sync.RWMutex
    cfg     config.A2AAgentConfig
    server  *Server
    clients map[string]*Client
    store   *Store
    onEvent func(event string, data interface{}) // Wails EventsEmit callback
}
```

---

## 4. 模块实现计划

### Phase 1: A2A Server（基础能力）

#### 4.1 `internal/a2a/server.go` — HTTP JSON-RPC Server

**职责**：启动 HTTP 服务器，处理 A2A JSON-RPC 请求。

**端点**：
| 路径 | 方法 | 说明 |
|------|------|------|
| `/.well-known/agent-card.json` | GET | 返回 AgentCard |
| `/` | POST | JSON-RPC 2.0 统一入口 |

**支持的 JSON-RPC methods**：
| Method | 说明 |
|--------|------|
| `tasks/send` | 创建并执行一个任务（同步返回结果） |
| `tasks/get` | 查询任务状态和结果 |
| `tasks/cancel` | 取消正在执行的任务 |

**实现细节**：
- 使用 `net/http` 标准库，不引入第三方框架
- JSON-RPC 请求解析 → 路由到 handler → 序列化返回
- 任务执行内部复用 EverEvo 现有的 Chat 能力（调用 LLM + Tool Calling Loop）
- 任务存储：内存 map，键为 task ID
- 优雅关闭：`http.Server.Shutdown()`

**文件**：`internal/a2a/server.go`（~200 行）

#### 4.2 `internal/a2a/client.go` — HTTP JSON-RPC Client

**职责**：连接远程 A2A Agent，发送任务。

**核心方法**：
```go
func NewClient(url string) *Client
func (c *Client) Connect(ctx context.Context) error     // GET /.well-known/agent-card.json → validate
func (c *Client) Ping(ctx context.Context) error          // quick health check
func (c *Client) SendTask(ctx context.Context, msg Message) (*Task, error)      // tasks/send
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error)     // tasks/get
func (c *Client) CancelTask(ctx context.Context, taskID string) error            // tasks/cancel
```

**实现细节**：
- `net/http.Client` + `encoding/json` 构建 JSON-RPC 请求
- 超时默认 60s（可配置）
- 错误处理：网络超时、JSON-RPC error 统一包装

**文件**：`internal/a2a/client.go`（~150 行）

#### 4.3 `internal/a2a/manager.go` — 统一管理层

**职责**：
- 管理 Server 生命周期（Start / Stop）
- 管理多个 Client 连接（Add / Remove / Connect / Disconnect）
- 持久化远程 Agent 列表
- 通过回调通知前端状态变化

**核心方法**：
```go
func NewManager(onEvent func(string, interface{})) *Manager

// Server
func (m *Manager) StartServer(cfg config.A2AAgentConfig) error
func (m *Manager) StopServer() error
func (m *Manager) ServerStatus() ServerStatus

// Clients
func (m *Manager) ListRemoteAgents() []config.A2ARemoteAgent
func (m *Manager) AddRemoteAgent(agent config.A2ARemoteAgent) error
func (m *Manager) RemoveRemoteAgent(id string) error
func (m *Manager) ConnectRemoteAgent(id string) error
func (m *Manager) DisconnectRemoteAgent(id string) error
func (m *Manager) SendTask(agentID string, msg a2a.Message) (*a2a.Task, error)
```

**文件**：`internal/a2a/manager.go`（~200 行）

#### 4.4 `internal/a2a/store.go` — 持久化层

**职责**：将 Remote Agent 列表持久化到磁盘。

- 路径：`<data>/a2a_agents.json`
- 格式：`[]A2ARemoteAgent` JSON 数组
- 原子写入（复用 `internal/atomic`）

**文件**：`internal/a2a/store.go`（~60 行）

### Phase 2: 后端 API 层

#### 4.5 `app_agent.go` — Wails 绑定方法

**新增文件**，负责将 A2A Manager 的能力暴露给前端。

```go
// app_agent.go

// Agent config
func (a *App) GetA2AConfig() config.A2AAgentConfig
func (a *App) UpdateA2AConfig(cfg config.A2AAgentConfig) error

// Server control
func (a *App) StartAgentServer() error
func (a *App) StopAgentServer() error
func (a *App) GetAgentServerStatus() a2a.ServerStatus

// Remote agents
func (a *App) ListRemoteAgents() []config.A2ARemoteAgent
func (a *App) AddRemoteAgent(agent config.A2ARemoteAgent) error
func (a *App) RemoveRemoteAgent(id string) error
func (a *App) ConnectRemoteAgent(id string) error
func (a *App) DisconnectRemoteAgent(id string) error

// Task operations
func (a *App) SendAgentTask(agentID string, msg a2a.Message) (*a2a.Task, error)
func (a *App) GetAgentTask(agentID, taskID string) (*a2a.Task, error)
```

**文件**：`app_agent.go`（~150 行）

#### 4.6 `app.go` 修改

在 `App` struct 新增字段：
```go
a2aManager *a2a.Manager
```

在 `startup()` 中初始化：
```go
a.a2aManager = a2a.NewManager(func(event string, data interface{}) {
    wailsRuntime.EventsEmit(a.ctx, event, data)
})
// Load persisted remote agents
a.a2aManager.LoadRemoteAgents()
// Auto-start A2A server if enabled
if a.cfg.LLM.A2AConfig.Enabled {
    go a.a2aManager.StartServer(a.cfg.LLM.A2AConfig)
}
```

在 `shutdown()` 中清理：
```go
if a.a2aManager != nil {
    a.a2aManager.StopServer()
    a.a2aManager.DisconnectAll()
}
```

### Phase 3: 配置持久化

#### 4.7 `internal/config/config.go` 修改

1. 新增 `A2AAgentConfig` 和 `A2ARemoteAgent` 结构体
2. `LLMConfig` 新增 `A2AConfig` 和 `RemoteAgents` 字段
3. `Defaults()` 中设置默认值：
   ```go
   A2AConfig: A2AAgentConfig{
       Name:    "EverEvo Agent",
       Version: "0.1.0",
       Port:    19801,
   },
   RemoteAgents: []A2ARemoteAgent{},
   ```

### Phase 4: 前端 Agent 配置页

#### 4.8 `frontend/src/api/agent.ts` — Agent API 模块

**新增文件**，遵循现有 `mcp.ts` 模式：
```ts
export interface A2AConfig {
    enabled: boolean
    name: string
    description: string
    version: string
    port: number
}

export interface RemoteAgent {
    id: string
    name: string
    url: string
    status: 'connected' | 'disconnected' | 'error'
    error?: string
    card?: any
    connectedAt?: number
}

export interface AgentServerStatus {
    running: boolean
    port: number
    url: string
}

export const agentApi = {
    // Config
    getConfig() { return call<A2AConfig>(() => App().GetA2AConfig()) },
    updateConfig(cfg: A2AConfig) { return call(() => App().UpdateA2AConfig(cfg)) },

    // Server
    getServerStatus() { return call<AgentServerStatus>(() => App().GetAgentServerStatus()) },
    startServer() { return call(() => App().StartAgentServer()) },
    stopServer() { return call(() => App().StopAgentServer()) },

    // Remote agents
    listRemoteAgents() { return call<RemoteAgent[]>(() => App().ListRemoteAgents()) },
    addRemoteAgent(agent: Partial<RemoteAgent>) { return call(() => App().AddRemoteAgent(agent)) },
    removeRemoteAgent(id: string) { return call(() => App().RemoveRemoteAgent(id)) },
    connectRemoteAgent(id: string) { return call(() => App().ConnectRemoteAgent(id)) },
    disconnectRemoteAgent(id: string) { return call(() => App().DisconnectRemoteAgent(id)) },

    // Tasks
    sendTask(agentID: string, text: string) { return call<any>(() => App().SendAgentTask(agentID, { role: 'user', parts: [{ kind: 'text', text }] })) },
    getTask(agentID: string, taskID: string) { return call<any>(() => App().GetAgentTask(agentID, taskID)) },
}
```

#### 4.9 `frontend/src/components/llm/LLMAgent.vue` — Agent 配置页

**新增文件**，作为 LLMConfig.vue 的新 Tab。

**布局设计**：
```
┌ Agent ───────────────────────────────────────────┐
│                                                   │
│  ┌ 本机 Agent 服务 ──────────────────────────┐   │
│  │  ○ / ●   运行中 / 已停止                    │   │
│  │  URL: http://127.0.0.1:19801               │   │
│  │  Card: /.well-known/agent-card.json        │   │
│  │  ┌─────────────────────────────────────┐   │   │
│  │  │ 名称:  [EverEvo Agent          ]   │   │   │
│  │  │ 描述:  [                          ]   │   │   │
│  │  │ 端口:  [19801                    ]   │   │   │
│  │  │ [保存并重启]  [启动] / [停止]      │   │   │
│  │  └─────────────────────────────────────┘   │   │
│  └────────────────────────────────────────────┘   │
│                                                   │
│  ┌ 远端 Agent 连接 ── [+ 添加远端 Agent] ────┐   │
│  │  ┌ local-agent ───────────────────────┐    │   │
│  │  │ ● 已连接  http://127.0.0.1:19802  │    │   │
│  │  │ [断开] [测试] [删除]              │    │   │
│  │  └────────────────────────────────────┘    │   │
│  │  ┌ remote-helper ─────────────────────┐    │   │
│  │  │ ○ 已断开  http://192.168.1.5:19801│    │   │
│  │  │ [连接] [编辑] [删除]              │    │   │
│  │  └────────────────────────────────────┘    │   │
│  └────────────────────────────────────────────┘   │
│                                                   │
└───────────────────────────────────────────────────┘
```

**核心状态**：
- 本地 Agent Card 配置表单（name, description, port）
- Server 启停开关 + 状态指示
- 远端 Agent 列表（连接状态、Agent Card 信息）
- 添加/编辑远端 Agent 对话框

**文件**：`frontend/src/components/llm/LLMAgent.vue`（~350 行）

#### 4.10 `frontend/src/components/llm/LLMConfig.vue` 修改

在 Tab 栏新增：
```html
<button :class="['llmcfg-tab', { active: tab === 'agent' }]" @click="tab = 'agent'">◉ 代理</button>
```

新增 Tab 内容区：
```html
<div v-if="tab === 'agent'" class="llmcfg-body">
    <LLMAgent />
</div>
```

Import 新组件：
```ts
import LLMAgent from './LLMAgent.vue'
```

#### 4.11 API 层注册

修改 `frontend/src/api/index.ts`，新增：
```ts
export { agentApi } from './agent'
export type { A2AConfig, RemoteAgent, AgentServerStatus } from './agent'
```

---

## 5. 实施步骤

### Step 1: A2A Server 实现
- [ ] 新建 `internal/a2a/server.go`
  - HTTP Server 启动/停止
  - `/.well-known/agent-card.json` 端点
  - JSON-RPC 统一入口（`tasks/send`, `tasks/get`, `tasks/cancel`）
  - 任务执行器（调用 EverEvo Chat 能力）
  - 任务状态管理（内存存储）
- **验证**：启动 Server → curl `http://127.0.0.1:19801/.well-known/agent-card.json` 返回 JSON
- **验证**：发送 `tasks/send` JSON-RPC 请求 → 返回 Task 结果

### Step 2: A2A Client 实现
- [ ] 新建 `internal/a2a/client.go`
  - 连接远端 Agent（GET agent-card → 验证）
  - tasks/send, tasks/get, tasks/cancel
  - 超时和错误处理
- **验证**：Client 连接 Step 1 的 Server → 发送任务 → 收到响应

### Step 3: A2A Manager 实现
- [ ] 新建 `internal/a2a/manager.go`
  - Server 生命周期管理
  - Client 连接池管理
  - 事件回调（通知前端状态变化）
- [ ] 新建 `internal/a2a/store.go`
  - Remote Agent 列表持久化
- **验证**：Manager 启动 Server + 连接本地 Client → 任务闭环

### Step 4: 配置模型扩展
- [ ] 修改 `internal/config/config.go`
  - 新增 `A2AAgentConfig`、`A2ARemoteAgent` 结构体
  - `LLMConfig` 新增 `A2AConfig` 和 `RemoteAgents` 字段
  - `Defaults()` 设置默认值
- **验证**：编译通过，配置序列化/反序列化正确

### Step 5: 后端 API 层
- [ ] 新建 `app_agent.go`
  - 所有 Wails 绑定方法
  - 每个变更操作后 emit `agent:changed` 事件
- [ ] 修改 `app.go`
  - 新增 `a2aManager` 字段
  - `startup()` 初始化 + 自动恢复
  - `shutdown()` 清理
- **验证**：Wails 编译通过，前端 `window.go.main.App.GetA2AConfig()` 可调用

### Step 6: 前端 API 模块
- [ ] 新建 `frontend/src/api/agent.ts`
- [ ] 修改 `frontend/src/api/index.ts` 注册导出
- **验证**：API 模块类型正确，IDE 无报错

### Step 7: Agent 配置 UI
- [ ] 新建 `frontend/src/components/llm/LLMAgent.vue`
  - 本机 Agent Card 配置表单
  - Server 启停控制
  - 远端 Agent 列表
  - 添加/编辑对话框（inline dialog）
- [ ] 修改 `frontend/src/components/llm/LLMConfig.vue`
  - 新增 "代理" Tab
- **验证**：UI 可操作，数据可读写

### Step 8: 集成测试
- [ ] 本地 loopback 测试：Server 启动 → Client 连接 `127.0.0.1:19801` → 发送任务 → 收到结果
- [ ] 配置持久化测试：修改 Agent 配置 → 重启应用 → 配置恢复
- [ ] 远端 Agent 连接/断开测试
- [ ] UI 完整操作流测试

---

## 6. 文件清单

| 操作 | 文件路径 | 说明 |
|------|----------|------|
| **NEW** | `internal/a2a/server.go` | A2A HTTP JSON-RPC Server |
| **NEW** | `internal/a2a/client.go` | A2A HTTP JSON-RPC Client |
| **NEW** | `internal/a2a/manager.go` | Server + Client 统一管理层 |
| **NEW** | `internal/a2a/store.go` | Remote Agent 持久化 |
| **MODIFY** | `internal/a2a/types.go` | 可能需要补充少量类型 |
| **MODIFY** | `internal/config/config.go` | 新增 Agent 配置结构 |
| **NEW** | `app_agent.go` | Wails 绑定 API |
| **MODIFY** | `app.go` | 集成 a2aManager |
| **NEW** | `frontend/src/api/agent.ts` | 前端 Agent API |
| **MODIFY** | `frontend/src/api/index.ts` | 注册导出 |
| **NEW** | `frontend/src/components/llm/LLMAgent.vue` | Agent 配置 UI |
| **MODIFY** | `frontend/src/components/llm/LLMConfig.vue` | 新增 Agent Tab |

---

## 7. 验证清单

- [ ] A2A Server 启动成功，`/.well-known/agent-card.json` 可访问
- [ ] A2A JSON-RPC `tasks/send` 可接收并返回结果
- [ ] A2A Client 可连接远端 Agent 并获取 AgentCard
- [ ] A2A Client 可发送任务并接收响应
- [ ] Manager 可同时管理 Server + 多个 Client
- [ ] 配置持久化：重启后 Agent 设置和远端连接列表恢复
- [ ] 配置持久化：重启后若 `enabled=true` 则自动启动 Server
- [ ] UI：本机 Agent Card 配置可编辑保存
- [ ] UI：Server 启停按钮可用，状态正确显示
- [ ] UI：远端 Agent 列表可增删改查
- [ ] UI：远端 Agent 连接/断开按钮可用
- [ ] 本地 loopback：自己连自己，发送任务闭环
- [ ] 与 Chat 模块集成：Agent 调用的 LLM 回复与主 Chat 一致
