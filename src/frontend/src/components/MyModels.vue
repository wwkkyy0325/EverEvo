<template>
  <div class="my-models">
    <div class="toolbar">
      <h2 class="title">我的模型</h2>
      <div class="toolbar-actions">
        <button type="button" class="btn" @click="importModel">手动导入模型</button>
      </div>
    </div>

    <!-- Tabs -->
    <div class="mm-tabs">
      <button :class="['mm-tab', { active: tab === 'models' }]" @click="tab = 'models'">□ 模型</button>
      <button :class="['mm-tab', { active: tab === 'downloads' }]" @click="tab = 'downloads'">
        ↓ 下载中心
        <span v-if="downloadCount" class="mm-tab-badge">{{ downloadCount }}</span>
      </button>
    </div>

    <!-- ═══ Tab 1: Models ═══ -->
    <div v-show="tab === 'models'">
      <!-- 已加载的模型 -->
      <div v-if="models.length" class="cards">
        <ModelCard
          v-for="m in models"
          :key="m.id"
          :model="m"
          @unload="id => doUnload(id)"
        />
      </div>

      <!-- 已下载的模型包 -->
      <section v-if="packages.length" class="pkg-section">
        <h4 class="section-title">已下载的模型包</h4>
        <div class="pkg-list">
          <div v-for="pkg in packages" :key="pkg.name" class="pkg-card glass-panel">
            <div class="pkg-head" @click="togglePkg(pkg.name)">
              <button type="button" class="pkg-arrow"
                :class="{ expanded: isPkgExpanded(pkg.name) }"
                @click.stop="togglePkg(pkg.name)"
                :title="isPkgExpanded(pkg.name) ? '收起' : '展开'">▶</button>
              <span class="pkg-icon">{{ isPkgExpanded(pkg.name) ? '📂' : '📁' }}</span>
              <span class="pkg-name">{{ pkg.name }}</span>
              <span class="pkg-meta">{{ pkg.fileCount }} 个文件 · {{ fmtSize(pkg.totalSize) }}</span>
              <button type="button" class="btn btn-sm pkg-open-dir" @click.stop="openPkgFolder(pkg.pkgPath)" title="打开所在文件夹">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><polyline points="9 14 12 11 15 14"/></svg>
              </button>
              <button type="button" class="btn btn-sm btn-danger pkg-del"
                @click.stop="deletePkg(pkg.name)" title="删除整个模型包">删除</button>
            </div>
            <div v-if="isPkgExpanded(pkg.name)" class="pkg-tree">
              <PackageTreeNode
                v-for="node in pkg.tree" :key="node.path"
                :node="node"
                :level="0"
                :package-name="pkg.name"
                :pkg-path="pkg.pkgPath"
                @load="onLoadFile"
                @delete="(r: string, d: boolean) => onDeleteFile(r, d)"
              />
            </div>
          </div>
        </div>
      </section>

      <!-- 空状态 -->
      <div v-if="!models.length && !packages.length" class="empty">
        <div class="empty-art">
          <svg width="64" height="64" viewBox="0 0 64 64" fill="none">
            <rect x="8" y="12" width="48" height="40" rx="6" stroke="currentColor" stroke-width="1.5" opacity="0.35"/>
            <rect x="14" y="20" width="36" height="24" rx="3" fill="currentColor" opacity="0.06"/>
            <circle cx="28" cy="32" r="5" stroke="currentColor" stroke-width="1.5" opacity="0.25"/>
            <path d="M36 28l8 8M44 28l-8 8" stroke="currentColor" stroke-width="1.5" opacity="0.25" stroke-linecap="round"/>
            <rect x="24" y="46" width="16" height="3" rx="1.5" fill="currentColor" opacity="0.1"/>
          </svg>
        </div>
        <p class="empty-title">尚未添加模型</p>
        <p class="empty-hint">从<b>模型市场</b>浏览下载，或点击右上角<b>「手动导入」</b>添加本地模型文件</p>
      </div>
    </div>

    <!-- ═══ Tab 2: Download Center ═══ -->
    <div v-show="tab === 'downloads'" class="dl-tab">
      <div class="dl-head-actions">
        <button class="btn btn-sm" @click="openDownloadDir">打开下载目录</button>
        <button v-if="dlHistory.length" class="btn btn-sm" @click="clearHistory">清除记录</button>
      </div>

      <!-- Empty -->
      <div v-if="!dlActive.length && !dlHistory.length" class="empty-state">
        <div class="empty-icon">↓</div>
        <div class="empty-title">暂无下载任务</div>
        <div class="empty-hint">从模型市场下载模型，或在设置中下载推理引擎</div>
      </div>

      <!-- Active downloads -->
      <section v-if="dlActive.length" class="dl-section">
        <h3 class="section-title">正在下载 <span class="count">{{ dlActive.length }}</span></h3>
        <div class="dl-cards">
          <div v-for="t in dlActive" :key="t.id" class="glass-panel dl-card" :class="'dl-' + t.status">
            <div class="dl-main">
              <div class="dl-filename" :title="t.filename">{{ t.filename }}</div>
              <div class="dl-progress-row">
                <div class="dl-bar-track">
                  <div class="dl-bar-fill" :style="{ width: t.pct + '%' }"
                    :class="{ 'bar-running': t.status === 'downloading' }"></div>
                </div>
                <span class="dl-pct">{{ t.pct }}%</span>
              </div>
              <div class="dl-meta">
                <span>{{ fmtSize(t.written) }} / {{ fmtSize(t.total) }}</span>
                <span v-if="t.status === 'downloading' && t.speed > 0">{{ fmtSpeed(t.speed) }}</span>
                <span v-if="t.status === 'downloading' && t.speed > 0 && t.total > t.written">剩余 {{ eta(t) }}</span>
                <span v-if="t.status === 'paused'" class="dl-status-paused">已暂停</span>
              </div>
            </div>
            <div class="dl-actions">
              <button v-if="t.status === 'downloading'" class="btn btn-xs" @click="dlPause(t.id)" title="暂停">⏸</button>
              <button v-if="t.status === 'paused'" class="btn btn-xs btn-go" @click="dlResume(t.id)" title="继续">▶</button>
              <button class="btn btn-xs btn-del" @click="dlCancel(t.id)" title="取消">✕</button>
            </div>
          </div>
        </div>
      </section>

      <!-- Completed -->
      <section v-if="dlCompleted.length" class="dl-section">
        <h3 class="section-title">已完成 <span class="count">{{ dlCompleted.length }}</span></h3>
        <div class="dl-list">
          <div v-for="t in dlCompleted" :key="t.id" class="dl-row dl-row-ok">
            <span class="dl-icon-ok">✓</span>
            <span class="dl-file" :title="t.filename">{{ t.filename }}</span>
            <span class="dl-size">{{ t.total > 0 ? fmtSize(t.total) : '—' }}</span>
            <span class="dl-time">{{ timeAgo(t.completedAt) }}</span>
            <button class="btn btn-xs" @click="openDownloadedFileDir(t.filename)" title="打开所在文件夹">📁</button>
            <button class="btn btn-xs" @click="dlCancel(t.id)" title="清除">✕</button>
          </div>
        </div>
      </section>

      <!-- Failed -->
      <section v-if="dlFailed.length" class="dl-section">
        <h3 class="section-title">下载失败 <span class="count count-err">{{ dlFailed.length }}</span></h3>
        <div class="dl-list">
          <div v-for="t in dlFailed" :key="t.id" class="dl-row dl-row-err">
            <span class="dl-icon-err">✕</span>
            <span class="dl-file" :title="t.filename">{{ t.filename }}</span>
            <span class="dl-reason" :title="t.reason">{{ t.reason || '未知错误' }}</span>
            <button class="btn btn-xs" @click="openDownloadedFileDir(t.filename)" title="打开所在文件夹">📁</button>
            <button class="btn btn-xs" @click="dlCancel(t.id)" title="清除">✕</button>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { storeToRefs } from 'pinia'
