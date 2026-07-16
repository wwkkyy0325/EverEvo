# Task: 协同工作台全观测 + 统一活动日志 + 历史回放

> 把协同工作台变成「观察 AI 工作的窗口」：实时全观测（agent name/活动/通信/工作流/工具）+ 统一活动日志持久化 + 历史回放。

## Goal

- 工作台实时显示 agent 名字（非 ID）与当前活动、agent 间通信、运行中的工作流节点、工具调用。
- 所有 AI 工作进统一活动日志（SQLite），可按会话/类型/来源/时间过滤回放，重启不丢。
- 两套事件总线（collab bus + workflow Wails）统一汇聚到单一 `collab:event` 流 + 单一日志。

## Context

- 痛点（代码核实）：agent 节点显示 ID（[CollabWorkbench.vue:137](frontend/src/components/CollabWorkbench.vue#L137)）；无 agent.start/message/tool.call 事件；工作流事件走独立 Wails 总线、前端无人订阅、执行完即驱逐；collab 事件纯内存无持久化。
- 关键汇聚点：collab bus 的 `forward` sink（[bus.go:159](internal/collab/bus.go#L159) → [app.go:240](app.go#L240)）；工具调用单一入口 `CallTool`（[app_tools.go:152](app/tools/../app_tools.go#L152)）；agent 循环调用点 [app_agent_exec.go:408](app_agent_exec.go#L408)。

## Steps

- [x] 1. **A1** `activity_log` 表 + `LogActivity`/`ListActivity`（[store.go](internal/memory/store.go)，ts+原子 seq 保唯一）→ verify: `TestLogAndListActivity` 通过。
- [x] 2. **A2** `app_activity.go`：recordActivity 队列（buffered 1024，满则丢最旧）+ mapEventToActivity + agentDisplayName + runActivityWriter；[app.go](app.go) forward 回调挂钩 + 启动写 goroutine → verify: `go build .`。
- [x] 3. **A3** [app_workflow.go](app_workflow.go) `workflowEventEmitter` 加 `app`，Emit 时桥接进 `collab:event` + 日志（保留原 wf-* 直发）→ verify: 编译通过。
- [x] 4. **A4** 补事件：`agent.<id>.start`（[app_collab.go](app_collab.go) 异步派发 goroutine）；`tool.<agentID>.call`（[app_agent_exec.go](app_agent_exec.go) 每次 CallTool 后）。**agent.message 不单独发**——通信由 tool.call（dispatch/message 工具的 args 含 targetAgentId）派生。
- [x] 5. **A5** `App.ListActivity` 绑定 + [api/collab.ts](frontend/src/api/collab.ts) `listActivity` + `ActivityRow` 类型；name 映射复用 `agentsApi.list()`。
- [x] 6. **B** [CollabWorkbench.vue](frontend/src/components/CollabWorkbench.vue) 重写：agent 节点显示 name+活动（task/调用工具/写黑板）；工作流节点（wf-exec-start/node-*/done，含进度）；通信边（tool.call 派 dispatch/message → 动画边）；工具调用进事件流（TOOL 标签）；agent/工作流点开抽屉看明细。
- [x] 7. **C** 新 [ActivityHistory.vue](frontend/src/components/ActivityHistory.vue) + 路由 `/activity`（[router/index.ts](frontend/src/router/index.ts)）+ 导航「活动历史」（[App.vue](frontend/src/App.vue)）：过滤 + 会话运行卡片回放 + 时间线 + payload 详情 + 实时追加。

## Notes

- **日志写异步**：buffered channel + 单 goroutine，满则丢最旧并告警（护事件总线不被磁盘 IO 阻塞）。drop 行为在 changelog 注明。
- **工作流历史走活动日志**：Manager 执行完即驱逐（[manager.go:265](internal/workflow/manager.go#L265)），故历史不依赖 Manager。
- **已知限制**：工作台运行中途挂载会错过早期工作流事件（事件驱动，无 mount 快照）；聊天面板工具调用未归属（缺 caller 上下文，留后续）。
- **deviation**：计划提的 collab e2e「发布→落日志」断言未加（需重 App 装配，重）；以 memory 单测覆盖日志层 + 运行期实测代替。

## Result

前后端全观测打通。`go build .` + `go vet` + `TestLogAndListActivity` + `vue-tsc`（改动文件零错误）均通过。运行期需桌面端实测：collab_create/dispatch → 工作台见 name/活动/通信；workflow_execute → 工作流节点；重启 → 活动历史可回放。
