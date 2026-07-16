<template>
  <div class="knowledge">
    <!-- Domain tab bar -->
    <div class="domain-bar">
      <button class="domain-tab-scroll-btn" @click="scrollTabs(-200)" title="向左滚动">◀</button>
      <div class="domain-tabs" ref="domainTabsRef">
        <button v-for="lib in libraries" :key="lib.id"
          class="domain-tab" :class="{ active: activeLibId === lib.id }"
          @click="activeLibId = lib.id"
          :title="lib.description || lib.name">
          <span class="domain-tab-icon">{{ lib.autoCreated ? '🤖' : '◈' }}</span>
          <span class="domain-tab-name">{{ isCoreLib(lib) ? '核心领域' : lib.name }}</span>
          <span class="domain-tab-count">{{ (libAgents[lib.id] || []).length }}a · {{ libKBCount(lib.id) }}kb</span>
          <button v-if="!isCoreLib(lib)" class="domain-tab-del" @click.stop="doDeleteLibrary(lib)" title="删除领域库">×</button>
        </button>
      </div>
      <button class="domain-tab-scroll-btn" @click="scrollTabs(200)" title="向右滚动">▶</button>
      <button class="btn btn-sm btn-primary domain-add-btn" @click="doCreateLibrary" :disabled="busy">+ 新建领域</button>
    </div>

    <!-- Create library dialog -->
    <div v-if="showCreateLib" class="overlay" @click.self="showCreateLib = false">
      <div class="glass-panel dialog lib-dialog">
        <h3>新建领域库</h3>
        <div class="ag-section">
          <label class="ag-label">名称</label>
          <input v-model="newLibName" type="text" placeholder="如：法律、技术、医学" class="field ag-input" @keyup.enter="confirmCreateLibrary" />
        </div>
        <div class="ag-section">
          <label class="ag-label">描述 <span class="ag-label-hint">— 可选</span></label>
          <input v-model="newLibDesc" type="text" placeholder="该领域的用途描述" class="field ag-input" @keyup.enter="confirmCreateLibrary" />
        </div>
        <div class="ag-dialog-foot">
          <button class="btn btn-sm" @click="showCreateLib = false">取消</button>
          <button class="btn btn-sm btn-primary" @click="confirmCreateLibrary" :disabled="!newLibName.trim()">创建</button>
        </div>
      </div>
    </div>


    <!-- Agent management (per-domain) -->
    <div class="glass-panel mem-panel">
      <div class="mem-head">
        <span class="mem-icon">🧠</span>
        <span class="mem-title">Agent</span>
        <span class="tag tag-accent">{{ (libAgents[activeLibId] || []).length }} 个</span>
        <button class="btn btn-sm btn-primary" @click="openCreateAgent" :disabled="busy">+ 新建 Agent</button>
      </div>
      <div v-if="libAgents[activeLibId]?.length" class="mem-list" style="max-height:200px;">
        <div v-for="ag in libAgents[activeLibId]" :key="ag.id" class="mem-item" style="cursor:pointer;" @click="openEditAgent(ag)" :title="'点击编辑 ' + ag.name">
          <span class="mem-kind" style="background:var(--accent-dim);color:var(--accent);">{{ ag.icon || '◉' }}</span>
          <div style="flex:1;min-width:0;">
            <span class="mem-text" style="font-weight:600;">{{ ag.name }}</span>
            <span class="mem-text" style="color:var(--text-tertiary);font-size:10px;display:block;">{{ ag.description || '（无描述）' }}</span>
          </div>
          <span v-if="ag.isDefault" class="tag tag-accent" style="font-size:9px;">默认</span>
        </div>
      </div>
      <div v-else class="mem-hint" style="text-align:center;">暂无 Agent，点击上方按钮创建</div>
    </div>


    <!-- wiki文档 (P6.1) — doc browser + semantic search -->
    <div class="glass-panel mem-panel wiki-panel">
      <div class="mem-head">
        <span class="mem-icon">📄</span>
        <span class="mem-title">wiki文档</span>
        <span class="tag tag-accent">{{ wikiStatus?.pages || 0 }} 页 / {{ wikiStatus?.chunks || 0 }} 段</span>
        <div class="mem-actions">
          <button class="btn btn-sm btn-primary" @click="startWikiCreate" :disabled="busy">+ 新建</button>
          <button class="btn btn-sm" @click="doReindexWiki" :disabled="busy">重建索引</button>
        </div>
      </div>
      <!-- Search bar -->
      <div class="wiki-search-row">
        <input v-model="wikiQuery" class="kg-search wiki-search-input" placeholder="语义检索文档…" @keyup.enter="doSearchWiki" />
        <button class="btn btn-xs btn-primary" @click="doSearchWiki" :disabled="busy || !wikiQuery.trim()">检索</button>
        <button v-if="wikiResults.length" class="btn btn-xs" @click="wikiResults = []; wikiQuery = ''">清除</button>
      </div>
      <!-- Search results -->
      <div v-if="wikiResults.length" class="wiki-results">
        <div v-for="(r, i) in wikiResults" :key="i" class="wiki-result-item"
             @click="openWikiPage(r.page)">
          <div class="wiki-result-head">
            <span class="wiki-result-page">{{ r.page }}</span>
            <span v-if="r.heading" class="wiki-result-heading">› {{ r.heading }}</span>
          </div>
          <div class="wiki-result-snippet">{{ r.content.slice(0, 200) }}{{ r.content.length > 200 ? '…' : '' }}</div>
        </div>
      </div>
      <!-- Page browser -->
      <div class="wiki-browser">
        <div class="wiki-pagelist">
          <div v-for="p in wikiPages" :key="p.id" class="wiki-pageitem"
               :class="{ active: wikiActivePage === p.id }"
               @click="openWikiPage(p.id)">
            <span class="wiki-pageicon">{{ wikiActivePage === p.id ? '📖' : '📄' }}</span>
            <span class="wiki-pagename">{{ p.id }}</span>
            <span class="wiki-pagechunks">{{ p.chunkCount }}</span>
            <button class="btn btn-xs btn-danger wiki-page-del"
                    @click.stop="doDeleteWikiPage(p.id)" title="删除">✕</button>
          </div>
          <div v-if="!wikiPages.length" class="mem-hint" style="text-align:center;padding:8px;">
            暂无页面。点击「重建索引」加载项目文档。
          </div>
        </div>
        <!-- Inline editor (create/edit) -->
        <div class="wiki-content" v-if="wikiEditing">
          <div class="wiki-content-head">
            <input v-model="wikiEditTitle" class="wiki-edit-title" placeholder="页面标题" />
            <button class="btn btn-xs btn-primary" @click="doSaveWikiPage" :disabled="!wikiEditTitle.trim() || !wikiEditContent.trim()">保存</button>
            <button class="btn btn-xs" @click="wikiEditing = false">取消</button>
          </div>
          <textarea v-model="wikiEditContent" class="wiki-edit-body" placeholder="Markdown 内容…"></textarea>
        </div>
        <!-- Page viewer -->
        <div class="wiki-content" v-else-if="wikiPageContent">
          <div class="wiki-content-head">
            <strong>{{ wikiActivePage }}</strong>
            <button class="btn btn-xs" @click="startWikiEdit(wikiActivePage, wikiPageContent.content)">✎ 编辑</button>
            <button class="btn btn-xs" @click="wikiPageContent = null; wikiActivePage = ''">✕ 关闭</button>
          </div>
          <div class="wiki-content-body" v-html="wikiRendered"></div>
        </div>
        <div class="wiki-content wiki-content-empty" v-if="!wikiPageContent && !wikiEditing && wikiPages.length">
          ← 选择页面阅读
        </div>
      </div>
    </div>

    <!-- 对话记忆 (P1.5) — hidden for core domain -->
    <div class="glass-panel mem-panel" v-if="!isCoreDomain">
      <div class="mem-head">
        <span class="mem-icon">💬</span>
        <span class="mem-title">对话记忆</span>
        <span v-if="memStatus?.bound" class="tag tag-accent">{{ memStatus.turnCount }} 问 / {{ memStatus.factCount }} 事实</span>
        <div class="mem-actions">
          <button class="btn btn-sm" @click="refreshMemory" :disabled="busy" title="刷新">↻</button>
          <button class="btn btn-sm btn-danger" @click="doClearMemory" :disabled="busy || !((memStatus?.turnCount || 0) + (memStatus?.factCount || 0))">清空</button>
        </div>
      </div>
      <div class="mem-model-row">
        <span class="mem-model-label">嵌入模型</span>
        <select v-model="memModelDir" class="mem-model-select" :disabled="busy">
          <option v-for="m in embedModels" :key="m.dir" :value="m.dir">{{ m.name }}</option>
        </select>
        <button class="btn btn-sm btn-primary" @click="applyMemoryModel" :disabled="busy || !memModelDir">
          {{ (memStatus?.turnCount || 0) + (memStatus?.factCount || 0) > 0 ? '迁移' : '绑定' }}
        </button>
        <span v-if="!embedModels.length" class="cp-hint">未检测到句向量模型</span>
      </div>
      <div v-if="!memStatus?.bound && !embedModels.length" class="mem-hint">
        未绑定句向量模型。下载 sentence-transformers 模型后即可在上方手动绑定，无需重启。
      </div>
      <div v-else>
        <!-- Tab switcher -->
        <div class="mem-tabs">
          <button class="mem-tab" :class="{ active: memTab === 'turn' }" @click="memTab = 'turn'">
            💬 问答 · {{ memStatus?.turnCount ?? turnItems.length }}
          </button>
          <button class="mem-tab" :class="{ active: memTab === 'fact' }" @click="memTab = 'fact'">
            📌 事实 · {{ memStatus?.factCount ?? factItems.length }}
          </button>
        </div>
        <div class="mem-list" v-if="memTab === 'turn' && turnItems.length">
          <div v-for="m in turnItems" :key="m.id" class="mem-item">
            <span class="mem-kind turn">💬</span>
            <div class="mem-text-block">
              <span class="mem-text">Q: {{ m.content }}</span>
              <span v-if="m.reply" class="mem-reply">A: {{ m.reply }}</span>
            </div>
            <button class="btn btn-xs btn-danger" @click="deleteMemoryItem(m.id)" title="删除">✕</button>
          </div>
        </div>
        <div class="mem-list" v-else-if="memTab === 'fact' && factItems.length">
          <div v-for="m in factItems" :key="m.id" class="mem-item">
            <span class="mem-kind fact">实</span>
            <span class="mem-text">[{{ m.category }}] {{ m.content }}</span>
            <button class="btn btn-xs btn-danger" @click="deleteMemoryItem(m.id)" title="删除">✕</button>
          </div>
        </div>
        <div v-if="!turnItems.length && !factItems.length" class="mem-hint">暂无记忆。对话后助手会自动记住你的偏好与关键信息（每 5 轮抽取事实）。</div>
      </div>
    </div>

    <!-- 知识图谱 (P4) -->
    <div class="glass-panel mem-panel">
      <div class="mem-head">
        <span class="mem-icon">🕸️</span>
        <span class="mem-title">知识图谱</span>
        <span class="tag tag-accent">{{ memStatus.nodeCount || 0 }} 实体 / {{ memStatus.edgeCount || 0 }} 关系</span>
        <div class="mem-actions">
          <button class="btn btn-sm" @click="refreshMemory" :disabled="busy" title="刷新">↻</button>
        </div>
      </div>
      <div v-if="kgNodes.length" class="kg-toolbar">
        <input v-model="kgSearch" class="kg-search" placeholder="搜索实体/类型…" />
        <button class="btn btn-xs" :class="{ 'btn-primary': kgLayoutMode === 'force' }" @click="kgLayoutMode = 'force'; applyLayout()" title="力导向布局">⬡</button>
        <button class="btn btn-xs" :class="{ 'btn-primary': kgLayoutMode === 'hier' }" @click="kgLayoutMode = 'hier'; applyLayout()" title="层次布局">≡</button>
        <button class="btn btn-xs cluster-btn" :class="{ 'btn-primary': kgClusterMode !== 0 }"
                @click="cycleCluster()" :title="clusterTitle">
          {{ clusterLabel }}
        </button>
        <span class="kg-stats-inline">{{ kgFilteredNodes.length }} 实体 · {{ kgFilteredEdges.length }} 关系</span>
      </div>
      <div v-if="kgStats?.topHubs?.length" class="kg-stats">
        核心实体：{{ kgStats.topHubs.map(h => h.name + '(' + h.degree + ')').join(' · ') }}
      </div>
      <div class="kg-viewer" v-if="kgNodes.length">
        <div class="kg-graph-wrapper">
          <div ref="kgContainer" class="kg-net"></div>
          <Transition name="kg-panel-slide" mode="out-in">
            <!-- Node detail card -->
            <div v-if="kgSelected" :key="'n-' + kgSelected.id" class="kg-detail kg-detail-overlay">
              <button class="kg-card-close" @click="kgSelected = null" title="关闭">✕</button>
              <div class="kg-card-icon" :style="{color: (STAR_COLORS[kgSelected.type] || STAR_COLORS.entity).halo}">●</div>
              <div class="kg-card-name">{{ kgSelected.name }}</div>
              <div class="kg-card-type">{{ kgSelected.type || 'entity' }}</div>
              <div class="kg-card-divider"></div>
              <div class="kg-rename">
                <input v-model="kgRenameDraft" class="kg-search" :placeholder="'重命名...'" @keyup.enter="doRename" />
                <button class="btn btn-xs btn-primary" @click="doRename" :disabled="!kgRenameDraft.trim()">改名</button>
              </div>
              <div class="kg-relations" v-if="kgSelectedRelations.length">
                <div v-for="r in kgSelectedRelations" :key="r.id" class="kg-rel">
                  <span class="kg-rel-dir">{{ r.srcId === kgSelected.id ? '→' : '←' }}</span>
                  <span class="kg-rel-pred">{{ r.type }}</span>
                  <span class="kg-rel-name">{{ r.srcId === kgSelected.id ? r.dstName : r.srcName }}</span>
                  <span v-if="r.validTo" class="kg-rel-old">失效</span>
                </div>
              </div>
              <div v-else class="mem-hint" style="font-size:10px;text-align:center;padding:6px 0;">暂无关系</div>
              <div class="kg-card-divider"></div>
              <button class="kg-card-danger" @click="deleteKgNode(kgSelected.id); kgSelected = null">删除实体</button>
            </div>
            <!-- Edge detail card -->
            <div v-else-if="kgSelectedEdge" :key="'e-' + kgSelectedEdge.id" class="kg-detail kg-detail-overlay">
              <button class="kg-card-close" @click="kgSelectedEdge = null; kgSelected = null" title="关闭">✕</button>
              <div class="kg-card-edge-label">{{ kgSelectedEdge.type }}</div>
              <div class="kg-card-edge-path">
                <span class="kg-card-edge-src">{{ kgSelectedEdge.srcName }}</span>
                <span class="kg-card-edge-arrow">→</span>
                <span class="kg-card-edge-dst">{{ kgSelectedEdge.dstName }}</span>
              </div>
              <div class="kg-card-divider"></div>
              <div class="kg-rename">
                <input v-model="kgEdgeRenameDraft" class="kg-search" :placeholder="'重命名关系...'" @keyup.enter="doRenameEdge" />
                <button class="btn btn-xs btn-primary" @click="doRenameEdge" :disabled="!kgEdgeRenameDraft.trim()">改名</button>
              </div>
              <div class="kg-card-meta">
                <span>权重 ×{{ kgSelectedEdge.weight || 1 }}</span>
                <span class="kg-card-dot">·</span>
                <span :class="kgSelectedEdge.validTo ? 'kg-card-invalid' : 'kg-card-valid'">
                  {{ kgSelectedEdge.validTo ? '已失效' : '有效' }}
                </span>
              </div>
              <div class="kg-card-divider"></div>
              <button class="kg-card-danger" @click="doDeleteEdge(kgSelectedEdge.id); kgSelectedEdge = null">删除关系</button>
            </div>
          </Transition>
        </div>
        <div class="kg-hint">点节点看详情、点边编辑关系；黄虚线 = 已失效。</div>
      </div>
      <div v-if="!kgNodes.length" class="mem-hint">暂无图谱。对话积累后会自动抽取实体与关系（每 5 轮一次）。</div>
    </div>

    <!-- P8: Cross-domain entity links -->
    <div class="glass-panel mem-panel" v-if="entityLinks.length">
      <div class="mem-head">
        <span class="mem-icon">🔗</span>
        <span class="mem-title">跨域链接</span>
        <span class="tag tag-accent">{{ entityLinks.length }} 条</span>
        <button class="btn btn-sm" @click="loadEntityLinks" :disabled="busy" title="刷新">↻</button>
      </div>
      <div class="mem-list" style="max-height:160px;">
        <div v-for="el in entityLinks" :key="el.id" class="mem-item">
          <span class="mem-kind" :style="{background: el.linkType==='sameAs'?'var(--success)':'var(--accent-dim)', color:'#fff'}">{{ el.linkType === 'sameAs' ? '≡' : '→' }}</span>
          <span class="mem-text">{{ el.srcName }} <span style="color:var(--text-tertiary);font-size:10px;">[{{ libName(el.srcLibrary) }}]</span> → {{ el.dstName }} <span style="color:var(--text-tertiary);font-size:10px;">[{{ libName(el.dstLibrary) }}]</span></span>
        </div>
      </div>
    </div>

    <!-- P8: Experience items (reflection loop distillations) — hidden for core domain -->
    <div class="glass-panel mem-panel" v-if="experienceItems.length && !isCoreDomain">
      <div class="mem-head">
        <span class="mem-icon">💡</span>
        <span class="mem-title">经验教训</span>
        <span class="tag tag-accent">{{ experienceItems.length }} 条</span>
        <button class="btn btn-sm" @click="loadExperience" :disabled="busy" title="刷新">↻</button>
      </div>
      <div class="mem-list" style="max-height:160px;">
        <div v-for="e in experienceItems" :key="e.id" class="mem-item">
          <span class="mem-kind" :style="{background: kindBg(e.kind), color:'#fff'}">{{ kindIcon(e.kind) }}</span>
          <span class="mem-text">{{ e.content || '（空内容）' }}</span>
          <button class="btn btn-xs btn-danger" @click="deleteExperience(e.id)" title="删除此条经验">✕</button>
        </div>
      </div>
    </div>

    <!-- Unified RAG search + KB accordion -->
    <div class="glass-panel mem-panel">
      <div class="mem-head">
        <span class="mem-icon">🔍</span>
        <span class="mem-title">知识检索</span>
        <span class="tag tag-accent">{{ kbs.length }} 个知识库</span>
        <button v-if="!isCoreDomain" class="btn btn-sm btn-primary" @click="showCreate = true" :disabled="busy">+ 新建</button>
      </div>
      <!-- Unified search bar -->
      <div class="kb-search-row" style="margin-bottom:8px;">
        <input v-model="ragQuery" class="kb-search-input" placeholder="输入问题跨所有知识库搜索..." @keyup.enter="doRagSearch" />
        <button class="btn btn-primary" @click="doRagSearch" :disabled="busy || !ragQuery.trim()">搜索</button>
      </div>
      <div v-if="ragBusy" class="mem-hint" style="text-align:center;">搜索中...</div>
      <div v-if="ragResults.length" class="mem-list" style="max-height:420px;">
        <div v-for="(r, i) in ragResults" :key="i" class="mem-item rag-result-item" @click="expandRagResult(i)">
          <div style="flex:1;min-width:0;cursor:pointer;">
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:3px;">
              <span class="mem-kind" style="background:var(--accent-dim);color:var(--accent);font-size:10px;">{{ (r.similarity * 100).toFixed(0) }}%</span>
              <span style="font-size:0.75em;color:var(--accent);">{{ r.source || r.metadata?.source || r.metadata?.filename || '—' }}</span>
            </div>
            <div class="mem-text" v-if="ragExpanded === i || r.content.length <= 200">{{ r.content }}</div>
            <div class="mem-text" v-else>{{ r.content.slice(0, 200) }}… <span style="color:var(--accent);font-size:0.8em;">点击展开</span></div>
          </div>
        </div>
      </div>
      <div v-if="ragSearched && !ragResults.length" class="mem-hint" style="text-align:center;">未找到相关知识</div>
      <div v-if="!ragSearched && kbs.length" class="mem-hint" style="text-align:center;">输入问题跨所有知识库统一搜索</div>

      <!-- KB accordion list -->
      <div v-if="kbs.length" class="kb-accordion" style="margin-top:10px; border-top:1px solid var(--border-subtle); padding-top:8px;">
        <div v-for="kb in kbs" :key="kb.id" class="kb-acc-item">
          <div class="kb-acc-head" @click="toggleKB(kb.id)">
            <span class="kb-acc-arrow">{{ activeKB === kb.id ? '▾' : '▸' }}</span>
            <span class="kb-acc-name">{{ kb.name }}</span>
            <span class="kb-acc-model" :class="{ 'kb-model-bound': kb.modelDir, 'kb-model-unbound': !kb.modelDir }">{{ kb.modelDir ? kbModelName(kb.modelDir) : '⚠ 未绑定模型' }}</span>
            <span class="kb-acc-count">{{ kb.chunkCount || 0 }} 条</span>
            <button v-if="!isCoreDomain" class="btn btn-xs btn-danger" @click.stop="doDelete(kb.id)" :disabled="busy" style="margin-left:auto;">✕</button>
          </div>
          <div v-if="activeKB === kb.id" class="kb-acc-body">
            <div v-if="!embedModels.length" class="mem-hint" style="padding:8px 0;text-align:center;">
              ⚠ 未检测到句向量模型，请先在模型市场下载 sentence-embedding 模型<br/>
              <span style="font-size:0.8em;color:#777;">下载后刷新页面即可绑定知识库</span>
            </div>
            <div v-else class="kb-model-row">
              <span class="kb-model-label">模型</span>
              <select class="kb-model-select" :value="kbModelDraft[kb.id] || kb.modelDir" @change="kbModelDraft[kb.id] = ($event.target as HTMLSelectElement).value" :disabled="busy">
                <option v-for="m in embedModels" :key="m.dir" :value="m.dir">{{ m.name }}</option>
              </select>
              <button class="btn btn-sm btn-primary" @click="applyKBModel(kb)" :disabled="busy || !kbModelDraft[kb.id] || kbModelDraft[kb.id] === kb.modelDir">
                {{ !kb.modelDir ? '绑定' : (kb.chunkCount || 0) > 0 ? '迁移' : '换绑' }}
              </button>
            </div>
            <KnowledgeAddText v-if="kb.modelDir" :kb-id="kb.id" :busy="busy" @done="onTabDone" />
            <div v-else class="mem-hint" style="text-align:center;padding:8px 0;">请先绑定句向量模型，才能添加文档</div>
          </div>
        </div>
      </div>
      <div v-if="!kbs.length" class="mem-hint" style="text-align:center; padding:12px 0;">{{ isCoreDomain ? '暂无知识库，可通过导入功能创建' : '暂无知识库，点击「+ 新建」创建' }}</div>
    </div>

    <!-- 新建知识库面板 -->
    <div v-if="showCreate && !isCoreDomain" class="glass-panel create-panel">
      <h3 class="cp-title">新建知识库</h3>
      <div class="cp-row">
        <label class="cp-label">名称</label>
        <input v-model="newKB.name" type="text" class="cp-input" placeholder="我的知识库" />
      </div>
      <div class="cp-row">
        <label class="cp-label">嵌入模型</label>
        <select v-model="newKB.modelDir" class="cp-select">
          <option v-for="m in embedModels" :key="m.dir" :value="m.dir">{{ m.name }}</option>
        </select>
        <span v-if="!embedModels.length" class="cp-hint">未检测到句向量模型</span>
      </div>
      <div class="cp-notice">
        <span class="cp-notice-icon">ⓘ</span>
        <span>嵌入模型<strong>创建后不可更换</strong>——不同模型的向量维度不同，无法混合匹配。如需迁移，请新建知识库并重新导入。</span>
      </div>
      <div class="cp-actions">
        <button class="btn btn-primary" @click="doCreate" :disabled="busy || !newKB.name || !newKB.modelDir">{{ busy ? '创建中…' : '创建' }}</button>
        <button class="btn" @click="showCreate = false">取消</button>
      </div>
    </div>
  </div>
  <!-- Reusable LLMAgents dialog (overlay appears on demand) -->
  <LLMAgents ref="agentsRef" class="embedded-agents" />
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { marked } from 'marked'
import { useToast } from '../composables/useToast'
import { useDataChanged } from '../composables/useDataChanged'
import { useActiveLibrary } from '../composables/useActiveLibrary'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { knowledgeApi } from '../api/knowledge'
import { memoryApi } from '../api/memory'
import { agentsApi } from '../api/agents'
import type { LocalAgent } from '../api/agents'
import { wikiApi } from '../api/wiki'
import type { MemoryStatus, MemoryItem, KgNode, KgEdge, GraphStats } from '../api/memory'
import { lastGraphTrace } from '../stores/chatStore'
import { Network } from 'vis-network'
import { DataSet } from 'vis-data'
import 'vis-network/styles/vis-network.css'
import { modelsApi } from '../api/models'
import KnowledgeAddText from './knowledge/KnowledgeAddText.vue'
import LLMAgents from './llm/LLMAgents.vue'

