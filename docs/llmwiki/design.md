# EverEvo — Project Design

> Living summary. Read this before working on the project; update it when the design changes.
> Counterpart to the top-level `README.md`, which covers build/run/usage for end users.

## Overview

EverEvo 是一个**开源模型工具箱**：一站式「找模型、下模型、按类型用模型」。基于 Wails v2（Go 后端 + Vue 3 前端）。

核心理念：AI 模型的输入/输出语义差异巨大（句向量要两段文本算相似度、图像分类要一张图、检测要画框），通用「文本框」硬套不实用。EverEvo 按**模型类型**组织专门的推理工具，每个工具的输入/输出都符合该类型的语义。

## 产品定义（2026-06 重定义）

此前定位是「通用模型加载/运行器」（一个文本框跑任何模型）。这被证明不实用，故转向：

- **目标用户**：开源社区（需 README + 各工具说明 + 稳定 UX）。
- **三模块**：
  1. **市场**（保留）：HF / MS 模型浏览 / 搜索 / 下载集成站。已可用。
  2. **模型库**：已下载模型，按类型归类索引。
  3. **工具箱**（新核心）：按模型类型的推理工具。先支持两类：
     - 句向量 → 语义相似度（两文本→分数）/ 语义搜索（句库→Top-K）
     - 图像分类 → 选图 → 类别名 + 置信度
- **工具框架**：加载模型 → **探测类型**（metadata / `config.json` 的 task / I/O 形状启发式）→ 归类 → 对应工具；识别不了的不硬跑。
- **取代**：旧「我的模型」页的通用文本运行框（`ModelCard.doRun`）将被工具箱取代——它对异构模型不实用。MiniLM 的真实推理（tokenize + mean-pool，Phase 1 成果）不浪费，成为句向量工具的内核。

## Architecture（实际）

```
Frontend (Vue 3 + Vite) → Go Backend (Wails v2 API, 15 app_*.go)
                              ├─ ONNX Runtime   (yalue/onnxruntime_go, CGo, 运行时加载 onnxruntime.dll)
                              ├─ Tokenizer      (sugarme/tokenizer, 纯 Go)
                              ├─ Catalog        (HF / ModelScope 市场接入)
                              ├─ Downloader     (分段并发 + 队列 + 自动重试 + 断点续传)
                              ├─ MCP Server     (HTTP JSON-RPC 2.0, tools/resources/prompts)
                              ├─ MCP Client     (对接外部 MCP Server)
                              ├─ Workflow       (DAG 引擎 + 节点解析 + 条件分支)
                              ├─ Plugin         (外部插件加载 + 生命周期)
                              ├─ RAG            (切片 + 嵌入 + chromem-go 向量检索)
                              ├─ Memory         (会话持久化 SQLite + 时序知识图谱 + 分层记忆[核心/衰减/TTL] + 硬件自适应策略)
                              ├─ Skills         (9 内置 Skill, .mbskill.json 导入/导出)
                              ├─ Toolbox        (模型类型探测 + 句向量/图像分类)
                              ├─ Marketplace    (社区模型分享)
                              ├─ Agents         (本地 Agent 人格 + 编排: agent_create/run)
                              ├─ Context        (三层上下文模型: 轻量索引 + 按需加载 + 输出压缩)
                              └─ Config / Storage / Install / SysInfo / Auth / Security / Guides
```

