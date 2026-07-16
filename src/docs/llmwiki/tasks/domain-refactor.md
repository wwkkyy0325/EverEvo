# Domain-as-Container Refactoring — 领域即容器

> **Status:** Proposal | **Date:** 2026-07-11 | **Author:** System diagnosis + web research

## 0. Current State — 代码审核确认

通过深入探索代码库（71 次搜索），确认以下现状：

### 0.1 两层隔离体系（正交）

EverEvo 有 **两个独立的隔离层**，容易混淆：

| 层 | 实现 | 存储 | 用途 |
|----|------|------|------|
| **Zone（运行区）** | `internal/zone/` + `app_zone.go` | 文件系统 `%APPDATA%/EverEvo/zones/<name>/` | 运行时实例隔离：production / experiment / backup |
| **Domain Library（领域库）** | `internal/memory/store.go` `domain_libraries` 表 | SQLite | 知识数据隔离：按主题组织 KB/Agent/Memory/Wiki |

Zone 是"复制整个数据目录"级别的隔离（共享模型/缓存/插件），Domain Library 是"同一数据库内按 `workspace_id` 列隔离"。两者正交。

### 0.2 各子系统的 domain/library 归属现状

| 子系统 | 存储位置 | 有 LibraryID? | 强制? | 实际状态 |
|--------|---------|--------------|-------|---------|
| KB | `internal/rag/` meta.json | ✅ `KnowledgeBase.LibraryID` | ❌ 可选 | 创建时可指定；"知识"KB 的 `lib_18c0842c0b947f70` 是悬挂引用 |
| Agent | `internal/agents/agent.go` | ✅ `Agent.LibraryID` | ❌ 可选 | 有字段；startup 回填；`ListByLibrary()` 可用 |
| MCP Server | `internal/mcp/client/` 内存 | ❌ **无** | — | `ServerConfig` 结构体无此字段；全局共享 |
| Wiki | `internal/wiki/` per-library store | ✅ `Store.libraryID` | ✅ 构造时必传 | `wikiStores map[string]*wiki.Store` keyed by libraryID |
| Skill | `internal/skills/skill.go` | ❌ **无** | — | `Skill` 结构体无此字段；`skills.json` 全局持久化 |
| Memory | `internal/memory/store.go` | ✅ `workspace_id` 列 | ❌ 可选 | `memory_items`/`kg_nodes`/`kg_edges`/`user_facts` 都有列 |
| Collab | `internal/collab/` | ❌ **无** | — | 有意全局（跨域编排），不需要域归属 |

### 0.3 已存在但未强制执行的机制

- **Memory 去重**: `AddFactMemory` 有精确（SQL COUNT 同 content）+ 语义（cosine ≥ 0.90）去重（`store.go:659-696`）。但用户仍看到 50+ 条重复——可能是去重阈值太高或只对新写入生效
- **Agent LibraryID 回填**: `EnsureLibraryIDs()` 在 startup 调用（`app.go:355`），但仅对空字段生效
- **运行时代入**: `runAgentLoop` 自动将 Agent 的 `libraryId` 注入 memory-scoped tool 调用（`app_agent_exec.go:144-153`）
- **Wiki 按库隔离**: Wiki store 构造时就绑定了 libraryID（`wiki.go:125`），但前端创建 Wiki 页面时未必传 libraryId

### 0.4 确认的六类问题（对齐用户诊断）

1. **孤儿 KB**: "知识" KB 的 `libraryId` = `lib_18c0842c0b947f70`，在所有 `domain_libraries` 行中不存在 → 悬挂引用
2. **Skill 三套体系割裂**: `skills.json`（8 个）/ System Prompt（13 个能力角色）/ `data/skills/`（30+ 开源模板）互不同步；Skill 结构体无 LibraryID 字段
3. **MCP Server 无领域归属**: `ServerConfig` 结构体完全是全局的；MCP 领域里看不到任何 MCP Server
4. **Agent 与领域脱钩**: 虽有 `LibraryID` 字段，但前端 Agent 列表不按领域分组；创建 Agent 时领域是可选项
5. **Wiki 归属混乱**: Wiki 底层按 libraryID 隔离，但前端 Wiki 列表不按领域过滤
6. **核心记忆冗余**: 去重逻辑存在但不足以阻止累积性重复（每次对话 LLM 重新抽取相同事实）

## 1. Problem Statement — 六类问题的根因

当前 EverEvo 的 `domain_libraries` 表只是记忆系统内部的分组标签，**不是架构级容器**。每个子系统可以绕过领域独立创建资源，导致：

- **MCP/Skill 完全不在领域体系内**（结构体无 LibraryID 字段）
- **KB/Agent/Wiki 有字段但不强制**（可选参数，允许空值）
- **前端不按领域组织**（各列表页是全局视图）

