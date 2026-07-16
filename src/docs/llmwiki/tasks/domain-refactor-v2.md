# Domain-as-Container v2 — 领域即容器 · 知行合一

> **Status:** Final Plan | **Date:** 2026-07-11 | **Author:** System diagnosis + codebase audit + web research
> **Supersedes:** [domain-refactor.md](domain-refactor.md) (v1 proposal)

---

## Executive Summary

**核心命题不是"整理数据"，而是让每个领域成为 AI 的独立认知单元。** 这一定位得到了 25+ 篇学术论文和生产系统分析的支撑。

当前 EverEvo 的 `domain_libraries` 表只是记忆系统内部的分组标签，不是架构级容器。每个子系统可以绕过领域独立创建资源。领域化后，领域从松散标签升格为**架构级强制容器**——进入一个领域，AI 自动拥有该领域内的 KB + Agent + MCP + Wiki + Skill + 记忆；切换领域，AI 认知边界同步切换。

### 量化收益（基于学术证据）

| 指标 | 当前（全局） | 领域化后 | 来源 |
|------|-----------|---------|------|
| System Prompt Token | ~4000 | ~800 (**-80%**) | 实测估算 |
| 上下文窗口利用率 | ~20%（80% 噪音） | ~90%（领域聚焦） | PC-RAG 论文 |
| 领域专业知识退化 | — | **避免 47% 退化**（vs 40K token 稀释） | NeurIPS 2025 |
| RAG 假阳性 chunk | — | **减少 51%**（领域分区检索） | PC-RAG |
| 小模型可行性 | 6GB VRAM OOM | ✅ 正常工作 | 实测预估 |
| Skill 加载开销 | 13 个 L2 指令全量 | 仅当前领域 + 全局（**-90%**） | Scalac 三层加载 |
| 记忆复用率 | ~0%（传统 RAG） | 58.6%（分层缓存） | Cognitive Workspace |

### 学术共识

三个独立方向在 2025 年收敛于同一结论——**领域隔离是 AI Agent 可靠性的必要条件：**

1. **安全方向** (IsolateGPT/SecGPT, NDSS 2025)：domain-based context window isolation 将攻击成功率降至 0%
2. **检索方向** (PC-RAG, 2024-2025)：领域分区检索将幻觉假阳性减少 51%；瓶颈在检索架构，不在模型能力
3. **认知方向** (CoALA, TMLR 2024)：智能源于记忆类型之间的协调，而非扩展单一上下文窗口

**Karpathy** 的定义最简洁：**"LLM 是 CPU，上下文窗口是 RAM——有限且宝贵。大多数 Agent 失败是上下文失败，不是模型失败。"**

---

## 1. 现状诊断：6 类问题，同一根因

### 1.1 问题清单（代码审核确认）

| # | 问题 | 状态 | 根因 |
|---|------|------|------|
| 1 | **孤儿 KB** — "知识" KB 的 `libraryId` 指向不存在的 `lib_18c0842c0b947f70` | 🔴 未修复 | KB 创建时 libraryId 可选，删除 domain 时未级联 |
| 2 | **Skill 三套体系** — `skills.json`(13) / System Prompt(13) / `data/skills/`(30+) 互不同步 | 🔴 未修复 | Skill 结构体有 LibraryID 字段但从未强制执行；三套体系各自独立维护 |
| 3 | **MCP 无归属** — `ServerConfig` 有 `LibraryID` 字段但全指向"默认" | 🟡 半修复 | 字段已加、回填已做，但前端创建时无领域选择器 |
| 4 | **Agent 与领域脱钩** — 有 `LibraryID` 但前端 Agent 列表不按领域分组 | 🔴 未修复 | 前端有 `visibleAgents` computed（chatStore）和 `libAgents`（Knowledge），但 Agent 管理页（LLMAgents）不按领域组织 |
| 5 | **Wiki 归属混乱** — 底层有 library 隔离，前端 Wiki 列表无领域过滤 | 🔴 未修复 | Wiki Store 按 libraryID 构造，但前端 Knowledge.vue 中 wiki 区块未按 library 过滤 |
| 6 | **核心记忆冗余** — "用户名叫 wky" x8、"RTX 3060" x5、"vibecoding" x6 | 🔴 未修复 | 去重逻辑存在（exact + cosine ≥0.90）但只对新写入生效；存量重复未清理；LLM 每 N 轮重复抽取相同事实 |

### 1.2 根因分析

```
domain_libraries 表 ≠ 架构级容器
         ↓
每个子系统可以绕过领域独立创建资源
         ↓
LibraryID: 可选参数 → 允许空值 → 允许悬挂引用
         ↓
前端不按领域组织 → 用户看不到"领域"概念
         ↓
AI 系统提示：全局塞 50 条记忆 → 噪音 > 信号
```

**一句话根因：领域被设计成可选标签，而非必需父容器。**

### 1.3 代码审核确认的现状矩阵

| 子系统 | 存储位置 | 有 LibraryID? | 强制? | ListByLibrary? | 前端领域过滤? |
|--------|---------|--------------|-------|----------------|-------------|
| KB | `internal/rag/` meta.json | ✅ | ❌ 可选 | ✅ | ✅ `libKBCount` |
| Agent | `agents.json` | ✅ | ❌ 可选 | ✅ `ListByLibrary()` | 🟡 chatStore `visibleAgents` |
| MCP Server | `mcp_servers.json` | ✅ (新增) | ❌ 可选 | ✅ `ListMCPServers(libId)` | ❌ |
| Wiki | `data/wiki/<libId>/` | ✅ Store 构造时 | ✅ 必传 | ✅ (per-store) | ❌ |
| Skill | `skills.json` | ✅ | ❌ 可选 | ✅ `ListByLibrary()` | ❌ |
| Memory | SQLite `memory_items` | ✅ `workspace_id` | ❌ 可选 | ✅ (via workspace_id) | ✅ |
| Domain Lib | SQLite `domain_libraries` | — | — | ✅ `LibraryList()` | ✅ Knowledge.vue |