| 层 | 技术 | 职责 |
|----|------|------|
| GUI | Wails v2 + Vue 3.5 + TypeScript + Pinia + vue-router | 桌面窗口、市场、工具箱 UI、AI 能力管理、工作流编辑器 |
| 前端架构 | 2026-07-04 全面迁移到 `<script setup lang="ts">`；2026-07-05 代码质量大修；2026-07-06 Pinia v3 响应式修复；2026-07-07 本地 Agent 人格 | 35 组件（Knowledge 三 Tab + ModelDetailPanel + LLMAgents），11 模块 API 层（0 处 `window` 绕过），3 Pinia stores，`useToast` composable，代码分割懒加载。**Pinia v3 关键规则：setup store 返回值被 `reactive()` 包裹，`store.xxx` 自动解包 ref/computed → 必须使用 `storeToRefs(store)` 提取响应式引用，禁止 `const x = store.refName` 直接赋值。** |
| 迁移知识库 | `docs/llmwiki/tasks/vue3-migration-pitfalls.md` | Options API → `<script setup>` 常见坑（`$emit`、props 解构丢响应式、KeepAlive 匹配等） |
| 后端 | Go（17 个 app_*.go 按模块解耦，2026-07-05） | 业务逻辑：市场/下载/模型/MCP/工作流/插件/RAG/Skill/市场/引导/系统；tools registry RWMutex 保护 |
| ONNX | yalue/onnxruntime_go (CGo) | ONNX 推理（运行时加载 `onnxruntime.dll` 1.26） |
| 分词 | sugarme/tokenizer (纯 Go) | NLP 模型分词（BERT WordPiece 等） |

> **历史设计**曾规划 Rust 引擎 + CGo FFI（`internal/bridge` / `engine/`），**未实现**。项目当前为纯 Go + 一个 CGo 依赖（yalue，用于 ONNX）。

## Key Modules

### 后端入口层（解耦后的 app.go，2026-07-05）

原单体 `app.go` 拆分为 17 个文件按模块组织：

- `main.go` — 入口；启动 Wails。
- `app.go` — Wails `App` 核心（startup/shutdown 中初始化所有内部组件：ONNX、catalog、downloader、MCP server、skill manager、workflow engine 等）。
- `app_catalog.go` — 模型市场 API（搜索 / 详情 / 文件树 / 下载文件 / 缓存）。
- `app_download.go` — 下载管理 API（启动 / 暂停 / 恢复 / 取消 / 历史 / 引擎下载）。
- `app_models.go` — 模型加载 / 卸载 / 运行 + 模型文件管理（ListDownloadedModels / ListToolModels / EmbedTexts）。
- `app_tools.go` — 工具调用分发（48+ 工具 handler map + CallTool 路由）。
- `app_chat.go` — AI 聊天 API（Anthropic / OpenAI 流式对话 + Tool Calling Loop）。
- `app_mcp.go` — MCP Server/Client API（启停 + 端口 + 外部 MCP 服务管理 + ResourceProvider）。
- `app_marketplace.go` — Skill 市场 API（浏览 / 安装 / 卸载 / 刷新，2026-07-05 从 app_mcp.go 拆出）。
- `app_skills.go` — Skill 系统 API（列出 / 启用 / 导出 / 导入）。
- `app_providers.go` — LLM 供应商配置 API。
- `app_capability.go` — AI 能力探测 API（并行探测供应商可达性，2026-07-05 移出快捷方式方法）。
- `app_workflow.go` — 工作流引擎 API（CRUD + 执行 + 适配器）。
- `app_knowledge.go` — 知识库（RAG）API。
- `app_plugins.go` — 插件管理 API。
- `app_guides.go` — 引导中心 API。
- `app_system.go` — 系统信息 + 文件操作 + 开始菜单快捷方式 API。

### 后端内部模块

