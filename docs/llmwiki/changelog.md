# Changelog

> Append-only record of meaningful changes to EverEvo. Newest entry at the top.

## 2026-07-13 — 上下文优化架构设计（Context Window Optimization）

**Why:** 当前 ~110 工具全部 upfront 注册 → ~27k tokens/request 固定开销，工具输出无截断、子代理继承全量上下文。对标 Claude Code ToolSearch（134k→5k，95% 削减）、MCP 层级化工具管理、CMU JIT Schema Passing 等 2025 业界实践。

**Design (not yet implemented):**
- *三层上下文模型*: L1 轻量工具索引（300 tokens）+ 6 核心工具（800 tokens）；L2 按需 schema 加载 via `tool_search`；L3 工具输出自动截断 + 消费后压缩
- *工具输出压缩*: 按工具定义 `OutputPolicy`（head+tail+marker），消费后替换为摘要标记
- *子代理隔离*: 仅继承 agent 配置的 Tools+Skills 子集，baseline <5k tokens
- *WebFetch 门控*: 轻量模型先读后摘要，原始 HTML 不入主上下文

详见 [tasks/context-optimization.md](tasks/context-optimization.md)

## 2026-07-13 — 存储层纯便携化（Portable-Only Refactoring）

**Why:** EverEvo 始终以源码形式分发（自进化需要编译能力），不存在独立 EXE 发行场景。原有的 Portable/User 双模式 + AppData 迁移层是多余的。

**Changes:**
- *storage*: 移除 `IsPortable()` / `appDataRoot()` / `OldAppDataDir()` / `MigrateLegacyData()` / `RootAppDataDir()` 及所有 legacy fallback；新增 `PythonDir()`；`DataDir()`/`RuntimeDir()` 始终从项目根解析
- *config*: 移除全部迁移代码（`migrateLegacyConfig` / `migrateAllData` / `migrateOldLLM` / `backupZoneData` 等 ~170 行）；移除 `GlobalPath()`（无调用者）；简化 `Load()`/`Path()`
- *zone*: `ZonesDir()`/`portRegistryPath()` 直接使用 `storage.DataDir()`，移除 `RootAppDataDir()` 中间层；更新包文档
- *backends*: `detectPython()` 便携路径从 `%APPDATA%/EverEvo/python/` → `runtime/python/`；移除 `appDataPath()`
- *app.go*: 移除双模式日志和 `MigrateLegacyData()` 调用
- *docs*: 更新 design.md 配置路径描述

**Net:** ~300 行代码净删除，零新增逻辑。

## 2026-07-11 — 领域即容器 v2 实施（Phase 0–5）

**Why:** 领域化是将 `domain_libraries` 从松散标签升格为架构级强制容器的核心重构。基于 [domain-refactor-v2.md](tasks/domain-refactor-v2.md) 的完整设计，结合 25+ 篇学术论文（CoALA/MemGPT/IsolateGPT/PC-RAG等）的业界共识。

**Changes — Phase 0: Schema & Data Foundation**
- *Memory store*: 新增 `ListLibraryIDs()` / `IsValidLibrary(id)` 验证方法；新增 `DedupUserFacts()` 按 key 去重核心记忆（保留最新 + 合并 access_count）
- *RAG store*: `BackfillLibraryIDs` 增强为修复悬挂引用（如孤儿 `lib_18c0842c0b947f70`）——不仅补空值，也修复指向不存在 domain 的 LibraryID
- *MCP manager*: `BackfillLibraryIDs` 同步增强，接收 `validIDs` 参数修复悬挂引用
- *Agent manager*: `EnsureLibraryIDs` 同步增强，接收 `validIDs` 参数修复悬挂引用
- *Skill manager*: `EnsureLibraryIDs` 同步增强，接收 `validIDs` 参数修复悬挂引用
- *app.go startup*: 回填链路统一传递 `validIDs`；新增 `DedupUserFacts()` 调用

**Changes — Phase 1: Backend API Hardening**
- *app.go*: 新增 `validateLibraryID(id)` 和 `resolveLibraryID(id)` 共享校验/解析 helpers
- *app_agents.go*: `CreateAgent` 强制校验 `agent.LibraryID` 非空且存在
- *app_mcp.go*: `AddMCPServer` 强制校验 `cfg.LibraryID` 非空且存在；补 `fmt` import
- *app_knowledge.go*: `CreateKnowledgeBase` 强制校验 `libraryID` 非空且存在
- *app_skills.go*: `CreateSkill` 校验 `LibraryID`（空 = 全局 Skill，允许；非空必须存在）；补 `fmt` import

**Changes — Phase 2: Frontend Domain-First Navigation**
- *App.vue*: 侧边栏新增领域切换器（`<select>` 下拉 + 折叠态图标）。使用 `useActiveLibrary` 共享状态。`onMounted` 加载领域列表。新增 domain switcher CSS
- *LLMMCP.vue*: 新增领域过滤 —— `mcpServers` 从 `ref` 改为 `computed`（按 `activeLibraryId` 过滤）；新增 "显示全部领域" checkbox；服务器卡片显示领域标签；`saveMCPServer` cfg 自动注入 `libraryId`

**Changes — Phase 3: System Prompt Dynamic Generation**
- *app_agent_exec.go*: 新增 `BuildDomainSystemPrompt(domainId)` —— 生成领域限定的系统提示片段（该领域的 Agent + Skill + MCP Server）
- *chatStore.ts*: 全局模式和 Agent 模式均自动注入领域上下文（`BuildDomainSystemPrompt` 调用）。记忆/知识库/Wiki/经验召回已使用 `activeLibraryId`

**Phase 4 & 5:**
- 记忆去重在 Phase 0 中完成（`DedupAllFacts` + `DedupUserFacts`）
- Skill 领域过滤在 Phase 3 中完成（`ListEnabledByLibrary` → `BuildDomainSystemPrompt`）
- 全量测试通过：10 个测试包 0 失败

**设计要点:**
- **零破坏向后兼容**：空 `libraryId` → 默认领域；旧数据自动回填
- **不改存储格式**：agents.json / skills.json / mcp_servers.json 保持现有结构
- **学术支撑**：30 条参考文献覆盖 DDD / CoALA / MemGPT / IsolateGPT / PC-RAG / Security Knowledge Dilution 等

**结果:** `go build ./...` ✓ / `go test ./internal/...` (10 packages) ✓ / `vue-tsc --noEmit`（改动文件 0 新错误，预存错误未增加）

## 2026-07-11 — 领域即容器重构提案

**Why:** 系统诊断发现 6 类问题源于同一根因：`domain_libraries` 只是记忆系统的内部分类标签而非架构级容器。KB/Agent 有 `LibraryID` 但不强制，MCP Server/Skill 完全无领域归属，Wiki 底层隔离但前端不体现。用户看到 18 个"领域"、2 个 KB 中一个悬挂引用、3 个 MCP Server 全部全局、3 套 Skill 体系互不同步、50+ 条重复核心记忆。

**提案:** [tasks/domain-refactor.md](tasks/domain-refactor.md) — 5 阶段重构：
- Phase 0: Schema 加固（补 FK/default/missing columns）
- Phase 1: 后端 API 强制 libraryId（所有创建 API 必传，列表 API 支持过滤）
- Phase 2: 前端领域优先导航（DomainPanel 为中心，各子系统默认按领域过滤）
- Phase 3: Skill 体系统一（持久化到 DB + 绑定领域 + 三套合并）
- Phase 4: 记忆去重增强 + 领域隔离
- Phase 5: 数据迁移清理（修复悬挂引用、回填空字段）

**设计决策:** 默认领域策略（空 libraryId = 默认领域，向后兼容）；跨域访问需显式声明；保留 LLM 自动发现领域但标记 🤖。

## 2026-07-11 — 协同工作台全观测 + 统一活动日志 + 历史回放

**Why:** 工作台要成为「观察 AI 工作的窗口」。痛点：agent 卡片显示 ID 非 name；无 agent.start/message/tool.call 事件；工作流走独立 Wails 总线前端无人订阅、执行完即驱逐；collab 事件纯内存重启即失。

**核心设计**：两套事件总线（collab bus + workflow Wails）统一汇聚到**单一 `collab:event` 流 + 单一 SQLite 日志**。

**Changes:**
- *统一活动日志*：[store.go](internal/memory/store.go) `activity_log` 表 + `LogActivity`/`ListActivity`（ID 用 ts+原子 seq 保唯一，空结果返回 `[]` 非 null）。
- *ActivityLogger 汇聚*：新 [app_activity.go](app_activity.go) `recordActivity`（buffered 1024 队列 + 单写 goroutine，满则丢最旧护总线）+ `mapEventToActivity`（topic→kind/summary）+ `agentDisplayName`（ID→name）+ `bridgeWorkflowEvent` + `App.ListActivity` 绑定；[app.go](app.go) forward 回调挂钩（单一汇聚点）+ 启动写 goroutine。
- *工作流桥接*：[app_workflow.go](app_workflow.go) `workflowEventEmitter` 加 `app`，Emit 时桥接 `wf-*` 进 `collab:event` + 日志（保留原直发）。
- *补事件*：[app_collab.go](app_collab.go) 异步派发发 `agent.<id>.start`；[app_agent_exec.go](app_agent_exec.go) 每次 CallTool 后发 `tool.<agentID>.call`（agent 间通信由 dispatch/message 工具的 tool.call 派生，不单独发 message）。
- *工作台实时全观测*：[CollabWorkbench.vue](frontend/src/components/CollabWorkbench.vue) 重写——agent 节点显示 **name + 当前活动**（task/调用工具/写黑板）、工作流节点（wf-exec-start/node-*/done + 进度）、**通信动画边**（tool.call 派 dispatch/message）、工具调用进事件流（TOOL）、agent/工作流点开抽屉看明细；name 映射复用 `agentsApi.list()`。
- *历史回放*：新 [ActivityHistory.vue](frontend/src/components/ActivityHistory.vue) + 路由 `/activity` + 导航「活动历史」——过滤（类型/来源/条数）+ 会话运行卡片回放（按 sessionId）+ 时间线 + payload 详情 + 实时追加。

**核实/限制**：工作流历史走活动日志（Manager 执行完即驱逐）；工作台运行中途挂载会漏早期工作流事件（事件驱动）；聊天面板工具调用未归属（缺 caller 上下文，后续）。日志写异步满则丢最旧。

**测试**：`internal/memory` `TestLogAndListActivity`；`go build .` + `go vet` + `vue-tsc`（改动文件零错误）。运行期需桌面端实测（collab_create/dispatch → 工作台 name/活动/通信；workflow_execute → 工作流节点；重启 → 活动历史回放）。

## 2026-07-11 — 手动修复清单批次（#5/#6/#7/#8/#10）

**Why:** 用户列出 10 项手动修复，#1-3 已修。逐一代码核实剩余 7 项：#4（memoryStore 顺序）、#9（MCP 自启动）工作树中已正确实现 → 不动；另 5 项为真实 bug/增强。

**Changes:**
- *#7 graph_list nodes null*：[app_tools_control.go](app_tools_control.go) `hGraphList` 把 `result["nodes"].([]any)` 改为 `[]memory.GraphNode`（Go 禁止 typed slice→`[]any` 断言，原断言恒 nil → LLM 工具路径 nodes 恒 null）。补 `everevo/internal/memory` import。前端 Wails 绑定一直正常。
- *#8 episodic fact 去重*：[store.go](internal/memory/store.go) `AddFactMemory` 插入前加精确（SQL COUNT 同 content）+ 语义（`QueryFacts` cosine ≥ `factDedupThreshold=0.90`）去重，对齐 `AddUserFact` 已有的 key+value 去重。修 LLM 每 N 轮重复抽取导致同一事实存 3-5 次。
- *#10 token 验证用 Bearer*：[verify.go](internal/auth/verify.go) `looksLikeToken`（`hf_`/`ms-` 前缀或无 `=`/`;`）判别；`verifyHF` 对 token 发 `Authorization: Bearer`（原 Cookie 路径保留）；`verifyMS` 新增 `verifyMSByToken`（API Bearer，best-effort）失败回退 Cookie 抓取（不回归）。Reason 细化（网络错误 / 被拒绝 / 过期 / HTTP %d）。
- *#5 插件健康检查致命化*：[app_plugins.go](app_plugins.go) `StartPlugin` 健康检查失败 → `host.Stop` + 返回错误（原仅 log），RPC 故障即时暴露而非 30s 超时。
- *#6 攻略预置 EverEvo 使用指南*：[internal/guides/](internal/guides/) `//go:embed userguides/*.md`（6 篇：快速上手/市场下载/工具箱/AI能力/记忆知识/工作流）+ 新增内部 `local` 源类型 `syncLocal`（幂等写出 embed 文件）+ `NewManager` 返回 `(mgr, seeded)` 首次 seed `everevo` 源；[app.go](app.go) seeded 时 `go SyncAll()`。不依赖外网、不 404。

**核实未改：** #4（app.go:281 memoryStore 在 collab restore :305 之前，guard 有效）、#9（app.go:362-369 自动启动 MCP + 持久化端口，server.go 正确）。

**测试：** `internal/memory`（`TestAddFactMemoryExactDedup` / `SemanticDedup` / `TestListNodesEmptyMarshalsToArray`）、`internal/guides`（`TestEmbeddedUserGuidesPresent` / `TestDefaultEverEvoSource`）。`go build .` + 改动包 `go vet` 全通过。

## 2026-07-09 — P9: Python Portable 基础设施

- **Python 检测**: 三层优先级——便携 Python (AppData) > Conda > 系统 PATH
- **一键安装**: `InstallPythonPortable()` 下载 python-3.11.9-embed-amd64.zip 并解压到 `%APPDATA%/EverEvo/python/`
- **工作区 venv**: `CreateVenv(venvPath)` 创建隔离虚拟环境，`PipInstall(venvPath, packages)` 装包
- **工具就绪**: `BestPython()` / `RunInVenv()` 供后续 Agent 工具调用
- **文件**: `internal/backends/python.go`(新), `backends.go`(detectPython), `app_download.go`(InstallPythonPortable)