**根因: 没有"领域即容器"的强制约束。** 领域被设计成可选标签而非必需父容器。

## 2. Best Practices — 业界共识

### 2.1 Karpathy LLM Wiki: 三层架构

```
raw/  (不可变原始资料) → wiki/ (LLM 编译维护的知识) → index.md (全局目录)
```

核心思想: **每一层是下一层的容器，索引层指向所有资源。** `wiki/index.md` 是必过之门——没有不在索引里的 wiki 页面。

### 2.2 Cursor Context Layers: 层级继承

```
OS → Workspace → Project → Rules
```

资源约束从上层往下继承，下层可覆盖但不可绕过上层。根目录的 `.cursor/rules/` 自动应用到所有子目录。

### 2.3 DDD + Multi-Tenant: 领域隔离

来自 enterprise-multi-agent-orchestrator 和 Microsoft Multi-Agent Reference Architecture:

```
Orchestrator
├── Domain Agent A (隔离容器: 自己的 RAG / Tools / Memory / Policy)
└── Domain Agent B
```

关键原则: **Domain 是 bounded context——跨域共享需显式声明，默认隔离。**

### 2.4 PARA + Obsidian: 强制层级

Projects → Areas → Resources → Archives，最多 4 层。每层有 `_Index.md` 做目录。**核心约束: 每个 note 必须属于且仅属于一个顶层分类。**

### 2.5 综合最佳实践

| 原则 | 来源 | 实施方式 |
|------|------|---------|
| **容器强制** | DDD / Cursor | 子资源创建时必须指定父容器，不允许孤儿 |
| **索引必过** | Karpathy Wiki | 每个容器维护 `_index`，资源必须出现在索引中 |
| **层级继承** | Cursor | 子资源继承父容器的默认策略/权限/模型 |
| **默认隔离** | Multi-Agent | 跨域访问需显式声明 |
| **Schema 先行** | DDD | 数据库层 enforce `NOT NULL` + FK |
| **自动发现 + 人工确认** | EverEvo 现有 | LLM 可建议新领域，但创建需确认 |

## 3. Target Architecture — 目标架构

### 3.1 核心模型

```
┌──────────────────────────────────────────────────┐
│                  DOMAIN (领域)                      │
│  id, name, description, icon, tags, auto_created   │
│  ┌─────────┐ ┌─────────┐ ┌──────┐ ┌──────┐       │
│  │Agents   │ │KB 知识库│ │MCP   │ │Wiki  │       │
│  │  ├─ A1  │ │  ├─ K1  │ │ ├─M1 │ │ ├─W1 │       │
│  │  └─ A2  │ │  └─ K2  │ │ └─M2 │ │ └─W2 │       │
│  └─────────┘ └─────────┘ └──────┘ └──────┘       │
│  ┌─────────┐ ┌──────────┐                         │
│  │Skills   │ │Memory    │                         │
│  │  ├─ S1  │ │  ├ facts │                         │
│  │  └─ S2  │ │  └ graph │                         │
│  └─────────┘ └──────────┘                         │
└──────────────────────────────────────────────────┘
```

**Invariant: 每个 KB / Agent / MCP Server / Wiki Page / Skill 必须属于恰好一个 Domain。**

### 3.2 数据库 Schema 变更

```sql
-- domain_libraries 升格为主表（已有，需加固）
ALTER TABLE domain_libraries ADD COLUMN icon TEXT NOT NULL DEFAULT '📚';
ALTER TABLE domain_libraries ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

-- 各子系统加 FK（已有字段的不变，没加的补）
-- agents: 已有 library_id，加固 NOT NULL + default
-- knowledge_bases (rag meta): 已有 library_id
-- mcp_servers: 需新增
ALTER TABLE mcp_servers ADD COLUMN library_id TEXT NOT NULL DEFAULT '';
-- wiki_pages: 需新增
ALTER TABLE wiki_pages ADD COLUMN library_id TEXT NOT NULL DEFAULT '';
-- skills: 需新增（新建 skills 表）
CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    library_id TEXT NOT NULL,
    name TEXT NOT NULL,
    ...
);
```

### 3.3 API 变更

**创建资源时必须传 `libraryId`：**

```
// Before (允许不指定领域)
CreateAgent(name, description, ...) → Agent

// After (领域必传)
CreateAgent(libraryId, name, description, ...) → Agent

// Before
AddMCPServer(name, transport, ...) → MCPServer

// After
AddMCPServer(libraryId, name, transport, ...) → MCPServer
```

**列表 API 默认按当前领域过滤：**

```
// Before
ListAgents() → Agent[]

// After
ListAgents(libraryId) → Agent[]  // 空 = 全部（仅管理页用）
```