- `internal/model/` — `ModelRunner` 接口 + 生命周期 manager。Runners：Placeholder（占位）、ONNX（**真实推理**：tokenize→RunInt64→attention-mask 加权 mean-pool）、Llama（**stub**）、SafeTensors（仅元数据）、PyTorch（占位）。
- `internal/backends/onnx/` — ONNX 绑定（yalue）。`onnx.go`（幂等 Init/Close）、`session.go`（`LoadModel` / `RunInt64` 按 input name 构造张量）。
- `internal/backends/llama/` — llama.cpp 绑定。**当前 stub**（Windows Go 绑定生态不可用，见 Key Decisions）。
- `internal/backends/safetensors/` — SafeTensors 格式解析。
- `internal/tokenizer/` — 分词（sugarme）。加载模型自带 `tokenizer.json`，复现 BERT WordPiece。
- `internal/catalog/` — HF / MS 模型市场（搜索 / 详情 / 文件列表 / 缓存 / 鉴权）。
- `internal/downloader/` — 下载引擎（分段并发 + 队列 `<maxConcurrent 3>` + 自动重试 3 次指数退避 + 断点续传）。
- `internal/mcp/` — 内置 MCP Server（HTTP JSON-RPC 2.0，tools/resources/prompts）+ MCP 客户端（对接外部 Server）。
- `internal/tools/` — 工具注册表（9 类工具：model / plugin / kb / catalog / download / system / toolbox / guide / workflow），MCP 兼容 ToolDef。全局 registry 由 `sync.RWMutex` 保护并发安全。`app_tools.go` 的 handler map 为实际分发层。
- `internal/skills/` — Skill 抽象层（10 内置 Skill，含 `agent-orchestration` + `.mbskill.json` 导入/导出）。
- `internal/agents/` — **本地 Agent 人格**（2026-07-07 新增）。`Agent` = 可复用 profile：name/description/icon + 完整 systemPrompt + provider/model 覆盖 + `InheritSkills`（继承全局已启用）或显式 `Skills` 子集 + `Tools`/`MCPTools` + temperature/maxTokens。`Manager` 持久化到 `data/agents.json`，首次启动 seed 默认主 Agent（复刻原聊天行为）。执行内核在 [app_agent_exec.go](app_agent_exec.go)：`resolveAgentProvider` / `buildAgentSystemPrompt` / `buildAgentToolNames` / `runAgentLoop`（≤5 轮工具循环，供 `agent_run` 委派；子 Agent 剥离 `agent_*` 工具防递归）/ `GetAgentChatContext`（前端聊天切换用）。编排工具 `agent_list`/`agent_create`/`agent_run` 经 `agent-orchestration` Skill 暴露给主聊天。详见 [tasks/local-agents.md](tasks/local-agents.md)。
- `internal/workflow/` — 工作流引擎（DAG + 节点类型 + 条件分支 + 参数解析器）。数据流契约（2026-07-07 修复）：`workflow_execute` 的 inputs 经 input 节点产出（`executeInput` 用真实输入覆盖字段默认值）；节点配置中的字符串字段（`userPrompt`/`systemPrompt`/`source`/`expression` 等）执行前用 `resolver.go` 渲染。模板语法仅支持双花括号 `{{...}}`，路径首段可是**节点 ID、节点 Title、或顶层输入键**——即 `{{n1.field}}`/`{{节点标题.field}}`/`{{fieldName}}` 三者皆可（首段被 `resolvePath` 当作 `nodeOutputs` 的键查找）。LLM 节点输出归一化为 `{output, content, raw}`，下游用 `{{llmNode.output}}` 取文本；`executeLLM` 接受 `prompt` 作为 `userPrompt` 别名。**2026-07-07 bug 修复**：① 新增 `agent` 节点类型（`executeAgent` 读 agentId+prompt → `AgentRunner`→`App.runAgentLoop` 跑本地 Agent 人格，输出归一化 `{output,content,raw}`，下游 `{{agentNode.output}}`）；② 输出节点裸路径 `a.output` 现在也解析（`executeOutput` 无 `{{}}` 时走 `resolvePath`，路径无效才保留字面量），不再返回字面量；③ `executeNode` 改为 for-select + 2s 进度心跳，发 `workflow-progress-<execId>` + `NodeRun.Progress`（「正在生成…」等），LLM/agent 长节点不再静默，轮询 `workflow_status` 也能看到进度；④ 聊天工具循环改为「60 总轮 + 25 有效轮」双计数，纯查询工具（`ReadOnlyHint`）不计入有效预算。
- `internal/plugin/` — 插件系统（Manifest 解析 + 进程宿主 + RPC 客户端）。
- `internal/collab/` — **多 Agent 协同内核**：in-process 事件总线（`EventBus`，prefix 订阅 + `forward` sink 镜像到前端 `collab:event`）+ 共享黑板（ephemeral）+ 本地/远程 dispatcher + 协同会话/计划。**统一活动日志（2026-07-11）**：collab 事件 + 桥接进来的工作流 `wf-*` 事件经 `bus.forward` 单一汇聚点（[app.go](app.go)）→ [app_activity.go](app_activity.go) 异步落 `activity_log` 表（SQLite，buffered queue 满则丢最旧）；补 `agent.<id>.start` / `tool.<agentID>.call` 事件。前端工作台 [CollabWorkbench.vue](frontend/src/components/CollabWorkbench.vue)（agent 显示 name+活动 / 工作流节点 / 通信边 / 工具调用）与历史回放 [ActivityHistory.vue](frontend/src/components/ActivityHistory.vue)（`/activity`，过滤 + 按会话回放 + 时间线）共享这条单一日志。详见 [tasks/collab-observability.md](tasks/collab-observability.md)。