// ── Toast ──
const toast = useToast()

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : typeof e === 'string' ? e : String(e)
}

function kindIcon(kind: string): string {
  const map: Record<string, string> = { insight: '💡', strategy: '🎯', lesson: '📖', error_pattern: '⚠' }
  return map[kind] || '⚠'
}
function kindBg(kind: string): string {
  const map: Record<string, string> = { insight: 'var(--accent-dim)', strategy: 'var(--success)', lesson: 'var(--warning-dim)', error_pattern: 'var(--warning-dim)' }
  return map[kind] || 'var(--warning-dim)'
}

// ── State ──
const kbs = ref<any[]>([])
const embedModels = ref<any[]>([])
const showCreate = ref(false)
const newKB = reactive({ name: '', modelDir: '' })
const activeKB = ref<string | null>(null)
const kbTab = ref('add')
const kbDocs = ref<any[]>([])
// P7 domain library switcher
const libraries = ref<any[]>([])
const activeLibId = ref('')
// P8 cross-domain links + experience items
const entityLinks = ref<any[]>([])
const experienceItems = ref<any[]>([])
async function loadEntityLinks() { try { entityLinks.value = await memoryApi.entityLinks() || [] } catch (_) { entityLinks.value = [] } }
async function loadExperience() { try { experienceItems.value = await memoryApi.recallExperience(activeLibId.value, 10) || [] } catch (_) { experienceItems.value = [] } }
async function deleteExperience(id: string) {
  if (!await toast.confirm('删除经验', '确定删除这条经验教训？')) return
  try { await memoryApi.experienceDelete(id); await loadExperience() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}
async function deleteMemoryItem(id: string) {
  if (!await toast.confirm('删除记忆', '确定删除这条对话记忆？')) return
  try { await memoryApi.itemDelete(id); await refreshMemory() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}
function isCoreLib(lib: any) { return lib.id === libraries.value[0]?.id }
function libName(id: string) { return libraries.value.find((l: any) => l.id === id)?.name || id }
function libKBCount(libId: string) { return kbs.value.filter((kb: any) => kb.libraryId === libId || (!kb.libraryId && libId === libraries.value[0]?.id)).length }
async function loadLibraries() {
  try { libraries.value = (await memoryApi.libraryList()) || [] } catch (_) { libraries.value = [] }
  if (libraries.value.length && !activeLibId.value) activeLibId.value = libraries.value[0].id
}
const activeLibName = computed(() => libraries.value.find(l => l.id === activeLibId.value)?.name || '领域')

async function doDeleteLibrary(lib: any) {
  if (isCoreLib(lib)) return // core domain is undeletable
  if (!await toast.confirm('删除领域库', `确定删除「${lib.name}」？该领域的 KB、记忆、Agent 将迁移到核心领域。`)) return
  try {
    const wasActive = activeLibId.value === lib.id
    await memoryApi.libraryDelete(lib.id)
    toast.show('success', '已删除', lib.name)
    if (wasActive && libraries.value.length > 0) activeLibId.value = libraries.value[0]?.id
    await loadLibraries()
    await loadLibAgents()
  } catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}

const showCreateLib = ref(false)
const newLibName = ref('')
const newLibDesc = ref('')
const domainTabsRef = ref<HTMLElement | null>(null)
function scrollTabs(delta: number) { domainTabsRef.value?.scrollBy({ left: delta, behavior: 'smooth' }) }

function doCreateLibrary() { newLibName.value = ''; newLibDesc.value = ''; showCreateLib.value = true }
async function confirmCreateLibrary() {
  const name = newLibName.value.trim()
  if (!name) return
  showCreateLib.value = false
  try { await memoryApi.libraryCreate(name, newLibDesc.value.trim(), '', false); await loadLibraries() } catch (e: unknown) { toast.show('error', '创建失败', errMsg(e)) }
}

// A library is the core domain if it's the first (default) library in the list.
const isCoreDomain = computed(() => {
  if (!activeLibId.value || !libraries.value.length) return true
  return libraries.value[0]?.id === activeLibId.value
})

// Shared active library state — sync with other views (chat, agents).
const { activeLibraryId } = useActiveLibrary()

// Agents grouped by library ID (for display in library cards).
const libAgents = ref<Record<string, LocalAgent[]>>({})
async function loadLibAgents() {
  try {
    const all = await agentsApi.list()
    const map: Record<string, LocalAgent[]> = {}
    for (const lib of libraries.value) {
      map[lib.id] = all.filter(a => a.libraryId === lib.id || (!a.libraryId && lib.id === libraries.value[0]?.id))
    }
    libAgents.value = map
  } catch (_) { libAgents.value = {} }
}

// P8: Simplified RAG search state for non-core domains.
const ragQuery = ref('')
const ragResults = ref<any[]>([])
const ragBusy = ref(false)
const ragSearched = ref(false)
const ragExpanded = ref(-1)
function expandRagResult(i: number) { ragExpanded.value = ragExpanded.value === i ? -1 : i }
async function doRagSearch() {
  const q = ragQuery.value.trim()
  if (!q || !kbs.value.length) return
  ragBusy.value = true; ragSearched.value = true; ragExpanded.value = -1
  try {
    const all: any[] = []
    for (const kb of kbs.value) {
      const hits = await knowledgeApi.search(kb.id, q, 3)
      if (hits?.length) all.push(...hits)
    }
    all.sort((a, b) => b.similarity - a.similarity)
    ragResults.value = all.slice(0, 8)
  } catch (_) { ragResults.value = [] }
  ragBusy.value = false
}

// Agent dialog ref — reuses the full LLMAgents dialog component.
const agentsRef = ref<InstanceType<typeof LLMAgents> | null>(null)
function openCreateAgent() {
  agentsRef.value?.openCreate(activeLibId.value)
}
function openEditAgent(ag: LocalAgent) {
  agentsRef.value?.openEdit(ag)
}

// When the user switches domain, refresh KB list and sync shared state.
watch(activeLibId, (id) => {
  activeLibraryId.value = id
  memoryApi.libraryBumpUse(id) // track usage frequency
  showCreate.value = false; ragResults.value = []; ragSearched.value = false; ragQuery.value = ''; wikiResults.value = []; wikiPageContent.value = null; wikiActivePage.value = ''; wikiRendered.value = ''; wikiPages.value = []; wikiStatus.value = null; memItems.value = []; memStatus.value = null; kgNodes.value = []; kgEdges.value = []
  refreshKBs()
  refreshMemory()        // KG + wiki needs refreshing for all domains
  loadEntityLinks()       // cross-domain links may highlight current domain
  loadExperience()        // experience is domain-scoped
  loadLibAgents()         // agent list per domain
  loadWikiPages()         // wiki page list per domain
})

const busy = ref(false)
// P7 workspace switcher
const wsStore = useWorkspaceStore()
const workspaces = computed(() => wsStore.workspaces)
const activeWsId = computed({ get: () => wsStore.activeId, set: (v: string) => wsStore.setActive(v) })

// ── 对话记忆 (P1.5) ──
const memStatus = ref<MemoryStatus | null>(null)
const memItems = ref<MemoryItem[]>([])
const memTab = ref<'turn' | 'fact'>('turn')
const turnItems = computed(() => memItems.value.filter(m => m.kind === 'turn'))
const factItems = computed(() => memItems.value.filter(m => m.kind === 'fact'))
const memModelDir = ref('')
const kgNodes = ref<KgNode[]>([])
const kgEdges = ref<KgEdge[]>([])
// vis-network viewer (P4: detail panel, search/filter, layout switch, cluster, history).
const kgContainer = ref<HTMLElement | null>(null)
let kgNetwork: Network | null = null
const kgSearch = ref('')
const kgShowHistory = ref(false)
const kgLayoutMode = ref<'force' | 'hier'>('force')
const kgClusterMode = ref(0) // 0=展开, 1=离群折叠, 2=类型聚合

const clusterLabel = computed(() => ['◉ 展开', '◉ 离群', '◉ 类型'][kgClusterMode.value])
const clusterTitle = computed(() => ['全部展开', '离群节点已折叠', '按类型聚合'][kgClusterMode.value])
const kgSelected = ref<KgNode | null>(null)
const kgSelectedEdge = ref<KgEdge | null>(null)
const kgStats = ref<GraphStats | null>(null)
const kgRenameDraft = ref('')
const kgEdgeRenameDraft = ref('')
// P6.1 project docs (llmwiki index) — browser + search
const wikiStatus = ref<{ pages: number; chunks: number } | null>(null)
const wikiQuery = ref('')
const wikiResults = ref<any[]>([])
const wikiPages = ref<any[]>([])
const wikiActivePage = ref('')
const wikiPageContent = ref<any>(null)
const wikiRendered = ref('')
const wikiEditing = ref(false)
const wikiEditTitle = ref('')
const wikiEditContent = ref('')

// Filtered views: search (name/type substring).
const kgFilteredNodes = computed(() => {
  const q = kgSearch.value.trim().toLowerCase()
  if (!q) return kgNodes.value
  return kgNodes.value.filter(n => (n.name || '').toLowerCase().includes(q) || (n.type || '').toLowerCase().includes(q))
})
const kgFilteredNodeIDs = computed(() => new Set(kgFilteredNodes.value.map(n => n.id)))
const kgFilteredEdges = computed(() => {
  const ids = kgFilteredNodeIDs.value
  return kgEdges.value.filter(e => ids.has(e.srcId) && ids.has(e.dstId))
})
// Relations of the selected node (for the detail panel).
const kgSelectedRelations = computed(() => {
  const id = kgSelected.value?.id
  if (!id) return []
  return kgEdges.value.filter(e => e.srcId === id || e.dstId === id)
})

function computeNodeDegrees() {
  const deg: Record<string, number> = {}
  for (const e of kgFilteredEdges.value) {
    deg[e.srcId] = (deg[e.srcId] || 0) + 1
    deg[e.dstId] = (deg[e.dstId] || 0) + 1
  }
  return deg
}

// Starfield color palette — deep space gradient from core (white) to halo (colored).
const STAR_COLORS: Record<string, { core: string; halo: string; glow: string }> = {
  person:   { core: '#e8f4ff', halo: '#4da6ff', glow: 'rgba(77,166,255,0.35)' },
  language: { core: '#f0e6ff', halo: '#b366ff', glow: 'rgba(179,102,255,0.35)' },
  company:  { core: '#ffe8f0', halo: '#ff66a3', glow: 'rgba(255,102,163,0.30)' },
  project:  { core: '#fff8e0', halo: '#ffb84d', glow: 'rgba(255,184,77,0.30)' },
  entity:   { core: '#e0e8f0', halo: '#6b8aaa', glow: 'rgba(107,138,170,0.25)' },
}

function buildNodes() {
  const degrees = computeNodeDegrees()
  return new DataSet<any>(kgFilteredNodes.value.map(n => {
    const d = degrees[n.id] || 0
    // Star sizing: lone entities = small stars, hubs = bright giants
    const size = d <= 1 ? 10 : d <= 3 ? 16 : d <= 8 ? 22 : d <= 15 ? 28 : 34
    const c = STAR_COLORS[n.type] || STAR_COLORS.entity
    return {
      id: n.id,
      label: n.name,
      title: `${n.type || 'entity'}\n${d} connections`,
      group: n.type || 'entity',
      size,
      color: { background: c.core, border: c.halo, highlight: { background: c.halo, border: '#ffffff' } },
      borderWidth: d <= 1 ? 1 : 2,
      shadow: { enabled: true, color: c.glow, size: size / 2 },
    }
  }))
}
function buildEdges() {
  function edgeWidth(w: number) {
    if (w <= 1) return 0.8
    if (w <= 3) return 1.4
    if (w <= 7) return 2.2
    if (w <= 15) return 3.2
    return 4.5
  }
  function edgeAlpha(w: number) {
    if (w <= 1) return 0.22
    if (w <= 3) return 0.38
    if (w <= 7) return 0.55
    if (w <= 15) return 0.70
    return 0.85
  }

  // Detect edges sharing same src→dst pair and assign distinct curves.
  const pairKeys = new Map<string, number>()
  for (const e of kgFilteredEdges.value) {
    const key = `${e.srcId}→${e.dstId}`
    pairKeys.set(key, (pairKeys.get(key) || 0) + 1)
  }
  const pairIndex = new Map<string, number>()

  return new DataSet<any>(kgFilteredEdges.value.map(e => {
    const w = e.weight || 1
    const width = edgeWidth(w)
    const alpha = edgeAlpha(w)
    const label = w > 1 ? `${e.type} (×${w})` : e.type
    const edgeColor = e.validTo > 0
      ? `rgba(210,153,34,${Math.max(0.35, alpha)})`
      : `rgba(140,185,230,${alpha})`
    const arrowColor = e.validTo > 0
      ? `rgba(210,153,34,${Math.max(0.5, alpha + 0.15)})`
      : `rgba(160,200,240,${Math.min(0.95, alpha + 0.2)})`

    // Separate overlapping edges between same src→dst by alternating curve
    // direction and varying roundness.
    const pairKey = `${e.srcId}→${e.dstId}`
    const idx = pairIndex.get(pairKey) || 0
    pairIndex.set(pairKey, idx + 1)
    const total = pairKeys.get(pairKey) || 1
    const dir = idx % 2 === 0 ? 'curvedCW' as const : 'curvedCCW' as const
    const r = total <= 1 ? 0.12 : 0.12 + idx * 0.08

    return {
      id: e.id, from: e.srcId, to: e.dstId, label,
      title: w > 1 ? `权重 ×${w}` : (e.validTo ? '已失效' : ''),
      dashes: e.validTo > 0,
      width,
      color: { color: edgeColor, highlight: arrowColor, hover: arrowColor },
      arrows: { to: { enabled: true, scaleFactor: 0.7, type: 'arrow' } },
      smooth: { type: dir, roundness: r },
    }
  }))
}

// applyLayout switches between force-directed and hierarchical.
function applyLayout() {
  if (!kgNetwork) return
  if (kgLayoutMode.value === 'hier') {
    kgNetwork.setOptions({ layout: { hierarchical: { enabled: true, direction: 'UD' } }, physics: { enabled: false } })
  } else {
    kgNetwork.setOptions({ layout: { hierarchical: { enabled: false } }, physics: { enabled: true, barnesHut: { gravitationalConstant: -25000, springLength: 250, springConstant: 0.015, damping: 0.35, centralGravity: 0 } } })
    kgNetwork.once('stabilizationIterationsDone', () => { if (kgNetwork) kgNetwork.setOptions({ physics: { enabled: false } }) })
  }
}

// cycleCluster: 0→1→2→0. Each mode re-renders the graph with clustering applied.
function cycleCluster() {
  kgClusterMode.value = (kgClusterMode.value + 1) % 3
  destroyGraph()
  renderGraph()
}

// applyClusterMode runs the current cluster mode on the network.
function applyClusterMode() {
  if (!kgNetwork) return
  switch (kgClusterMode.value) {
    case 1: // outlier fold
      kgNetwork.clusterOutliers()
      break
    case 2: // cluster by type/group
      // Cluster each group that has >1 node
      const groups = new Map<string, string[]>()
      kgNodes.value.forEach(n => {
        const g = n.type || 'entity'
        if (!groups.has(g)) groups.set(g, [])
        groups.get(g)!.push(n.id)
      })
      groups.forEach((ids) => {
        if (ids.length > 1) {
          kgNetwork!.cluster({ joinCondition: (no: any) => ids.includes(no.id), clusterNodeProperties: { label: `${ids.length} nodes`, shape: 'dot', size: 20 } })
        }
      })
      break
    default: // mode 0 — full expand, no clustering (handled by fresh render)
      break
  }
}

function destroyGraph() {
  if (kgNetwork) { kgNetwork.destroy(); kgNetwork = null }
}

function renderGraph() {
  if (!kgContainer.value) return
  const nodes = buildNodes()
  const edges = buildEdges()
  // Always destroy and re-create on data change — vis-network setData()
  // can glitch when node IDs completely change (domain switch).
  destroyGraph()
  if (nodes.length === 0) return
  kgNetwork = new Network(kgContainer.value, { nodes, edges }, {
      autoResize: true,
      interaction: { hover: true, tooltipDelay: 100, hoverConnectedEdges: true },
      nodes: {
        shape: 'dot',
        font: { size: 12, color: '#d0dce8', face: 'system-ui', strokeWidth: 0 },
        borderWidth: 2,
        borderWidthSelected: 3,
        color: { background: '#e0e8f0', border: '#6b8aaa', highlight: { background: '#ffffff', border: '#ffffff' } },
      },
      edges: {
        arrows: { to: { enabled: true, scaleFactor: 0.8, type: 'arrow' } },
        font: { size: 9, color: '#8899aa', align: 'middle', strokeWidth: 3, strokeColor: 'rgba(5,5,16,0.8)' },
        selectionWidth: 1.5,
      },
      groups: {},
      physics: {
        enabled: true,
        barnesHut: {
          gravitationalConstant: -25000,  // very strong repulsion — subgraphs fully separate
          centralGravity: 0,              // no center pull
          springLength: 250,              // longer edges
          springConstant: 0.015,          // very soft springs
          damping: 0.35,                  // slow settle
        },
        stabilization: { iterations: 250, fit: true },
        maxVelocity: 25,
        minVelocity: 0.3,
      },
    })
    kgNetwork.on('click', (params: any) => {
      if (params.nodes?.length) {
        kgSelectedEdge.value = null
        kgSelected.value = kgNodes.value.find(x => x.id === params.nodes[0]) || null
        if (kgNetwork) kgNetwork.selectNodes([params.nodes[0]])
      } else if (params.edges?.length) {
        kgSelected.value = null
        kgSelectedEdge.value = kgEdges.value.find(e => e.id === params.edges[0]) || null
        kgEdgeRenameDraft.value = ''
        if (kgNetwork) kgNetwork.selectEdges([params.edges[0]])
      } else {
        kgSelected.value = null
        kgSelectedEdge.value = null
      }
    })
    if (kgLayoutMode.value === 'hier') {
      applyLayout()
    } else {
      kgNetwork!.once('stabilizationIterationsDone', () => {
        if (kgNetwork) kgNetwork.setOptions({ physics: { enabled: false } })
      })
    }
    applyClusterMode()
}

// nextTick: the graph container is inside v-if="memStatus?.bound"; on first load
// memStatus is set in the same tick as kgNodes, so the container isn't in the
// DOM yet when the watch fires. Defer to the next tick (after Vue updates the v-if).
watch([kgFilteredNodes, kgFilteredEdges], () => { nextTick(renderGraph) })
watch(kgShowHistory, () => { destroyGraph(); nextTick(renderGraph) })
const kbModelDraft = reactive<Record<string, string>>({})

// ── Data operations ──
async function refreshKBs() {
  try { kbs.value = (await knowledgeApi.list(activeLibId.value)) || [] } catch (_) { kbs.value = [] }
}

async function refreshModels() {
  try {
    const all = (await modelsApi.listToolModels()) || []
    embedModels.value = all.filter((m: any) => m.type === 'sentence-embedding')
    if (embedModels.value.length && !newKB.modelDir) {
      newKB.modelDir = embedModels.value[0].dir
    }
  } catch (_) { embedModels.value = [] }
}

function toggleKB(id: string) {
  if (activeKB.value === id) { activeKB.value = null; return }
  activeKB.value = id
  kbTab.value = 'add'
  kbDocs.value = []
  const kb = kbs.value.find(k => k.id === id)
  if (kb) kbModelDraft[id] = kb.modelDir
}

function kbModelName(dir: string): string {
  const m = embedModels.value.find((x: any) => x.dir === dir)
  return m ? m.name : dir.slice(Math.max(0, dir.lastIndexOf('/') + 1))
}

function onTabDone() {
  busy.value = false
  refreshKBs()
}

async function doCreate() {
  if (!newKB.name || !newKB.modelDir) return
  busy.value = true
  try {
    await knowledgeApi.create(newKB.name, newKB.modelDir, activeLibId.value)
    showCreate.value = false
    newKB.name = ''
    toast.show('success', '知识库已创建', '')
    await refreshKBs()
  } catch (e: unknown) { toast.show('error', '创建失败', errMsg(e)) }
  busy.value = false
}

async function doDelete(kbId: string) {
  if (!await toast.confirm('删除知识库', '此操作不可撤销，将删除知识库及其所有文档数据。')) return
  busy.value = true
  try {
    await knowledgeApi.delete(kbId)
    toast.show('success', '已删除', '')
    if (activeKB.value === kbId) activeKB.value = null
    await refreshKBs()
  } catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
  busy.value = false
}

function modelName(dir: string) {
  const m = embedModels.value.find(x => x.dir === dir)
  return m ? m.name : ''
}

async function switchBrowse(kbId: string) {
  kbTab.value = 'browse'
  busy.value = true
  kbDocs.value = []
  try {
    kbDocs.value = (await knowledgeApi.listDocuments(kbId)) || []
  } catch (e: unknown) { toast.show('error', '加载文档列表失败', errMsg(e)) }
  busy.value = false
}

async function doClear(kbId: string) {
  if (!await toast.confirm('清空知识库', '此操作不可撤销，所有文档数据将被删除，但知识库名称和模型绑定会保留。')) return
  busy.value = true
  try {
    await knowledgeApi.clear(kbId)
    toast.show('success', '已清空', '')
    kbDocs.value = []
    await refreshKBs()
  } catch (e: unknown) { toast.show('error', '清空失败', errMsg(e)) }
  busy.value = false
}

async function doDeleteDoc(kbId: string, docId: string) {
  if (!await toast.confirm('删除文档', '确定删除此文档？')) return
  busy.value = true
  try {
    await knowledgeApi.deleteChunks(kbId, [docId])
    kbDocs.value = kbDocs.value.filter(d => d.id !== docId)
    toast.show('success', '已删除', '')
    await refreshKBs()
  } catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
  busy.value = false
}

// ── 对话记忆 ──
async function refreshMemory() {
  try {
    const s = await memoryApi.status(activeLibId.value)
    memStatus.value = s
    memItems.value = (await memoryApi.list(50, activeLibId.value)) || []
    if (!memModelDir.value) memModelDir.value = s.modelDir || (embedModels.value[0]?.dir || '')
    const g = await memoryApi.listGraph(kgShowHistory.value, activeLibId.value)
    kgNodes.value = g?.nodes || []
    kgEdges.value = g?.edges || []
    try { kgStats.value = await memoryApi.graphStats() } catch (_) { kgStats.value = null }
    try { wikiStatus.value = await wikiApi.status(activeLibId.value) } catch (_) { wikiStatus.value = null }
  } catch (_) { /* best-effort */ }
}

async function applyMemoryModel() {
  if (!memModelDir.value || busy.value) return
  const has = (memStatus.value?.turnCount || 0) + (memStatus.value?.factCount || 0) > 0
  if (has && !await toast.confirm('迁移记忆嵌入模型', '将用新模型重新嵌入所有记忆向量（内容不变）。此操作不可撤销。')) return
  busy.value = true
  try {
    if (has) await memoryApi.migrateModel(memModelDir.value)
    else await memoryApi.setEmbeddingModel(memModelDir.value)
    toast.show('success', has ? '记忆已迁移' : '模型已绑定', '')
    await refreshMemory()
  } catch (e: unknown) { toast.show('error', '操作失败', errMsg(e)) }
  busy.value = false
}

async function applyKBModel(kb: any) {
  const newDir = kbModelDraft[kb.id]
  if (!newDir || newDir === kb.modelDir || busy.value) return
  const has = (kb.chunkCount || 0) > 0
  if (has && !await toast.confirm('迁移知识库嵌入模型', `将用新模型重新嵌入「${kb.name}」的全部文档（${kb.chunkCount} 条，内容不变，向量重算）。此操作不可撤销。`)) return
  busy.value = true
  try {
    if (has) await knowledgeApi.migrateModel(kb.id, newDir)
    else await knowledgeApi.updateModelDir(kb.id, newDir)
    toast.show('success', has ? '已迁移' : '已绑定', '')
    await refreshKBs()
  } catch (e: unknown) { toast.show('error', '操作失败', errMsg(e)) }
  busy.value = false
}

// P7 workspace
async function doCreateWorkspace() {
  const name = prompt('新建工作区名称')
  if (name?.trim()) await wsStore.create(name.trim())
}

async function doClearMemory() {
  if (!await toast.confirm('清空对话记忆', '将删除所有自动记住的问题与事实。此操作不可撤销。')) return
  busy.value = true
  try {
    await memoryApi.clear()
    toast.show('success', '记忆已清空', '')
    await refreshMemory()
  } catch (e: unknown) { toast.show('error', '清空失败', errMsg(e)) }
  busy.value = false
}

// ── 知识图谱 viewer (P3.5) — click a node/edge to delete (correct extraction errors) ──
async function deleteKgNode(id: string) {
  if (!await toast.confirm('删除实体', '删除该实体及其所有关系？')) return
  try { await memoryApi.deleteNode(id); await refreshMemory() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}
async function deleteKgEdge(id: string) {
  if (!await toast.confirm('删除关系', '删除该关系？')) return
  try { await memoryApi.deleteEdge(id); await refreshMemory() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}
async function doDeleteEdge(id: string) {
  if (!await toast.confirm('删除关系', '删除该关系？此操作不可撤销。')) return
  try { await memoryApi.deleteEdge(id); kgSelectedEdge.value = null; await refreshMemory() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}
async function doRenameEdge() {
  const id = kgSelectedEdge.value?.id
  const newType = kgEdgeRenameDraft.value.trim()
  if (!id || !newType) return
  try { await memoryApi.renameEdge(id, newType); kgEdgeRenameDraft.value = ''; await refreshMemory() }
  catch (e: unknown) { toast.show('error', '重命名失败', errMsg(e)) }
}

// Highlight the subgraph the last chat recall used (cross-view via the chatStore trace).
function highlightRecall() {
  if (!kgNetwork) return
  const t = lastGraphTrace.value
  if (!t?.seedIds?.length) { toast.show('info', '无召回记录', '先在对话中提问以触发图谱召回'); return }
  kgNetwork.setSelection({ nodes: t.seedIds, edges: t.edgeIds || [] })
}

// Rename the selected entity.
async function doRename() {
  const id = kgSelected.value?.id
  const name = kgRenameDraft.value.trim()
  if (!id || !name) return
  try { await memoryApi.renameNode(id, name); kgRenameDraft.value = ''; await refreshMemory() }
  catch (e: unknown) { toast.show('error', '重命名失败', errMsg(e)) }
}

// ── Project docs (P6.1) — llmwiki index ──
async function doReindexWiki() {
  busy.value = true
  try {
    const r = await wikiApi.reindex(activeLibId.value)
    toast.show('success', '索引重建完成', (r?.pages || 0) + ' 页 / ' + (r?.chunks || 0) + ' 段')
    await refreshMemory()
  } catch (e: unknown) { toast.show('error', '重建失败', errMsg(e)) }
  busy.value = false
}
async function doSearchWiki() {
  if (!wikiQuery.value.trim()) return
  try {
    const hits = await wikiApi.search(activeLibId.value, wikiQuery.value.trim())
    wikiResults.value = hits || []
  } catch (e: unknown) { toast.show('error', '检索失败', errMsg(e)) }
}
async function loadWikiPages() {
  try { wikiPages.value = (await wikiApi.listPages(activeLibId.value)) || [] } catch (_) { wikiPages.value = [] }
}
async function openWikiPage(pageId: string) {
  wikiActivePage.value = pageId; wikiEditing.value = false
  try {
    const data = await wikiApi.readPage(activeLibId.value, pageId)
    wikiPageContent.value = data
    wikiRendered.value = marked.parse(data.content, { breaks: true, gfm: true }) as string
  } catch (e: unknown) { toast.show('error', '读取失败', errMsg(e)) }
}
function startWikiCreate() {
  wikiActivePage.value = ''; wikiPageContent.value = null
  wikiEditTitle.value = ''; wikiEditContent.value = ''; wikiEditing.value = true
}
function startWikiEdit(id: string, content: string) {
  wikiEditTitle.value = id; wikiEditContent.value = content; wikiEditing.value = true
}
async function doSaveWikiPage() {
  const title = wikiEditTitle.value.trim()
  if (!title || !wikiEditContent.value.trim()) return
  const pageId = title.toLowerCase().replace(/[^a-z0-9一-鿿]+/g, '-').replace(/^-|-$/g, '')
  try {
    await wikiApi.savePage(activeLibId.value, pageId, title, wikiEditContent.value)
    wikiEditing.value = false; await loadWikiPages(); await refreshMemory()
    toast.show('success', '已保存', title)
  } catch (e: unknown) { toast.show('error', '保存失败', errMsg(e)) }
}
async function doDeleteWikiPage(pageId: string) {
  if (!await toast.confirm('删除页面', `确定删除 "${pageId}"？`)) return
  try { await wikiApi.deletePage(activeLibId.value, pageId); await loadWikiPages(); await refreshMemory() }
  catch (e: unknown) { toast.show('error', '删除失败', errMsg(e)) }
}

// ── Lifecycle ──
onMounted(async () => { await loadLibraries(); await wsStore.load(); await refreshKBs(); await refreshModels(); await refreshMemory(); await loadLibAgents(); await loadEntityLinks(); await loadExperience(); await loadWikiPages() })
onBeforeUnmount(() => { destroyGraph() })
// Unified refresh — all data mutations trigger a full refresh of the current domain.
function refreshAll() {
  refreshKBs()
  refreshMemory()      // memItems, kgNodes, kgEdges, wikiStatus
  loadEntityLinks()
  loadExperience()
  loadWikiPages()
  loadLibAgents()
}
useDataChanged('kb:changed', () => { refreshAll() })
useDataChanged('memory:changed', () => { refreshAll() })
useDataChanged('agents:changed', () => { refreshAll() })
useDataChanged('wiki:changed', () => { refreshAll() })
</script>

<style scoped>
.knowledge { width: 100%; }

/* Domain tab bar */
.domain-bar { display: flex; align-items: center; gap: 0; margin-bottom: 12px; position: relative; }
.domain-tabs {
  display: flex; align-items: center; gap: 2px; flex: 1; overflow-x: auto;
  scroll-behavior: smooth; -webkit-overflow-scrolling: touch;
  /* Hide scrollbar */
  scrollbar-width: none; -ms-overflow-style: none;
  mask-image: linear-gradient(to right, black 92%, transparent 100%);
  -webkit-mask-image: linear-gradient(to right, black 92%, transparent 100%);
}
.domain-tabs::-webkit-scrollbar { display: none; }
.domain-tab-scroll-btn {
  width: 24px; height: 28px; flex-shrink: 0; padding: 0; border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-secondary);
  cursor: pointer; font-size: 12px; display: flex; align-items: center; justify-content: center;
  opacity: 0.6; transition: opacity 0.15s;
}
.domain-tab-scroll-btn:hover { opacity: 1; background: var(--bg-hover); }
.domain-tab {
  display: flex; align-items: center; gap: 6px; padding: 8px 14px;
  border: 1px solid var(--border-subtle); border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer;
  font-size: 12px; font-family: var(--font); font-weight: 500; white-space: nowrap;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  position: relative; flex-shrink: 0;
  /* Prevent layout shift on active — same border width always */
  border-bottom-width: 2px;
  border-bottom-color: transparent;
  margin-bottom: 0;
}
.domain-tab:hover { background: var(--bg-hover); color: var(--text-primary); }
.domain-tab.active {
  background: var(--bg-active); color: var(--accent);
  border-bottom-color: var(--accent);
}
.domain-tab-icon { font-size: 14px; flex-shrink: 0; }
.domain-tab-name { font-weight: 500; }
.domain-tab-count { font-size: 9px; color: var(--text-tertiary); margin-left: 2px; }
.domain-tab-del {
  width: 16px; height: 16px; padding: 0; border: none; border-radius: 3px;
  background: transparent; color: var(--text-tertiary); font-size: 12px; cursor: pointer;
  display: flex; align-items: center; justify-content: center; margin-left: 2px;
  opacity: 0; transition: all 0.15s;
}
.domain-tab:hover .domain-tab-del { opacity: 1; }
.domain-tab-del:hover { background: var(--danger-dim); color: var(--danger); }
.domain-add-btn { flex-shrink: 0; margin-left: 8px; }

/* Create library dialog */
.lib-dialog { width: 380px; padding: 20px 24px; }
.lib-dialog h3 { font-size: 15px; font-weight: 600; margin-bottom: 16px; }
.lib-dialog .ag-label { font-size: 12px; color: var(--text-secondary); display: block; margin-bottom: 4px; }
.lib-dialog .ag-input { width: 100%; }
.lib-dialog .ag-section { margin-bottom: 12px; }
.lib-dialog .ag-dialog-foot { display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px; }
.lib-dialog .ag-label-hint { font-size: 10px; color: var(--text-tertiary); font-weight: 400; }
.lib-dialog .field { padding: 7px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-inset); color: var(--text-primary); font-size: 13px; outline: none; }
.lib-dialog .field:focus { border-color: var(--accent); }

/* Agent chips inside library cards (keep for lib-agents in other contexts) */
.lib-agents { display: flex; flex-wrap: wrap; gap: 3px; margin-top: 6px; }
.lib-agent-chip { font-size: 10px; padding: 1px 6px; background: var(--accent-dim); color: var(--accent); border-radius: 3px; white-space: nowrap; }


/* 对话记忆面板 */
.mem-panel { padding: 14px 16px; margin-bottom: 20px; }

/* 知识图谱 viewer (P3.5) */
.kg-viewer { margin-top: 8px; }
.kg-graph-wrapper { position: relative; height: 420px; overflow: hidden; }
.kg-net {
  width: 100%; height: 420px;
  background: radial-gradient(ellipse at center, #0d1421 0%, #080c14 40%, #050510 100%);
  border: 1px solid rgba(100,140,180,0.12);
  border-radius: var(--radius-sm);
}
.kg-hint { font-size: 10px; color: var(--text-tertiary); margin-top: 4px; }
.kg-toolbar { display: flex; align-items: center; gap: 6px; margin-top: 8px; flex-wrap: wrap; }
.kg-search { flex: 1; min-width: 120px; padding: 4px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; }
.kg-toggle { font-size: 11px; color: var(--text-secondary); display: flex; align-items: center; gap: 3px; cursor: pointer; }
.kg-stats-inline { font-size: 10px; color: var(--text-tertiary); margin-left: auto; white-space: nowrap; }

/* Detail card — frosted glass overlay, flush with graph right edge */
.kg-detail-overlay {
  position: absolute; top: 0; right: 0; bottom: 0;
  width: 230px; max-height: 100%; overflow-y: auto;
  z-index: 5;
  padding: 14px 14px 12px;
  background: rgba(8,12,20,0.82);
  backdrop-filter: blur(14px) saturate(0.6);
  -webkit-backdrop-filter: blur(14px) saturate(0.6);
  border-left: 1px solid rgba(100,150,210,0.18);
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  box-shadow: -4px 0 24px rgba(0,0,0,0.45), inset 0 0 0 1px rgba(255,255,255,0.03);
}
/* Close button — pinned top-right */
.kg-card-close {
  position: absolute; top: 6px; right: 6px;
  width: 20px; height: 20px; padding: 0; border: none; border-radius: 4px;
  background: transparent; color: rgba(255,255,255,0.35); font-size: 12px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.15s;
}
.kg-card-close:hover { background: rgba(255,255,255,0.08); color: rgba(255,255,255,0.8); }
/* Node icon */
.kg-card-icon { font-size: 18px; margin-bottom: 6px; }
/* Node/edge name */
.kg-card-name { font-size: 14px; font-weight: 600; color: #e8ecf2; line-height: 1.3; word-break: break-word; }
.kg-card-type { font-size: 10px; color: rgba(255,255,255,0.4); text-transform: uppercase; letter-spacing: 0.06em; margin-top: 2px; }
.kg-card-edge-label { font-size: 13px; font-weight: 600; color: #a0c8f0; margin-bottom: 4px; }
.kg-card-edge-path { display: flex; align-items: center; gap: 6px; font-size: 12px; margin-bottom: 2px; flex-wrap: wrap; }
.kg-card-edge-src, .kg-card-edge-dst { color: #d0d8e8; font-weight: 500; }
.kg-card-edge-arrow { color: rgba(255,255,255,0.25); }
.kg-card-divider { height: 1px; background: rgba(255,255,255,0.06); margin: 10px 0; }
.kg-card-meta { font-size: 10px; color: rgba(255,255,255,0.35); display: flex; gap: 4px; align-items: center; margin-top: 6px; }
.kg-card-dot { opacity: 0.4; }
.kg-card-valid { color: rgba(100,200,140,0.7); }
.kg-card-invalid { color: rgba(210,153,34,0.7); }
.kg-card-danger {
  width: 100%; padding: 5px 0; border: 1px solid rgba(255,80,80,0.2); border-radius: var(--radius-sm);
  background: transparent; color: rgba(255,100,100,0.7); font-size: 11px; cursor: pointer;
  transition: all 0.15s;
}
.kg-card-danger:hover { background: rgba(255,60,60,0.12); border-color: rgba(255,80,80,0.4); color: rgba(255,120,120,0.9); }

.kg-detail { }
.kg-detail-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.kg-detail-actions { margin-left: auto; display: flex; gap: 4px; }
.kg-relations { display: flex; flex-direction: column; gap: 2px; max-height: 140px; overflow-y: auto; }
.kg-rel { font-size: 10px; color: rgba(255,255,255,0.5); display: flex; gap: 4px; align-items: center; padding: 3px 0; }
.kg-rel-dir { color: rgba(255,255,255,0.3); flex-shrink: 0; }
.kg-rel-pred { color: rgba(255,255,255,0.7); font-weight: 500; }
.kg-rel-old { color: rgba(255,255,255,0.2); font-style: italic; font-size: 9px; }
.kg-rename { display: flex; gap: 4px; }
.kg-rename .kg-search { font-size: 11px; }

/* Slide-in/out with mode="out-in": old leaves then new enters */
.kg-panel-slide-enter-active { transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1); }
.kg-panel-slide-leave-active { transition: all 0.12s ease-in; }
.kg-panel-slide-enter-from { transform: translateX(30px); opacity: 0; }
.kg-panel-slide-leave-to   { transform: translateX(16px); opacity: 0; }
.kg-core-add { display: flex; gap: 4px; margin: 8px 0; }
.mem-kind.locked { background: var(--accent); color: #fff; }
.kg-stats { font-size: 10px; color: var(--text-tertiary); margin-top: 6px; }
.mem-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.mem-icon { font-size: 16px; }
.mem-title { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.mem-actions { margin-left: auto; display: flex; gap: 6px; }
.mem-hint { font-size: 12px; color: var(--text-tertiary); line-height: 1.6; }
.mem-tabs { display: flex; gap: 4px; margin-bottom: 8px; }
.mem-tab {
  padding: 4px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: transparent; color: var(--text-secondary); font-size: 11px; cursor: pointer;
  transition: all 0.12s;
}
.mem-tab:hover { background: var(--bg-hover); }
.mem-tab.active { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); font-weight: 550; }
.mem-list { display: flex; flex-direction: column; gap: 4px; max-height: 260px; overflow-y: auto; }
.mem-item { display: flex; align-items: flex-start; gap: 8px; padding: 5px 8px; border-radius: var(--radius-sm); background: rgba(255,255,255,0.02); }
.mem-kind {
  flex-shrink: 0; width: 18px; height: 18px; display: inline-flex; align-items: center; justify-content: center;
  border-radius: 4px; font-size: 10px; font-weight: 600; background: var(--accent-dim); color: var(--accent);
}
.mem-kind.fact { background: rgba(48,209,88,0.15); color: var(--success); }
.mem-text { font-size: 12px; color: var(--text-secondary); line-height: 1.5; word-break: break-word; }
.mem-reply { font-size: 10px; color: var(--text-tertiary); line-height: 1.4; word-break: break-word; display: block; margin-top: 2px; }
.mem-text-block { flex: 1; min-width: 0; }
.mem-item .btn-danger, .wiki-pageitem .btn-danger { margin-left: auto; flex-shrink: 0; }

/* 嵌入模型选择行（对话记忆 + KB 卡片共用） */
.mem-model-row, .kb-model-row {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 8px 0; margin-bottom: 6px;
}
.mem-model-row { padding: 0; margin: 8px 0 4px; }
.kb-model-row { padding: 12px 18px 6px; border-bottom: 1px solid var(--border-subtle); }
.mem-model-label, .kb-model-label { font-size: 11px; color: var(--text-tertiary); font-weight: 500; }
.mem-model-select, .kb-model-select {
  flex: 1; min-width: 140px; max-width: 280px;
  padding: 4px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-secondary);
  font-size: 11px; font-family: var(--font-mono); outline: none; cursor: pointer;
}
.mem-model-select:focus, .kb-model-select:focus { border-color: var(--accent); color: var(--text-primary); }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.toolbar-actions { display: flex; gap: 8px; }

/* 新建面板 */
.create-panel { padding: 18px 20px; margin-bottom: 20px; }
.cp-title { font-size: 14px; font-weight: 600; margin-bottom: 14px; }
.cp-row { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.cp-label { font-size: 12px; color: var(--text-secondary); min-width: 64px; font-weight: 500; }
.cp-input {
  flex: 1; padding: 7px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: rgba(255,255,255,0.04); color: var(--text-primary); font-size: 13px; outline: none;
  transition: border-color var(--transition);
}
.cp-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.cp-select {
  flex: 1; padding: 7px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: rgba(255,255,255,0.04); color: var(--text-primary); font-size: 13px; outline: none;
  font-family: var(--font-mono); cursor: pointer;
}
.cp-select:focus { border-color: var(--accent); }
.cp-hint { font-size: 11px; color: var(--text-tertiary); }
.cp-notice {
  display: flex; align-items: flex-start; gap: 8px;
  margin-top: 12px; padding: 10px 12px;
  background: var(--accent-dim); border-radius: var(--radius-sm);
  font-size: 12px; color: var(--text-secondary); line-height: 1.5;
}
.cp-notice-icon { color: var(--accent); font-weight: 700; flex-shrink: 0; font-style: normal; }
.cp-notice strong { color: var(--text-primary); font-weight: 550; }
.cp-actions { display: flex; gap: 8px; margin-top: 16px; }

/* 空状态 */
.empty { text-align: center; padding: 80px 0; }
.empty-icon { font-size: 40px; opacity: 0.25; margin-bottom: 12px; }
.empty-title { font-size: 15px; font-weight: 500; margin-bottom: 4px; }
.empty-hint { font-size: 13px; color: var(--text-tertiary); }

/* KB accordion */
.kb-accordion { display: flex; flex-direction: column; gap: 2px; }
.kb-acc-item { border-radius: var(--radius-sm); overflow: hidden; }
.kb-acc-head {
  display: flex; align-items: center; gap: 8px; padding: 6px 10px;
  cursor: pointer; border-radius: var(--radius-sm); transition: background 0.12s;
  font-size: 12px;
}
.kb-acc-head:hover { background: rgba(255,255,255,0.04); }
.kb-acc-arrow { font-size: 10px; color: var(--text-tertiary); width: 14px; flex-shrink: 0; }
.kb-acc-name { font-weight: 550; color: var(--text-primary); flex: 1; }
.kb-acc-model { font-size: 10px; padding: 1px 6px; border-radius: 8px; flex-shrink: 0; }
.kb-model-bound { background: #1a2a1a; color: #5a5; }
.kb-model-unbound { background: #2a1a1a; color: #a55; }
.kb-acc-count { font-size: 10px; color: var(--text-tertiary); }

/* RAG result items */
.rag-result-item { cursor: pointer; transition: background .15s; flex-direction: column; align-items: stretch; }
.rag-result-item:hover { background: #1e1e24; }
.kb-acc-body { padding: 8px 10px 4px; border-top: 1px solid var(--border-subtle); }
.kb-model-row { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; }
.kb-model-label { font-size: 11px; color: var(--text-tertiary); flex-shrink: 0; }
.kb-model-select {
  flex: 1; padding: 3px 6px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-secondary); font-size: 11px; outline: none; cursor: pointer;
}
.kb-model-select:focus { border-color: var(--accent); }

/* Search row */
.kb-search-row { display: flex; gap: 8px; }
.kb-search-input {
  flex: 1; padding: 8px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: rgba(255,255,255,0.04); color: var(--text-primary); font-size: 13px; outline: none;
  transition: border-color var(--transition);
}
.kb-search-input:focus { border-color: var(--accent); }

.btn-danger { color: var(--danger) !important; }

/* Wiki document browser */
.wiki-panel { padding-bottom: 10px; }
.wiki-search-row { display: flex; gap: 6px; margin-bottom: 8px; }
.wiki-search-input { flex: 1; }
.wiki-results { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; max-height: 240px; overflow-y: auto; }
.wiki-result-item {
  padding: 8px 10px; border-radius: var(--radius-sm); cursor: pointer;
  background: var(--bg-inset); border: 1px solid var(--border-subtle);
  transition: border-color 0.15s;
}
.wiki-result-item:hover { border-color: var(--accent); }
.wiki-result-head { display: flex; gap: 4px; align-items: baseline; margin-bottom: 4px; }
.wiki-result-page { font-weight: 600; font-size: 12px; color: var(--accent); }
.wiki-result-heading { font-size: 11px; color: var(--text-secondary); }
.wiki-result-snippet { font-size: 11px; color: var(--text-tertiary); line-height: 1.45; }
.wiki-browser { display: flex; gap: 0; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); overflow: hidden; min-height: 200px; max-height: 420px; }
.wiki-pagelist { width: 180px; flex-shrink: 0; overflow-y: auto; border-right: 1px solid var(--border-soft); background: var(--bg-inset); }
.wiki-pageitem {
  display: flex; align-items: center; gap: 6px; padding: 6px 10px;
  cursor: pointer; font-size: 12px; color: var(--text-secondary);
  border-bottom: 1px solid var(--border-subtle); transition: background 0.12s;
}
.wiki-pageitem:hover { background: var(--bg-hover); color: var(--text-primary); }
.wiki-pageitem.active { background: var(--accent-dim); color: var(--accent); font-weight: 550; }
.wiki-pageicon { flex-shrink: 0; font-size: 13px; }
.wiki-pagename { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.wiki-pagechunks { font-size: 9px; color: var(--text-tertiary); flex-shrink: 0; }
.wiki-content { flex: 1; overflow-y: auto; padding: 12px 16px; }
.wiki-content-empty { display: flex; align-items: center; justify-content: center; color: var(--text-tertiary); font-size: 13px; }
.wiki-content-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; padding-bottom: 8px; border-bottom: 1px solid var(--border-subtle); font-size: 13px; font-weight: 600; }
.wiki-content-body { font-size: 13px; line-height: 1.65; color: var(--text-primary); }
.wiki-content-body :deep(h1) { font-size: 18px; margin: 16px 0 8px; }
.wiki-content-body :deep(h2) { font-size: 15px; margin: 14px 0 6px; }
.wiki-content-body :deep(h3) { font-size: 13px; margin: 10px 0 5px; }
.wiki-content-body :deep(p) { margin: 0 0 8px; }
.wiki-content-body :deep(ul), .wiki-content-body :deep(ol) { margin: 6px 0; padding-left: 20px; }
.wiki-content-body :deep(li) { margin-bottom: 3px; }
.wiki-content-body :deep(code) { font-size: 11px; background: rgba(255,255,255,0.08); padding: 1px 5px; border-radius: 3px; }
.wiki-content-body :deep(pre) { margin: 8px 0; padding: 10px; background: rgba(0,0,0,0.25); border-radius: var(--radius-sm); font-size: 11px; overflow-x: auto; }
.wiki-content-body :deep(blockquote) { margin: 8px 0; padding: 6px 12px; border-left: 3px solid var(--accent); background: rgba(0,122,255,0.06); border-radius: 0 6px 6px 0; }
.wiki-content-body :deep(a) { color: var(--accent); }
.wiki-page-del { flex-shrink: 0; }
.wiki-edit-title {
  flex: 1; padding: 4px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-primary); font-size: 13px; outline: none;
}
.wiki-edit-title:focus { border-color: var(--accent); }
.wiki-edit-body {
  width: 100%; min-height: 240px; padding: 10px 12px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-primary); font-family: var(--font-mono);
  font-size: 12px; line-height: 1.6; resize: vertical; outline: none;
}
.wiki-edit-body:focus { border-color: var(--accent); }

/* Hide embedded LLMAgents toolbar + cards, keep dialog overlay accessible */
.embedded-agents :deep(.agents-toolbar) { display: none; }
.embedded-agents :deep(.agents-list) { display: none; }

</style>