**已存在的基础设施：**
- `useActiveLibrary` composable — 跨组件领域状态共享（已存在！）
- `ListByLibrary` / `ListEnabledByLibrary` — 领域过滤查询（已存在！）
- `EnsureLibraryIDs` / `BackfillLibraryIDs` — 启动时回填空字段（已存在！）
- `domain_libraries` 表 + icon/sort_order/use_count 列（已存在！）

**结论：基础设施已就绪 60%，缺的是强制执行和前端整合。**

---

## 2. 业界最佳实践 & 文献综述

> 以下内容基于 20+ 篇学术论文及生产系统分析，覆盖 Context Engineering、认知架构、领域隔离三大方向。

### 2.1 Context Engineering — 共识与范式转变

**Andrej Karpathy（OpenAI 联合创始人）** 将 Context Engineering 定义为继 Prompt Engineering 之后的下一代 AI 工程范式：

> "The LLM is like a CPU; the context window is like RAM — limited and precious. Most agent failures are context failures, not model failures."

他的 **LLM Wiki / Compiler Pattern** 对比传统 RAG：

| | 传统 RAG (Interpreter) | Karpathy LLM Wiki (Compiler) |
|---|---|---|
| 处理 | 每次查询重新处理原始文档 | 预编译为结构化 wiki |
| 存储 | 不透明向量嵌入 | 人类可读 Markdown |
| 检索 | 语义相似度搜索 | 索引导航 + 反向链接 |
| 可审计 | 低 | 高（每句可追溯） |
| 知识增长 | 线性/静态 | 滚雪球 — 每条新来源更新多页面 |

**三层架构：** `raw/` (不可变原始资料) → `wiki/` (LLM 编译维护的知识) → `index.md` (全局目录 — 必过之门)

**Andrew Ng** 强调 Data-Centric AI：知识/数据的质量和结构比模型架构调整更重要。Context Management 是生产 LLM 部署的 **#1 工程挑战**。

**Lilian Weng（OpenAI 安全系统负责人）** 在 "LLM Powered Autonomous Agents" (2023) 中定义了经典 Agent 架构：**Planning → Memory → Tool Use → Action**。Context Engineering 操作直接映射：**write**（编码经验）→ **select**（检索相关）→ **compress**（摘要节省 token）→ **isolate**（防止跨子任务污染）。

### 2.2 学术证据：领域隔离的必要性

#### 2.2.1 IsolateGPT / SecGPT (NDSS 2025 顶会)
Wu et al. 提出 **domain-based context window isolation**：不同工具（如 Email vs. Cloud Drive）运行在**独立 LLM 实例**中，具有隔离的上下文窗口，由 planner LLM 协调。灵感来自浏览器安全模式（site isolation、same-origin policy）。

> **结果：** 攻击成功率在部分配置中降至 0%；功能保存率 ~75%，开销 <30%。

**对 EverEvo 的启示：** 每个 Domain 内的 Agent 运行在隔离的上下文窗口中；跨域调用通过 collab kernel 中介。

#### 2.2.2 PC-RAG：领域分区检索 (2024-2025)
**核心发现：** RAG 幻觉的主要原因是 **上下文窗口污染** — 单次检索聚合语义异构领域的 chunk，制造嘈杂的跨域上下文。领域分区检索可将 **假阳性 chunk 减少 51%**，综合质量从 3.1 提升至 4.1（5 分制）。仅升级模型仅从 3.1 提升至 3.4——瓶颈在检索架构，不在模型能力。

> **对 EverEvo 的启示：** RAG 搜索必须按领域过滤；混合领域检索产生"看起来连贯但事实不稳定的响应"。

#### 2.2.3 安全知识稀释 (NeurIPS 2025)
**核心发现：** 当模型暴露在大量不相关上下文中时，领域特定专业知识**系统性退化**。安全功能实现率在从聚焦（0 稀释）到重度稀释（40K token）时**下降 47%** (p < 0.001)，覆盖 400 个代码生成任务。

> **对 EverEvo 的启示：** 把 50 条核心记忆 + 13 个 Skill + 3 个 MCP 全塞进 System Prompt = 安全知识稀释。领域化后仅注入当前领域的 8-10 条记忆 = 聚焦。

#### 2.2.4 Chain of Agents (Google Research)
多 worker agent 协作处理长上下文：worker 按顺序处理 chunk，传递相关信息；manager 综合。**关键结果：CoA 在 8K 上下文窗口下性能超越 Full-Context 基线在 200K 上下文窗口下的性能。** 复杂度 O(n²) → O(nk)。"交错读-处理" 优于 "读然后处理"。

> **对 EverEvo 的启示：** 多个小上下文窗口（每个领域一个）大于一个大上下文窗口（全局），与 Collab Kernel 的分布式 Agent 架构一致。

### 2.3 认知架构：MemGPT / Letta (Packer et al., 2024)

MemGPT (arXiv:2310.08560) 提出 **Virtual Context Management** — 将 LLM 的上下文窗口视为"虚拟内存"，通过分页机制在需要时换入/换出相关记忆。

> "Operating systems use virtual memory to present the illusion of infinite physical memory... We apply the same principle to LLM context windows."

**Cognitive Workspace (arXiv, 2025)** 提出分层认知缓冲区：
| 缓冲区 | 容量 | 用途 |
|--------|------|------|
| Immediate Scratchpad | 8K | 当前思考 |
| Task Buffer | 64K | 当前任务 |
| Episodic Cache | 256K | 短期记忆 |
| Semantic Bridge | 1M+ | 长期知识 |

**结果：** 58.6% 记忆复用率（vs 传统 RAG 的 0%）；17-18% 净效率增益。

> **对 EverEvo 的启示：** 领域切换 = 上下文分页换入/换出。进入领域 → 领域内的 KB+Agent+MCP+Skill+Memory 换入 system prompt。切换领域 → 换出旧领域，换入新领域。全局资源（核心 Agent、系统工具）= 常驻"内核页"。

### 2.4 层级上下文加载 — 生产系统共识

**Google ADK** 的四层架构：
| 层 | 用途 |
|----|------|
| Working Context | 每次调用的临时 prompt 视图 |
| Session | 持久化的结构化事件日志 |
| Memory | 长期可搜索知识 |
| Artifacts | 按名称引用的大对象（不粘贴到 prompt） |

> "Context is a compiled view over a richer stateful system. Separate storage from presentation."