**实时同步模式（2026-07-07 推广）:** 后端任意变更经 `*App.emitChanged(event, action, id)`（app_workflow.go）发 Wails 事件 `<entity>:changed`；前端页面用组合式 `useDataChanged(event, cb)`（composables/useDataChanged.ts）订阅、挂载/卸载自动管理。事件加在各实体的 `*App` 绑定方法的**成功路径**——这样 LLM 的 tool handler 与前端的 Wails 绑定两条路径都覆盖（二者都路由经过同一批绑定方法）。已接入：`workflow/models/plugins/kb/mcp/providers/guides/agents/feishu`。未接入（只读或已轮询/已有事件）：catalog/system/toolbox/downloads/skills。

**领域即容器（Domain-as-Container，2026-07-11 实施）:** 领域从松散标签升格为架构级强制容器。核心约束：所有 KB/Agent/MCP Server/Skill 强制归属一个 Domain；Domain 是 AI 的最小认知单元。

- *数据模型*: `domain_libraries` 表（SQLite）为顶层容器，KB/Agent/MCP/Skill/Memory 均通过 `library_id` / `workspace_id` 归属。`IsValidLibrary(id)` / `ListLibraryIDs()` 提供验证。`DedupUserFacts()` 按 key 去重核心记忆。
- *API 强制*: 所有创建 API（`CreateAgent`/`AddMCPServer`/`CreateKnowledgeBase`/`CreateSkill`）通过 `validateLibraryID()` 校验 domainId 非空且存在。全局 Skill 允许 `LibraryID=""`。
- *前端*: `useActiveLibrary` composable 跨组件共享当前领域。侧边栏领域切换器（`App.vue`）始终可见。MCP 列表按领域过滤（`LLMMCP.vue` computed filter）。记忆/知识库/Wiki/经验召回均使用 `activeLibraryId` 过滤。
- *AI 认知*: `BuildDomainSystemPrompt(domainId)` 动态生成领域限定的 System Prompt（仅当前领域的 Agent + Skill + MCP），替代全局注入。上下文窗口节省约 60-70%。
- *设计来源*: 基于 25+ 篇学术论文和生产系统分析（CoALA/MemGPT/IsolateGPT/PC-RAG/Security Knowledge Dilution/Karpathy LLM Wiki 等）。完整设计见 [tasks/domain-refactor-v2.md](tasks/domain-refactor-v2.md)。
- `internal/rag/` — RAG 引擎（chunker 切片 + embedder 向量化 + store 检索）。
- `internal/memory/` — 记忆系统（2026-07-07，P0+P1.5）。`Store` 包装纯 Go `modernc.org/sqlite`（无 CGo）持久化到 `data/memory/memory.db`（WAL + busy_timeout）。P0：会话/消息 CRUD（`sessions`/`messages`，schema 预留 P2 的 `nodes`/`edges`/`user_facts`）。`app_memory.go` 暴露 `MemorySession*`/`MemoryMessage*` 绑定，变更经 `emitChanged('memory:changed')`。前端 chatStore 从纯内存 `messages` 改为后端持久化 + 多会话切换；ChatPanel header 加会话切换器。图存储引擎定为 **SQL 图建模**（邻接表 + 递归 CTE + bi-temporal）。**P1.5（融合写入 + 双路召回）已完成**：单 chromem collection `mem_longterm` 用 metadata `kind` 区分 turn/fact；SQLite `memory_items` 表做 manifest（list/count/clear）。写入：每轮只存 **user 问题向量**（reply 走 metadata），每 5 轮异步 LLM 抽事实（`extract_facts` function calling，复用 `chatCompletion`）。召回：embed 一次 → chromem 分 `kind=turn`/`kind=fact` 各 top-3（filter 不支持 $or 故分查）→ 合并注入 system prompt。可见：[Knowledge.vue](frontend/src/components/Knowledge.vue) 顶部「💬 对话记忆」区块（绑定状态 + 条目数 + 预览 + 清空）。startup 自动探测绑定 sentence-embedding 模型（找不到则降级静默关闭）。**模型可手动绑/迁移**（2026-07-07）：对话记忆 + KB 都支持 UI 选模型（`Detect` 增强：兜底 `onnx/config.json` + 扩 model_type 白名单，修精简包漏判）；换模型时记忆用 `memory_items` 重 embed、KB 用 manifest IDs + chromem `GetByID` 取 content 重 embed（先全部 re-embed 成功再 drop 旧 collection，manifest 不全则中止）。P2（时序知识图谱：nodes/edges + 多跳递归 CTE + bi-temporal 软失效）见 [tasks/memory-graph.md](tasks/memory-graph.md)。