## 2026-07-08 — 记忆系统 P7：领域知识库网格（Domain Library Grid）——替代工作区

**Why:** P7.2 workspace 是「人手项目文件夹」——用户建工作区、手动切换、数据完全隔离。但模型的知识系统应该是 **AI 自主管理**：AI 入库时自判「法律 / 数学 / 生活」、检索时先路由到库再精查、库间可跨连接。2025 业界（AutoGraph/AGENTiGraph/sage-wiki）共识：domain library > workspace，taxonomy 从内容涌现而非人预建。

**Changes:**
- *P7.1 领域库表*：[store.go](internal/memory/store.go) `domain_libraries`（id/name/description/tags/auto_created）+ `cross_tags` 列（memory_items/kg_edges）；`LibraryList/Create/Delete/Merge` CRUD + 自动 seed。
- *P7.2 入库自动分类*：[app_memory.go](app_memory.go) `extract_facts`/`extract_graph` schema 加 `domains:[...]`；LLM 返回领域标签 → `resolveOrCreateLibrary` 路由（不存在的库自动创建 🤖）；`AddFactMemory/IngestGraph/AddEdge` 写 `workspace_id` + `cross_tags`。
- *P7.3 语义路由*：[chatStore.ts](frontend/src/stores/chatStore.ts) `classifyQuery` 规则匹配（query 关键词 vs library name/description/tags）→ 「领域库匹配：法律, 生活」注入 system prompt。
- *P7.4 跨库图谱*：[graph.go](internal/memory/graph.go) `GraphEdge.CrossTags` + SELECT/INSERT 全链路 cross_tags；图谱边可标属多个领域库，viewer 按库筛选（schema-ready，写路径待 maybeExtractGraph domain routing 补齐）。
- *P7.5 前端网格*：[Knowledge.vue](frontend/src/components/Knowledge.vue) 领域库 toolbar dropdown → **library grid 卡片**（name/desc/tags + 🤖 auto-created badge），替换 workspace 切换器。

**设计要点:**
- **domain_libraries 替代 workspace**：AI 管理而非人手切；`auto_created=1` 标自动发现库。
- **入库分类零额外 LLM 调用**：`domains` 字段塞进现有 `extract_facts`/`extract_graph` schema。
- **跨库连接**：`cross_tags` JSON 让一条知识可从多个库召回——"租房"(生活) 关联 "合同"(法律)。
- **P7.2 workspace 列保留**（`workspace_id` = `library_id` 语义一致），零破坏迁移。

**结果:** `go build ./...` ✓ / `go test ./internal/memory/...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 领域库网格 + AI 入库路由 + 聊天路由注入待用户实测。

## 2026-07-08 — 记忆系统 P6.1：开发文档接入召回（llmwiki 索引 + 检索注入）

**Why:** llmwiki (`docs/llmwiki/`，12 文件) 是项目设计/任务记录，从未被索引/召回——聊天无法回答「记忆系统怎么设计的」。P6.1 让 AI 能召回项目文档。

**Changes:**
- [go.mod](go.mod)：加 `github.com/yuin/goldmark`（Go 标准 markdown parser，heading-aware 分块 + 链接 AST 抽取）。
- [storage.go](internal/storage/storage.go)：`EnsureDataDir` 加 `"wiki"` + `"wiki/chromem"`。
- [internal/wiki/wiki.go](internal/wiki/wiki.go)（新包）：`ParseMarkdown`（goldmark AST → 标题边界分块 + `[text](path)` 链接抽取，跳过源码引用）；`Store`（chromem `wiki_docs` + SQLite `wiki_pages`/`wiki_links` 页图）；`Search`/`Clear`/`IndexPage`/`IndexLinks`。
- [app_wiki.go](app_wiki.go)（新）：`//go:embed all:docs/llmwiki` 嵌入文档 → `WikiReindex`（启动全量索引）；`WikiStatus`/`WikiSearch`（UI 检索测试）/`WikiRecall`（embed query → top-5 chunks → "📄 page › heading\npreview"，聊天注入）。
- [chatStore.ts](frontend/src/stores/chatStore.ts)：recall 加 wiki 注入（`WikiRecall` → `项目文档（与问题相关的设计/任务记录）：\n`）。
- 前端：[api/wiki.ts](frontend/src/api/wiki.ts)（新）+ [Knowledge.vue](frontend/src/components/Knowledge.vue)「项目文档」面板（页/段计数 + 重建索引按钮 + 检索测试输入）。

**设计要点:**
- **独立 data/wiki/ 存储**（隔离于 memory/RAG）：chromem 向量 + SQLite 页图；重建全量 re-embed，index 不大（~188KB docs）。
- **embed 同 memory 共用模型**（`EmbeddingModelDir()`）——不增加额外 ONNX 开销；无模型时静默 no-op。
- **goldmark AST 分块**（标题边界，优于原 `ChunkText` 的段落句号切分），召回返回完整段落而非散句。
- **P6.2 产品级用户 Wiki** 复用同一管线，单独 plan。

**结果:** `go build ./...` ✓ / `go test ./internal/wiki/...` ✓（ParseMarkdown + resolveLink）/ `go test ./internal/memory/...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 索引 + 检索 + 聊天注入待用户实测。

## 2026-07-08 — 记忆系统 P5：分层记忆 + 热度衰减 + TTL + 硬件自适应策略 + 核心隔离

**Why:** P0–P4 的记忆是「扁平且永久」——无时间衰减、无过期、无重要度区分，身份信息与一次性闲聊混在同一池，任何 TTL 都有「忘了我是谁」的风险。依 FadeMem/MemoryBank/GraphRAG/arXiv 2509.19376 共识：分层（核心永久 + 情节衰减 + TTL）+ 指数热度衰减 + 重要度分级，且策略参数按宿主 RAM/磁盘自适应。

**Changes:**
- *Schema + 策略 (12.1)*：[store.go](internal/memory/store.go) `memory_items` 加 `last_access`/`access_count`/`importance`（ALTER 兼容旧库）；新增 `user_facts` 表（核心层）；`MemoryPolicy{Tier,HalfLifeDays,TTLDays,RecallK,ItemCap,CoreCap,Alpha}` 存 meta；[app_memory.go](app_memory.go) `applyMemoryPolicy` 启动时读 `sysinfo.CollectDynamic()`（availRAM/diskFree）→ tier（low<6GB / standard 6-16 / high>16，disk<20GB 降一级）。
- *核心层 + 重要度 (12.2)*：`extract_facts` schema 加 `importance{high,medium,low}`；high（身份/偏好/约束）→ `user_facts`（永久，锁定可手锁）；其余 → `memory_items`（带 importance）；`MemoryRecall` 强制注入 `core`（无衰减无 TTL）。
- *衰减召回 (12.3)*：`TurnHit`/`FactHit` 透传 chromem `Similarity`；`QueryMemory` 重排 `Score = α·cos + (1-α)·0.5^(ageDays/halfLife)`（α=0.7，low importance age×2）；命中刷新 `last_access`/`access_count`（LRU 热度）；TTL 删 SQLite 后 chromem orphan 由 decayRank 过滤。
- *TTL 清理 (12.4)*：`SweepExpiredPolicy`（age≥TTL 且 score<0.05 → 删 memory_items，**永不碰 user_facts**）；`runMemorySweep` 启动一次 + 每 24h ticker（a2a 模式），`memSweepDone` chan shutdown 取消。
- *UI (12.5)*：[Knowledge.vue](frontend/src/components/Knowledge.vue) 「核心记忆」面板（添加/🔒锁定/删除）+ 策略行；[chatStore.ts](frontend/src/stores/chatStore.ts) 注入「核心记忆（永久）」为首段。

**设计要点:**
- **核心隔离是关键**：TTL/衰减只动 `memory_items`；`user_facts` + 图谱身份节点永久——直接回答「90 天会忘了我吗？不会」。
- **硬件自适应**：低配机（<6GB RAM 或 <20GB 磁盘）自动降级（半衰期 7d/TTL 30d/cap 500），高配宽松（30d/180d/10000）。
- **衰减公式**（arXiv 2509.19376）：`α·cos+(1-α)·0.5^(age/h)`，14 天半衰期，越旧权重指数下跌；低重要度 age×2 加速遗忘。
- **orphan 无害**：TTL 删 SQLite，chromem 残留 doc 由 decayRank 过滤（SQLite 行不在即跳过），避开 chromem Delete API 不确定性。
- **策略启动算一次**（一次 CollectDynamic，廉价）；运行时改需重启。

**结果:** `go build ./...` ✓ / `go test ./internal/memory/...` ✓（`TestDecayScore` 覆盖 fresh/半衰期/ephemeral/clamp/createdAt fallback + `TestDefaultMemoryPolicy`）/ `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 核心面板/衰减召回/TTL 清理待用户实测。

## 2026-07-08 — 记忆系统 P4：图谱 viewer 强化 + 时序视图 + 编辑 + 抽取 coref + 召回高亮

**Why:** P3 的图谱 viewer 只是「看 + 删」，bi-temporal 历史不可见、抽取消歧仅 name 归一、不可编辑、召回与图谱不联动。P4 把这四块补齐，让知识图谱真正可用、可读、可纠错、可追溯。

**Changes:**
- *viewer 强化 (11.1)*：[Knowledge.vue](frontend/src/components/Knowledge.vue) 详情面板（点节点看全部关系）、搜索/类型筛选、布局切换（力导向⬡ / 层次≡）、聚类（◉ outliers）、邻居高亮（`selectNodes`）、历史 toggle + 矛盾高亮（黄虚线 = 已失效边）。
- *时序视图 (11.2)*：[graph.go](internal/memory/graph.go) `GraphEdge.RecordedAt` + `ListAllEdgesIncludeHistory`（去 `valid_to IS NULL` 过滤）；[app_memory.go](app_memory.go) `MemoryGraphList(history)`；前端"显示历史"展示被取代的旧事实。
- *抽取质量 (11.3)*：[ingestor.go](internal/memory/ingestor.go) `normalizeType`（type 同义词表：人物/人→person…）+ **实体 coreference**：upsert 前 `EntitySearch` 找 embedding 相似（chromem `Similarity` ≥ 0.92）的已有实体直接复用，合并 `用户/User/我` 这类近重复。
- *编辑 + 统计 (11.4)*：[graph.go](internal/memory/graph.go) `RenameNode`/`MergeNodes`（re-point 边 + 去重 + 删冗余节点）/`Stats`（按 type 计数 + top hub）；Wails 绑定 + viewer 改名输入 + 核心实体统计行。（`addEdge`/`merge` 的 API 已就绪，UI 留 follow-up。）
- *召回高亮 (11.5)*：[retriever.go](internal/memory/retriever.go) `RetrieveGraphTrace`（返回 seed/edge ids）+ `MemoryRecall.graphTrace`；[chatStore.ts](frontend/src/stores/chatStore.ts) module-level `lastGraphTrace` 跨视图传递；viewer「⚡召回」按钮 `setSelection` 高亮上次对话命中的子图。

**设计要点:**
- **vis-network 能力边界**：`clusterByGroup` 缺（用 `cluster({joinCondition})` workaround）、minimap 无原生（用 cluster+fit 替代，不强做）；`setOptions`/`selectNodes`/`setSelection`/`clusterOutliers` 原生可用。
- **coref 是 embedding 启发式**（cosine ≥ 0.92），非 LLM —— 便宜可逆，覆盖 `用户/User/我` 及多数近重复；真 LLM coref 留后续。
- **历史可见化**：bi-temporal 的价值首次在 UI 体现（被 `replaces` 关掉的旧边以黄虚线 + "已失效" 展示），这是知识图谱区别于普通图的核心。
- **orphan 向量无害**：RenameNode/MergeNodes 只动 SQLite，chromem entity doc 留 orphan（检索按 id join SQLite，不受影响）——避开 chromem Delete API 的不确定性。

