# EverEvo

> 开源模型工具箱：一站式「找模型、下模型、按类型用模型」。
> Browse/download models from Hugging Face & ModelScope, then run them through purpose-built tools per model type.

EverEvo 是一个基于 [Wails v2](https://wails.io)（Go 后端 + Vue 3 前端）的桌面应用。

**为什么不是「通用模型运行器」？** AI 模型的输入/输出语义差异巨大——句向量模型要两段文本算相似度，图像分类要一张图，目标检测要画框。用一个通用「文本框」硬套任何模型都不实用。EverEvo 按**模型类型**组织专门的推理工具，每个工具的输入/输出都符合该类型的语义（相似度分数、类别、框），而不是把向量/概率塞进文本框。

## 特性

- **模型市场**：浏览 / 搜索 / 下载 Hugging Face 与 ModelScope 的模型（集成站，含分页文件树、断点续传下载、队列管理、自动重试）。
- **模型库**：已下载模型，按类型归类索引。
- **工具箱**（核心）：按模型类型的推理工具——
  - 句向量工具 — 语义相似度 / 语义搜索（`all-MiniLM-L6-v2` 等）
  - 图像分类工具 — 传图出类别 + 置信度（ResNet / MobileNet 等）
  - _更多类型逐步加入_
- **模型类型自动探测**：加载模型时读 metadata / `config.json` / I/O 形状判断类型，路由到对应工具；识别不了的给明确提示。
- **MCP Server**：内置 MCP（Model Context Protocol）服务，对外暴露工具 / 资源 / 提示词，兼容 Claude Desktop / Cursor 等客户端。
- **AI 能力管理**：AI 聊天（Tool Calling Loop）、Skill 系统（9 个内置 Skill，可按类型启用/禁用，支持导入/导出）、LLM 供应商配置。
- **Skill 市场**：在线浏览 / 安装 / 卸载社区 Skill（自动添加依赖 MCP 服务）。
- **工作流引擎**：可视化拖拽编排模型管线（DAG 节点 + 条件分支 + 循环）。
- **插件系统**：外部插件加载与生命周期管理。
- **知识库（RAG）**：文档切片、向量嵌入、语义检索（三 Tab 界面：添加 / 搜索 / 浏览）。
- **引导中心**：新手引导 + 模板同步。

## 架构

```
Frontend (Vue 3 + Vite) → Go Backend (Wails v2 API)
         │                        ├─ ONNX Runtime   (yalue/onnxruntime_go, CGo)
         │                        ├─ Tokenizer      (sugarme/tokenizer, 纯 Go)
         │                        ├─ Catalog        (HF / ModelScope 市场接入)
         │                        ├─ Downloader     (并发下载 + 队列 + 断点续传 + 自动重试)
         │                        ├─ MCP Server     (JSON-RPC 2.0, tools/resources/prompts)
         │                        ├─ Workflow       (DAG 引擎 + 节点解析 + 条件分支)
         │                        ├─ Plugin         (外部插件加载 + 生命周期)
         │                        ├─ RAG            (切片 + 嵌入 + 检索)
         │                        ├─ Skills         (7 内置 Skill, 导入/导出)
         │                        ├─ Toolbox        (模型类型探测 + 句向量/图像分类)
         │                        ├─ Marketplace    (社区模型分享)
         │                        └─ Config / Storage / Install / SysInfo / Auth / Security / Guides
```

| 层 | 技术 | 职责 |
|----|------|------|
| GUI | Wails v2 + Vue 3.5 + TypeScript + Pinia + vue-router | 桌面窗口、市场、工具箱、AI 能力、工作流编辑器 |
| 前端架构 | 30 组件全部 `<script setup lang="ts">`；API 层 9 模块封装全部 Wails 调用（0 处 `window` 绕过）；Pinia stores + composables；代码分割懒加载 | 类型安全的 API 隔离、统一超时/错误处理 |
| 后端 | Go（17 个 app_*.go 按模块解耦） | 业务逻辑、模型生命周期、市场、下载、MCP、工作流 |
| ONNX | yalue/onnxruntime_go (CGo) | ONNX 推理（运行时加载 `onnxruntime.dll` 1.26） |
| 分词 | sugarme/tokenizer (纯 Go) | NLP 模型分词（BERT WordPiece 等） |

> 历史设计曾规划 Rust 引擎 + CGo FFI（`internal/bridge` / `engine/`），但**未实现**。项目当前为纯 Go + 一个 CGo 依赖（yalue，用于 ONNX）。详见 [docs/llmwiki/design.md](docs/llmwiki/design.md)。

## 前置

- Go 1.26+
- Node.js 18+
- gcc（CGo 编译 ONNX 绑定需要；Windows 用 MinGW-w64）
- Wails CLI：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- ONNX Runtime 1.26 `onnxruntime.dll`（已 bundled 在 `third_party/onnxruntime/win-x64/`，构建时拷到 EXE 旁）

## 构建（Windows）

```powershell
.\scripts\build.ps1 all      # 前端 + Go + bundle onnxruntime.dll
.\scripts\build.ps1 run      # 运行
.\scripts\build.ps1 dev      # 开发热重载
```

## 用法

1. **市场**：搜索 / 浏览模型，下载所需文件到模型库。
2. **工具箱**：选模型类型对应的工具，载入模型，按工具的输入方式使用（相似度工具输两段文本；图像分类工具选图）。
3. **AI 能力**：配置 LLM 供应商 → 启用 Skill → 通过 MCP 暴露工具给外部客户端，或在应用内直接聊天操作软件。
4. **工作流**：拖拽节点编排模型管线，支持预处理→推理→后处理的 DAG 流程。
5. **知识库**：上传文档 → 自动切片 / 向量化 → 语义检索。

## 支持的模型类型

| 类型 | 工具 | 状态 |
|------|------|------|
| 句向量（sentence-transformers） | 相似度 / 语义搜索 | ✅ 可用（MiniLM 真实推理验证通过） |
| 图像分类 | 选图 → 类别 + 置信度 | 🚧 规划中 |
| 文本分类 | 文本 → 类别 | 📋 计划 |
| 目标检测 | 选图 → 框 + 类别 | 📋 计划 |
| GGUF / LLM（llama.cpp） | 文本生成 | ⏸ 受阻（Windows Go 绑定生态不成熟） |
| RAG 检索增强 | 文档 QA | ✅ 可用（切片 + 嵌入 + 语义检索） |

## 项目结构

```
EverEvo/
├── main.go                        # 入口；启动 Wails
├── app.go                         # Wails App 核心（startup/shutdown + 内部组件初始化）
├── app_catalog.go                 # 模型市场 API
├── app_download.go                # 下载管理 API（队列 + 进度事件 + 引擎下载）
├── app_models.go                  # 模型加载 / 卸载 / 运行 + 模型文件管理
├── app_tools.go                   # 工具调用分发（48+ 工具 handler map）
├── app_chat.go                    # AI 聊天 API（Anthropic / OpenAI 流式对话）
├── app_mcp.go                     # MCP Server/Client API（启停 + 端口 + 外部服务管理）
├── app_marketplace.go             # Skill 市场 API（浏览 / 安装 / 卸载）
├── app_skills.go                  # Skill 系统 API
├── app_providers.go               # LLM 供应商配置 API
├── app_capability.go              # AI 能力探测 API
├── app_workflow.go                # 工作流引擎 API
├── app_knowledge.go               # 知识库（RAG）API
├── app_plugins.go                 # 插件管理 API
├── app_guides.go                  # 引导中心 API
├── app_system.go                  # 系统信息 + 文件操作 + 开始菜单 API
├── dialog_windows.go              # 系统对话框（Windows）
├── shortcut_windows.go            # 快捷方式创建（Windows COM）
├── internal/
│   ├── backends/
│   │   ├── onnx/                  # ONNX 推理（yalue 绑定 + session 管理）
│   │   ├── llama/                 # llama.cpp（当前 stub）
│   │   └── safetensors/           # SafeTensors 格式读取
│   ├── catalog/                   # HF / ModelScope 模型市场（搜索 / 详情 / 文件树 / 缓存）
│   ├── config/                    # 配置管理 + AI 能力探测
│   ├── downloader/                # 下载引擎（分段并发 + 队列 + 自动重试 + 断点续传）
│   ├── guides/                    # 引导内容 + 模板同步
│   ├── marketplace/               # 社区模型市场（发布 / 搜索）
│   ├── mcp/
│   │   ├── server.go              # HTTP MCP Server（JSON-RPC 2.0）
│   │   ├── protocol.go            # MCP 类型定义
│   │   ├── tools.go               # tools/list + tools/call 处理器
│   │   ├── resources.go           # resources/list + resources/read
│   │   ├── prompts.go             # prompts/list + prompts/get
│   │   ├── lifecycle.go           # initialize / shutdown
│   │   └── client/                # MCP 客户端（对接外部 MCP Server）
│   ├── model/                     # ModelRunner 接口 + 生命周期 Manager
│   ├── plugin/                    # 插件加载 + 主机 + Manifest
│   ├── rag/                       # RAG 引擎（chunker + embedder + store）
│   ├── security/                  # 安全策略
│   ├── skills/                    # Skill 抽象层（9 内置 Skill + 导入/导出）
│   ├── storage/                   # 数据目录管理
│   ├── sysinfo/                   # 系统信息采集（CPU / GPU / 内存）
│   ├── tokenizer/                 # 分词（sugarme, BERT WordPiece）
│   ├── toolbox/                   # 工具检测（类型探测 + 句向量推理内核）
│   ├── tools/                     # 工具注册表（RWMutex 保护 + MCP 工具定义）
│   └── workflow/                  # 工作流引擎（DAG + 节点 + 条件 + 解析器）
├── frontend/src/                  # Vue 3 SPA（TypeScript + `<script setup>`）
│   ├── api/                       # API 层（9 模块，全部 Wails 调用通过 `call()` 包装）
│   │   ├── client.ts              # 统一超时/取消/错误格式
│   │   ├── models.ts              # 模型 / 下载 API
│   │   ├── providers.ts           # LLM 供应商 API
│   │   ├── mcp.ts                 # MCP 服务 API
│   │   ├── skills.ts              # Skill / 市场 / Guide API
│   │   ├── knowledge.ts           # 知识库 RAG API
│   │   ├── system.ts              # 系统信息 API
│   │   ├── plugins.ts             # 插件管理 API
│   │   └── workflow.ts            # 工作流 API
│   ├── components/                # 34 个组件
│   │   ├── ModelCatalog.vue       # 模型市场（搜索 + 网格）
│   │   ├── ModelDetailPanel.vue   # 模型详情侧边栏
│   │   ├── Knowledge.vue          # 知识库容器
│   │   ├── knowledge/             # 知识库 Tab 子组件
│   │   │   ├── KnowledgeAddText.vue
│   │   │   ├── KnowledgeSearch.vue
│   │   │   └── KnowledgeBrowse.vue
│   │   ├── llm/                   # AI 能力子组件（LLMConfig / LLMSkills / LLMMCP / …）
│   │   └── viewers/               # 输出查看器（DynamicForm / ImagePreview / …）
│   ├── composables/               # 复用逻辑（useToast）
│   ├── router/                    # vue-router hash mode + KeepAlive + 懒加载
│   ├── stores/                    # Pinia stores（download / provider / chat）
│   ├── styles/                    # 统一样式
│   ├── types/                     # TypeScript 类型声明
│   └── utils/                     # 工具函数（formatters / workflow-mapper / icons）
├── third_party/onnxruntime/       # bundled onnxruntime.dll 1.26
└── docs/llmwiki/                  # 设计 / 任务 / 变更记录
```

## 设计文档

- [docs/llmwiki/design.md](docs/llmwiki/design.md) — 架构、模块、关键决策、路线图
- [docs/llmwiki/changelog.md](docs/llmwiki/changelog.md) — 变更记录

## License

MIT