**P2–P5 已完成**（详见 [tasks/memory-graph.md](tasks/memory-graph.md)）：
- **P2 时序知识图谱**：`kg_nodes`/`kg_edges`（bi-temporal，`valid_from`/`valid_to`/`recorded_at`）+ 递归 CTE 多跳 + 向量种子（chromem `kind=entity`）混合检索（LightRAG 式）；LLM `extract_graph` 抽实体/关系；`replaces` 语义（换偏好 close 旧边 / 加偏好共存）。
- **P3 质量/成本/UI/摘要**：谓词归一（同义词表）+ 增量抽取（`lastExtractAt`）+ 专用 `ExtractionProvider`（便宜模型）+ 会话摘要（`UpdateSummary` 每 10 轮）+ vis-network 力导向 viewer（点击删除）。
- **P4 viewer 强化**：详情面板 / 搜索筛选 / 布局切换（力导向⬡/层次≡）/ 聚类 / 邻居高亮 / **历史视图**（被取代的旧边黄虚线——bi-temporal 首次可见）/ 改名 / 实体 coref（embedding 相似 ≥0.92 合并）/ 召回高亮（⚡召回 跨视图高亮命中的子图）。
- **P5 分层记忆**（核心）：**核心层 `user_facts`**（身份/偏好/约束，永久，可🔒锁定，召回强制注入）+ **情节层 `memory_items`**（指数衰减 `Score = α·cos+(1-α)·0.5^(ageDays/halfLife)`，α=0.7，low importance age×2）+ **TTL 清理**（age≥TTL 且 score<0.05 → 删情节，**永不碰核心**）+ **硬件自适应策略**（启动读 `sysinfo.CollectDynamic` 的 RAM/磁盘 → tier low/std/high → 半衰期 7/14/30 天、TTL 30/90/180 天、cap 500/2000/10000）。
- **核心隔离原则**：身份/人设/约束永久（`user_facts` + 图谱身份节点），TTL/衰减只动情节——直接回答「90 天会忘了我吗？不会」。
- `internal/marketplace/` — 社区模型市场（发布 / 搜索 / 详情）。
- `internal/guides/` — 引导内容 + 模板同步。内置 EverEvo 使用指南（`//go:embed userguides/*.md` + 内部 `local` 源类型 `syncLocal`，首次启动 `NewManager` seed `everevo` 源并自动 `SyncAll`，不依赖外网）。
- `internal/toolbox/` — 模型类型探测（metadata / config.json / I/O 形状启发式）+ 句向量推理内核（MiniLM mean-pool）。
- `internal/security/` — 安全策略管理。
- `internal/config/` — 配置（项目根目录 `data/zones/{zone}/config.json`）。
- `internal/storage/` / `internal/sysinfo/` / `internal/auth/` — 数据目录、系统信息（CPU/GPU/内存）、平台鉴权。