**结果:** `go build ./...` ✓ / `go test ./internal/memory/...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓（vis-network 体积致 chunk-size 提示，无害）。GUI 详情面板/历史视图/改名/召回高亮待用户实测。

## 2026-07-08 — 记忆系统 P3：图谱质量 + 召回 + 成本 + 时序语义 + 图谱 UI + 会话摘要

**Why:** P2 的时序知识图谱可用但有六处短板：谓词不归一化（`使用/用/采用` 成不同边，bi-temporal close 失效）、抽取不一致、固定 hop/k 无新鲜度、每轮全量重抽 + 用主聊天模型、`(src,type)` 一刀切 close（分不清换/加偏好）、图谱不可见不可纠错、`UpdateSummary` 死代码。P3 全部补齐。

**Changes:**
- *图谱质量 (10.1)*：[graph.go](internal/memory/graph.go) `normalizePredicate`（同义词表：用/采用→使用，喜爱/偏好→喜欢…）；[ingestor.go](internal/memory/ingestor.go) 写入前归一；[app_memory.go](app_memory.go) `extract_graph` prompt 升级（few-shot + 「用户」统一称谓 + 规范谓词）。
- *时序语义 (10.2)*：`extract_graph` schema 加 `relation.replaces`；`ExtractedRelation.Replaces`；`AddEdge(...,replaces)`——replaces=true 才 close 同 (src,type) 旧边，false 共存。prompt 教「改用/换成→true，也/还→false」。
- *成本 (10.3)*：[store.go](internal/memory/store.go) `ListMemoryItemsSince(ts)`；meta `lastExtractAt`——`maybeExtractFacts` 增量只抽新 turn；[config.go](internal/config/config.go) `LLMConfig.ExtractionProvider` + `resolveExtractionProvider`（空→活动）+ `SetExtractionProvider`；[LLMConfig.vue](frontend/src/components/llm/LLMConfig.vue)「记忆抽取模型」下拉。
- *召回 (10.4)*：[retriever.go](internal/memory/retriever.go) 自适应 hop（seed≤2→3 否则 2）+ 按 `valid_from` DESC 排序 + 12 行上限；[chatStore.ts](frontend/src/stores/chatStore.ts) 长期记忆块 >1200 字截断。
- *会话摘要 (10.5)*：[store.go](internal/memory/store.go) `CountMessages`；`maybeSummarize`（每 10 条消息 → `chatCompletion` 生成 1-3 句 → 复用 `UpdateSummary`）；[chatStore.ts](frontend/src/stores/chatStore.ts) 注入「会话摘要」段（currentSession.summary，已由 sessionList 加载）。
- *图谱 UI (10.6)*：[graph.go](internal/memory/graph.go) `ListNodes/ListAllEdges/DeleteNode/DeleteEdge` + `GraphNode` DTO；[app_memory.go](app_memory.go) `MemoryGraphList/MemoryNodeDelete/MemoryEdgeDelete` Wails；[api/memory.ts](frontend/src/api/memory.ts) `KgNode/KgEdge` + listGraph/deleteNode/deleteEdge；[Knowledge.vue](frontend/src/components/Knowledge.vue) SVG viewer（复用 dagre `autoLayout`，点击节点/边删除，显示 nodeCount/edgeCount）。

**设计要点:**
- **replaces 而非一刀切 close**：让 LLM 区分「换偏好」（互斥，close 旧）与「加偏好」（共存），贴合真实语义。
- **增量抽取**：lastExtractAt 记录水位，每 5 轮只处理新 turn，避免重复抽取 + 重复 embed。
- **抽取专用模型**：ExtractionProvider 可指向更便宜模型，抽取不挤占主聊天预算。
- **图谱 UI 用 SVG 而非 VueFlow**：read-mostly + 几十节点，复用 dagre autoLayout，避免把 VueFlow 复杂度引入 Knowledge；数据层与 VueFlow 一致（将来可换）。
- **消歧仍仅 name 归一化**：prompt 统一「用户」称谓覆盖最常见自指；LLM coref 留 P4。

**结果:** `go build ./...` ✓ / `go test ./internal/memory/...` ✓（normalizePredicate + replaces 共存/close + QueryGraph hops）/ `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 图谱纠错 + 增量抽取 + 摘要注入待用户实测。

## 2026-07-08 — 记忆系统 P2：时序知识图谱（实体/关系抽取 + 多跳召回 + bi-temporal 边）

**Why:** P1.5 只有扁平事实（`memory_items`，类别字符串），无实体/关系/多跳/"曾经相信什么"。P2 在同一 SQLite handle 上落地 Graphiti 式 bi-temporal 知识图谱 + LightRAG 式混合检索（向量种子实体 → 图扩展 → 时序过滤）。

**Changes:**
- *internal/memory/store.go*：migrate 追加 `kg_nodes`(id/type/name/name_raw/props/embedding_id/created_at) + `kg_edges`(id/src_id/dst_id/type/props/valid_from/valid_to/recorded_at/session_id, bi-temporal)；新增 `EntityHit` DTO + `AddEntity`/`EntitySearch` 转发。
- *internal/memory/vector.go*：加 `kind="entity"`（`AddEntity`/`QueryEntities`）—— 实体可被向量种子检索。
- *internal/memory/graph.go*（新）：`UpsertNode`（按归一化 name 消歧合并，回调式 embed 保持包 ONNX-free）、`AddEdge`（bi-temporal 软失效：同 `src+type` 的当前有效边 close `valid_to`，不删，保留历史）、`QueryGraph`（递归 CTE 双向扩展 hops 跳，`DISTINCT id` 防多路径重复）、`NodeCount`/`CurrentEdgeCount`。
- *internal/memory/ingestor.go*（新）：`IngestGraph(entities, relations, sessionID, embedFn)` —— 实体 upsert 建图 + 关系解析成边；relation 引用未列实体时按需 upsert 保持图连通。纯数据入口（LLM 在 app 层）。
- *internal/memory/retriever.go*（新）：`RetrieveGraph(emb, k)` —— 复用同一 query 嵌入（不二次 embed）→ `EntitySearch` 种子 → `QueryGraph` 2-hop → 仅当前有效边 → `subject —predicate→ object` 文本块；无种子/无边返回 `""`（零影响降级）。
- *app_memory.go*：`callExtractGraph`/`parseExtractGraph`（克隆 `callExtractFacts` 模式 + `extract_graph` tool schema：`entities[{name,type}]` + `relations[{subject,predicate,object}]`）；`maybeExtractFacts` 每 N 轮同时触发 `maybeExtractGraph`（共用 dialogue，P1.5 facts 不动）；`MemoryRecall` 加 `graph` 字段；`MemoryStatus` 加 `nodeCount`/`edgeCount`。
- *前端*：[api/memory.ts](frontend/src/api/memory.ts) `RecallResult.graph` + `MemoryStatus` 加 node/edge count；[chatStore.ts](frontend/src/stores/chatStore.ts) recall 注入第三段「知识图谱（相关实体与关系）」。

**设计要点:**
- **bi-temporal 软失效范围**：按 `(src_id, type)` close（实体+关系类型级）—— 贴合"偏好从 A 变 B"；旧边 `valid_to` 置位而非删除，"曾经相信"可查。
- **消歧仅 name 归一化**（ToLower+TrimSpace），单用户记忆够用；不同同名实体碰撞留作 P3（LLM coref）。
- **memory 包保持 ONNX-free**：embed 一律回调注入（`rag.EmbedQuery`），graph/ingestor/retriever 不碰 ONNX/toolbox。
- **抽取成本**：复用每 5 轮 gate + 同一 dialogue，每轮多一次 `chatCompletion`，无逐轮开销。
- **零破坏降级**：无嵌入模型/无 provider/抽取失败 → 图谱静默 no-op，聊天不受影响（同 P1.5 契约）。

**结果:** `go build ./...` ✓ / `go test ./internal/memory/...` ✓（UpsertNode 消歧、AddEdge bi-temporal close、QueryGraph hops 三例）/ `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 多跳召回 + 偏好变更历史待用户实测。

## 2026-07-07 — embedding model 灵活绑定 + 迁移（修「下载未绑定」+ 空 KB 绑模型 + 换模型迁移）

**Why:** 用户下载句向量模型后对话记忆仍显示「未绑定」——根因：startup 自动探测只跑一次 + `Detect` 对精简/ONNX-only 包（config 在 `onnx/` 子目录、架构名不在白名单）漏判。且模型一旦绑定不可改（KB 创建时锁死，记忆 startup 探测一次）。本次给 embedding model 全程「可手动选 / 可改绑 / 可迁移」能力。

**Changes:**
- *internal/toolbox/detect.go*：`readHFConfig` 兜底读 `onnx/config.json`（修 ONNX-only 包漏判）；`isTextModel` 的 model_type 白名单扩 `roberta/e5/bge/gte/nomic/jina`。
- *internal/memory/store.go*：`MigrateModel(newDir, embedBatch)`——读全部 `memory_items` → 批量 re-embed → 清 chromem → 重写 → 改绑（先 embed 全部成功才动 chromem，失败不破坏现有数据）。
- *internal/rag/store.go*：`UpdateKBModelDir`（改绑，持久化 meta.json）+ `MigrateKBModel`（manifest IDs → chromem `GetByID` 取完整 content → re-embed → drop+recreate collection → 改绑；manifest 与 count 不一致则中止）。
- *app_memory.go*：`MemorySetEmbeddingModel(dir)`（校验可 embed 后绑定）+ `MemoryMigrateModel(newDir)`。
- *app_knowledge.go*：`UpdateKBModelDir`（空 KB 校验 + 改绑）+ `MigrateKBModel`。
- *前端*：[api/memory.ts](frontend/src/api/memory.ts) + [api/knowledge.ts](frontend/src/api/knowledge.ts) 加 `setEmbeddingModel`/`migrateModel`/`updateModelDir`；[Knowledge.vue](frontend/src/components/Knowledge.vue) 对话记忆区块 + KB 卡片展开体各加「嵌入模型」下拉 + 按钮——**空数据→绑定/换绑，有数据→迁移**（迁移弹确认）。

**设计要点:**
- **手动绑定优先于自动探测**：startup 探测仍作首次默认，但用户可随时 UI 选模型，不依赖重启/探测命中（修「下载未绑定」）。
- **KB 迁移靠 GetByID**：chromem 无 list-all，但 manifest 有全部 ID，逐个 `GetByID` 取完整 content re-embed；manifest 与 collection count 不一致则中止（防数据丢失）。
- **安全顺序**：re-embed 全部到内存成功 → 才 drop 旧 collection/清向量 → 写新。中途失败不破坏现有数据。
- **记忆迁移**：SQLite `memory_items` 存完整 content，直接 re-embed；chromem 同 itemId 重写。

**结果:** `go build ./...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 绑定/迁移待用户实测。

## 2026-07-07 — 记忆系统 P1.5：融合写入（分离存储 + LLM 抽取事实）+ 双路召回 + 可见性

**Why:** P1 的召回**实际失效**——写入把 `"用户:X\n助手:Y"` 拼成一条向量，召回只用「问题」查，向量空间不匹配（弱模型下「问答拼接」vs「纯问题」对不上），跨会话召不回；且记忆无 UI 可见、[App.vue](frontend/src/App.vue) 有重复的「清空」按钮。按用户确认的**融合方案**重做：分离存储（保召回准）+ LLM 抽取事实（提质量）+ 知识库 tab 可见性。

**Changes:**
- *internal/memory/vector.go*：`QueryWithEmbedding` 加 `filter` 参数（chromem filter 等值+AND，不支持 $or → 分 kind 各查一次）；新增 `AddTurn`/`AddFact`（metadata `kind` 区分，turn 带 reply）、`QueryTurns`/`QueryFacts`（typed）、`Clear`（drop+recreate collection）。
- *internal/memory/store.go*：加 `memory_items` 表（manifest，支撑 list/count/clear）；`AddTurnMemory`/`AddFactMemory`（SQLite + chromem 双写，同 itemId 关联）；`QueryMemory` 返回 `(turns, facts)` 双路；`ListMemoryItems`/`CountMemory`/`ClearMemory`。
- *app_memory.go*：`MemoryRemember(userText, reply, sessionID)` 只存 **user 问题向量**（reply 走 metadata）+ 每 5 轮异步 `maybeExtractFacts`；`MemoryRecall` 返回 `{turns,facts}` 双路；新增 `MemoryStatus`/`MemoryList`/`MemoryClear`；`callExtractFacts`（复用 `chatCompletion` + extract_facts function calling）+ `parseExtractFacts`（解析 `choices[0].message.tool_calls[0].function.arguments` JSON string）。
- *前端*：[api/memory.ts](frontend/src/api/memory.ts) 加 `TurnHit`/`FactHit`/`RecallResult`/`MemoryStatus`/`MemoryItem` 类型 + `status`/`list`/`clear`；[chatStore.ts](frontend/src/stores/chatStore.ts) recall 注入分「相关历史问答」+「已知事实」两段，remember 带 sessionId、错误 console 可见（不再全吞）；[Knowledge.vue](frontend/src/components/Knowledge.vue) 顶部加「💬 对话记忆」区块（绑定状态 + 条目数 + 预览 + 清空 + 刷新）；[App.vue](frontend/src/App.vue) 移除重复的「清空」按钮 + `clearChatMessages` + 孤儿 import。

**设计要点:**
- **只存 user 问题向量**（不单独存 assistant 向量）：召回主场景是「新问题匹配历史问题」，reply 通过 metadata 关联带上，存储减半。
- **双路召回**：embed(query) 一次 → chromem filter `kind=turn` + `kind=fact` 各 top-3 → 合并注入（chromem filter 不支持 $or，故分查）。
- **抽取降频异步**：每 5 个 turn 才抽一次事实，goroutine 不阻塞对话，失败仅日志。
- **可见性**：SQLite `memory_items` 当 manifest（chromem doc map unexported 无法直接 list），UI 能 list/count/clear。
- **零破坏降级**：未绑定 embedding 模型时 recall 返回空、区块显示提示，聊天不受影响。

