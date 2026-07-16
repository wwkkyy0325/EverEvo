# AI 能力与聊天

EverEvo 内置完整的 AI 能力层：配置 LLM 供应商后在应用内聊天，并通过 Tool Calling 直接操作软件功能；也可经 MCP Server 把能力暴露给外部客户端。

## 配置 LLM 供应商

打开 **AI 能力 → 供应商**，添加一个供应商：

- 支持 OpenAI、Anthropic，以及任何 **OpenAI 兼容**的自建端点（vLLM / Ollama / LM Studio 等）。
- 填入 Base URL、API Key、模型名。
- 点「探测」验证可达性。

## 聊天与 Tool Calling

- 在聊天面板直接对话。模型可调用 EverEvo 注册的全部工具（48+：搜索模型、下载、跑工具箱、管理工作流、读写记忆……）。
- 工具循环自动多轮执行直到任务完成（纯查询工具不计入有效轮预算，避免浪费）。

## Skill 系统

- 9 个内置 Skill，按类型启用 / 禁用（市场导购、模型运行、工作流、记忆、协同……）。
- 每个 Skill 是一组「工具 + 系统提示词」的打包，决定模型在某领域能做什么。
- 支持从 **Skill 市场** 安装社区 Skill，或导出 / 导入 `.mbskill.json`。

## 本地 Agent（人格）

- 可创建多个 **Agent 人格**：各自带名字、系统提示词、绑定的供应商 / 模型、Skill 子集、工具白名单。
- 主 Agent 复刻默认聊天行为；可建专用 Agent（如「市场导购」「工作流编排」）并通过 `agent_run` 委派。
- 在聊天顶部切换会话与 Agent。

## MCP Server

- 启动后内置 MCP Server 自动运行（默认端口 19400，可在设置改）。
- 外部客户端（Claude Desktop / Cursor 等）用 JSON-RPC 2.0 调用 `http://127.0.0.1:<port>/mcp`。
- 也可反向：在 **MCP** 页连接外部 MCP Server，复用其工具。