import ModelCard from './ModelCard.vue'
import PackageTreeNode from './PackageTreeNode.vue'
import { useDownloadStore } from '../stores/downloadStore'
import { useToast } from '../composables/useToast'
import { useDataChanged } from '../composables/useDataChanged'
import { modelsApi } from '../api/models'
import { systemApi } from '../api/system'
import { fmtSize } from '../utils/formatters'

// ── Props & Emits ──
defineProps<{ models?: any[] }>()
const emit = defineEmits<{
  (e: 'download-count', count: number): void
}>()

const toast = useToast()

// ── Download store (single source of truth) ──
const dlStore = useDownloadStore()
const {
  activeTasks: dlActive,
  historyTasks: dlHistory,
  completed: dlCompleted,
  failed: dlFailed,
  downloadCount,
} = storeToRefs(dlStore)

// ── State ──
const tab = ref('models')
const models = ref<any[]>([])
const downloaded = ref<any[]>([])
const expandedPkgs = ref<Record<string, boolean>>({})

// ── buildTree (standalone, used by packages computed) ──
function buildTree(files: any[]) {
  const visible = files.filter(f => !f.subPath.endsWith('.meta') && !f.subPath.endsWith('.part'))
  const root: any = { name: '', path: '', isDir: true, children: [], childFileCount: 0 }
  for (const f of visible) {
    const segs: string[] = f.subPath.split('/').filter(Boolean)
    let cur: any = root
    for (let i = 0; i < segs.length; i++) {
      const seg = segs[i]
      const isLeaf = i === segs.length - 1
      // If the file entry itself is a directory marker, the leaf is a directory
      const isDir = isLeaf && !!f.isDir
      if (isLeaf && !isDir) {
        cur.children.push({ name: seg, path: f.path, isDir: false, size: f.size, ext: f.ext, baseName: f.baseName, relName: f.relName, childFileCount: 0 })
        cur.childFileCount++
      } else {
        let child = cur.children.find((c: any) => c.isDir && c.name === seg)
        if (!child) {
          child = { name: seg, path: segs.slice(0, i + 1).join('/'), isDir: true, children: [], childFileCount: 0 }
          cur.children.push(child)
        }
        cur = child
      }
    }
  }
  const sortNodes = (nodes: any[]) => {
    nodes.sort((a: any, b: any) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
      return a.name.localeCompare(b.name)
    })
    for (const n of nodes) if (n.children) sortNodes(n.children)
  }
  sortNodes(root.children)
  return root.children
}