**结果:** `go build ./...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓。GUI 召回 + 记忆区块待用户实测。

## 2026-07-07 — 记忆系统 P1：长期向量记忆（跨会话语义召回）

**Why:** P0 解决了对话持久化，但仍无"记忆"——新会话不知道用户过往说过什么。P1 在 SQLite 之上叠加语义记忆层：每轮最终回复向量化存入独立 chromem collection，下一轮自动 recall top-3 注入 system prompt，实现跨会话召回。整体架构借鉴 Zep/Graphiti（记忆即图）+ LightRAG（向量种子），P2 再升级为时序知识图谱。

**Changes:**
- *internal/memory/vector.go*（新）：`VectorStore` 包装 chromem-go，独立 DB 于 `data/memory/chromem`（`mem_longterm` collection，与 RAG 知识库分离）；`AddWithEmbedding`/`QueryWithEmbedding`/`QueryContents`。**不依赖 ONNX**——只存预计算 embedding，向量由 app 层生成，memory 包保持解耦。
- *internal/memory/store.go*：`Store` 组合 `*VectorStore`（best-effort，初始化失败降级为仅 SQLite）；加 `meta` 表 + `SetMeta`/`GetMeta`；`EmbeddingModelDir`/`SetEmbeddingModel` 绑定模型；`AddMemory`/`QueryMemory`/`HasVector` 转发向量层。
- *app.go*：startup 自动探测 `data/models` 下首个 sentence-embedding 模型（`toolbox.Detect`）绑定到 memory（`detectEmbeddingModelDir`）；找不到则日志提示并静默降级（recall 返回空，对聊天零影响）。
- *app_memory.go*：`MemoryRecall(query, k)`（embed query → top-K 片段文本）+ `MemoryRemember(userText, reply)`（embed "用户→助手" 片段 → 入库）。
- *前端*：[api/memory.ts](frontend/src/api/memory.ts) 加 `recall`/`remember`；[chatStore.ts](frontend/src/stores/chatStore.ts) chatLoop 组装 `apiMsgs` 前 recall top-3 注入 system prompt「长期记忆」段，最终回复（非工具轮）触发 `remember`；`lastUserContent` helper。

**设计要点:**
- **独立 chromem DB**：记忆向量在 `data/memory/chromem`，与 KB 的 `data/knowledge/chromem` 分离，不耦合 `rag.Store`。
- **embedding 在 app 层**：memory 包不碰 ONNX/toolbox，由 app 用已验证的 `rag.EmbedQuery`（MiniLM）生成向量传给 memory。降级路径清晰（无模型 → recall 空 → 无注入）。
- **成本可控**：仅最终回复 remember（工具轮跳过）；recall top-3 限定 token；本地 ONNX embed 毫秒级。
- **零破坏**：embedding 模型未绑定时 recall 返回空，system prompt 无变化，P0 行为完全不受影响。

**结果:** `go build ./...` ✓ / `vue-tsc --noEmit` ✓ / `npm run build` ✓（built in 2.64s）。跨会话 GUI 召回待用户实测（会话 A 说一句，再到会话 B 问相关问题验证召回）。

## 2026-07-07 — 记忆系统 P0：对话持久化（SQLite + 多会话）

**Why:** 此前对话是纯内存态（chatStore `messages` ref + 后端无状态 `ChatStream`），重启即丢、无会话概念、无记忆。本次落地"时序记忆知识图谱"三阶段计划（[tasks/memory-graph.md](tasks/memory-graph.md)）的 **P0**：对话持久化 + 多会话。整体架构借鉴 Zep/Graphiti 的 bi-temporal 知识图谱 + LightRAG 的混合检索，P1/P2 后续推进。

**Changes:**
- *go.mod*：新增 `modernc.org/sqlite`（**纯 Go SQLite，无 CGo**，保持 Wails 单工具链——项目仅 yalue/ONNX 一个 CGo）。
- *internal/storage/storage.go*：`EnsureDataDir` 加 `memory` 子目录。
- *internal/memory/store.go*（新）：`Store` 包装 SQLite（WAL + busy_timeout）；schema `sessions`/`messages`（`CREATE TABLE IF NOT EXISTS`，预留给 P2 的 `nodes`/`edges`/`user_facts`）；会话 CRUD（Create/List/Get/Rename/Delete 级联/UpdateSummary）+ 消息 CRUD（Append 自动 seq + touch updated_at / ListBySession / ClearBySession）。
- *app.go*：App struct 加 `memoryStore *memory.Store`；startup 初始化、shutdown `Close()`。
- *app_memory.go*（新）：Wails 绑定 `MemorySessionList/Create/Rename/Delete` + `MemoryMessageList/Append/Clear`；session 增删改经 `emitChanged("memory:changed")`（消息追加不广播——频率太高）。无后端 seed，前端 `loadSessions` 在无会话时自动建。
- *前端*：[api/memory.ts](frontend/src/api/memory.ts)（新）+ index.ts barrel；[chatStore.ts](frontend/src/stores/chatStore.ts) 加 `sessions`/`currentSessionId` + `loadSessions/createSession/selectSession/renameSession/deleteSession`，`persist()` 把每轮定型的 user/assistant 写库（空内容跳过，如纯工具轮），`clearMessages` 改为"新建会话"（旧历史保留）；[ChatPanel.vue](frontend/src/components/ChatPanel.vue) header 加会话切换器（下拉 + 新建/重命名/删除），`memory:changed` 实时同步。

**设计要点:**
- **纯 Go 嵌入式**：选 `modernc.org/sqlite` 而非 CGo 版 `mattn/go-sqlite3`，不给 Wails 加第二个 CGo。WAL 适配单进程桌面。
- **向量归 chromem-go，关系/对话归 SQLite**：二者以 `embedding_id`/`entity_id` 关联（P2 落地）。不重复造向量轮子，也不让 SQLite 干向量活。
- **持久化范围**：P0 只存 user/assistant 文本 content（tool 调用细节不重放，避免新上下文出现悬空 `tool_calls`）。重建时 filter user/assistant。
- **图存储引擎已定**：用户在 SQL 图建模 / bundle 原生图引擎 / Kùzu fork 中选定 **SQL 图建模**（邻接表 + 递归 CTE + bi-temporal），P2 用同一 SQLite handle 落地时序知识图谱（KùzuDB 已于 2025-10 被官方废弃，排除）。

**结果:** `go build ./...` ✓ / `npm run build` ✓（174 模块）/ `vue-tsc --noEmit` ✓。GUI 端到端（重启看历史、多会话切换）待用户实测。

## 2026-07-07 — 工作流 + 聊天 4 个 bug 修复（agent 节点 / 输出解析 / 进度推送 / 轮次限制）

**Why:** 用户实测本地 Agent + 工作流联动后报 4 个问题。核心两个：①Agent↔工作流没打通（`unknown node type: agent`）；③工作流长节点（LLM ~33s）无进度、只能反复轮询。附带 ②输出节点变量返回字面量、④聊天 10 轮限制阻断长流程。

**Changes:**
- **Fix 1 🔴 agent 节点**：`internal/workflow/model.go` 加 `NodeAgent` 常量；`nodes.go` GetNodeExecutor 加 `case NodeAgent` + `executeAgent`（读 agentId + userPrompt/prompt 别名 → ResolveTemplate → 调 `AgentRunner.RunAgent` → 归一化 `{output, content, raw}`）+ `AgentRunner` 接口；`engine.go` 加 `agentRunner` 字段 + `SetAgentRunner` setter（不改 NewEngine 签名，保测试稳定）+ 300s 超时分支；`app_workflow.go` 加 `workflowAgentAdapter`（按 id/名解析 Agent → `runAgentLoop`）并在 ExecuteWorkflow 注入。打通 Agent↔Workflow，下游可 `{{agentNode.output}}`。
- **Fix 2 🟡 输出节点变量解析**：`executeOutput` 原来对裸 `llm_audit.output`（无 `{{}}`）落到 `ResolveTemplate` 正则无匹配 → 原样返回字面量。改为：有 `{{}}` 走 `ResolveExpression`；否则走 `resolvePath` 直接解析，路径无效则保留字面量（向后兼容静态文本）。`{{llm_audit.output}}` 与裸路径现在都解析到真实内容。
- **Fix 3 🟡 进度推送**：`engine.go` executeNode 改为 for-select + 2s ticker，运行中持续发 `workflow-progress-<execId>` `{execId,nodeId,progress}`；`emitNodeStart` 设 `NodeRun.Progress`（按类型「正在生成… / 调用工具中…」）并发一个即时心跳；done/timeout 清空 Progress。`NodeRun` 加 `Progress` 字段（轮询 `workflow_status` 也能看到）。前端 `WorkflowEditor.vue` 订阅该事件 → 合并进 `nodeRuns` → 节点 + 顶栏实时显示进度文案，resetExec/unmount 清理。
- **Fix 4 🔵 聊天轮次限制**：`chatStore.ts` chatLoop 把 `for round<10` 改为「MAX_ITERATIONS=60（绝对上限）+ MAX_PRODUCTIVE=25（非只读轮上限）」双计数；纯查询轮（工具 `annotations.readOnlyHint=true`，如 workflow_status/plugin_status/system_dynamic）不计入有效预算，反复轮询不再撞墙；两个计数都有上限防死循环。`ToolDef`/`AgentToolDef` 加 `annotations`。后端 `plugin_tools.go`/`system_tools.go` 给 plugin_list/plugin_status/system_info/system_dynamic 补 `ReadOnlyHint`。
- **测试**：`engine_test.go` 加 `TestAgentNodeRunsRunner`（agent 节点派发 + 模板解析 + 输出归一化）和 `TestOutputBarePathResolves`（裸路径解析 + 静态文本保留）。既有 brace-form 测试仍通过。
- **对抗式审查后加固（2 处）**：① 递归守卫 `isOrchestrationTool` 扩展为也剥离 `workflow_execute/create/update/delete`——子 Agent 不能再经 workflow_execute 起新引擎无限递归（`agent_run`/`agent_create`/`agent_list` 本就剥离）；只读的 workflow_list/get/status/validate 保留。② `executeAgent` 改为派生 `context.WithTimeout(eng.ctx, agentNodeTimeout)` 传给 `RunAgent`（原来传 `eng.ctx`，节点 300s 超时不会中断 agent loop）——节点超时后 `runAgentLoop` 的轮间 `ctx.Done()` 现在会真正生效，不再继续跑 LLM 轮次和带副作用工具调用。`agentNodeTimeout` 提为常量供 engine.go 与 executeAgent 共用。加 `app_agent_exec_test.go` 锁定递归守卫。审查驳回 2 项（裸路径拼写静默 fallback 非回归；GetWorkflowExecutionStatus 并发竞态为预存问题、本次未加重）。

**结果:** `go build ./...` ✓ / `go vet ./...` ✓ / `go test ./internal/workflow/... . ` ✓（含 3 个新测试）/ `npm run build` ✓。运行时端到端（拖入 agent 节点跑、聊天委派轮询）尚未在 GUI 实测。

## 2026-07-07 — 本地 Agent（智能体人格）：定义、管理、编排、聊天切换

**Why:** A2A 栈（对内对外互联）已就绪，但缺「Agent 本身」——此前聊天是单一全局助手（系统提示=基础+所有已启用 Skill；工具=已启用 Skill 工具+MCP；模型=全局活动供应商），没有多 Agent / 人格概念。本次引入**本地 Agent 人格**：每个 Agent 是一套可复用 profile（人格/系统提示/能力子集/模型覆盖/温度/maxTokens），主 Agent 可 `agent_create` / `agent_run` 调用，用户可手动管理，聊天面板可切换人格。完整规划见 [tasks/local-agents.md](tasks/local-agents.md)。

**Changes:**
- *internal/agents/agent.go*（新，仿 internal/skills）：`Agent` 结构（name/description/icon/systemPrompt/providerId+model 覆盖/inheritSkills+skills 子集/tools/mcpTools/temperature/maxTokens/isDefault）+ `Manager`（List/Get/FindByName/Create/Update/Delete）+ 持久化到 `data/agents.json`（atomic）+ 首次启动 seed 默认主 Agent（`InheritSkills=true`，复刻原聊天行为）。
- *app_agents.go*（新）：CRUD Wails 绑定（ListAgents/GetAgent/CreateAgent/UpdateAgent/DeleteAgent），变更经 `emitChanged("agents:changed")`。
- *app_agent_exec.go*（新）：执行内核 —— `resolveAgentProvider`（provider/model 覆盖）、`agentSelectedSkills` / `buildAgentSystemPrompt` / `buildAgentToolNames`（人格+能力子集解析）、`resolveAgentToolDefs`（子 Agent 剥离 `agent_*` 防递归）、`runAgentLoop`（≤5 轮工具循环，非流式，供委派）、`GetAgentChatContext`（前端聊天用，含 systemPrompt+tools+provider+model）。
- *app_chat.go*：抽出 `chatCompletion(p, messages, tools, chatOpts{Temperature,MaxTokens})`（ChatProxy 与 runAgentLoop 共用，OpenAI 分支空工具守卫）；`runChatStream` 改为接收显式 `*config.LLMProvider`+opts；新增 `ChatStreamAs(streamID, messages, tools, providerId, model, temperature, maxTokens)` + `resolveProviderForStream`（Agent 人格指定 provider/model/温度/maxTokens 流式对话）。
- *app_tools.go* + *internal/tools/agent_tools.go*（新）：编排工具 `agent_list` / `agent_create` / `agent_run`（schema + handler hAgentList/Create/Run + RegisterAll + init map）。
- *internal/skills/skill.go*：新增内置 Skill `agent-orchestration`（默认启用，含上述三工具），让主聊天自动获得编排能力。
- *app.go*：App struct 加 `agentManager`；startup 初始化。
- *前端*：[api/agents.ts](frontend/src/api/agents.ts)（新，复数命名避开 A2A 的 agent.ts）+ index.ts barrel；[LLMAgents.vue](frontend/src/components/llm/LLMAgents.vue)（新，管理页：Agent 卡片列表 + 新建/编辑对话框含全部字段，provider 下拉 + skills 多选）；[LLMConfig.vue](frontend/src/components/llm/LLMConfig.vue) 新增「⬡ 智能体」Tab；[chatStore.ts](frontend/src/stores/chatStore.ts) 加 `agents`/`selectedAgentId`/`loadAgents`/`selectAgent`，chatLoop 分支（选中 Agent → GetAgentChatContext + ChatStreamAs，否则原全局路径行为不变）；[ChatPanel.vue](frontend/src/components/ChatPanel.vue) 输入区上方加 Agent 选择器 + `agents:changed` 实时同步。

**设计要点:**
- **默认 Agent = 原聊天行为**：seed 的主 Agent `InheritSkills=true` + BaseSystemPrompt，byte-for-byte 复刻原有系统提示/工具集，向后兼容。
- **子 Agent 不递归**：`runAgentLoop` 用 `resolveAgentToolDefs(agent, excludeOrchestration=true)` 剥离 `agent_*`，防止 `agent_run` 调自己无限递归；聊天面板路径保留编排工具。
- **零工具 Agent**：OpenAI 分支空工具时省略 `tool_choice`，避免部分供应商 400。

**结果:** `go build ./...` ✓ / `npm run build` ✓（dist 产出正常，LLMConfig chunk 86.82kB）。

## 2026-07-07 — 飞书机器人集成（WebSocket 长连接，双向群对话）

**Why:** 用户想通过飞书和 EverEvo 交互、飞书做跳板连本地。障碍：飞书在云端无法直连 `localhost`。方案：飞书企业自建应用 + WebSocket 长连接 —— EverEvo 主动出站连飞书，飞书推群消息事件下来，免公网 IP / 内网穿透。范围：双向群对话（@机器人 → LLM → 回复）。

**前置（用户在飞书开放平台）:** 创建企业自建应用 + 机器人能力 + 权限 `im:message` / `im:message:send_as_bot` + 事件订阅「长连接」+ `im.message.receive_v1` + 拿 App ID/Secret + 加群。

**Changes:**
- *新依赖* `github.com/larksuite/oapi-sdk-go/v3`（go.mod + go.sum，`go mod tidy` 补全 gogo/protobuf 等传递依赖）。
- *internal/feishu/config.go*（新）：`Config` / `Status`。
- *internal/feishu/client.go*（新）：长连接 bot —— `Start` 起 goroutine 跑阻塞的 `ws.Start`；`OnP2MessageReceiveV1` 收消息 → 异步调 handler（独立 120s ctx，避免阻塞 SDK 事件回调致重试）→ `Im.Message.Create` 回复；`Stop` 用 `cancel + ws.Close`。生命周期仿 a2a manager（done chan）。
- *config.go*：`LLMConfig` 加 `FeishuConfig`（Enabled/AppID/AppSecret/VerificationToken/AgentName）+ `Defaults`。
- *app_feishu.go*（新，`//go:build windows`）：Wails 绑定（Get/Update/Start/Stop/GetStatus）+ `initFeishuClient`（auto-start if enabled）+ `handleFeishuMessage`（复用 `executeA2ATask` 走 LLM，system prompt 一致）+ `emitChanged("feishu:changed")`。
- *app.go*：App struct 加 `feishuClient`；startup `initFeishuClient`；shutdown `Stop`。
- *前端*：[api/feishu.ts](frontend/src/api/feishu.ts)（新，仿 agent.ts）+ index.ts barrel + [LLMFeishu.vue](frontend/src/components/llm/LLMFeishu.vue)（新，仿 LLMAgent：状态卡 + 配置卡 + 接入指南）+ [LLMConfig.vue](frontend/src/components/llm/LLMConfig.vue) 第 5 tab「⊹ 飞书」。