### 前端

- `frontend/src/` — Vue 3 SPA，34 个组件，TypeScript + `<script setup>`。
- `src/api/` — API 层 9 模块（client / models / providers / mcp / skills / system / plugins / workflow / knowledge / accounts / index），全部 Wails 调用通过 `call()` 包装（超时/取消/错误格式）；0 处 `(window as any)` 绕过。
- `src/components/` — 市场（ModelCatalog, ModelDetailPanel, FileTreeNode, PackageTreeNode）、模型库（MyModels, ModelCard, ModelList）、工具箱（Toolbox, SimilarityTool, ImageClassifierTool）、AI 能力（LLMConfig, LLMSkills, LLMMCP, LLMAgent [A2A], LLMAgents [本地人格], LLMFeishu, ProviderDialog, SkillDialog, SkillMarket, MCPServerForm）、工作流（WorkflowEditor, WorkflowNodeConfigPanel）、插件（PluginManager, PluginUse）、知识库（Knowledge + KnowledgeAddText / KnowledgeSearch / KnowledgeBrowse）、引导中心（GuideCenter）、系统（SystemInfo, SettingsPanel, AccountMenu, DownloadCenter, ChatPanel）、通用（ErrorBoundary, ToastContainer, ConfirmModal, DialogModal, ResultPanel, DynamicForm, TextOutput, ImagePreview）。
- `src/stores/` — Pinia stores（download / provider / chat）。
- `src/composables/` — 复用逻辑（useToast；其余 5 个死代码 composables 已于 2026-07-05 移除）。
- `src/utils/` — 共享工具（formatters: fmtSize / fmtCount / fmtNum / fmtCtx；workflow-mapper；icons）。
- `src/router/` — vue-router hash mode + KeepAlive + 懒加载代码分割（主 bundle 186KB）。

## Model Interface

```go
type ModelRunner interface {
    ID() string
    Info() ModelInfo
    Load() error
    Unload() error
    Run(ctx context.Context, input []byte) ([]byte, error)
}
```

> 通用 `Run([]byte)→[]byte` 对异构模型不实用（工具箱方向将按类型提供专用工具，而非通用 Run）。该接口目前仍被 manager/卡片使用，工具箱落地后会演进。

## Build

- `scripts/build.ps1 all`（Windows）：前端 → Go → `Bundle-Runtime`（拷 `onnxruntime.dll` 到 EXE 旁）。
- `scripts/build.ps1 dev`：热重载。
- ONNX Runtime 1.26 DLL bundled 在 `third_party/onnxruntime/win-x64/`；`backends.findDLL` 优先 EXE 目录，故 bundled DLL 胜过系统 DLL。

## Key Decisions