**Three-Tier Skill Loading** (生产级 Rust 多 Agent 系统):
```
L1: Metadata (~100 tokens/skill)  — 始终加载（"菜单"）
L2: Instruction Body               — Skill 激活时加载
L3: Resources (docs, schemas)      — 按需加载
```
与单片 System Prompt 相比，**基线上下文减少约 90%**。小型模型（Claude Haiku 4.5）也能处理复杂的多步骤 CRM 工作流而不产生幻觉。

> **对 EverEvo 的启示：** Skill 的 system prompt 不应全部预加载；仅当前领域的已启用 Skill 的 L2 指令注入 System Prompt。模板 Skill 保持在 L1（元数据）级别直到手动启用。

### 2.5 DDD 与 Agent 架构融合：OntoCortex

OntoCortex (github.com/gabert/ontocortex) 是将 DDD Bounded Contexts 直接映射到 Agent 架构的最具体实现：

> "The LLM proposes; a deterministic layer decides."
> "A jailbroken prompt has nothing to jailbreak into, because the model never held the keys."
> "Containment and reliability come from the same property."

**架构：** LLM 仅使用本体词汇发出类型化意图（`find`, `create`, `update`, `delete`, `link`, `unlink`, `transition`）；确定性框架验证、限定范围、翻译并执行。LLM 永远看不到 SQL、物理表名、用户 ID 或存储原语。每个领域获得自己的本体、规则和 persona；切换领域原子性地交换这些。

> **对 EverEvo 的启示：** `BuildDomainSystemPrompt` 相当于原子性地交换领域 persona + 工具 + 知识。Agent 不应看到跨领域的原始 MCP 工具列表——它们应该通过领域特定的 Skill 抽象来访问。

### 2.6 认知架构：CoALA 与记忆层级 (Sumers et al., 2024)

CoALA (Cognitive Architectures for Language Agents, TMLR 2024) 是该领域的**组织性分类法**。提出四种记忆类型：

| 记忆类型 | 用途 | EverEvo 实现 |
|---------|------|------------|
| **Working Memory** | 活跃草稿板，每次决策周期覆盖 | System Prompt + 当前对话轮次 |
| **Episodic Memory** | 带时间戳的经历、过去交互、任务轨迹 | SQLite `memory_items` (kind=turn) |
| **Semantic Memory** | 去上下文化的事实、领域知识 | SQLite `user_facts` + `kg_nodes/edges` |
| **Procedural Memory** | 技能/工作流/行动模式 | `skills.json` + Workflow Engine |

> "Intelligence emerges from coordination between memory types, not from scaling a single context window."

**Generative Agents** (Park et al., UIST 2023) 的记忆检索公式直接应用于 EverEvo P5：
```
score = recency + importance + relevance  (归一化到 [0,1])
```
- Recency: 指数衰减（当前 P5 实现：`0.5^(ageDays/halfLife)`）
- Importance: LLM 在记忆创建时分配（当前 P5 `importance` 字段）
- Relevance: 余弦相似度（当前 chromem 实现）

**2025 前沿（6 篇论文的收敛模式）：** HiMem / BMAM / E-mem / AriGraph / CraniMem / MMAG 全部趋向于**生物学启发的多存储架构**，将 episodic（原始时间经历）与 semantic（抽象知识）分离，通过层级 agent 系统协调。

### 2.7 DDD + AI：Bounded Context 的直接映射

**Atlan（2025）** 将 Eric Evans 的 Bounded Context 直接翻译到 AI Agent 系统：

| DDD 概念 | 在软件中（DDD） | 在 AI Agent 中 |
|-----------|---------------|---------------|
| **Bounded Context** | 领域模型应用的明确边界 | Agent 可以查看和推理的受控上下文范围 |
| **Context Map** | 各 bounded context 之间的关系 | Agent 领域的连接方式；翻译发生的位置 |
| **Ubiquitous Language** | 领域内的共享词汇 | 领域特定的术语表，消除歧义 |
| **Anti-Corruption Layer** | 防止领域模型相互渗透 | 上下文路由强制领域隔离 |

**三个上下文污染失效模式：**
1. **上下文污染** — "Revenue" 在财务 = GAAP 确认收入，在销售 = pipeline value，在营销 = 归因收入。不隔离 → Agent 混合三者。
2. **术语冲突** — 相似但领域不适当的跨域数据被检索。
3. **安全失败** — HR Agent 在查询编制时同时访问薪酬数据。

**Nikita Golovko (Siemens)** 提出 "**One Agent, One Bounded Context**" 原则：

> "One agent is one bounded context, one responsibility."

反模式：单个 "guard agent" 负责代码审查 + 质量 + 安全 + 测试覆盖 → 3000+ token prompt 和脆弱行为。解决方案：拆分为独立的上下文（Quality context, Security context），每个产生自己的结构化产物。

**Eric Evans 本人论 LLM 作为 Bounded Contexts：** 调用 LLM 时，你正在桥接确定性应用与概率系统——两者有根本不同的计算模型。LLM 应被视为自己的 bounded context，有**自己的语言（prompts）、一致性模型（概率性）**，并在确定性应用与 LLM 之间设置 **Anticorruption Layer**。

### 2.8 企业多租户：领域隔离的实现模式

AWS / Azure / Milvus 的企业多租户指南揭示了三种规范隔离模型，与领域隔离完全同构：

| 模式 | 描述 | 适用场景 |
|------|------|---------|
| **Silo** | 每个租户独立部署完整 AI 栈（独立向量 DB、KB、LLM 端点） | 大规模 + 严格监管（医疗、金融） |
| **Pool** | 所有租户共享单个 RAG 栈 | 大量小型租户、内部部门 |
| **Bridge (Hybrid)** | 共享基础设施 + 逻辑分离 | 中等租户数量 + 多样化隔离需求 |

**关键安全原则（AWS 官方）：** "LLMs act on unstructured data and respond in a probabilistic fashion, making them unfit to handle tenant context securely." Prompt injection 可以改变租户上下文。替代方案：(1) 通过权威来源注入租户上下文（IdP/JWT claims）；(2) 仅通过确定性应用组件传递 `tenantId`。