**结果:** `go build ./...` / `go vet ./...` / `vue-tsc` 全通过。

**部署:** wails dev/build 自动生成飞书 Wails 绑定（App.d.ts）。配 App ID/Secret → 启动 → 飞书群 @机器人 → EverEvo 调 LLM 回复。

## 2026-07-07 — A2A 飞书式签名校验 + agent-card 一致性校验（安全增强）

**Why:** 此前 A2A 连接仅凭 URL、无认证，任何人知道 URL 即可发 task。参照[飞书自定义机器人签名校验](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot)给 A2A 加 HMAC 签名 + 时间戳防重放，并补 agent-card 身份一致性校验。

**签名算法（飞书式）:**
```
string_to_sign = timestamp + "\n" + secret
signature      = base64(HMAC-SHA256(key=string_to_sign, msg=""))
headers: X-A2A-Timestamp + X-A2A-Signature,  timestamp ±5 分钟防重放
```

**Changes:**
- *internal/a2a/sign.go（新）*：`Sign` / `VerifySignature`。secret 为空时跳过校验（向后兼容）。
- *internal/a2a/sign_test.go（新）*：5 用例（正常往返 / 错 secret / 过期 timestamp / 空 secret 跳过 / 缺 header）。
- *internal/a2a/e2e_test.go（新）*：4 用例端到端（httptest 真 server + client）—— 签名往返成功 / 未签名被拒 / 错 secret 被拒 / 无 secret 向后兼容。
- *internal/a2a/server.go* `handleAgentCard`：`card.URL` 改用请求实际 `Host`（原写死 `127.0.0.1:<port>`，代理/测试后不准），host 一致性校验由此在 e2e 可跑通。
- *internal/a2a/server.go*：`NewServer(card, executor, secret)`；task 端点入站验签，不符返回 JSON-RPC `-32001 Unauthorized`。agent-card 发现端点保持公开（否则对端无法连接）。
- *internal/a2a/client.go*：`NewClient(url, secret)`；secret 非空则签名出站请求。**agent-card host 一致性**：`Connect` 拉 card 后校验 `card.URL` host 与连接 host 一致，不符拒绝（防伪造身份）。
- *internal/a2a/manager.go* + *config* + *app_agent.go*：`config.A2AAgentConfig.Secret`（本机 server 验签）+ `RemoteAgentConfig.Secret`（连远端签名），均持久化；`Manager.SetServerSecret`；`StartServer` 用 secret 建 server；`ConnectRemoteAgent` 用 `agent.Secret` 建 client；`AddRemoteAgent`/`UpdateRemoteAgent` 加 secret 参数；`UpdateA2AConfig` 同步 `SetServerSecret`（下次重启生效）。
- *前端*：[agent.ts](frontend/src/api/agent.ts) `A2AConfig.secret` + `RemoteAgent.secret` + add/update 传 secret；[LLMAgent.vue](frontend/src/components/llm/LLMAgent.vue) 本机配置区与远端表单各加"签名密钥"输入框。

**结果:** `go build ./...` / `go vet ./internal/a2a/` / `go test ./internal/a2a/`（9 用例通过：5 签名 + 4 e2e）/ `vue-tsc` 全通过。

**部署:** 向后兼容 —— 不配 secret 时验签跳过，行为不变。启用方式：本机 server 配 secret，对端 client 配相同 secret（自连测试两者须一致）。

**未做（需更大重构）:** task/session 归属校验 —— A2A task 当前无 owner 概念，强加归属需改协议/数据模型，暂缓。

## 2026-07-07 — A2A 连接保活 + 任务 system prompt（设计层面 2/3 落地）

**Why:** 落实上一条「未动（设计层面）」中的两项；第三项（build tag）调研后判定非疏忽，不改。

**Changes:**
- **连接保活**（[internal/a2a/manager.go](internal/a2a/manager.go)）：`NewManager` 启动 `pingLoop`（每 60s 对所有 connected 远端 agent 调 `client.Ping`），不可达者置 `status=error` + 记录原因并移出 `clients`；`SendTask` 失败同样置 error。新增 `Close()` 停止 pinger，[app.go](app.go) shutdown 调用。远端宕机不再长期显示"已连接"。
- **A2A 任务 system prompt**（[app_agent.go](app_agent.go) `executeA2ATask`）：转出的 OpenAI messages 最前插入 system 消息，用 `cfg.Name` 声明 agent 身份（"你是 X，通过 A2A 协议接收请求的智能体…"），peer 消息不再无框架到达。
- **build tag（不改）**：[app_agent.go](app_agent.go) 的 `//go:build windows` 经查是跟随 [app.go](app.go)/[main.go](main.go) 的 Windows 限制（ONNX CGo 为 Windows-only），非疏忽；a2a 包本身已跨平台。去掉它不会让 Linux/macOS 可用（main 包仍因 ONNX 缺失无法构建），反令非 Windows 从"跳过 main 包"变为"main 包缺 main 函数"。真正跨平台需先解 ONNX，另立任务。

**结果:** `go build ./...` / `go vet ./internal/a2a/` 通过。

## 2026-07-07 — A2A 全链路健壮性修复（死锁 / 内存泄漏 / 超时 / 锁占用）

**Why:** 系统性审查 a2a 全链路（[internal/a2a/](internal/a2a/) server / client / manager + app 层集成 + 前端 API），修复 8 处明确问题。设计层面（保活 / build tag / system prompt）未动，待议。

**Changes:**

*internal/a2a/server.go*
- **`Stop()` 互锁隐患**：原持 `s.mu` 写锁调 `httpSrv.Shutdown()`，而 HTTP handler 也要 `s.mu` → 关停瞬间有活跃请求即互锁（5s 超时兜底，但拖慢关停 / 重启）。改为先取 `httpSrv`、置 `running=false`、释放锁，再 `Shutdown`。
- **`tasks` 内存泄漏**：每个 `tasks/send` 都存 Task、从不删除。新增 `cleanupLoop` 后台协程（每 10 分钟清已完成且超过 1 小时的 task；进行中保留），`Start` 启动、`Stop` 关 `done` channel 终止。
- **死代码**：删除未被调用的 `addContextMessages`（及唯一的 `strings` import）。
- **注释**：`writeRPCError` 注释与代码矛盾（注释说"不响应"，代码响应了；代码符合 JSON-RPC spec），改正注释。

*internal/a2a/client.go*
- **超时不对称**：原 `httpClient.Timeout 30s`，但 server executor 可跑 120s → 客户端先超时、服务端任务堆积。去掉全局 timeout，每个调用按需设 ctx 超时：`Connect`/`Ping`/`GetTask`/`CancelTask` 30s，`SendTask` 180s（覆盖 server 120s + 余量）。
- **HTTP status 未检查**：`call` 遇非 200 只报 "decode response"。改为先查 `StatusCode`，返回含状态码 + body 的清晰错误。

*internal/a2a/manager.go*
- **持锁调 `server.Stop()`**：`StopServer`/`StartServer` 原持 `m.mu` 调 `server.Stop()`（最长 5s），阻塞所有并发 manager 调用。改为先取 server、置 `nil`、释放锁，再 `Stop`。
- **持锁 `emit`**：`StartServer`/`StopServer` 原持锁 `emit`（→ EventsEmit）。emit 移到锁外。

**结果:** `go build ./...` / `go vet ./internal/a2a/` 通过（a2a 包无测试文件）。纯内部实现修复，公开 API 签名未变，app 层（app_agent.go / app_tools.go）与前端不受影响。

**未动（设计层面，待议）:**
- 连接保活：远端 agent 宕机后 status 仍 "connected"（可加 SendTask 失败置 error + 定期 Ping）。
- [app_agent.go](app_agent.go) 的 `//go:build windows`：a2a 是纯 Go，本应跨平台（疑似疏忽）。
- `executeA2ATask` 调 ChatProxy 无 system prompt（A2A 任务未定义 agent 身份）。

## 2026-07-07 — 修复关闭程序时卡死（A2A DisconnectAll 自死锁）

**Why:** 退出程序时日志停在 `[a2a] server stopped` 后再无任何输出（缺"再见 👋"），进程挂起、需强制结束。

**根因:** [internal/a2a/manager.go](internal/a2a/manager.go) `DisconnectAll()` 持有 `m.mu` 写锁时调用 `m.saveAgents()`，而 `saveAgents()` 内部又 `m.mu.RLock()`。Go `sync.RWMutex` 不支持嵌套 —— 同 goroutine 持写锁再取读锁会**永久自死锁**。同文件其他方法（`RemoveRemoteAgent` / `DisconnectRemoteAgent` 等）都是 `Lock…Unlock` **之后**才调 `saveAgents()`，唯独 `DisconnectAll` 写错。用户场景 `m.clients` 为空 → 循环跳过 → 直接走到 `saveAgents()` 触发死锁，与"日志无 `[a2a] disconnecting`、卡在 server stopped 之后"的现象吻合；Go runtime 因仍有其他 goroutine 存活未报 deadlock，故挂起而非 panic。

**Fix:** `DisconnectAll` 改为先 `Unlock` 再 `saveAgents`（对齐其他方法模式），顺手清理无用的 `_ = client`。

**结果:** `go build` / `go vet ./internal/a2a/` 通过。纯锁顺序修复，无行为 / 接口变更。

**遗留隐患（未改）:** [internal/a2a/server.go](internal/a2a/server.go) `Stop()` 持 `s.mu` 写锁时调 `httpSrv.Shutdown()`，而 handler（如 `handleAgentCard`）取 `s.mu` 读锁 —— 关停瞬间若有活跃请求会互锁，但有 5s 超时兜底（不会永久卡）。本次用户症状非此处，按 surgical 原则未动；如需彻底可再处理。

## 2026-07-07 — Agent 页面样式对齐 MCP 配置页

**Why:** Agent 标签页（[LLMAgent.vue](frontend/src/components/llm/LLMAgent.vue)）从 MCP 模板复制，但承载了更多内容（Agent 名称输入、创建 Skill / 测试按钮、agent-card description/skills），与 MCP 配置页出现偏差。以 LLMMCP 为标杆对齐。

**Changes（单文件，纯 CSS + 2 行 template）:**
- **卡片背景透明修复**：删除 LLMAgent.vue 内的 scoped `.glass-panel`（半透明 `--bg-glass`），改走全局 `.glass-panel`（`--bg-elevated` 实色）。根因：MCP 未自定义 `.glass-panel`（走全局 = 黑色实色），Agent 却 scoped 自定义了半透明版本（scoped 选择器优先级高于全局 → 透明）；删除后两者同走全局，背景一致。
- **状态卡片**：对齐 LLMMCP `.mcp-status-card` —— 朴素 glass-panel，无运行/停止色差（不沿用 LLMConfig 的激活态高亮）。
- **本机服务卡片区**：端口卡 `gap 12 → 10`、`local-cards` 网格 `1fr 280px → 1fr 220px`，贴近 MCP 的 `1fr 200px`（220 容纳 Agent 特有的「名称 + 端口」双行）。
- **远端 Agent 操作按钮**：`编辑` / `删除` 文字按钮 → icon 按钮（✎ ✕，新增 `.agent-icon-btn`），对齐 LLMConfig / LLMSkills 的 `.pbar-btn`；`.mcpsrv-actions` 由 `flex-wrap: wrap` 改 `nowrap`，消除 5 个按钮换行导致的卡片高度参差。主操作（连接 / 断开 / 创建 Skill / 测试）仍为文字按钮常驻。
- **接入指南代码块** `pre` 字号 `10px → 11px`（+ `line-height: 1.5`），与 LLMMCP 同类指南一致。

**结果:** `vue-tsc --noEmit` 通过（0 错误）。纯样式，无行为 / 接口变更。

## 2026-07-07 — 实时同步推广到所有主要页面（LLM 操作 → 当前页面实时刷新）

**Why:** 用户要求"操作什么我在什么页面就能看见实时变化"。此前只接了工作流页；本次把同一机制推广到所有 LLM 可变更的主要实体页面。

