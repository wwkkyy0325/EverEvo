# 快速上手

欢迎来到 EverEvo —— 开源模型工具箱。EverEvo 的理念是「**找模型、下模型、按类型用模型**」：从 Hugging Face / ModelScope 找到模型，下载，然后按模型类型用专门的工具来推理，而不是用一个通用文本框硬套任何模型。

## 三大模块

1. **市场**：浏览 / 搜索 / 下载 Hugging Face 与 ModelScope 的模型。
2. **模型库**：已下载的模型，按类型归类索引。
3. **工具箱**（核心）：按模型类型组织的推理工具，例如句向量（语义相似度 / 搜索）、图像分类。

此外还有 AI 能力（聊天 / Skill / Agent）、工作流引擎、知识库（RAG）、记忆系统、插件、MCP Server 等进阶能力。

## 第一次使用建议

1. 打开 **市场**，搜索一个句向量模型（如 `all-MiniLM-L6-v2`），下载。
2. 进入 **工具箱**，选「语义相似度」工具，载入刚下载的模型，输入两段文本即可得到相似度分数。
3. 打开 **AI 能力**，配置一个 LLM 供应商（OpenAI / Anthropic / 自建 OpenAI 兼容端点），就能在应用内直接聊天，并通过 Tool Calling 操作软件的各项功能。

> 想让外部客户端（Claude Desktop / Cursor）调用 EverEvo？启动后会自动拉起内置 MCP Server，在 **AI 能力 → MCP** 页查看地址。

## 接下来

- 要下载模型：看 [市场与下载](marketplace-download.md)
- 要跑推理：看 [工具箱](toolbox.md)
- 要用 AI 聊天 / Agent：看 [AI 能力与聊天](ai-chat.md)
- 要记住你的偏好：看 [记忆与知识](memory-knowledge.md)
- 要编排流水线：看 [工作流](workflow.md)