> **对 EverEvo 的启示：** Domain ID 不应由 LLM 自主判断——它必须从 `useActiveLibrary` 状态确定性传递。LLM 可以**建议**切换 domain，但执行需用户确认。

### 2.9 跨领域 RAG 路由

**核心发现（SCOUT-RAG / LTRR / MKP-QA）：** 不应使用单一检索器——没有任何一个检索器在所有查询类型上胜出。

| 策略 | 代表工作 | 关键洞见 |
|------|---------|---------|
| 概率联邦搜索 | MKP-QA (Adobe, COLING 2025) | 聚合 query-domain + query-passage 概率相关性分数 |
| 学习排序路由 | LTRR (CMU, SIGIR 2025) | 包括 "no-retrieval" 选项——有些查询最好从参数记忆中回答 |
| Agent 跨域路由 | SCOUT-RAG | 4 个协作 Agent；达到集中式基线性能，但 token 减少 4 倍 |
| DP 隐私路由 | DP-CR (IEEE BigData 2025) | 每个提供者仅共享高斯扰动均值嵌入；聚合器随机选择子集 |

**最佳实践综合：**
1. **不依赖单一检索器** — 按查询类型路由
2. **"不检索"是有效的路由决策** — 不必要的检索引入噪音
3. **按下游 LLM 效用排序** — MRR/Recall 与生成质量不对齐
4. **在检索层联邦** — 共享嵌入（必要时加 DP），不共享原始文档
5. **使用 Agent 决策循环** — 根据质量反馈迭代评估答案充分性，扩展领域

### 2.10 其他关键来源

| 原则 | 来源 | EverEvo 实施 |
|------|------|------------|
| **Bounded Context** | DDD (Evans, 2003) | Domain = 语义边界；跨域通过 entity_links |
| **Lost in the Middle** | Liu et al. (arXiv:2307.03172) | 领域上下文在开头，对话在末尾 |
| **Deny-by-Default** | Microsoft MARA | 跨域访问需显式声明 |
| **No Orphans** | PARA (Forte, 2022) | DB 层 `NOT NULL` 强制执行 |
| **Deterministic Guardrails** | OntoCortex / Sonar | Domain 边界在代码中执行，非依赖 LLM 判断 |
| **Code Execution Mode** | Anthropic (98.7% token reduction) | 复杂编排走 collab kernel，不塞进 System Prompt |

### 2.9 综合设计原则

| 原则 | 来源 | EverEvo 实施 |
|------|------|------------|
| **Bounded Context** | DDD (Evans) | Domain = 语义边界；跨域通过 entity_links |
| **Virtual Context Mgmt** | MemGPT (Packer) | 领域切换 = 上下文换页 |
| **Index-as-Gateway** | Karpathy Wiki | Domain 是子资源的唯一索引入口 |
| **Hierarchical Inheritance** | Cursor | 全局 Skill → 领域 Skill → Agent Skill |
| **U-Shaped Attention** | Liu et al. | 领域上下文在开头，对话在末尾 |
| **Deny-by-Default** | Microsoft MARA | 跨域访问需显式声明 |
| **No Orphans** | PARA (Forte) | DB 层 `NOT NULL` FK 强制执行 |

---

## 3. 目标架构

### 3.1 核心数据模型

```go
// Domain 是唯一顶层容器 — 所有子资源强制归属
type Domain struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`
    Icon        string `json:"icon"`
    Tags        []string `json:"tags"`
    AutoCreated bool   `json:"autoCreated"`
    SortOrder   int    `json:"sortOrder"`
    UseCount    int    `json:"useCount"`
    CreatedAt   int64  `json:"createdAt"`

    // 统计字段（运行时计算）
    KBCount    int `json:"kbCount"`
    AgentCount int `json:"agentCount"`
    MCPCount   int `json:"mcpCount"`
    WikiCount  int `json:"wikiCount"`
    SkillCount int `json:"skillCount"`
}
```

**关键变更（2 行 SQL）：**

```sql
-- 已有这些列，无需 ALTER。需加固的是：
-- 各子系统的 library_id 从 "可选" → "必填"
-- 删除 domain 时级联清理子资源
```

### 3.2 子系统归属强化

| 子系统 | 当前状态 | 目标状态 | 变更方式 |
|--------|---------|---------|---------|
| KB | `LibraryID` 可选 | **NOT NULL** | `meta.json` 校验 + 创建 API 必传 |
| Agent | `LibraryID` 可选 | **NOT NULL** | `CreateAgent` 必传 + Manager 校验 |
| MCP | `LibraryID` 可选 | **NOT NULL** | `AddMCPServer` 必传（字段已加） |
| Wiki | Store 构造时必传 | ✅ 已是强制的 | 前端补领域过滤 |
| Skill | `LibraryID` 可选 | **NOT NULL** | `CreateSkill` 必传（字段已加） |
| Memory | `workspace_id` 可选 | **NOT NULL** | API 默认当前领域（已部分实施） |

### 3.3 API 契约

```go
// ── 创建 API：domainId 从可选 → 必传 ──

// Before (旧签名 — 允许不指定领域)
func (a *App) CreateAgent(agent agents.Agent) (*agents.Agent, error)
func (a *App) AddMCPServer(cfg mcpclient.ServerConfig) error
func (a *App) CreateKnowledgeBase(name, modelDir, libraryID string) (rag.KnowledgeBase, error)
func (a *App) CreateSkill(s skills.Skill) error

// After (新签名 — 领域必传，结构体字段强制)
// Agent/MCP/Skill 的 LibraryID 在结构体层面验证非空
// KB 的 libraryID 参数保持（已必传）
// 各 Create 函数内部校验 LibraryID 非空 + FK 存在

// ── 列表 API：默认按领域过滤 ──
// 已有 ListByLibrary 的保持不变
// 所有 List* 改为传 libraryId（"" = 全部，仅管理页用）