**机制（可复用）:**
- 后端：`*App` 上的 `emitChanged(event, action, id)` 助手（[app_workflow.go](app_workflow.go)），在变更型绑定方法的**成功路径**发 Wails 事件 `<entity>:changed`。原 `emitWorkflowChanged` 泛化为 `emitChanged`，workflow 改用它。
- 前端：组合式 `useDataChanged(event, cb)`（[composables/useDataChanged.ts](frontend/src/composables/useDataChanged.ts)），挂载时订阅、卸载时取消；每页一行接入。
- 关键架构点：LLM 走 tool handler，前端走 Wails 绑定——两者都路由经过同一批 `*App` 绑定方法，所以事件加在绑定方法里即可同时覆盖两条路径（workflow 已验证此点）。

**接入的实体（6 agent 并行实现，文件互不冲突）:**

| 实体 | 事件 | 后端变更方法（成功后发） | 页面 / reload |
|---|---|---|---|
| models | `models:changed` | LoadModelFile / UnloadModel | MyModels / `refreshModels` |
| plugins | `plugins:changed` | Start / Stop / Restart / Install / Delete | PluginManager / `refresh` |
| kb | `kb:changed` | Create / AddTexts / Delete / Clear / DeleteChunks | Knowledge / `refreshKBs` |
| mcp | `mcp:changed` | Add / Remove / Connect / Disconnect | LLMMCP / `loadMCPServers` |
| providers | `providers:changed` | Create / Update / Delete / SetActive | LLMConfig / `loadChatCfg` |
| guides | `guides:changed` | AddSource / RemoveSource / Sync / SyncOne | GuideCenter / `loadAll` |

所有事件均在操作成功后发（错误路径不发，避免误导刷新）。

**未接入（有意）:** Catalog/System/Toolbox（只读搜索或已轮询）、Downloads（已有进度事件 + 1s 轮询覆盖删除等变化）、Skills（无 LLM tool 入口，仅前端操作，操作者本人可见）。需要可按同模式补。

**结果:** `go build` / `go vet` / `go test ./internal/workflow/` 通过；前端 `vue-tsc` 0 错误。

## 2026-07-07 — 工作流：LLM 建图无连线 + LLM 编辑实时同步

**Why:** 两个问题：(1) 让大模型用 `workflow_create`/`workflow_update` 建工作流时，节点出现但**连线消失**；(2) 大模型编辑工作流时，用户停在工作流页面看不到实时变化。

**根因：**
- (1) `workflow_update` 工具的 edge schema 只声明 `source/target/sourceHandle`、**无 `id`**；后端 `manager.Create/Update/Import` 存盘时不补 id；前端 `workflowEdgeToFlowEdge` 直接 `id: we.id`（空）。Vue Flow 以 id 为键存 edge，多个空 id 互相覆盖 → 连线消失。
- (2) CRUD 不发任何事件，前端只在自己发起请求后刷新；更关键的是 **LLM 走的是 tool handler（`hWorkflowCreate/Update/Delete`），直接调 `workflowManager`，前端无从感知**——即使加了事件也漏掉了 LLM 路径。

**Changes:**
- **`frontend/src/utils/workflow-mapper.ts`**: `workflowEdgeToFlowEdge` 在 `we.id` 缺失时生成确定性 id（`source→handle→target`，与后端 `EdgeID` 同格式）。
- **`internal/workflow/manager.go`**: 新增 `normalizeEdges`（给无 id 的 edge 补 `EdgeID`），在 `Create/Update/Import` 调用；`Create` 改为返回 `(id, error)`。
- **`app_workflow.go`**: `CreateWorkflow/UpdateWorkflow/DeleteWorkflow` 成功后 `EventsEmit("workflow:changed", {action, id})`（经 `emitWorkflowChanged` 辅助，复用执行事件同款 `a.ctx`）。
- **`app_tools.go`**: 三个工作流 tool handler（`hWorkflowCreate/Update/Delete`，LLM 的实际路径）改为路由经过 `a.CreateWorkflow/UpdateWorkflow/DeleteWorkflow`，而非直接调 `workflowManager`——确保 LLM 的操作也触发事件，且消除重复的 id-return 适配。
- **`frontend/src/components/WorkflowEditor.vue`**: 订阅 `workflow:changed`：始终刷新列表；`update` 命中当前且 `!dirty` 时重载当前工作流（实时看 LLM 改动，且不覆盖用户未保存的编辑）；`delete` 命中当前则清空；`create` 且无选中时自动打开新建的（便于看 LLM 逐步建图）。卸载时取消订阅。
- **`internal/workflow/engine_test.go`**: 新增 `TestNormalizeEdgesAssignsIDs`（验证补 id 与保留已有 id）。

**结果:** `go build` / `go vet` / `go test ./internal/workflow/`（4 测试）通过；前端 `vue-tsc` 0 错误。

**实时同步范围说明:** 本次只接工作流页（用户报告的具体场景）。同一模式（后端 CRUD 发事件 + 各页面订阅刷新）可推广到其他实体（模型/知识库/Skill 等），按需逐页接入。

## 2026-07-07 — 工作流引擎数据流修复（input / llm / output 节点间数据不通）

**Why:** 实测工作流节点间数据完全无法流通。先以 4-agent 并行诊断工作流（engine/nodes/resolver/handler 分头深读）交叉验证，再独立通读真实代码确认/修正根因：
- **Bug1（input 节点 output 恒为 null）**：`executeInput` 只回显字段默认值、从不读 exec inputs；`Engine.Run` 把 inputs 注入成 `nodeOutputs` 的**裸键**（`nodeOutputs["problem"]`），input 节点自身 output 是 `{字段:null}`。
- **Bug2（LLM 不发 prompt）**：执行器读 `userPrompt`，用户硬编码的字段叫 `prompt` → 取空；同时 `{{input.problem}}` 因 Bug1 解析成 null 是级联表现（agent 判断 Bug2 是 Bug1 级联，但用户"硬编码仍不发送"的实测说明字段名不匹配独立存在，故加别名）。
- **Bug3（模板不解析）**：resolver 仅支持 `{{nodeID.field}}` 双花括号，首段必须是节点 ID；用户按节点名/标题引用（`input`/`llm_evaluate`）→ 找不到 → 原文保留。单花括号 `{...}` 不匹配（且不能支持——会与 JSON body 冲突）。另外 LLM 节点返回原始 API 响应、无干净 `output` 字段，`{{llm.output}}` 也取不到。

**Changes:**
- **`internal/workflow/engine.go`**: Engine 增加 `inputs` 字段并在 `Run` 赋值；每个节点输出后按 `Title` 建别名 `nodeOutputs[title]`，模板可按节点标题引用；保留 inputs 裸键注入（`{{字段}}` 也可用）。
- **`internal/workflow/nodes.go`**: `executeInput` 用真实 exec inputs 覆盖字段默认值并透传未声明输入；`executeLLM` 增加 `prompt` 作为 `userPrompt` 别名，返回值从原始响应归一化为 `{output, content, raw}`（新增 `extractAssistantText` 从 OpenAI 形响应提取文本），下游用 `{{llmNode.output}}` 取文本。
- **`internal/workflow/engine_test.go`（新）**: 3 个测试——`TestInputLLMOutputDataFlow`（端到端 input→llm→output + 标题别名）、`TestLLMPromptAlias`（`prompt` 别名）、`TestBareInputReference`（裸键 `{{字段}}`）。
- **`frontend/src/components/WorkflowNodeConfigPanel.vue`**: LLM User Prompt 提示与 Output source 占位符改为 `{{节点标题.字段}}`，引导按标题引用上游。

**模板引用规则（已同步 design.md）:** `{{nodeID.field}}` / `{{节点标题.field}}` / 顶层输入 `{{fieldName}}` 三者皆可；LLM 输出取文本用 `{{llmNode.output}}`；仅支持双花括号。

**结果:** `go test ./internal/workflow/` 通过、`go build ./...` / `go vet` 通过、前端 `vue-tsc` 0 错误。

## 2026-07-06 — Go 静态分析提示修复（app_catalog / app_knowledge / auth）

**Why:** IDE（gopls）报三个静态分析提示（severity hint）：`app_catalog.go` 的 S1001（逐元素拷贝循环应改用 `copy`）、`app_knowledge.go` 的 nilness（`make` 的返回值不可能为 nil，`out == nil` 是恒假死分支）、`internal/auth/auth.go` 的 writestring（`WriteString` 入参含字符串拼接、产生中间分配）。

**Changes:**
- **`app_catalog.go`**: `ListRevisions` 中逐元素拷贝 `revs` 的循环改为 `copy(names, revs)`。
- **`app_knowledge.go`**: `ListKnowledgeBases` 删除恒假的 `if out == nil { out = []rag.KnowledgeBase{} }`——`make([]T, n)` 必返回非 nil 切片（n=0 时为空切片、非 nil），该分支是变量从 `var out` 改 `make` 后遗留的死代码。`go build ./...` 通过。
- **`internal/auth/auth.go`**: cookie 拼接 `sb.WriteString(c.Name + "=" + c.Value + "; ")` 拆为多次 `WriteString`，避免中间字符串分配。

## 2026-07-06 — 修复多处 TypeScript 编译错误

**Why:** IDE ts-plugin 报两个错误，阻塞类型检查。均为重构 `c6f776b` 后的接口签名遗留：`FileTreeNode` 的 `FileNode.type` 已收窄为 `'file' | 'directory'`，但 `ModelDetailPanel` 的本地 `FileTree` 仍是 `string`；`useToast.confirm` 简化为两参 `(title, message)`，但 `SkillMarket` 仍按旧三参调用。