### 3.4 前端变更

**领域导航成为顶层入口：**

```
App Layout
├── 侧边栏: 领域列表 (当前领域高亮)
│   ├── 🏠 核心领域
│   ├── 🤖 AI模型
│   ├── 💻 编程
│   └── ...
├── 领域详情页 (新):
│   ├── 概览 Tab: KB/Agent/MCP/Wiki/Skill 数量 + 最近活动
│   ├── 知识库 Tab: 该领域的 KB 列表 + 创建
│   ├── Agent Tab: 该领域的 Agent 列表 + 创建
│   ├── MCP Tab: 该领域的 MCP Server 列表 + 创建
│   ├── Wiki Tab: 该领域的 Wiki 页面列表 + 创建/编辑
│   ├── Skill Tab: 该领域的 Skill 列表 + 启用/禁用
│   └── 记忆 Tab: 核心记忆 + 图谱（该领域的）
└── 现有各子系统页面: 改为 domain-scoped 视图
```

## 4. Implementation Plan — 分阶段实施

### Phase 0: Database Foundation (预计 0.5 天)

**目标: Schema 加固，数据不丢。**

- [ ] `domain_libraries` 表加 `icon`, `sort_order` 列
- [ ] `mcp_servers` 表加 `library_id` 列（如果用了 SQLite 存储）
- [ ] `skills` 表新建（如果当前没有独立表）
- [ ] 所有现有资源的 `library_id` 回填为默认领域 ID
- [ ] 添加 UNIQUE constraint: `(library_id, name)` on agents, KBs, MCPs, skills
- [ ] **Verify:** `go test ./internal/memory/...` 全通过；所有现有资源可查到 library_id

### Phase 1: Backend API Hardening (预计 1 天)

**目标: 所有创建 API 强制要求 libraryId。**

- [ ] `app_agent.go`: `CreateAgent` 加必传 `libraryId`；`ListAgents` 支持 `libraryId` 过滤
- [ ] `app_mcp.go`: `AddMCPServer` 加必传 `libraryId`；`ListMCPServers` 支持过滤
- [ ] `app_knowledge.go`: `CreateKnowledgeBase` libraryId 改为必传（当前可选）
- [ ] `app_wiki.go`: Wiki CRUD 全部要求 `libraryId`
- [ ] `app_skills.go`: Skill 注册加 `libraryId`；`ListSkills` 支持过滤
- [ ] `app_memory.go`: `MemoryRemember/Recall/Status` 默认用当前领域
- [ ] `app_tools.go`: 所有 tool schema 加 `libraryId` 参数，LLM 调用时必须指定
- [ ] **新增** `app_domain.go`: Domain CRUD Wails 绑定（复用现有 `domain_libraries` 的 CRUD）
- [ ] **Verify:** `go build ./...` + `go vet ./...` 通过；MCP/Agent/KB 创建 API 缺少 libraryId 时返回明确错误

### Phase 2: Frontend Domain-First Navigation (预计 1.5 天)

**目标: 领域成为 UI 顶层导航，而非可选项。**

- [ ] **新组件** `DomainPanel.vue`: 领域详情页（取代/整合 ZonePanel）
  - 概览卡片（KB/Agent/MCP/Wiki/Skill 数量 + 最近活动）
  - 子 Tab 切换（知识库 / Agent / MCP / Wiki / Skill / 记忆）
- [ ] **修改** `Knowledge.vue`: 默认只显示当前领域的 KB + 记忆
- [ ] **修改** `LLMAgents.vue`: 默认只显示当前领域的 Agent；移动创建按钮到领域页面
- [ ] **修改** `LLMMCP.vue`: 默认只显示当前领域的 MCP Server
- [ ] **修改** `ZonePanel.vue`: 整合进 DomainPanel 或重命名为运行区管理
- [ ] **修改** 侧边栏/导航: 加领域切换器（下拉或侧栏）
- [ ] **新增** `api/domain.ts`: Domain API 前端层
- [ ] **修改** `chatStore.ts`: 当前领域上下文注入 system prompt + 领域过滤 Agent 列表
- [ ] **Verify:** `vue-tsc --noEmit` 0 错误；`npm run build` 通过；UI 中所有创建流程都有领域选择/默认当前领域

### Phase 3: Skill System Unification (预计 1 天)

**目标: 三套 Skill 体系统一到领域容器下。**