- **产品方向（2026-06 重定义）**：从「通用模型运行器」转向「模型工具箱」——市场（下载浏览）+ 工具箱（每类模型专用工具）。通用文本运行框将被淘汰。
- **上下文优化（2026-07-13 设计）**：三层上下文模型——Layer 1 轻量工具索引（~300 tokens）+ 6 核心工具（~800 tokens）始终加载；Layer 2 完整 Tool Schema 按需通过 `tool_search` 获取；Layer 3 工具输出自动截断 + 消费后压缩。对标 Claude Code ToolSearch 模式（134k→5k，95% 减少）。子代理仅继承所需工具子集。WebFetch 经轻量模型摘要门控。详见 [context-optimization.md](tasks/context-optimization.md)。
- **ONNX via `yalue/onnxruntime_go`（首个也是唯一 CGo）**：`internal/backends/onnx/` 通过 yalue（v1.31.0，`ORT_API_VERSION 26`）调用 onnxruntime。旧的手写 syscall 绑定已删除（ABI bug → 闪退）。ORT C API forward-locked，故 bundled ONNX Runtime **1.26** DLL（`third_party/`，`scripts/build.ps1 Bundle-Runtime`）。
- **Tokenizer via `sugarme/tokenizer`（纯 Go）**：`internal/tokenizer/` 用 sugarme 加载模型自带 `tokenizer.json`，复现 HF BERT WordPiece 流水线。
- **llama.cpp 暂为 stub**：Windows 下三个 Go 绑定均不可用——`develerltd`（编译失败，`purego.Dlopen` Unix-only）、`dianlight` v0.1.0（`Decode` 仅 Darwin）、`tcpipuk`（go-get 包缺 `libbinding.a`/C++ 源，需 clone+make）。当前 stub 明确报错不崩。后续若采用 `tcpipuk`（需预编译 `libbinding.a`）可替换。
- **Rust 引擎 / CGo bridge 未实现**：历史设计规划 `internal/bridge` + `engine/`，截至当前未实现；项目纯 Go + 一个 CGo（yalue）。
- **平台隔离**：`_windows`/`_unix` build tag（install、sysinfo；ONNX/llama 路径 Windows-only via `app.go` build tag）。
- **技术栈开放（混合语言，轻量高效优先，2026-06）**：不强制纯 Go。核心原则是「轻量 + 高效」。当前是纯 Go + 一个 CGo（yalue/ONNX），但允许引入 C/C++/Rust 等以接入成熟生态——典型场景是 llama.cpp 的 C++ 生态，CGo/Rust 直连比纯 Go purego 绑定更可靠（purego 绑定在 Windows 频繁踩坑，见上）。新增语言需权衡构建复杂度与收益（构建链仍是 Go/wails 为主，混合部分作为可替换的后端模块）。

## Open Work / Roadmap（2026-07-05 更新）

### 已完成 ✅

- **前端全量重构**（2026-07-04）：30 组件 Options API → `<script setup lang="ts">`，vue-router + Pinia + API 层 + composables。
- **后端解耦**（2026-07-05）：单体 `app.go` → 15 个 `app_*.go` 按模块拆分。
- **下载流程全链路优化**（2026-07-05）：状态统一（downloadStore）、队列并发控制、自动重试、文件树进度增强。
- **MCP Server**（2026-06-30）：内置 MCP Server + Skill 系统 + AI 聊天 Tool Calling Loop。
- **句向量工具内核**：MiniLM tokenize + mean-pool 真实推理可用。
- **RAG 知识库**：切片 + 嵌入 + 语义检索可用。
- **记忆系统（Memory P0–P5）**：纯 Go SQLite 持久化（会话/消息）+ chromem 向量召回（turn/fact/entity）+ **时序知识图谱**（bi-temporal + 递归 CTE 多跳 + vis-network viewer）+ **分层记忆**（核心 `user_facts` 永久 / 情节 `memory_items` 指数衰减 + TTL）+ **硬件自适应策略**（RAM/磁盘 → tier → 半衰期/TTL/cap）。核心隔离：身份永久，TTL 只动情节。见 [tasks/memory-graph.md](tasks/memory-graph.md)。

### 进行中 🚧

- **工具箱框架完善**：模型类型探测 + 工具接口统一（预处理 + 推理 + 后处理 + UI 组件）。
- **图像分类工具**：选图 → 预处理（resize 224 / ImageNet normalize）→ 推理 → argmax + 标签表 + 置信度。
- **Super Agent 路线图**（Phase 1 MCP Client 部分完成）：完整的三阶段规划参见 `docs/llmwiki/tasks/super-agent-roadmap.md`。

### 计划中 📋

- **淘汰通用文本运行框**（`ModelCard.doRun` → 工具箱路由）。
- **llama.cpp 真实接入**：待 Windows Go 绑定生态成熟或采用 `tcpipuk` 方案。
- **文本分类 / 目标检测工具**：新模型类型支持。
