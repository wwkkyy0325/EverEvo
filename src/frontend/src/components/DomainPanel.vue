<template>
  <div class="domain-panel">
    <h2>领域管理</h2>
    <div v-if="err" class="dp-err">{{ err }}</div>
    <div v-else-if="!ready">加载中…</div>
    <div v-else>
      <!-- ═══════ 领域选择 + 领域内容 ═══════ -->
      <select v-model="activeLibId" @change="onSwitch" class="dp-select">
        <option v-for="lib in libs" :key="lib.id" :value="lib.id">{{ lib.icon || '📚' }} {{ lib.name }}</option>
      </select>

      <div class="dp-cards">
        <div class="dp-card">📄 知识库: {{ kbs.length }}</div>
        <div class="dp-card">⬡ 智能体: {{ agents.length }}</div>
        <div class="dp-card">⇌ MCP: {{ mcps.length }}</div>
        <div class="dp-card">⚡ 技能: {{ skills.length }}</div>
      </div>

      <!-- ─── 知识库列表 ─── -->
      <div class="glass-panel kb-section">
        <div class="mem-head">
          <span class="mem-icon">📚</span>
          <span class="mem-title">知识库</span>
          <span class="tag tag-accent">{{ kbs.length }} 个</span>
          <div class="mem-actions">
            <button class="btn btn-sm btn-primary" @click="showCreateKB = true" :disabled="busy">+ 新建</button>
          </div>
        </div>

        <!-- Create KB dialog -->
        <div v-if="showCreateKB" class="kb-create-row">
          <input v-model="newKBName" type="text" placeholder="知识库名称" class="field kb-input" @keyup.enter="doCreateKB" />
          <input v-model="newKBModelDir" type="text" placeholder="模型目录（可选）" class="field kb-input" />
          <button class="btn btn-sm btn-primary" @click="doCreateKB" :disabled="!newKBName.trim() || busy">创建</button>
          <button class="btn btn-sm" @click="showCreateKB = false">取消</button>
        </div>

        <!-- KB list -->
        <div v-if="kbs.length" class="kb-list">
          <div v-for="kb in kbs" :key="kb.id" class="kb-item" :class="{ 'kb-expanded': expandedKB === kb.id }">
            <div class="kb-item-head" @click="toggleKB(kb.id)">
              <span class="kb-icon">{{ expandedKB === kb.id ? '📖' : '📄' }}</span>
              <div class="kb-info">
                <span class="kb-name">{{ kb.name }}</span>
                <span class="kb-meta">ID: {{ kb.id.slice(0, 12) }}… · 模型: {{ kb.modelDir || '默认' }}</span>
              </div>
              <span class="kb-doccnt">{{ kb.docCount ?? '?' }} 文档</span>
              <button class="btn btn-xs btn-danger kb-del" @click.stop="doDeleteKB(kb.id)" title="删除知识库">×</button>
            </div>
            <!-- Expanded: search + document list -->
            <div v-if="expandedKB === kb.id" class="kb-detail">
              <div class="kb-search-row">
                <input v-model="kbSearchQueries[kb.id]" class="kg-search kb-search-input" placeholder="语义搜索…" @keyup.enter="doSearchKB(kb.id)" />
                <button class="btn btn-xs btn-primary" @click="doSearchKB(kb.id)" :disabled="busy">检索</button>
              </div>
              <div v-if="kbSearchResults[kb.id]?.length" class="kb-results">
                <div v-for="(r, i) in kbSearchResults[kb.id]" :key="i" class="kb-result-item">
                  <div class="kb-result-src">{{ r.source || r.metadata?.source }}</div>
                  <div class="kb-result-text">{{ r.content.slice(0, 300) }}{{ r.content.length > 300 ? '…' : '' }}</div>
                  <div class="kb-result-sim">相似度: {{ (r.similarity * 100).toFixed(1) }}%</div>
                </div>
              </div>
              <div v-else-if="kbSearched[kb.id]" class="mem-hint">无匹配结果</div>
              <button class="btn btn-xs kb-docs-btn" @click="loadKBDocs(kb.id)" :disabled="busy">
                {{ kbDocs[kb.id] ? '刷新文档列表' : '查看文档列表' }}
              </button>
              <div v-if="kbDocs[kb.id]?.length" class="kb-docs">
                <div v-for="d in kbDocs[kb.id]" :key="d.id" class="kb-doc-item">
                  <span class="kb-doc-src">{{ d.metadata?.source || d.metadata?.filename || d.id.slice(0, 8) }}</span>
                  <span class="kb-doc-preview">{{ d.content.slice(0, 120) }}{{ d.content.length > 120 ? '…' : '' }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="mem-hint">暂无知识库，点击上方按钮创建</div>
      </div>

      <!-- ─── 智能体列表 ─── -->
      <div v-if="agents.length" class="glass-panel">
        <div class="mem-head">
          <span class="mem-icon">⬡</span>
          <span class="mem-title">智能体</span>
          <span class="tag tag-accent">{{ agents.length }} 个</span>
        </div>
        <div v-for="a in agents" :key="a.id" class="dp-item">{{ a.icon || '⬡' }} {{ a.name }} <span class="dp-item-hint">{{ a.description || '' }}</span></div>
      </div>

      <!-- ─── MCP 列表 ─── -->
      <div v-if="mcps.length" class="glass-panel">
        <div class="mem-head">
          <span class="mem-icon">⇌</span>
          <span class="mem-title">MCP 服务器</span>
          <span class="tag tag-accent">{{ mcps.length }} 个</span>
        </div>
        <div v-for="m in mcps" :key="m.id" class="dp-item">⇌ {{ m.name }} <span class="tag" :class="m.status === 'connected' ? 'tag-green' : 'tag-dim'">{{ m.status }}</span></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue'
import { memoryApi } from '../api/memory'
import { knowledgeApi } from '../api/knowledge'
import { agentsApi } from '../api/agents'
import { mcpApi } from '../api/mcp'
import { skillsApi } from '../api/skills'

const libs = ref<any[]>([])
const activeLibId = ref('')
const kbs = ref<any[]>([])
const agents = ref<any[]>([])
const mcps = ref<any[]>([])
const skills = ref<any[]>([])
const err = ref('')
const ready = ref(false)
const busy = ref(false)


// KB management state
const showCreateKB = ref(false)
const newKBName = ref('')
const newKBModelDir = ref('')
const expandedKB = ref('')
const kbSearchQueries = reactive<Record<string, string>>({})
const kbSearchResults = reactive<Record<string, any[]>>({})
const kbSearched = reactive<Record<string, boolean>>({})
const kbDocs = reactive<Record<string, any[]>>({})

const activeLib = computed(() => libs.value.find((l: any) => l.id === activeLibId.value))

onMounted(async () => {
  try {
    libs.value = await memoryApi.libraryList()
    if (libs.value.length) activeLibId.value = libs.value[0].id
    await loadAll()
    ready.value = true
  } catch (e: any) {
    err.value = e?.message || String(e)
    console.error('[DomainPanel]', e)
    ready.value = true
  }
})

async function loadAll() {
  const libId = activeLibId.value
  if (!libId) return
  try { kbs.value = await knowledgeApi.list(libId) } catch (e: any) { console.warn('kb:', e?.message) }
  try { agents.value = await agentsApi.listByLibrary(libId) } catch (e: any) { console.warn('agents:', e?.message) }
  try { mcps.value = await mcpApi.listServers(libId) } catch (e: any) { console.warn('mcp:', e?.message) }
  try { skills.value = await skillsApi.list(libId) } catch (e: any) { console.warn('skills:', e?.message) }
}

async function onSwitch() {
  if (!activeLibId.value) return
  // Reset KB detail state
  expandedKB.value = ''
  for (const k of Object.keys(kbSearchResults)) delete kbSearchResults[k]
  for (const k of Object.keys(kbSearched)) delete kbSearched[k]
  for (const k of Object.keys(kbDocs)) delete kbDocs[k]
  await loadAll()
}

// ─── KB CRUD ───
async function doCreateKB() {
  if (!newKBName.value.trim()) return
  busy.value = true
  try {
    await knowledgeApi.create(newKBName.value.trim(), newKBModelDir.value.trim(), activeLibId.value)
    newKBName.value = ''
    newKBModelDir.value = ''
    showCreateKB.value = false
    kbs.value = await knowledgeApi.list(activeLibId.value)
  } catch (e: any) { alert('创建失败: ' + (e?.message || e)) }
  finally { busy.value = false }
}

async function doDeleteKB(kbId: string) {
  if (!confirm('确定删除该知识库及其所有文档？此操作不可恢复。')) return
  busy.value = true
  try {
    await knowledgeApi.delete(kbId)
    kbs.value = await knowledgeApi.list(activeLibId.value)
    if (expandedKB.value === kbId) expandedKB.value = ''
  } catch (e: any) { alert('删除失败: ' + (e?.message || e)) }
  finally { busy.value = false }
}

function toggleKB(kbId: string) {
  expandedKB.value = expandedKB.value === kbId ? '' : kbId
}

async function doSearchKB(kbId: string) {
  const q = (kbSearchQueries[kbId] || '').trim()
  if (!q) return
  busy.value = true
  try {
    kbSearchResults[kbId] = await knowledgeApi.search(kbId, q, 5)
    kbSearched[kbId] = true
  } catch (e: any) { console.warn('search:', e?.message) }
  finally { busy.value = false }
}

async function loadKBDocs(kbId: string) {
  busy.value = true
  try {
    kbDocs[kbId] = await knowledgeApi.listDocuments(kbId)
  } catch (e: any) { console.warn('docs:', e?.message) }
  finally { busy.value = false }
}
</script>

<style scoped>
.domain-panel { padding: 24px; max-width: 960px; margin: 0 auto; color: #e0e0e0; }
.dp-err { background:#3a1a1a; border:1px solid #5a2a2a; color:#f07070; padding:16px; border-radius:8px; margin-bottom:16px; }
.dp-select { width:100%; padding:8px 12px; background:#1a1a1e; border:1px solid #333; border-radius:6px; color:#e0e0e0; font-size:0.95em; margin-bottom:16px; }
.dp-cards { display:flex; gap:12px; margin-bottom:16px; flex-wrap:wrap; }
.dp-card { background:#1a1a1e; border:1px solid #2a2a2e; border-radius:8px; padding:12px 16px; font-size:0.9em; }
.dp-item { padding:8px 12px; background:#1a1a1e; border:1px solid #2a2a2e; border-radius:5px; margin-bottom:4px; font-size:0.88em; display:flex; justify-content:space-between; align-items:center; }
.dp-item-hint { color: #888; font-size: 0.82em; }

/* Glass panel (reuse pattern from Knowledge.vue) */
.glass-panel { background: rgba(26,26,30,0.85); border: 1px solid #2a2a2e; border-radius: 10px; padding: 16px; margin-bottom: 16px; }
.mem-head { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.mem-icon { font-size: 1.1em; }
.mem-title { font-weight: 600; }
.mem-actions { margin-left: auto; display: flex; gap: 6px; }
.mem-hint { color: #666; font-size: 0.85em; padding: 12px; }

/* KB section */
.kb-section { margin-top: 4px; }
.kb-create-row { display: flex; gap: 8px; margin-bottom: 12px; align-items: center; }
.kb-input { flex: 1; min-width: 120px; background: #121215; border: 1px solid #333; color: #e0e0e0; padding: 6px 10px; border-radius: 5px; font-size: 0.85em; }
.kb-list { display: flex; flex-direction: column; gap: 4px; }
.kb-item { background: #121215; border: 1px solid #2a2a2e; border-radius: 8px; overflow: hidden; }
.kb-item.kb-expanded { border-color: var(--accent-dim, #3a3a5a); }
.kb-item-head { display: flex; align-items: center; gap: 10px; padding: 10px 12px; cursor: pointer; transition: background .15s; }
.kb-item-head:hover { background: #1a1a20; }
.kb-icon { font-size: 1.1em; flex-shrink: 0; }
.kb-info { flex: 1; min-width: 0; }
.kb-name { font-weight: 600; font-size: 0.9em; display: block; }
.kb-meta { color: #777; font-size: 0.75em; }
.kb-doccnt { font-size: 0.78em; color: #888; flex-shrink: 0; }
.kb-del { flex-shrink: 0; opacity: 0.5; transition: opacity .15s; }
.kb-item-head:hover .kb-del { opacity: 1; }

.kb-detail { padding: 0 12px 12px; border-top: 1px solid #2a2a2e; }
.kb-search-row { display: flex; gap: 6px; margin: 10px 0; }
.kb-search-input { flex: 1; background: #0a0a0e; border: 1px solid #333; color: #e0e0e0; padding: 5px 10px; border-radius: 5px; font-size: 0.85em; }

.kb-results { display: flex; flex-direction: column; gap: 6px; margin-bottom: 8px; max-height: 300px; overflow-y: auto; }
.kb-result-item { background: #0a0a0e; border-radius: 6px; padding: 8px 10px; border: 1px solid #222; }
.kb-result-src { font-size: 0.75em; color: var(--accent, #7aa2f7); margin-bottom: 3px; }
.kb-result-text { font-size: 0.82em; color: #ccc; line-height: 1.4; }
.kb-result-sim { font-size: 0.72em; color: #666; margin-top: 4px; text-align: right; }

.kb-docs-btn { margin: 6px 0; background: #1a1a2e; color: #aaa; border: 1px solid #333; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 0.8em; }
.kb-docs { max-height: 200px; overflow-y: auto; }
.kb-doc-item { display: flex; gap: 10px; padding: 5px 8px; border-bottom: 1px solid #1a1a1e; font-size: 0.8em; }
.kb-doc-src { color: var(--accent, #7aa2f7); white-space: nowrap; max-width: 140px; overflow: hidden; text-overflow: ellipsis; }
.kb-doc-preview { color: #999; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* Shared tags */
.tag { font-size: 0.75em; padding: 2px 8px; border-radius: 10px; background: #222; color: #aaa; }
.tag-accent { background: var(--accent-dim, #2a2a4a); color: var(--accent, #7aa2f7); }
.tag-green { background: #1a2a1a; color: #5a5; }
.tag-dim { background: #1a1a1a; color: #666; }

/* Buttons (inline, to avoid dependency on global btn styles) */
.btn { border: 1px solid #444; background: #1a1a1e; color: #ccc; padding: 4px 12px; border-radius: 5px; cursor: pointer; font-size: 0.82em; transition: all .15s; }
.btn:hover { background: #2a2a30; }
.btn-primary { background: var(--accent, #7aa2f7); border-color: var(--accent, #7aa2f7); color: #111; }
.btn-primary:hover { opacity: 0.85; }
.btn-sm { padding: 4px 10px; font-size: 0.8em; }
.btn-xs { padding: 2px 7px; font-size: 0.72em; }
.btn-danger { border-color: #5a2a2a; color: #f07070; }
.btn-danger:hover { background: #3a1a1a; }

.field { background: #121215; border: 1px solid #333; border-radius: 5px; padding: 6px 10px; color: #e0e0e0; }

.kg-search { background: #0a0a0e; border: 1px solid #333; border-radius: 5px; color: #e0e0e0; padding: 4px 10px; font-size: 0.85em; }
</style>