- [ ] **新建** `internal/skills/` 下的 skills 持久化表（SQLite `skills` 表）
- [ ] `skills.json` 迁移到 DB；每个 Skill 绑定 `library_id`
- [ ] System Prompt 生成逻辑改为：列出当前领域已启用 Skill，而非全局
- [ ] `data/skills/` 30+ 开源模板 → 导入为"模板 Skill"（`builtin=1`），默认不启用
- [ ] Skill 启用/禁用按领域隔离
- [ ] **Verify:** System Prompt 生成的 Skill 列表 = 当前领域启用的；模板 Skill 不出现在能力页面除非手动启用

### Phase 4: Memory Dedup + Domain Scoping (预计 0.5 天)

**目标: 核心记忆去重，按领域隔离。**

- [ ] 新增 `memory_dedup` 任务：扫描 `user_facts` + `memory_items`，合并相似项
- [ ] `MemoryRecall/Remember` 默认只操作当前领域的数据
- [ ] 跨领域记忆查询需显式 `crossDomain=true`
- [ ] **Verify:** 同样的事实不再重复存储；切换领域后记忆内容不同

### Phase 5: Data Migration & Cleanup (预计 0.5 天)

**目标: 清理孤儿数据，修复悬挂引用。**

- [ ] 修复 "知识" KB 的悬挂 `libraryId` → 指向现有领域或删除
- [ ] MCP Server 回填 `libraryId` → 按命名启发式（SearXNG→MCP 领域，GitHub→编程领域，文件系统→默认领域）
- [ ] Agent 默认领域回填（当前 `EnsureLibraryIDs` 已做，确认都生效）
- [ ] Wiki 页面回填（按路径/标题启发式）
- [ ] **Verify:** `libraryId = ''` 的资源数为 0（除有意全局的）

## 5. Design Decisions

### 5.1 "默认领域"策略

- 系统始终有一个默认领域（当前是"核心领域"，id 为 seed 生成的第一个 library）
- 所有回填数据归入默认领域
- 用户不可删除默认领域
- 删除非默认领域时，资源可迁移到其他领域或默认领域

### 5.2 跨领域访问

- 默认隔离：KB 检索、记忆召回、Agent 列表默认只看当前领域
- 显式跨域：`agent_run` 的 `libraryId` 参数可指定目标领域；图谱边可标 `cross_tags`
- Chat 上下文默认注入当前领域的 KB + 记忆 + Wiki

### 5.3 自动发现 vs 人工管理

- 保留 LLM 自动创建领域的路径（`extract_facts` 返回 `domains` → `resolveOrCreateLibrary`）
- 自动创建的领域标记 `auto_created=1`，UI 显示 🤖 标记
- 用户可手动创建/重命名/合并/删除领域

### 5.4 向后兼容

- 所有 API 的 `libraryId` 参数新增 `?libraryId=` query 行为：空字符串 = 默认领域
- 旧前端代码传空 `libraryId` 时自动 fallback 到默认领域，不报错
- Phase 0-1 不删旧字段，只加新约束

## 6. Risk Mitigation

| 风险 | 可能性 | 缓解 |
|------|--------|------|
| 数据库迁移失败 | 低 | Phase 0 全程 `IF NOT EXISTS` + 事务；先备份 |
| 前端大面积改动引入 bug | 中 | 分 Phase，每个 Phase 独立可测；先改 API 再改 UI |
| LLM tool handler 不传 libraryId | 中 | `libraryId` 在 tool schema 标 Required；handler 内校验 |
| 用户不理解领域隔离 | 低 | 默认领域策略：不选领域 = 默认领域，行为不变 |

## 7. Success Criteria

- [ ] 所有 KB / Agent / MCP Server / Wiki Page / Skill 的 `library_id` 非空
- [ ] 删除领域时，其下所有子资源一起迁移或删除（无孤儿）
- [ ] 前端创建任何资源时，领域选择器可见且默认为当前领域
- [ ] 切换领域后，KB/Agent/MCP/Wiki/Skill 列表随之变化
- [ ] System Prompt 里的 Skill 列表 = 当前领域启用的 Skill
- [ ] 核心记忆无重复（同一 fact 不出现多次）
- [ ] `go build ./...` / `go test ./...` / `vue-tsc --noEmit` / `npm run build` 全通过

---

*References:*
- [Karpathy LLM Wiki Pattern](https://github.com/Astro-Han/karpathy-llm-wiki) — 三层架构：raw → wiki → index
- [Cursor Context Layers](https://github.com/slava-kudzinau/cursor-guide) — OS → Workspace → Project → Rules
- [Enterprise Multi-Agent Orchestrator](https://github.com/bhaveshjgithub/enterprise-multi-agent-orchestrator) — Domain isolation + DLP
- [Microsoft Multi-Agent Reference Architecture](https://microsoft.github.io/multi-agent-reference-architecture/) — Agent registry + skill binding
- [Obsidian PARA Method](https://github.com/portellam/Obsidian-PARA) — 强制 4 层容器
