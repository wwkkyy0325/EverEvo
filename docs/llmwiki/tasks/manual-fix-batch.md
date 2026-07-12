# Task: 手动修复清单批次（#5 增强 + #6/#7/#8/#10）

> 用户给出的 10 项手动修复清单中，#1/#2/#3 已修复。本任务处理剩余 7 项中经代码核实的 5 项真实 bug/增强。

## Goal

- 修复 4 个真实 bug（攻略无源 / graph_list nodes null / episodic fact 重复 / token 验证误用 Cookie）+ 1 项增强（插件健康检查致命化）。
- 核实后确认 #4（memoryStore 顺序）、#9（MCP 自启动）在工作树中已正确实现，不做无谓改动。

## Context

- Relevant parts of `design.md`: guides 模块、memory 分层（core vs episodic）、MCP Server、plugin 系统。
- Constraints / prior decisions: 最小改动（surgical）；复用已有函数（`QueryFacts`、`contains`）；不依赖外网（指南用 embed）。

## Steps

- [x] 1. **#7** `hGraphList` 类型断言 `[]any`→`[]memory.GraphNode` + 补 import → verify: `grep -n '.(\[\]any)' app_tools_control.go` 无残留；`go build .` 通过。
- [x] 2. **#8** `AddFactMemory` 加精确（SQL COUNT）+ 语义（`QueryFacts` ≥0.90）去重 → verify: `TestAddFactMemoryExactDedup` / `TestAddFactMemorySemanticDedup` 通过。
- [x] 3. **#10** `verifyHF/MS` 对 token 用 `Authorization: Bearer`（`looksLikeToken` 判别），MS 失败回退 Cookie；细化 Reason → verify: `go build ./internal/auth` 通过（运行期验证见下）。
- [x] 4. **#5** `StartPlugin` 健康检查失败 → `host.Stop` + 返回错误 → verify: 代码审阅 + 编译通过。
- [x] 5. **#6** 攻略预置：`//go:embed userguides/*.md` + `syncLocal` + `NewManager` 首次 seed `everevo` 源 + app.go 首次 `SyncAll` → verify: `TestEmbeddedUserGuidesPresent` 通过；`go build .` 通过。
- [x] 6. **核实 #4/#9**：app.go:281（memoryStore）在 collab restore :305 之前；app.go:362-369 自动启动 MCP + 持久化端口 → 结论：已就绪，未改。

## Notes

- **#7 根因**：Go 不允许把 `[]GraphNode` 断言为 `[]any`（元素类型不同），原断言恒返回 nil → JSON null。仅影响 LLM 工具路径；前端 Wails 绑定 `MemoryGraphList` 直接 marshal typed slice，一直正常。
- **#8 对比**：`AddUserFact`（core 层）早有 key+value 去重，`AddFactMemory`（episodic 层）漏了，故同一事实被 LLM 每 N 轮重复抽取存 3-5 次。
- **#10**：HF token 必须走 `Authorization: Bearer`；whoami-v2 收到 `Cookie: hf_xxx` 必 401。MS 走 API Bearer 是 best-effort（响应 shape 宽松解析），失败回退原有 Cookie 抓取，不回归。
- **#6**：攻略中心定位是"介绍怎么使用本应用"（用户确认），故预置 EverEvo 自身使用指南（6 篇），用 embed 打进二进制，新增内部 `local` 源类型（前端 AddGuideSource 仍用 git/url，不受影响）。

## Result

5 项全部完成并编译通过；#7/#8/#6 附带单测。运行期需桌面端实测确认的：#5（坏插件应即时报错）、#10（HF token 验证返回 username）、#6（攻略中心非空）。