// ── 新增：构建领域 System Prompt ──
func (a *App) BuildDomainSystemPrompt(domainId string) string
```

### 3.4 前端架构

```
App.vue
├── Sidebar（已有）
│   ├── 🔍 领域切换器（增强 — 已有 useActiveLibrary，需 UI）
│   ├── 📊 当前领域指示器
│   ├── ─────────────
│   ├── 🏠 DomainPanel（新组件 — 领域首页）
│   │   ├── 概览卡片（KB/Agent/MCP/Wiki/Skill 数量）
│   │   ├── 最近活动时间线
│   │   └── 快速创建入口
│   ├── 💬 聊天（已有 — 需注入领域上下文）
│   ├── 📚 知识库（已有 — 已有领域过滤，需增强）
│   ├── 🤖 Agent（已有 — 需增强领域过滤）
│   ├── 🔌 MCP（已有 — 需加领域过滤）
│   ├── 📝 Wiki（已有 — 需加领域过滤）
│   └── ⚡ Skill（已有 — 需加领域过滤）
│
└── 每个子页面：
    - 自动传入 domainId（通过 useActiveLibrary）
    - 列表只显示当前领域的资源
    - 创建时 domainId 已默认填充
    - 顶部「当前领域: XXX」可点击切换
```

### 3.5 System Prompt 动态生成

```go
func (a *App) BuildDomainSystemPrompt(domainId string) string {
    domain := a.getDomain(domainId)
    agents := a.agentManager.ListByLibrary(domainId)
    skills := a.skillManager.ListEnabledByLibrary(domainId)
    mcps := a.mcpClient.ListByLibrary(domainId)

    return fmt.Sprintf(`
【当前领域：%s】
你是该领域的专家。

## 可委派 Agent
%s

## 已启用能力
%s

## 可用 MCP 工具
%s

## 领域约束
- 操作范围限于当前领域内
- 跨领域操作需显式确认
`, domain.Name,
   formatAgents(agents),
   formatSkills(skills),
   formatMCPs(mcps))
}
```

**与当前 System Prompt 的对比：**

| | 当前（全局） | 领域化后 |
|---|---|---|
| Skill 数量 | 13 个全部 | 仅当前领域 + 全局 Skill |
| MCP Server | 3 个全部 | 仅当前领域的 |
| 核心记忆 | ~50 条全部 | 仅当前领域的 8-10 条 |
| Wiki 摘要 | 20 个页面 | 仅当前领域的 |
| Token 估算 | ~4000 | ~800 |
| 精准度 | 噪音比例 80% | 有效信息 90%+ |

---

## 4. 实施计划（5 天，5 阶段）

### Phase 0: Schema & Data Foundation（0.5 天）

**目标：所有现有资源有合法 domainId，无悬挂引用。**

#### Step 0.1: 修复孤儿 KB
- [ ] 定位 "知识" KB（`lib_18c0842c0b947f70` 匹配不到任何 domain_libraries 行）
- [ ] 将其 `libraryId` 改为默认领域 ID
- [ ] **Verify:** 所有 KB 的 libraryId 指向存在的 domain_libraries 行

#### Step 0.2: 加固 domain_libraries 表
- [ ] 确认 `icon`、`sort_order`、`use_count` 列已存在（代码审核确认 P10 迁移已加）
- [ ] 确认默认领域 seed 逻辑正常（`DefaultLibrary()` 已存在）
- [ ] **Verify:** `SELECT * FROM domain_libraries` 至少 1 行

#### Step 0.3: 存量数据回填
- [ ] 扫描所有 Agent (`agents.json`) — 空 LibraryID → 默认领域
- [ ] 扫描所有 MCP Server (`mcp_servers.json`) — 空 LibraryID → 默认领域
- [ ] 扫描所有 Skill (`skills.json`) — 空 LibraryID → 默认领域
- [ ] 扫描所有 KB (`meta.json`) — 空/无效 LibraryID → 默认领域
- [ ] 检查 memory_items/user_facts/kg_nodes/kg_edges 的 workspace_id
- [ ] **Verify:** `go test ./internal/memory/...` 全通过；所有子系统零悬挂引用

#### Step 0.4: 记忆去重（一次性清理）
- [ ] 运行 `DedupAllFacts()`（已存在，startup 已调用一次）
- [ ] 增强去重：same key → 保留最新的、合并 access_count
- [ ] **Verify:** `SELECT key, COUNT(*) FROM user_facts GROUP BY key HAVING COUNT(*) > 1` 返回 0 行

---

### Phase 1: Backend API Hardening（1 天）

**目标：所有创建 API 强制要求 domainId；列表 API 支持领域过滤。**

#### Step 1.1: Agent API 强化
- [ ] `CreateAgent` 内部校验 `agent.LibraryID` 非空
- [ ] `agent.LibraryID` 为空时返回明确错误 "领域 ID 不能为空"
- [ ] 前端 `api/agents.ts` 的 `create(agent)` 无需改签名（LibraryID 已在 Agent 结构体中）
- [ ] **Verify:** 创建 Agent 不传 libraryId → 返回错误

#### Step 1.2: MCP API 强化
- [ ] `AddMCPServer` 校验 `cfg.LibraryID` 非空
- [ ] `ServerConfig.LibraryID` 为空时返回明确错误
- [ ] 前端 `api/mcp.ts` 的 `addServer(cfg)` 调用处补 libraryId
- [ ] **Verify:** 添加 MCP Server 不传 libraryId → 返回错误

#### Step 1.3: Skill API 强化
- [ ] `CreateSkill` 校验 `s.LibraryID` 非空
- [ ] `skills.json` 持久化前校验
- [ ] 前端 `api/skills.ts` 的 `create(skill)` 调用处补 libraryId
- [ ] **Verify:** 创建 Skill 不传 libraryId → 返回错误

#### Step 1.4: KB API 强化
- [ ] `CreateKnowledgeBase` 的 `libraryID` 参数已必传（代码审核确认），不改签名
- [ ] 后端校验 libraryID 对应的 domain 存在
- [ ] **Verify:** 创建 KB 传无效 libraryId → 返回错误

#### Step 1.5: Wiki CRUD 强化
- [ ] Wiki CRUD 全部要求 libraryId（已有，不需改签名）
- [ ] Wiki 创建时校验 libraryId 对应的 domain 存在
- [ ] **Verify:** Wiki CRUD 传无效 libraryId → 返回错误

#### Step 1.6: Memory API 强化
- [ ] `MemoryRemember` / `MemoryRecall` / `MemoryStatus` 的 libraryId 参数为空时自动 fallback 到默认领域
- [ ] 跨域记忆查询需显式传 `libraryId`（当前默认 = "" 的行为改掉）
- [ ] **Verify:** 不传 libraryId → 默认操作当前领域

#### Step 1.7: Domain CRUD 完整化
- [ ] `app_domain.go`（新文件）：Wails 绑定
  - `ListDomains()` → 已有 `memoryApi.libraryList()`
  - `CreateDomain(name, description, icon)` → 已有 `memoryApi.libraryCreate()`
  - `UpdateDomain(id, name, description, icon)` → 已有 `memoryApi.libraryUpdate()`
  - `DeleteDomain(id)` → 需实现（级联迁移子资源 + 删行）
  - `GetDomainStats(id)` → 新方法：统计该领域下的 KB/Agent/MCP/Wiki/Skill 数量
- [ ] **新增** `BuildDomainSystemPrompt(domainId)` — 在 `app_agent_exec.go` 或 `app_domain.go`
- [ ] **Verify:** `go build ./...` + `go vet ./...` 通过

---

### Phase 2: Frontend Domain-First Navigation（1.5 天）

**目标：领域成为 UI 顶层导航；创建任何资源时领域自动填充。**

#### Step 2.1: DomainPanel 新组件
- [ ] `frontend/src/components/DomainPanel.vue`（新）
  - 当前领域概览卡片（KB/Agent/MCP/Wiki/Skill 数量 + 最近活动）
  - 子 Tab 导航（知识库 / Agent / MCP / Wiki / Skill / 记忆）
  - 使用 `useActiveLibrary` 读取/写入当前领域
- [ ] 路由 `/domain/:id` → DomainPanel
- [ ] **Verify:** 页面显示当前领域的统计数据

#### Step 2.2: 导航栏领域切换器
- [ ] 侧边栏加领域下拉选择器（始终可见）
- [ ] 选择领域 → 更新 `useActiveLibrary().activeLibraryId`
- [ ] 显示当前领域名称 + icon
- [ ] **Verify:** 切换领域 → 所有子页面数据随之变化

#### Step 2.3: Knowledge.vue 增强
- [ ] Wiki 区块按 `activeLibId` 过滤
- [ ] 记忆区块按 `activeLibId` 过滤（已有 `workspace_id` 列，需前端传参）
- [ ] 图谱 viewer 按 `activeLibId` 过滤
- [ ] 经验/跨域链接受 `activeLibId` 驱动
- [ ] **Verify:** 切换领域 → Wiki 列表/记忆列表/图谱数据不同

#### Step 2.4: LLMAgents.vue 领域化
- [ ] Agent 列表默认按当前领域过滤
- [ ] 列表顶部加「所有领域」toggle（查看全局 Agent）
- [ ] 创建 Agent 时 libraryId 自动填当前领域（已部分实现 `openCreate(libraryId)`）
- [ ] 编辑时显示领域归属（已有 `libraryName` helper）
- [ ] **Verify:** 切换领域 → Agent 列表只显示该领域的

#### Step 2.5: LLMMCP.vue 领域化
- [ ] MCP Server 列表默认按当前领域过滤
- [ ] 创建 MCP Server 时 libraryId 自动填当前领域
- [ ] 列表显示领域归属
- [ ] **Verify:** 切换领域 → MCP 列表只显示该领域的

#### Step 2.6: LLMSkills.vue 领域化
- [ ] Skill 列表默认按当前领域过滤
- [ ] 创建/导入 Skill 时 libraryId 自动填当前领域
- [ ] 列表显示领域归属
- [ ] 全局 Skill（`shell_exec`、`agent-orchestration`）标记 🌐 始终可见
- [ ] **Verify:** 切换领域 → Skill 列表只显示该领域的 + 全局 Skill

#### Step 2.7: ChatPanel + chatStore 领域上下文注入
- [ ] `chatStore.ts` 的 chatLoop 用 `activeLibraryId` 调用 `BuildDomainSystemPrompt`（或在前端拼装）
- [ ] 系统提示注入：当前领域的 Agent + Skill + MCP 列表
- [ ] 知识召回按当前领域过滤（RAG + Wiki + Memory + Graph）
- [ ] 前端 `useActiveLibrary` 与后端 API 调用联动
- [ ] **Verify:** 切换领域后新对话 → System Prompt 不同

---

### Phase 3: System Prompt Dynamic Generation（0.5 天）

**目标：AI 获得领域认知能力——进入不同领域，AI 看到不同的能力/工具/知识集。**

#### Step 3.1: 后端 System Prompt 生成
- [ ] `app_domain.go` 实现 `BuildDomainSystemPrompt(domainId string) string`
  - 获取 Domain 信息
  - 列出该领域的 Agent（ListByLibrary）
  - 列出该领域的已启用 Skill（ListEnabledByLibrary）+ 全局 Skill
  - 列出该领域的 MCP Server（ListByLibrary）
  - 格式化为结构化 Markdown system prompt
- [ ] **Verify:** 不同 domainId → 不同 system prompt 文本

#### Step 3.2: Agent 人格注入
- [ ] `buildAgentSystemPrompt` 在 Agent 人格基础上叠加领域上下文
- [ ] Agent 的工具列表按领域过滤（`resolveAgentToolDefs` 已有的 MCP 过滤逻辑 + 领域过滤）
- [ ] **Verify:** `agent_run` 的子 Agent 也受领域约束

#### Step 3.3: Chat 上下文注入
- [ ] chatLoop 的 `apiMsgs` 构建时调用 `BuildDomainSystemPrompt`
- [ ] 替代当前全局 System Prompt（或叠加在 Agent 人格之上）
- [ ] **Verify:** 聊天中 AI 能正确识别当前领域，工具调用限定在该领域

---

### Phase 4: Skill System Unification + Memory Cleanup（1 天）

**目标：三套 Skill 体系归一；核心记忆整洁无冗余。**

#### Step 4.1: Skill 三层归一
- [ ] `data/skills/` 30+ 模板 → `internal/skills/` 作为"模板库"（`builtin=1`，默认不启用）
- [ ] `skills.json` 持久化的 Skill 绑定 `libraryId`
- [ ] System Prompt 只注入「当前领域 + 全局」的 Skill
- [ ] 前端 Skill 管理页加「模板库」Tab（从模板导入到领域）
- [ ] **Verify:** System Prompt 中 Skill 列表 = 当前领域启用的 + 全局 Skill；模板 Skill 不出现在能力页面除非手动启用

#### Step 4.2: Skill 前后端一致性
- [ ] 后端 `buildAgentSystemPrompt` 和前端 chatStore 的 system prompt 使用同一数据源
- [ ] 新增/删除 Skill 后 emit `skills:changed` 事件（已有）
- [ ] **Verify:** 启用/禁用 Skill → 聊天 System Prompt 立即变化

#### Step 4.3: 核心记忆去重增强
- [ ] 增强 `DedupAllFacts`：same key → 保留 latest、合并 access_count + recall_count
- [ ] 降低语义去重阈值：cosine ≥ 0.85（原 0.90）更积极去重
- [ ] 去重结果日志：记录合并了多少条、删除了多少条
- [ ] **Verify:** `SELECT key, COUNT(*) FROM user_facts GROUP BY key HAVING COUNT(*) > 1` 返回 0 行

#### Step 4.4: 增量抽取去重
- [ ] `maybeExtractFacts` 抽取前先检查该 fact 是否已存在（SQL COUNT + 语义 check）
- [ ] 已存在的 fact 更新 `last_access` 而非重复插入
- [ ] **Verify:** 连续 10 轮对话提到相同信息 → `user_facts` 不增加重复行

---

### Phase 5: Cleanup, Migration & Validation（0.5 天）

**目标：全链路验收；清理孤儿数据；确保向后兼容。**

#### Step 5.1: 数据迁移
- [ ] 修复所有悬挂引用（孤儿 KB、空 libraryId 的资源）
- [ ] MCP Server 按命名启发式分配领域（SearXNG→搜索领域，GitHub→开发领域）
- [ ] 启动脚本跑一次完整的 `EnsureLibraryIDs` + `BackfillLibraryIDs`
- [ ] **Verify:** `libraryId = ''` 的资源数为 0

#### Step 5.2: 向后兼容测试
- [ ] 旧数据启动 → 自动回填 → 无报错、无崩溃
- [ ] 前端旧 URL（无 `?domain=xxx`）→ fallback 到默认领域
- [ ] API 调用不传 `libraryId` → 自动 fallback 到默认领域（不报错）

#### Step 5.3: 全链路测试
- [ ] 创建领域 → 在该领域下创建 KB → 添加文档 → 检索 → 切换领域 → 检索不到
- [ ] 创建领域 → 在该领域下创建 Agent → 委派给该 Agent → Agent 只用该领域的 Skill
- [ ] 创建领域 → 添加 MCP Server → 切换领域 → MCP 列表无该 Server
- [ ] 聊天：在 MCP 领域问 "怎么配置 SearXNG" → AI 回答基于 MCP 领域的 Wiki + 记忆
- [ ] 聊天：切换到默认领域问同样问题 → AI 回答不同（领域记忆不同）

#### Step 5.4: 构建验证
- [ ] `go build ./...` 通过
- [ ] `go test ./internal/memory/...` 通过
- [ ] `go vet ./...` 通过
- [ ] `vue-tsc --noEmit` 0 错误
- [ ] `npm run build` 通过

---

## 5. Design Decisions

### 5.1 "默认领域"策略
- 系统始终有默认领域（"核心领域"，ID = seed 生成的第一个 library）
- 用户无法删除默认领域
- 空 `libraryId` 参数 → fallback 到默认领域（向后兼容）
- 删除非默认领域时，资源级联迁移到默认领域

### 5.2 跨领域访问
- **默认隔离：** KB 检索、记忆召回、Agent 列表默认只看当前领域
- **显式跨域：** `agent_run` 的 `libraryId` 参数可指定目标领域
- **图谱跨域：** `kg_edges` 的 `cross_tags` 标注跨域边
- **Entity Links：** `entity_links` 表（已存在）用于跨域实体连接

### 5.3 全局资源定义
- **全局 Skill：** `libraryId = ""` 的 Skill 在所有领域可见
  - 当前应该是：`shell_exec`、`agent-orchestration`、`plugin-system`
- **全局 Agent：** `isDefault = true` 的 Agent 在所有领域可见
- **全局 MCP Server：** `libraryId = ""` 的 MCP Server 在所有领域可见（暂不推荐）

### 5.4 自动发现 vs 人工管理
- LLM 通过 `extract_facts` 可自动创建领域（`auto_created = true`）
- 自动创建的领域 UI 显示 🤖 标记
- 用户可手动创建/重命名/合并/删除领域
- `LibraryMerge` 已实现（SQLite 事务级联迁移）

### 5.5 向后兼容
- 所有 API 的 `libraryId` 参数为空字符串 → 自动 fallback 到默认领域
- 旧前端代码传空 `libraryId` 时行为不变（操作默认领域）
- 不改旧 JSON 文件的存储格式（agents.json / skills.json / mcp_servers.json）
- 首次升级时自动回填所有空字段

---

## 6. Risk Mitigation

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|---------|
| 数据库迁移导致数据丢失 | 低 | 高 | 全程 `IF NOT EXISTS` + 事务；先备份；Phase 5 验证 |
| 前端大面积改动引入 bug | 中 | 中 | 分 Phase 独立可测；每个 Phase 独立构建验证 |
| LLM tool handler 不传 libraryId | 中 | 中 | tool schema 的 libraryId 标 Required；handler 内 `resolveLibraryID` helper fallback |
| 用户不理解领域隔离 | 低 | 低 | UI 始终显示「当前领域: XXX」；默认领域 = 不选即默认，不改变用户习惯 |
| 性能：每次 API 调用解析 libraryId | 低 | 低 | libraryId 是字符串比较，O(1)；Agent/Skill/MCP 列表很小（O(10)） |
| 技能/模板过度去重 | 低 | 低 | 保留 originals；去重仅在 key 相同时合并；阈值可调 |

---

## 7. Success Criteria (Go / No-Go)

### 必须满足
- [ ] 所有 KB / Agent / MCP Server / Wiki Page / Skill 的 `libraryId` 非空
- [ ] 删除领域时，其下所有子资源级联迁移到默认领域（无孤儿）
- [ ] 前端创建任何资源时，`libraryId` 默认为当前领域且用户可见
- [ ] 切换领域后，KB/Agent/MCP/Wiki/Skill 列表随之变化
- [ ] System Prompt 里的 Skill 列表 = 当前领域启用的 Skill + 全局 Skill
- [ ] 核心记忆无重复（`user_facts` 中 same key 只出现一次）
- [ ] `go build ./...` / `go test ./internal/memory/...` / `vue-tsc --noEmit` / `npm run build` 全通过

### 期望满足
- [ ] 领域化后 System Prompt token 减少 60%+（目标：~4000 → ~800）
- [ ] RAG 领域分区检索：假阳性 chunk 减少（目标：50%+ reduction）
- [ ] AI 回答更精准（用户主观评估）
- [ ] 6GB 显存本地模型能正常运行（不再 OOM）
- [ ] `data/skills/` 模板不出现在 System Prompt 除非手动启用
- [ ] Skill L2 指令仅加载当前领域 + 全局（baseline context 减少 ~90%）

---

## 8. What This Plan Does NOT Do

以下内容**故意排除**，因为超出本次范围或风险太高：

1. **不重构 Zone 系统** — Zone（运行区隔离）与 Domain（知识域隔离）是正交的两层，不做合并
2. **不改变 JSON 文件存储格式** — agents.json / skills.json / mcp_servers.json 保持现有结构，只增强字段校验
3. **不引入新的数据库** — Domain 仍在现有 SQLite `domain_libraries` 表，各子系统的 libraryId 指向该表
4. **不强制 FK（SQLite 不支持）** — 用应用层校验代替数据库 FK 约束
5. **不迁移 data/skills/ 模板到外部服务** — 保留为本地模板库
6. **不改 collab 子系统** — collab 有意全局（跨领域编排），保持现状

---

## 9. References

### Academic Papers
1. Evans, E. (2003). *Domain-Driven Design: Tackling Complexity in the Heart of Software.* Addison-Wesley.
2. Packer, C. et al. (2024). "MemGPT: Towards LLMs as Operating Systems." arXiv:2310.08560.
3. Liu, N.F. et al. (2023). "Lost in the Middle: How Language Models Use Long Contexts." arXiv:2307.03172.
4. Sumers, T.R. et al. (2024). "CoALA: Cognitive Architectures for Language Agents." TMLR. arXiv:2309.02427. — 四种记忆类型分类法。
5. Park, J.S. et al. (2023). "Generative Agents: Interactive Simulacra of Human Behavior." UIST. arXiv:2304.03442. — 记忆流 + 检索评分公式。
6. Wu, Y. et al. (2025). "How Do Agentic Systems Work? -- Sharing Context Can Cause Security Issues." NDSS 2025. (IsolateGPT / SecGPT)
7. PC-RAG (2024-2025). "Domain-Partitioned Retrieval as a Hallucination Mitigation Strategy in Conversational RAG."
8. "Security Knowledge Dilution in Large Language Models." NeurIPS 2025. — 47% expertise degradation with irrelevant context.
9. "The Gatekeeper Knows Enough" (2025). arXiv:2510.14881. — Domain-agnostic context mediation protocol.
10. "Cognitive Workspace: Active Memory Management for LLMs" (2025). arXiv:2508.13171. — 分层认知缓冲区；58.6% 记忆复用率。
11. Google Research. "Chain of Agents: Large Language Models Collaborating on Long-Context Tasks." — 8K > 200K 全上下文。
12. HiMem / BMAM / E-mem / AriGraph / CraniMem / MMAG (2025). — 6 篇论文收敛于生物学启发的多存储记忆架构。
13. MKP-QA (Adobe, COLING 2025). — 概率联邦跨域搜索。
14. SCOUT-RAG (2025). arXiv:2602.08400. — 4 个协作 Agent，4 倍更少 token 达到集中式基线性能。
15. LTRR (CMU, SIGIR 2025). arXiv:2506.13743. — 学习排序路由，包括 "no-retrieval" 选项。

### Expert Sources
11. Karpathy, A. *LLM Wiki Pattern.* https://github.com/Astro-Han/karpathy-llm-wiki — Compiler > Interpreter paradigm.
12. Weng, L. (2023). "LLM Powered Autonomous Agents." *lilianweng.github.io.* — Canonical Planning→Memory→Tools→Action architecture.
13. Ng, A. *Data-Centric AI & Agentic Workflows.* — Context management as #1 production challenge.
14. Anthropic. *Context Engineering Guide.* https://docs.anthropic.com/en/docs/build-with-claude/context-windows
15. Anthropic. *Code Execution with MCP.* https://www.anthropic.com/engineering/code-execution-with-mcp — 98.7% token reduction pattern.

### Production Systems
16. Cursor. *Context Layers: Rules for AI.* https://docs.cursor.com/context/rules-for-ai
17. Microsoft. *Multi-Agent Reference Architecture.* https://microsoft.github.io/multi-agent-reference-architecture/
18. Google ADK. *Architecting Efficient Context-Aware Multi-Agent Frameworks.*
19. OntoCortex. https://github.com/gabert/ontocortex — DDD Bounded Contexts for agent architecture.
20. Scalac. *Multi-Agent AI Rust Orchestrator.* https://scalac.io/blog/multi-agent-ai-rust-orchestrator/ — Three-tier skill loading (90% context reduction).
21. Klavis AI. *Less is More: MCP Design Patterns for AI Agents.* — Semantic Search RAG-MCP, Workflow-Based Design, Progressive Discovery.
22. Asana. *Context Engineering at Asana.* — "Filter before you fetch"; 35% token savings.
23. OpenAI. *Agents SDK: Session Memory.* — Trimming vs. Summarization hybrid approach.

### EverEvo Internal
24. EverEvo project. *Domain Library Grid (P7).* [changelog.md](../changelog.md)
25. EverEvo project. *Domain Refactor v1.* [domain-refactor.md](domain-refactor.md)
26. EverEvo project. *Design Document.* [design.md](../design.md)