**Changes:**
- **`frontend/src/components/ModelDetailPanel.vue`**: `FileTree.type` 改为 `'file' | 'directory'`；`rootNode` 标注 `computed<FileTree | null>`，使对象字面量 `type: 'directory'` 在上下文类型下不被拓宽为 `string`，结构兼容 `FileNode`。
- **`frontend/src/components/llm/SkillMarket.vue`**: `toast.confirm` 调用去掉多余的第三个参数（原确认按钮文案）。
- **`frontend/src/components/ImageClassifierPage.vue`**: `active` 的 `id` 由可选改为必选；`open()` 改为只在 `loadModelFile` 成功后才 `active.value = { ...m, id }`。既消除 `:model` prop 的类型不兼容，也顺带修正"加载失败仍进入工具页（此时 `model.id` 缺失、推理必失败）"的潜在缺陷。
- **`frontend/src/components/SystemInfo.vue`**: `dyn` 改用 `DynamicInfo` 类型标注并补 `physicalDisks: []` 初始值。此前 `ref` 初始值缺该字段，推断类型不含 `physicalDisks`，而模板按物理磁盘分组访问它（[66](frontend/src/components/SystemInfo.vue#L66)、[68](frontend/src/components/SystemInfo.vue#L68)）报错；`api/system.ts` 的 `DynamicInfo` 与后端返回都包含该字段。
- **`frontend/src/components/ModelList.vue`、`GuideCenter.vue`**: `DialogModal` 由 `v-model` 改为 `v-model:visible`。该组件 v-model 名是 `visible`（emit `update:visible`），旧写法绑定 `modelValue`，既类型报错，运行时 `visible` prop 也收不到值——对话框根本打不开。
- **`frontend/src/components/GuideCenter.vue`、`api/skills.ts`**: 修两处 v-model 修复后才暴露的预存错误——`marked.parse(...)` 加 `as string`（沿用 [chatStore.ts:131](frontend/src/stores/chatStore.ts#L131) 先例，marked 默认同步）；`openGuidesDir` 用 `call<void>` 包装返回 `Promise`（原写法无 `return`、返回 `void`，调用方 `.catch()` 报错）。
- **`frontend/src/components/Toolbox.vue`**: `active` 类型补上必选 `id`（`(ToolModel & { id: string }) | null`），`open()` 改为始终用确定性 id `tool-<repoId>` 打开（`active.value = { ...m, id }`）。与 `ImageClassifierPage` 同型，但保留了"已加载也算成功"的原语义（`catch` 注释 `/* already loaded */`），不能照搬"只在成功时打开"。同一 `active` 同时喂给 `ImageClassifierTool`（要 `id`）和 `SimilarityTool`（要 `dir`），补 `id` 后两者皆满足。
- **`frontend/src/api/models.ts`、`api/system.ts`（根因清理）**: 消除"API 方法调用 `App()` 后漏 `return`、丢弃 Wails 返回的 Promise"这一整类 bug。`openDownloadedFileDir`（`MyModels.vue:326` 报错的直接原因）、`openFileLocation`、`logToTerminal`、`setProxyEnabled` 全部改为 `return call<void>(() => App().X())`，与同文件 `openDir`/`setProxy` 等一致。其中 `openFileLocation` 的调用方 [PackageTreeNode.vue:71](frontend/src/components/PackageTreeNode.vue#L71) 用了 `await … catch`，此前因返回 `void` 导致 `await` 立即 resolve、`catch` 永不触发、后端失败变静默 unhandled rejection——修复后才真正生效。全项目复扫确认 `api/*.ts` 已无"调用 `App()` 未 return"的方法。
- **`frontend/src/components/FileTreeNode.vue`**: `dlState` 的值类型由内联 `{ active; pct; status }` 改为复用 store 的 `FileProgress`（[downloadStore.ts:17](frontend/src/stores/downloadStore.ts#L17)）。模板用到的 `speed/written/total/reason` 等字段本就在 `FileProgress` 里，原内联类型只是写漏；用单一真相源避免再次漂移。
- **`frontend/src/components/llm/LLMConfig.vue`、`LLMMCP.vue`**: `toast.confirm` 去掉多余的第三参数（确认按钮文案），沿用 [SkillMarket.vue:113](frontend/src/components/llm/SkillMarket.vue#L113) 已修先例。全项目复扫确认三参 `toast.confirm` 已清零（SkillMarket / LLMConfig / LLMMCP 三处）。
- **`frontend/src/components/llm/LLMMCP.vue`（额外，不同类）**: `saveMCPServer` 的 `cfg` 标注 `Partial<MCPServer>` 并对 `transport` 做字面量联合断言。原写法 `srvForm.transport`（reactive 拓宽为 `string`）与 `status: 'disconnected'`（对象字面量无上下文拓宽为 `string`）都不兼容 `Partial<MCPServer>` 的字面量联合；标注后 `status` 由上下文类型自动收窄，`transport` 用断言（select 选项只有 `stdio`/`http`，运行时安全）。
- **`frontend/src/App.vue`**: 删除根模板里未使用的 `<DialogModal ref="dialogModal" />` 及其 import。该组件为声明式（`visible` prop 必选、无 `defineExpose` 命令式 API），而 App.vue 仅以 `ref` 挂载、既无 ref 声明也无任何调用（对比同处 `ConfirmModal` 有完整声明+`show()` 调用），属未完成的死代码——正是 `visible` 必选导致的报错来源。
- **`frontend/src/components/llm/MCPServerForm.vue`**: 与 `LLMMCP.vue` 完全同型——`cfg` 标注 `Partial<MCPServer>` + `transport` 字面量断言。
- **`frontend/src/stores/chatStore.ts`**: 工具结果消息 `toolMsg.role: 'tool'` 加 `as const`，避免对象字面量拓宽为 `string` 而不兼容 `ChatMessage` 的 role 联合。
- **`frontend/src/components/ChatPanel.vue`**: 实现未完成的"滚动跟踪"——模板原有 `@scroll="onScroll"` 但函数从未定义（每次滚动抛被吞掉的 TypeError）。新增 `atBottom` ref + `onScroll`（距底部 <40px 视为在底部）+ `scrollChat` 仅在 `atBottom` 时跟随；`busy` 变 true 时重置 `atBottom` 重新锚定。效果：流式输出时用户上滑阅读历史不再被拽回底部。
- **`frontend/src/stores/chatStore.ts`**: 工具调用回合 `apiMsgs.push(msg)` 中 `msg`（`resp.choices[0].message`，类型缺 `role`）改为 `apiMsgs.push({ role: 'assistant', content: msg.content || '', tool_calls: msg.tool_calls })`。按 OpenAI 规范该 message 运行时本就带 `role: 'assistant'`，原 API 响应类型定义不全；补齐后既满足 `APIMessage` 又保证运行时字段完整。

**结果：** `vue-tsc --noEmit` 全项目 0 错误，`go build ./...` 通过。

**经验：** IDE 在 `frontend/tsconfig.json:1` 报的"类型实例化过深，且可能无限"（TS2589）是 **TS 语言服务器的瞬时假报**——`vue-tsc`（Volar，更权威）从不复现，tsconfig 配置正常。连续快速多文件编辑后易触发，"Restart TS Server"/重载窗口即消失，**不要为它做投机性代码改动**。

## 2026-07-06 — 工作流画布点击/拖拽都无法添加节点修复

**Why:** 重构 commit `c6f776b` 后，从调色板「点击」或「拖拽」添加节点都不生效。两个独立缺陷，须同时修：

1. **递归更新死循环（主因，点击与拖拽的共同路径）**：`syncFlowToWorkflow()` 把 `currentWorkflow.value.nodes/edges` 重新赋值为新数组，而 [208-217] 的 deep watcher 正好监听这两个字段、回调里又调用 `syncFlowToWorkflow()` —— 自己触发自己，无限循环。Vue 检测到递归更新后中止本次渲染（`Maximum recursive updates exceeded`），导致 `flowNodes.value.push(node)` 虽已执行，节点却永远画不出来。
2. **自定义 MIME 在 WebView2 不可靠（仅拖拽）**：`onPaletteDragStart` 写 `application/node-type`，`onCanvasDrop` 完全依赖 `getData()` 读取；Wails/WebView2 在 drop 事件中对自定义 MIME 返回空串，触发 `if (!type) return` 提前退出。同项目 `LLMSkills.vue` 的拖拽可工作，靠的是组件级 ref 载体 + `text/plain` 兜底。

**Changes（`frontend/src/components/WorkflowEditor.vue`）:**
- `syncFlowToWorkflow` 执行期间置 `_restoring = true`，`nextTick` 后恢复，屏蔽 deep watcher 的回环触发，消除递归更新死循环（与 `restoreFlowFromWorkflow` 复用同一标志）。
- 新增组件级 `draggingType` ref 作为拖拽类型的可靠载体；`onPaletteDragStart` 同时写 `draggingType` 与 `dataTransfer('text/plain')`；`onCanvasDrop` 改为 `draggingType.value || getData('text/plain')`；新增 `onPaletteDragEnd` 在 `@dragend` 清理状态。

**经验：** Vue Flow 的 `v-model:nodes` 对 `.push()` 同步正常（源码以 `length` 为依赖），无需改 push；真正阻断渲染的是组件自身的 watcher 自循环。Wails 下拖拽数据应存组件级 ref，`dataTransfer` 仅用 `text/plain`。

## 2026-07-06 — 下载中心删除记录前端无响应修复（第二轮：Pinia v3 响应式断裂）

**Why:** 上一轮修复（2026-07-05）将 `CancelDownload` 改为返回 `error` 解决了后端问题，但前端 UI 仍无响应。诊断日志证明 store 状态已正确更新（`history 6→5`），但 Vue 组件不重渲染。根因是 **Pinia v3** 对 setup store 返回值做了 `reactive()` 包裹，导致 `store.completed` / `store.activeTasks` 等 computed/ref 被**自动解包**为静态值。`MyModels.vue` 中 `const dlCompleted = dlStore.completed` 拿到的是解包后的普通数组，不再是 ComputedRef——store 更新后该变量永远指向旧数组引用，模板无法感知变化。

**Changes:**
- **`frontend/src/components/MyModels.vue`**: 引入 `storeToRefs`，将 `const dlCompleted = dlStore.completed` 等 5 行直接赋值改为 `const { completed: dlCompleted, ... } = storeToRefs(dlStore)` 解构。`storeToRefs` 返回指向 store 内部状态的 `ToRef`，保持完整响应式链路。
- **`frontend/src/stores/downloadStore.ts`**: `cancelDownload` 去掉成功路径末尾的 `fetchAll()`（与 1s 轮询竞态），改用 `_cancelling` Set 过滤轮询回包中被取消中的条目。仅在后端失败时回刷恢复 UI。

**Pinia v3 响应式规则（已加入 design.md）：**
```ts
// ❌ 错误 — Pinia v3 的 reactive() 包裹会导致解包，丢失响应式
const myRef = store.someRef       // → 拿到的是解包后的静态值
const myComp = store.someComputed // → 同上

// ✅ 正确 — storeToRefs 提取真正的 Ref/ComputedRef，保持响应式链路
const { someRef, someComputed } = storeToRefs(store)

// ✅ 同样正确 — 直接在 template 中通过 store 访问
// <div>{{ store.someRef }}</div>
```

## 2026-07-05 — 下载中心删除记录前端无响应修复

**Why:** 用户在「我的模型→下载中心」点击已完成/失败记录的 ✕ 删除按钮时，Go 后端 `CancelDownload` 成功删除了记录，但前端 UI 无任何更新。根因是 `CancelDownload` Go 方法签名为无返回值 (`void`)，Wails v2 对该模式的 Promise resolve 行为不稳定；同时 `dequeueNext()` 和 `activeCount()` 操作 `m.tasks`/`m.pending` 时未加锁，存在数据竞争可能引发 panic 导致 Promise reject。前端 `cancelDownload` 在 `catch` 分支直接 `return` 跳过 `fetchAll()`，UI 无法刷新。

**Changes:**
- **`internal/downloader/downloader.go`**: `dequeueNext()` 加 `m.mu.Lock()`/`defer m.mu.Unlock()` 保护并发操作；`activeCount()` 文档标注调用者须持锁
- **`app_download.go`**: `CancelDownload` 改为返回 `error`（Wails v2 推荐模式 → Promise 可靠 resolve）
- **`frontend/src/stores/downloadStore.ts`**: `cancelDownload()` 重写——乐观更新（立即从列表移除）+ 无论后端成功/失败兜底调用 `fetchAll()`

## 2026-07-05 — HTTP 代理支持：统一客户端工厂

**Why:** 全项目 13 处 HTTP client 创建点均未配置代理。最严重的是 `app_chat.go` 中 `Transport: &http.Transport{DisableKeepAlives: true}` 覆盖了默认 Transport 的 `Proxy: http.ProxyFromEnvironment`，导致聊天 API **永远不走代理**。用户只有开启全局代理（Clash/V2Ray 等设置 `HTTP_PROXY` 环境变量）时才能联网。

**Changes:**
- 新建 `internal/httpclient/` 包 — `Transport()` 返回代理感知的 `*http.Transport`，`New(timeout)` 返回完整 `*http.Client`
- 替换全部 13 处裸 `&http.Client{}` / `http.DefaultClient` 为 `httpclient.New(timeout)`：
  - `app_chat.go` x2（ChatProxy 60s / ChatStream 300s）← 🔴 致命 bug
  - `internal/catalog/catalog.go`（全局共享 client）
  - `internal/downloader/downloader.go` x2（下载 HTTP 请求）
  - `internal/auth/verify.go` x2（MS/HF 验证）
  - `internal/config/capability_probe.go` x2（LLM 供应商探测）
  - `internal/marketplace/fetch.go`（市场数据拉取）
  - `internal/backends/backends.go` x2（引擎下载地址检测）
  - `internal/backends/llama/llama.go`（llama-server 健康检查）
  - `internal/mcp/client/transport.go`（外部 MCP HTTP 传输）

**Build:** Go 编译通过，零裸 `&http.Client{}` 残留。

## 2026-07-05 — 全栈代码质量大修（5 轮）

**Why:** 审计发现 35 处 `(window as any)` 绕过 API 层、6 个死 composables、7 处 fmtSize 重复定义、后端模块边界泄露（download/mcp/capability 混杂）、tools registry 缺并发保护、chatStore 全 `any` 类型、Knowledge/ModelCatalog 大组件不可维护。

### Round 1 — 后端 P0/P1 修复

- **tools/registry.go**: 全局 registry map 新增 `sync.RWMutex` 保护（`Register`/`RegisterExternal`/`UnregisterExternal`/`Lookup`/`IsExternal`/`List` 全部加锁）
- **tools/registry.go**: 删除死代码 `handlerRegistry` + `RegisterHandler` + `Dispatch`（88 行，被 app_tools.go handler map 取代）
- **workflow/nodes.go**: `ToolResult` 添加 JSON tag，与 `tools.ToolResult` 结构一致

### Round 2 — 后端模块边界重组

- **新建 app_marketplace.go**: `ListMarketSkills` / `RefreshMarketSkills` / `InstallMarketSkill` / `UninstallMarketSkill` 从 app_mcp.go 移出
- **app_mcp.go**: 移除 marketplace 方法（~85 行），只保留 MCP Server/Client/ResourceProvider
- **app_download.go**: 重写为纯下载文件。移除模型管理/Toolbox/RAG/文件操作方法（~200 行），移入 `resolveDownloadURL`（从 app_catalog.go）
- **app_models.go**: 新增 `DownloadedFile` / `ListDownloadedModels` / `ToolModel` / `ListToolModels` / `EmbedTexts` / `findOnnxInDir`
- **app_system.go**: 新增 `OpenFileLocation` / `OpenDir` / `DeleteDownloadedFile` / `DeleteDownloadedDir`
- **app_capability.go**: 移除快捷方式方法（移至 app_system.go），只保留 AI 能力探测。清理未使用 import。

### Round 3 — 前端 API 层重组 + 死代码清理

- **删除**: 5 个死 composables（useAsync / useDebounce / usePolling / useCleanup / useEvents）+ 1 个死 store（systemStore）
- **api/system.ts 拆分**: 新建 api/plugins.ts + api/workflow.ts；system.ts 只保留系统信息 API
- **api/index.ts**: barrel export 重写（3 独立来源）
- **api/accounts.ts + api/knowledge.ts**: `(window as any)` → `window.go.main.App`
- **8 组件 import 路径更新**: PluginManager / PluginUse → api/plugins；WorkflowEditor → api/workflow

### Round 4 — 前端 API 层隔离 + chatStore 类型化

- **ModelCatalog.vue**: 10 处 `(window as any)` → `modelsApi.*`（10 方法名重映射）
- **ProviderDialog.vue**: 5 处 → `providersApi.*`
- **SkillMarket.vue**: 5 处 → `skillsApi.*`（已有 marketplace 方法）
- **downloadStore.ts**: 10 处 `(window as any)` → `window.go` / `window.runtime`
- **chatStore.ts**: 5 处 `(window as any)` → `window.go` / `window.runtime`；16 处 `any` → 10 个 typed interfaces（`ToolDef` / `SkillInfo` / `ToolCallResult` / `APIMessage` / `StreamChunkData` 等）
- **api/accounts.ts**: `openLogin` 加 `call()` 包装（fire-and-forget → 有超时）
- **App.vue**: 移除未使用的 `useEvents` import
- **全局**: `(window as any).go.main.App` 35 处 → **0 处**

### Round 5 — 大组件分解 + fmtSize 去重

- **Knowledge.vue** (488→352 行): 提取三个 Tab 子组件 —— `knowledge/KnowledgeAddText.vue` (46行) / `knowledge/KnowledgeSearch.vue` (71行) / `knowledge/KnowledgeBrowse.vue` (34行)
- **ModelCatalog.vue** (698→632 行): 提取详情侧边栏 `ModelDetailPanel.vue` (214行)，Props/Emits 接口完整定义
- **fmtSize 去重**: 7 个组件内联定义 → 统一到 `utils/formatters.ts`（新增 `fmtCount`，`fmtSize` 处理 0→"—"）
- **WorkflowEditor**: 执行监控逻辑与父组件耦合太深，节点配置面板已在之前提取

**Build:** Go + Vite 全部编译通过。最终指标：`(window as any)` 0 处、死 composables 0 个、死 stores 0 个、后端模块文件 13→17、前端 API 模块 1→9、tools registry RWMutex 保护、fmtSize 1 处共享。

## 2026-07-05 — 后端 + 前端按模块解耦

**Why:** 单体 `app.go` 和前端大组件随功能增长不可维护——`app.go` 承担全部 15 类 API，`AICapability.vue` 单文件 2074 行。需要按模块边界拆分为独立文件。

**Backend — app.go 解耦（1 → 15 文件）:**

原单体 `app.go` 拆分为：
- `app.go` — 核心 App struct + startup/shutdown（初始化 ONNX、catalog、downloader、MCP server、skill manager、workflow engine 等内部组件）
- `app_catalog.go` — 模型市场 API（搜索 / 详情 / 文件树 / 下载）
- `app_download.go` — 下载管理 API（队列 + 进度事件）
- `app_models.go` — 模型加载 / 卸载 / 运行
- `app_tools.go` — 工具调用 + Tool Calling 分发（48+ 工具）
- `app_chat.go` — AI 聊天 API（Anthropic / OpenAI 流式对话）
- `app_mcp.go` — MCP Server 启停状态 + MCP Client 管理
- `app_skills.go` — Skill 系统 CRUD
- `app_providers.go` — LLM 供应商配置
- `app_capability.go` — AI 能力并行探测
- `app_workflow.go` — 工作流 CRUD + 执行
- `app_knowledge.go` — 知识库 RAG API
- `app_plugins.go` — 插件生命周期
- `app_guides.go` — 引导中心
- `app_system.go` — 系统信息

**Frontend — 大组件拆分:**

- `AICapability.vue`（2074 行）→ `LLMConfig.vue`（主容器）+ `LLMSkills.vue` + `LLMMCP.vue` + `ProviderDialog.vue` + `SkillDialog.vue` + `MCPServerForm.vue` + `SkillMarket.vue`
- `Knowledge.vue` 独立为知识库管理页
- `PluginManager.vue` 独立为插件管理页
- `GuideCenter.vue` 独立为引导中心页
- 新增 `ChatPanel.vue` 作为可复用聊天面板组件
- `WorkflowEditor.vue` + `WorkflowNodeConfigPanel.vue` 工作流编辑组件

**Build:** Go + Vite 均编译通过。

## 2026-07-05 — 下载流程全链路优化（5 阶段）

**Why:** 下载状态在 4 个组件中重复维护，EventsOn/Off 样板代码泛滥，无队列/并发控制，无自动重试。

**Phase A — Download State 统一 (downloadStore):**
- 新建 `src/stores/downloadStore.ts` — 单一事件监听源，所有组件只读
- 新建 `src/composables/useEvents.ts` — EventsOn/Off 自动清理
- 新建 `src/composables/useToast.ts` — 类型安全的 toast/confirm

**Phase B — 下载队列与并发控制 (backend):**
- Manager 新增 `maxConcurrent`（默认 3），超出排队
- 新增 `queued` 状态 + `dequeueNext()` 自动调度
- Resume 也经过队列检查
- Remove 后释放槽位

**Phase C — 自动重试 (backend):**
- Task 新增 `retryCount`/`maxRetries`，默认重试 3 次
- 指数退避：5s → 30s → 2min
- 可重试错误：timeout, connection reset, 502/503/504, 429
- 新增 `retrying` 状态事件，前端显示重试进度

**Phase D — 文件树进度增强:**
- FileTreeNode 显示实时速度、已下载大小
- 未知 total 时显示 indeterminate 动画 + 闪烁 ↓ 图标

**Phase E — 组件迁移:**
- App.vue：`downloadStore.setToast()` + `bindEvents()`，移除 `_onDLProgress`
- DownloadCenter.vue：全部状态/操作委托给 store，移除 timer/EventsOn/Off（~60 行 → ~20 行）
- MyModels.vue：同上，移除 download 相关状态/定时器/事件（~80 行 → ~20 行）
- ModelCatalog.vue：`dlState` → `dlStore.fileProgress`，package-done 用 store 轮询

**Build:** Go + Vite 均编译通过。

## 2026-07-05 — 下载流程修复：进度条闪烁 + 暂停续传 + 文件树进度条

**Why:** 三个紧密相关的下载 bug：
1. DownloadCenter 进度条在 0 和实际值之间抽搐
2. 暂停后继续会重新开始下载（ModelScope 路径）
3. 模型市场文件树的进度条一直是灰色

**Root cause:** `tryDownload`（单连接回退路径，ModelScope HEAD 返回 405 时使用）不追踪 written bytes、不保存 .part 文件、不支持断点续传。`info()` 调用 `totalWritten()` 总是返回 0（没有 segments）。前端 1 秒轮询用 0% 覆盖了事件驱动的真实进度。

**Backend fixes (`internal/downloader/downloader.go`):**
- Task 新增 `written int64` (atomic) 字段，追踪非分段下载的字节数
- `totalWritten()` 无 segment 时返回 atomic written（不再总是 0）
- `tryDownload` 重写：写入 .part 文件、每 tick 保存 meta、支持 Range 续传、完成时 rename 到目标
- `saveMeta/loadMeta` 同时持久化 segments 和 written 字段
- `run()` 在 loadMeta 失败后检查孤儿 .part 文件，路由到 tryDownload 续传
- 消除 tryDownload 中 `written` 局部变量的 data race

**Frontend fixes:**
- DownloadCenter/MyModels 的轮询只刷新 history，不再替换 active 列表（避免覆盖事件进度）
- FileTreeNode 新增 indeterminate 进度动画：total 未知时显示滑动渐变条 + 闪烁 ↓ 图标

**Build:** Go + Vite 均编译通过。

## 2026-07-04 — 全前端重构：Options API → `<script setup lang="ts">` + 架构升级

**Why:** 29 个组件全部 Options API，AICapability 单文件 2074 行，`v-show` 导致 10 个页面同时挂载+全部轮询运行，`$root.showToast` 全局耦合，CSS 在 15 个组件中重复定义，无 TypeScript，无测试。

**Frontend changes (30/30 components migrated):**
- **语言升级**: 纯 JS → TypeScript + `tsconfig.json`
- **组件语法**: Options API → `<script setup lang="ts">`
- **AICapability 拆分**: 1 个 2074 行文件 → 7 文件（`src/components/llm/LLMConfig.vue` 主容器 + LLMSkills / LLMMCP / ProviderDialog / SkillDialog / MCPServerForm / SkillMarket）
- **路由**: 手动 `v-show` page 变量 → vue-router hash mode + KeepAlive + 懒加载代码分割（主 bundle 497KB → 177KB）
- **状态管理**: 单一 `chatStore.js` reactive → Pinia stores（download / provider / system）
- **API 层**: `window.go.main.App.*` 散落 20+ 组件 → `src/api/` 7 文件封装 118 个方法（client.ts 统一超时/取消/错误格式）
- **Composables**: `src/composables/`（useAsync / useEvents / useDebounce / usePolling / useCleanup）
- **CSS**: 15 个组件重复定义 → `src/styles/components.css` 统一引入
- **错误边界**: `ErrorBoundary.vue` + `app.config.errorHandler` + `unhandledrejection` 监听
- **测试**: Vitest + 2 tests（`src/__tests__/api-client.test.ts`）
- **迁移知识库**: `docs/llmwiki/tasks/vue3-migration-pitfalls.md` — 记录 `$emit` 失效、props 解构丢响应式等高频坑

**Backend changes (防卡死):**
- `capability_probe.go`: 顺序 5 请求 → 3 个并行探测；超时 30s/120s → 15s/30s；新增 `context.Context` 全链路取消；最坏耗时 600s → 90s
- HTTP 超时统一收窄：catalog 15s→10s, marketplace 15s→10s, httpPost 15s→10s, Anthropic probe 15s→10s

**Bug fixes from migration:**
- `FileTreeNode` / `PackageTreeNode` / `ModelCard` 等 8 组件：模板 `$emit` → `emit`（`<script setup>` 下递归组件中 `$emit` 不生效）
- `FileTreeNode`: props 解构成 `const` 丢响应式 → 全部改为 `computed(() => props.xxx)` 直接读
- `MyModels` / `SystemInfo` / `SettingsPanel`: RouterView 不再传 props → 各组件自给自足
- `AccountMenu`: hover 逻辑恢复原始 Options API 版本

## 2026-07-01 — 前端布局响应式优化 + AI 聊天修复

**Why:** 各页面组件使用固定像素宽度和固定内边距，窗口缩放时内容区不能自适应。同时 AI 聊天对话框缺乏固定上下边界的视觉结构，用户体验差；且聊天循环中 tool_result 未持久化到 chatMsgs，导致多轮对话时 Anthropic API 返回 400 错误。

**Changes:**
- `App.vue`: `.main-inner` padding 从固定 `28px 32px` 改为 `clamp(16px, 3vw, 32px) clamp(12px, 2.5vw, 40px)`
- `ModelCatalog.vue`: 详情面板宽度从固定 `420px` 改为 `min(420px, 45vw)`，窄窗口自动缩小
- `GuideCenter.vue`: 左侧面板从固定 `280px` 改为 `clamp(220px, 28vw, 320px)`
- `SettingsPanel.vue`: 卡片网格最小列宽从 `340px` 降为 `280px`，中等窗口能容纳两列
- `SimilarityTool.vue`: `.sim-body` 从 `max-width: 720px` 改为 `max-width: min(720px, 100%)`，窄窗口撑满、宽窗口居中
- `ImageClassifierTool.vue`: 上传区和预览图高度改为视口相对 `clamp()`，随窗口高度缩放
- `style.css`: 为 flex 子元素添加 `min-width: 0` 防止溢出
- `AICapability.vue`:
  - **聊天界面**：新增固定顶部 header（显示当前模型/供应商 + 清空按钮），输入栏使用 `bg-elevated` 背景 + 圆角底边，与 header 形成上下对称的卡片边框；中间消息区独立滚动
  - **Bug 修复**：`chatLoop()` 中 tool_result 消息现同步写入 `this.chatMsgs`，确保下轮对话时 Anthropic API 的 tool_use/tool_result 顺序正确，不再触发 400 错误

## 2026-06-30 — MCP Server + AI 能力管理页 + Skill 系统

**Why:** 上一轮的 32 个 Tool 只是扁平列表，没有统一的外部标准协议，外部 LLM 客户端（Claude Desktop 等）无法发现和调用。用户要求：(1) 做专门的 AI 能力管理页 (2) 把应用能力拆分为 Skill (3) 内置 MCP Server (4) 兼容市面上 Skill 标准。

**MCP 就是标准。** MCP (Model Context Protocol) 由 Anthropic 定义，是 LLM-工具集成的事实标准，兼容 Claude Desktop、Cursor、Continue、VS Code Copilot 等。

### Phase 1 — MCP Server 内置 (`internal/mcp/`)

- **`protocol.go`**: 完整的 JSON-RPC 2.0 + MCP 类型定义（ToolDef, ResourceDef, PromptDef, CallToolResult, InitializeResult, ServerCaps 等）
- **`server.go`**: HTTP MCP Server，`POST /mcp` 处理 JSON-RPC 请求，CORS 中间件，端口可配置，仅绑定 127.0.0.1
- **`tools.go`**: `tools/list` + `tools/call` 处理器，从 `internal/tools` 转换到 MCP ToolDef 格式
- **`resources.go`**: `resources/list` (8 个资源: 模型/插件/KB/系统信息) + `resources/read`（按 URI 动态读取）
- **`prompts.go`**: `prompts/list` (6 个预置提示词: 搜模型/下载/图像分析/创建KB/搜索/管理插件) + `prompts/get`（按参数渲染模板）
- **`lifecycle.go`**: `initialize` 返回 ServerCaps (tools+resources+prompts) 和能力声明

### Phase 2 — 工具 MCP 增强 (`internal/tools/`)

- **`types.go`**: `ToolDef` 新增 `Title`, `OutputSchema`, `Annotations`（readOnlyHint/destructiveHint/idempotentHint）— 完全 MCP 兼容
- **`registry.go`**: 新增 `ListMCP()` 导出 MCP 格式工具列表
- **`model_tools.go`**: 6 个模型工具全部补充 Title 和 Annotations

### Phase 3 — Skill 抽象层 (`internal/skills/`)

- **`skill.go`**: `Skill` = 工具组 + 资源组 + 提示词组，带 name/title/description/icon/enabled/category
- 7 个内置 Skill（模型管理/插件系统/知识库/模型市场/下载管理/系统信息/工具箱）
- `Manager`: List/ListEnabled/SetEnabled/Export/Import/GetEnabledTools
- Skill 文件格式 (`.mbskill.json`)，支持导入导出

### Phase 4 — app.go 集成

- App struct 新增 `mcpServer *mcp.Server` + `skillManager *skills.Manager`
- startup() 中自动启动 MCP Server + 初始化 Skill Manager
- 实现 `mcp.ResourceProvider` 接口（9 个 *JSON 方法）
- 新增 10 个 Wails API: `GetMCPStatus`, `StartMCPServer`, `StopMCPServer`, `SetMCPPort`, `ListSkills`, `ListEnabledSkills`, `SetSkillEnabled`, `ExportSkills`, `ImportSkills`, `GetEnabledToolNames`
- `SaveLLMConfig` 新增 `mcpPort` 参数
- `config.go` 新增 `LLMConfig.MCPPort`（默认 19800）

### Phase 5 — AI 能力管理页 (`AICapability.vue`)

三 Tab 页面替换原有 AIChat.vue：
- **Tab 1 — MCP 服务**: 状态指示器、URL 复制、启动/停止/重启、Claude Desktop/Cursor 接入指南、端口设置
- **Tab 2 — 能力清单**: 7 个 Skill 卡片，每个显示工具/资源/提示词数量，启用/禁用开关，导入/导出 JSON
- **Tab 3 — AI 聊天**: 完整 Tool Calling Loop（仅使用已启用 Skill 的工具），自然语言操作软件

- App.vue 侧边栏 "AI 助手" → "AI 能力"，路由到 AICapability
- SettingsPanel 新增 MCP 端口配置字段

### 构建验证

- `go build` ✓ (0 错误)
- `npm run build` ✓ (56 modules, 1.24s)