const packages = computed(() => {
  if (!downloaded.value.length) return []
  const pkgDirs: Record<string, any> = {}
  // Pass 1: collect all directory entries (including subdirectories).
  // Each becomes a package skeleton. Subdirectories add placeholder
  // "files" so buildTree creates the full directory tree.
  for (const f of downloaded.value) {
    if (!f.isDir) continue
    const norm = String(f.name || '').replace(/\\/g, '/')
    const slash = norm.indexOf('/')
    if (slash < 0) {
      // Top-level directory
      if (!pkgDirs[norm]) pkgDirs[norm] = { name: norm, files: [], totalSize: 0, fileCount: 0, tree: [], pkgPath: f.path }
      continue
    }
    // Subdirectory: add to parent package as a directory placeholder
    const dirName = norm.substring(0, slash)
    const subPath = norm.substring(slash + 1) + '/' // trailing / marks as directory
    if (!pkgDirs[dirName]) pkgDirs[dirName] = { name: dirName, files: [], totalSize: 0, fileCount: 0, tree: [], pkgPath: '' }
    pkgDirs[dirName].files.push({ subPath, size: 0, ext: '', baseName: norm.split('/').pop(), relName: norm, path: f.path, isDir: true })
  }
  // Pass 2: collect files, group into packages.
  const roots: any[] = []
  for (const f of downloaded.value) {
    if (f.isDir) continue
    const norm = String(f.name || '').replace(/\\/g, '/')
    const slash = norm.indexOf('/')
    if (slash < 0) {
      roots.push({ name: f.name, path: f.path, size: f.size, ext: f.ext, baseName: f.name, relName: f.name, subPath: f.name })
      continue
    }
    const dirName = norm.substring(0, slash)
    const subPath = norm.substring(slash + 1)
    const ext = subPath.includes('.') ? subPath.slice(subPath.lastIndexOf('.')).toLowerCase() : ''
    if (!pkgDirs[dirName]) pkgDirs[dirName] = { name: dirName, files: [], totalSize: 0, fileCount: 0, tree: [], pkgPath: '' }
    if (!pkgDirs[dirName].pkgPath && f.path) {
      const relSuffix = norm.substring(dirName.length)
      const osSuffix = relSuffix.replace(/\//g, '\\')
      pkgDirs[dirName].pkgPath = f.path.slice(0, -osSuffix.length) || f.path
    }
    pkgDirs[dirName].files.push({ subPath, size: f.size, ext, baseName: subPath.split('/').pop(), relName: f.name, path: f.path })
    pkgDirs[dirName].totalSize += f.size
    pkgDirs[dirName].fileCount++
  }
  if (roots.length > 0) {
    const tree = roots.map(r => ({ name: r.baseName, path: r.path, isDir: false as const, size: r.size, ext: r.ext, baseName: r.baseName, relName: r.relName }))
    let pkgPath = ''
    if (roots[0].path) pkgPath = roots[0].path.replace(/\\/g, '/').replace(/\/[^/]+$/, '')
    pkgDirs['根目录文件'] = { name: '根目录文件', files: roots, totalSize: roots.reduce((s: number, r: any) => s + r.size, 0), fileCount: roots.length, tree, pkgPath }
  }
  const result = Object.values(pkgDirs)
  for (const pkg of result) {
    if (!pkg.tree || pkg.tree.length === 0) pkg.tree = buildTree(pkg.files)
  }
  return result
})

// ── Watchers ──
watch(downloadCount, (val) => { emit('download-count', val) })
// Refresh downloaded files list when a download completes.
watch(() => dlCompleted.value.length, () => { refreshDownloaded() })

// ── Formatters ── (fmtSize from utils/formatters.ts)

function fmtSpeed(n: number) {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + ' MB/s'
  if (n >= 1e3) return (n / 1e3).toFixed(0) + ' KB/s'
  return n + ' B/s'
}

function eta(t: any) {
  if (!t.speed || t.speed <= 0 || !t.total) return ''
  const remaining = t.total - t.written
  if (remaining <= 0) return ''
  const sec = Math.ceil(remaining / t.speed)
  if (sec < 60) return sec + '秒'
  if (sec < 3600) return Math.floor(sec / 60) + '分' + (sec % 60) + '秒'
  return Math.floor(sec / 3600) + '时' + Math.floor((sec % 3600) / 60) + '分'
}

function timeAgo(ms: number) {
  if (!ms) return ''
  const sec = Math.floor((Date.now() - ms) / 1000)
  if (sec < 60) return '刚刚'
  if (sec < 3600) return Math.floor(sec / 60) + '分钟前'
  if (sec < 86400) return Math.floor(sec / 3600) + '小时前'
  return Math.floor(sec / 86400) + '天前'
}

// ── Package helpers ──
function isPkgExpanded(name: string) {
  return expandedPkgs.value[name] === true
}

function togglePkg(name: string) {
  expandedPkgs.value = Object.assign({}, expandedPkgs.value, { [name]: !expandedPkgs.value[name] })
}

async function openDownloadedFileDir(filename: string) { modelsApi.openDownloadedFileDir(filename).catch(() => {}) }
async function openPkgFolder(dirPath: string) {
  try { await systemApi.openDir(dirPath) } catch (_) {}
}

async function doUnload(id: string) {
  try { await modelsApi.unloadModel(id); await refreshModels() } catch (_) {}
}
async function doLoadModel(path: string, name: string) {
  try {
    const id = 'import-' + Date.now()
    await modelsApi.loadModelFile(id, name, path)
    await refreshModels()
    toast.show('success', '模型已加载', name)
  } catch (e: any) { toast.show('error', '加载失败', e.message || String(e)) }
}
async function refreshModels() {
  try { models.value = await modelsApi.listModels() || [] } catch (_) {}
}

function onLoadFile(path: string, name: string) {
  doLoadModel(path, name)
}

async function onDeleteFile(relName: string, isDir: boolean = false) {
  const label = isDir ? '目录' : '文件'
  const name = relName.replace(/\\/g, '/').split('/').pop()
  if (!await toast.confirm('删除' + label, '确定要删除"' + name + '"吗？此操作不可撤销。')) return
  try {
    if (isDir) await modelsApi.deleteDir(relName)
    else await modelsApi.deleteFile(relName)
    toast.show('success', '已删除', relName)
    await refreshDownloaded()
  } catch (e: any) {
    toast.show('error', '删除失败', e.message || String(e))
  }
}

async function refreshDownloaded() {
  try { downloaded.value = (await modelsApi.listDownloaded()) || [] } catch (_) { downloaded.value = [] }
}

async function importModel() {
  const path = await modelsApi.pickModelFile()
  if (path) {
    const name = path.split('\\').pop() || path.split('/').pop()
    doLoadModel(path, name)
  }
}

async function deletePkg(dirName: string) {
  if (!await toast.confirm('删除模型包', '确定要删除整个模型包 "' + dirName + '" 吗？此操作不可撤销。')) return
  try {
    await modelsApi.deleteDir(dirName)
    toast.show('success', '已删除模型包', dirName)
    await refreshDownloaded()
  } catch (e: any) {
    toast.show('error', '删除失败', e.message || String(e))
  }
}

// ── Downloads (delegate to store) ──
const dlPause = dlStore.pauseDownload
const dlResume = dlStore.resumeDownload
const dlCancel = dlStore.cancelDownload
const clearHistory = dlStore.clearHistory
const openDownloadDir = dlStore.openDownloadDir

// ── Lifecycle ──
onMounted(async () => {
  try { systemApi.logToTerminal('[MyModels] mounted') } catch (_) {}
  await refreshModels()
  await refreshDownloaded()
  dlStore.fetchAll()
})

// Live-refresh when a model is loaded/unloaded (by LLM tools or this UI).
useDataChanged('models:changed', () => { refreshModels() })

onBeforeUnmount(() => {})
</script>

<style scoped>
.my-models { width: 100%; }
.toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.toolbar-actions { display: flex; gap: 8px; }

/* ── Tabs ── */
.mm-tabs { display: flex; gap: 4px; margin-bottom: 20px; }
.mm-tab {
  padding: 6px 16px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-secondary); font-size: 13px; cursor: pointer;
  transition: all var(--transition); display: flex; align-items: center; gap: 6px;
}
.mm-tab:hover { background: var(--bg-hover); color: var(--text-primary); }
.mm-tab.active { background: var(--accent); border-color: var(--accent); color: #fff; }
.mm-tab-badge {
  font-size: 10px; font-weight: 600; background: rgba(255,255,255,0.25); color: inherit;
  padding: 1px 6px; border-radius: 8px; line-height: 1.4;
}

/* ── Empty ── */
.empty { text-align: center; padding: 80px 0 60px; }
.empty-art { margin-bottom: 20px; color: var(--text-tertiary); opacity: 0.6; }
.empty-title { font-size: 16px; font-weight: 550; margin-bottom: 8px; color: var(--text-secondary); }
.empty-hint { font-size: 13px; color: var(--text-tertiary); line-height: 1.6; max-width: 360px; margin: 0 auto; }
.empty-hint b { color: var(--text-secondary); font-weight: 550; }
.cards { display: flex; flex-direction: column; gap: 10px; margin-bottom: 28px; }
.section-title {
  font-size: 13px; font-weight: 600;
  margin-bottom: 12px; padding-left: 4px;
  color: var(--text-secondary);
  display: flex; align-items: center; gap: 8px;
}
.section-title::before {
  content: ''; width: 3px; height: 14px; border-radius: 2px;
  background: var(--accent); opacity: 0.5;
}

/* ── Packages ── */
.pkg-section { margin-top: 24px; }
.pkg-list { display: flex; flex-direction: column; gap: 8px; }
.pkg-card { overflow: hidden; }
.pkg-head {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 14px; cursor: pointer;
  transition: background 0.12s; user-select: none;
}
.pkg-head:hover { background: rgba(255,255,255,0.03); }
.pkg-arrow {
  width: 20px; height: 20px; flex-shrink: 0;
  display: inline-flex; align-items: center; justify-content: center;
  border: none; border-radius: 4px;
  background: transparent; color: var(--text-tertiary);
  font-size: 10px; cursor: pointer; padding: 0;
  transition: transform 0.15s, background 0.1s;
}
.pkg-arrow:hover { background: rgba(255,255,255,0.08); color: var(--text-primary); }
.pkg-arrow.expanded { transform: rotate(90deg); }
.pkg-icon { font-size: 16px; flex-shrink: 0; }
.pkg-name {
  font-size: 13px; font-weight: 550; font-family: var(--font-mono);
  color: var(--text-primary); flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.pkg-meta { font-size: 11px; color: var(--text-tertiary); flex-shrink: 0; }
.pkg-open-dir {
  padding: 2px 7px !important; font-size: 11px; color: var(--text-tertiary); flex-shrink: 0;
  border: none !important; background: transparent !important;
}
.pkg-open-dir:hover { color: var(--accent); background: var(--bg-hover) !important; }
.pkg-del { padding: 3px 10px !important; font-size: 11px; flex-shrink: 0; }
.pkg-tree { border-top: 1px solid var(--border-subtle); }

/* ── Download Tab ── */
.dl-tab { display: flex; flex-direction: column; gap: 16px; }
.dl-head-actions { display: flex; gap: 8px; }
.empty-state {
  display: flex; flex-direction: column; align-items: center; gap: 10px;
  padding: 60px 20px; color: var(--text-tertiary); text-align: center;
}
.empty-icon { font-size: 40px; opacity: 0.3; }

.dl-section { display: flex; flex-direction: column; gap: 10px; }
.count {
  font-size: 11px; font-weight: 500; color: var(--text-tertiary);
  background: var(--bg-elevated); padding: 1px 8px; border-radius: 10px;
}
.count-err { color: var(--danger); background: var(--danger-dim); }

.dl-cards { display: flex; flex-direction: column; gap: 8px; }
.dl-card { display: flex; align-items: center; gap: 12px; padding: 12px 14px; }
.dl-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.dl-filename { font-size: 12px; font-weight: 500; color: var(--text-primary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.dl-progress-row { display: flex; align-items: center; gap: 10px; }
.dl-bar-track { flex: 1; height: 5px; border-radius: 3px; background: var(--bg-elevated); overflow: hidden; }
.dl-bar-fill { height: 100%; border-radius: 3px; background: var(--accent); transition: width 0.3s ease; }
.bar-running { background: linear-gradient(90deg, var(--accent), #5ac); animation: pulse 2s infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.7; } }
.dl-pct { font-size: 11px; font-weight: 600; color: var(--text-secondary); font-variant-numeric: tabular-nums; min-width: 32px; text-align: right; }
.dl-meta { display: flex; gap: 14px; font-size: 11px; color: var(--text-tertiary); }
.dl-status-paused { color: var(--warning); font-weight: 500; }
.dl-actions { display: flex; gap: 4px; flex-shrink: 0; }

.dl-list { display: flex; flex-direction: column; gap: 4px; }
.dl-size { font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; flex-shrink: 0; width: 70px; text-align: right; }
.dl-time { font-size: 11px; color: var(--text-tertiary); flex-shrink: 0; width: 60px; text-align: right; }

/* ── Button overrides (component-specific) ── */
.btn-danger { color: var(--danger) !important; }
.btn-xs {
  width: 24px; height: 24px; padding: 0; font-size: 11px; line-height: 1;
  border: 1px solid var(--border-soft); border-radius: 4px;
  background: transparent; color: var(--text-secondary); cursor: pointer;
  display: inline-flex; align-items: center; justify-content: center;
}
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
</style>
