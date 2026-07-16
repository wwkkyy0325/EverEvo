<template>
  <div class="guide-center">
    <header class="page-head">
      <div class="head-left">
        <h2 class="title">攻略中心</h2>
        <p class="subtitle">同步远程仓库的攻略和学习文档，供 AI 助手阅读参考</p>
      </div>
      <div class="head-actions">
        <button class="btn btn-sm btn-primary" @click="syncAll" :disabled="syncing">
          {{ syncing ? '同步中…' : '同步全部' }}
        </button>
        <button class="btn btn-sm" @click="showAddSource = true">添加来源</button>
        <button class="btn btn-sm" @click="openDir">打开目录</button>
      </div>
    </header>

    <div class="guide-layout">
      <!-- Left Panel: Sources + Guide List -->
      <div class="guide-left">
        <!-- Search -->
        <div class="search-row">
          <input v-model="searchQuery" type="text" class="search-input" placeholder="搜索攻略…" @input="doSearch" />
        </div>

        <!-- Source List (collapsible) -->
        <div class="source-section" v-if="sources.length">
          <div class="source-head" @click="showSources = !showSources">
            <span class="source-arrow">{{ showSources ? '▾' : '▸' }}</span>
            <span>来源 ({{ sources.length }})</span>
          </div>
          <div v-if="showSources" class="source-list">
            <div v-for="s in sources" :key="s.name" class="source-item" :class="{ disabled: !s.enabled }">
              <div class="src-info">
                <span class="src-name">{{ s.title || s.name }}</span>
                <span class="src-url" :title="s.url">{{ s.url }}</span>
              </div>
              <div class="src-actions">
                <button class="btn btn-xs" @click="syncOne(s.name)" title="同步">↻</button>
                <button class="btn btn-xs btn-danger" @click="removeSource(s.name)" title="删除">✕</button>
              </div>
            </div>
          </div>
        </div>

        <!-- Guide List -->
        <div class="guide-list">
          <div v-if="guides.length === 0 && !loading" class="empty-list">
            <span>暂无攻略文档</span>
            <span class="hint">添加来源后点击「同步全部」</span>
          </div>
          <div v-for="g in guides" :key="g.id"
            class="guide-item" :class="{ active: activeId === g.id }"
            @click="selectGuide(g)">
            <div class="gi-title">{{ g.title }}</div>
            <div class="gi-meta">
              <span class="gi-source">{{ g.source }}</span>
              <span class="gi-size">{{ fmtSize(g.size) }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Right Panel: Content Reader -->
      <div class="guide-right">
        <div v-if="!activeGuide" class="empty-reader">
          <div class="empty-icon">📖</div>
          <div class="empty-title">选择一篇攻略</div>
          <div class="empty-hint">从左侧列表点击文档即可阅读</div>
        </div>
        <div v-else class="reader">
          <div class="reader-head">
            <h3 class="reader-title">{{ activeGuide.title }}</h3>
            <span class="reader-source">{{ activeGuide.source }} / {{ activeGuide.path }}</span>
          </div>
          <div class="reader-body">
            <div v-if="loadingContent" class="loading">加载中…</div>
            <div v-else class="markdown-body" v-html="renderedContent"></div>
          </div>
        </div>
      </div>
    </div>

    <!-- Add Source Dialog -->
    <DialogModal v-model:visible="showAddSource" title="添加攻略来源" okLabel="添加并同步" :okDisabled="!newSource.name || !newSource.url" @ok="addSource" @cancel="showAddSource = false">
      <div class="form">
        <div class="form-row">
          <label>名称（唯一标识）</label>
          <input v-model="newSource.name" type="text" placeholder="例如 my-guides" class="field" />
        </div>
        <div class="form-row">
          <label>标题</label>
          <input v-model="newSource.title" type="text" placeholder="例如 我的攻略库" class="field" />
        </div>
        <div class="form-row">
          <label>类型</label>
          <select v-model="newSource.type" class="field">
            <option value="git">Git 仓库 (Gitee/GitHub)</option>
            <option value="url">单文件 URL</option>
          </select>
        </div>
        <div class="form-row">
          <label>URL</label>
          <input v-model="newSource.url" type="text"
            :placeholder="newSource.type === 'git' ? 'https://gitee.com/user/repo.git' : 'https://example.com/guide.md'"
            class="field" />
        </div>
        <div v-if="newSource.type === 'git'" class="form-row">
          <label>分支</label>
          <input v-model="newSource.branch" type="text" placeholder="main" class="field field-sm" />
        </div>
      </div>
      <div v-if="addMsg" class="msg" :class="addOk ? 'msg-ok' : 'msg-err'">{{ addMsg }}</div>
    </DialogModal>

    <!-- Sync Results Toast -->
    <div v-if="syncResults.length" class="sync-results glass-panel">
      <div class="sync-head">
        <span>同步结果</span>
        <button class="btn btn-xs" @click="syncResults = []">✕</button>
      </div>
      <div v-for="r in syncResults" :key="r" class="sync-line">{{ r }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { marked } from 'marked'
import DialogModal from './DialogModal.vue'
import { skillsApi } from '../api/skills'
import { fmtSize } from '../utils/formatters'
import { useDataChanged } from '../composables/useDataChanged'

const sources = ref<any[]>([]); const guides = ref<any[]>([]); const searchQuery = ref('')
const activeId = ref(''); const activeGuide = ref<any>(null); const renderedContent = ref('')
const loading = ref(false); const loadingContent = ref(false); const syncing = ref(false)
const syncResults = ref<string[]>([]); const showSources = ref(true); const showAddSource = ref(false)
const newSource = reactive({ name:'',title:'',url:'',branch:'main',type:'git' })
const addMsg = ref(''); const addOk = ref(false)
let _searchTimer: ReturnType<typeof setTimeout> | null = null

async function loadAll() { loading.value = true; try { sources.value = await skillsApi.listGuideSources() || []; guides.value = await skillsApi.listGuides('') || [] } catch (_) {}; loading.value = false }
function doSearch() { if (_searchTimer) clearTimeout(_searchTimer); _searchTimer = setTimeout(() => { _searchTimer = null; skillsApi.listGuides(searchQuery.value).then((list: any) => { guides.value = list || [] }).catch(() => {}) }, 300) }
async function selectGuide(g: any) { activeId.value = g.id; activeGuide.value = g; loadingContent.value = true; renderedContent.value = ''; try { const content = await skillsApi.readGuide(g.id); renderedContent.value = marked.parse(content || '') as string } catch (e: any) { renderedContent.value = '<p class="err">读取失败: ' + (e.message || e) + '</p>' }; loadingContent.value = false }
async function syncAll() { syncing.value = true; try { syncResults.value = await skillsApi.syncGuides() || []; await loadAll() } catch (e: any) { syncResults.value = ['同步失败: ' + (e.message || e)] }; syncing.value = false }
async function syncOne(name: string) { try { syncResults.value = [await skillsApi.syncOneGuide(name)]; await loadAll() } catch (e: any) { syncResults.value = ['同步失败: ' + (e.message || e)] } }
async function addSource() { try { await skillsApi.addGuideSource(newSource.name, newSource.title || newSource.name, newSource.url, newSource.branch || 'main', newSource.type); showAddSource.value = false; Object.assign(newSource, { name:'',title:'',url:'',branch:'main',type:'git' }); addMsg.value = ''; addOk.value = false; syncResults.value = [await skillsApi.syncOneGuide(newSource.name || '')]; await loadAll() } catch (e: any) { addMsg.value = '添加失败: ' + (e.message || e); addOk.value = false } }
async function removeSource(name: string) { try { await skillsApi.removeGuideSource(name); await loadAll() } catch (e: any) {} }
function openDir() { skillsApi.openGuidesDir().catch(() => {}) }
useDataChanged('guides:changed', () => { loadAll() })
onMounted(() => loadAll())
onBeforeUnmount(() => { if (_searchTimer) { clearTimeout(_searchTimer); _searchTimer = null } })
</script>

<style scoped>
.guide-center { display: flex; flex-direction: column; gap: 16px; height: 100%; }

/* ─── Header ─── */
.page-head {
  display: flex; align-items: flex-start; justify-content: space-between; gap: 16px;
  flex-shrink: 0;
}
.head-left { display: flex; flex-direction: column; gap: 4px; }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.subtitle { font-size: 12px; color: var(--text-tertiary); font-weight: 450; }
.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

/* ─── Layout ─── */
.guide-layout {
  display: flex; gap: 16px; flex: 1; min-height: 0;
}

/* ─── Left Panel ─── */
.guide-left {
  width: clamp(220px, 28vw, 320px); flex-shrink: 0;
  display: flex; flex-direction: column; gap: 10px;
  overflow-y: auto;
}
.search-row { flex-shrink: 0; }
.search-input {
  width: 100%; padding: 7px 10px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 12px; outline: none; box-sizing: border-box;
}
.search-input:focus { border-color: var(--accent); }

/* Sources */
.source-section { display: flex; flex-direction: column; flex-shrink: 0; }
.source-head {
  font-size: 12px; font-weight: 600; color: var(--text-secondary);
  padding: 4px 2px; cursor: pointer; user-select: none;
  display: flex; align-items: center; gap: 4px;
}
.source-arrow { font-size: 10px; width: 12px; }
.source-list { display: flex; flex-direction: column; gap: 3px; margin-top: 4px; }
.source-item {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  padding: 6px 8px; border-radius: 6px; font-size: 11px;
  background: var(--bg-elevated);
}
.source-item.disabled { opacity: 0.5; }
.src-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.src-name { font-weight: 500; color: var(--text-primary); }
.src-url { color: var(--text-tertiary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 10px; }
.src-actions { display: flex; gap: 2px; flex-shrink: 0; }

/* Guide List */
.guide-list { flex: 1; display: flex; flex-direction: column; gap: 3px; overflow-y: auto; }
.empty-list {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  padding: 20px 0; color: var(--text-tertiary); font-size: 12px;
}
.empty-list .hint { font-size: 11px; opacity: 0.7; }

.guide-item {
  padding: 8px 10px; border-radius: 6px; cursor: pointer;
  transition: background var(--transition);
}
.guide-item:hover { background: var(--bg-hover); }
.guide-item.active { background: var(--bg-active); }
.gi-title {
  font-size: 12px; font-weight: 500; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.gi-meta { display: flex; gap: 8px; margin-top: 2px; }
.gi-source { font-size: 10px; color: var(--accent); }
.gi-size { font-size: 10px; color: var(--text-tertiary); }

/* ─── Right Panel (Reader) ─── */
.guide-right { flex: 1; min-width: 0; overflow-y: auto; }
.empty-reader {
  display: flex; flex-direction: column; align-items: center; gap: 10px;
  padding: 60px 20px; color: var(--text-tertiary); text-align: center;
}
.empty-icon { font-size: 48px; opacity: 0.3; }
.empty-title { font-size: 16px; font-weight: 500; color: var(--text-secondary); }
.empty-hint { font-size: 12px; }

.reader { display: flex; flex-direction: column; gap: 14px; }
.reader-head {
  display: flex; flex-direction: column; gap: 4px;
  padding-bottom: 12px; border-bottom: 1px solid var(--border-subtle);
}
.reader-title { font-size: 18px; font-weight: 600; margin: 0; }
.reader-source { font-size: 11px; color: var(--text-tertiary); font-family: var(--font-mono); }
.loading { padding: 20px; color: var(--text-tertiary); text-align: center; }

/* Markdown styling */
.markdown-body { line-height: 1.7; font-size: 13px; color: var(--text-primary); }
.markdown-body :deep(h1) { font-size: 20px; font-weight: 600; margin: 16px 0 8px; }
.markdown-body :deep(h2) { font-size: 17px; font-weight: 600; margin: 14px 0 6px; }
.markdown-body :deep(h3) { font-size: 14px; font-weight: 600; margin: 12px 0 4px; }
.markdown-body :deep(p) { margin: 6px 0; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { padding-left: 20px; margin: 6px 0; }
.markdown-body :deep(li) { margin: 2px 0; }
.markdown-body :deep(code) {
  font-family: var(--font-mono); font-size: 11px;
  background: var(--bg-elevated); padding: 1px 6px; border-radius: 3px;
}
.markdown-body :deep(pre) {
  background: var(--bg-elevated); padding: 12px 14px; border-radius: 6px;
  overflow-x: auto; margin: 10px 0;
}
.markdown-body :deep(pre code) { background: none; padding: 0; }
.markdown-body :deep(blockquote) {
  border-left: 3px solid var(--accent); padding: 4px 12px; margin: 8px 0;
  color: var(--text-secondary); background: rgba(255,255,255,0.02);
}
.markdown-body :deep(a) { color: var(--accent); text-decoration: underline; }
.markdown-body :deep(table) { border-collapse: collapse; margin: 8px 0; font-size: 12px; }
.markdown-body :deep(th), .markdown-body :deep(td) {
  border: 1px solid var(--border-soft); padding: 4px 10px; text-align: left;
}
.markdown-body :deep(th) { background: var(--bg-elevated); font-weight: 600; }
.markdown-body :deep(img) { max-width: 100%; border-radius: 6px; }
.markdown-body :deep(hr) { border: none; border-top: 1px solid var(--border-subtle); margin: 14px 0; }
.markdown-body .err { color: var(--danger); }

/* ─── Form ─── */
.form { display: flex; flex-direction: column; gap: 12px; }
.form-row { display: flex; flex-direction: column; gap: 4px; }
.form-row label { font-size: 11px; font-weight: 500; color: var(--text-tertiary); }
.field {
  padding: 6px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 12px; outline: none; font-family: var(--font-mono);
}
.field:focus { border-color: var(--accent); }
.field-sm { max-width: 120px; }
.msg { padding: 8px 12px; border-radius: var(--radius-sm); font-size: 12px; margin-top: 10px; }
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }

/* Sync results overlay */
.sync-results {
  position: fixed; bottom: 20px; right: 20px; z-index: 90;
  padding: 12px 16px; max-width: 360px; max-height: 200px; overflow-y: auto;
  font-size: 12px;
}
.sync-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; font-weight: 600; }
.sync-line { font-size: 11px; color: var(--text-secondary); padding: 2px 0; }
</style>
