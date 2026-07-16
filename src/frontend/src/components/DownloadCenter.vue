<template>
  <div class="dl-center">
    <header class="page-head">
      <div class="head-left">
        <h2 class="title">下载中心</h2>
        <span class="subtitle">管理所有模型与引擎的下载任务</span>
      </div>
      <div class="head-actions">
        <button class="btn btn-sm" @click="doRefresh">刷新</button>
        <button class="btn btn-sm" @click="openDir">打开目录</button>
        <button v-if="history.length" class="btn btn-sm" @click="clearHistory">清除记录</button>
      </div>
    </header>

    <!-- 空状态 -->
    <div v-if="!active.length && !history.length" class="empty-state">
      <div class="empty-icon">📥</div>
      <div class="empty-title">暂无下载任务</div>
      <div class="empty-hint">从模型市场下载模型，或在设置中下载推理引擎</div>
    </div>

    <!-- 正在下载 -->
    <section v-if="active.length" class="dl-section">
      <h3 class="section-title">正在下载 <span class="count">{{ active.length }}</span></h3>
      <div class="dl-cards">
        <div v-for="t in active" :key="t.id" class="glass-panel dl-card" :class="'dl-' + t.status">
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
              <template v-if="t.status === 'queued'">
                <span class="dl-status-queued">⏳ 排队中</span>
              </template>
              <template v-else-if="t.status === 'retrying'">
                <span class="dl-status-retrying" :title="t.reason">🔄 {{ t.reason }}</span>
              </template>
              <template v-else>
                <span>{{ fmtSize(t.written) }} / {{ fmtSize(t.total) }}</span>
                <span v-if="t.status === 'downloading' && t.speed > 0">{{ fmtSpeed(t.speed) }}</span>
                <span v-if="t.status === 'downloading' && t.speed > 0 && t.total > t.written">
                  剩余 {{ eta(t) }}
                </span>
                <span v-if="t.status === 'paused'" class="dl-status-paused">已暂停</span>
              </template>
            </div>
          </div>
          <div class="dl-actions">
            <button v-if="t.status === 'downloading'" class="btn btn-xs" @click="pause(t.id)" title="暂停">⏸</button>
            <button v-if="t.status === 'paused'" class="btn btn-xs btn-go" @click="resume(t.id)" title="继续">▶</button>
            <button class="btn btn-xs btn-danger" @click="cancel(t.id)" title="取消">✕</button>
          </div>
        </div>
      </div>
    </section>

    <!-- 已完成 -->
    <section v-if="completed.length" class="dl-section">
      <h3 class="section-title">已完成 <span class="count">{{ completed.length }}</span></h3>
      <div class="dl-list">
        <div v-for="t in completed" :key="t.id" class="dl-row dl-row-ok">
          <span class="dl-icon-ok">✓</span>
          <span class="dl-file" :title="t.filename">{{ t.filename }}</span>
          <span class="dl-size">{{ t.total > 0 ? fmtSize(t.total) : '—' }}</span>
          <span class="dl-time">{{ timeAgo(t.completedAt) }}</span>
          <button class="btn btn-xs" @click="openDownloadedFileDir(t.filename)" title="打开所在文件夹">📁</button>
          <button class="btn btn-xs" @click="cancel(t.id)" title="清除">✕</button>
        </div>
      </div>
    </section>

    <!-- 下载失败 -->
    <section v-if="failed.length" class="dl-section">
      <h3 class="section-title">下载失败 <span class="count count-err">{{ failed.length }}</span></h3>
      <div class="dl-list">
        <div v-for="t in failed" :key="t.id" class="dl-row dl-row-err">
          <span class="dl-icon-err">✕</span>
          <span class="dl-file" :title="t.filename">{{ t.filename }}</span>
          <span class="dl-reason" :title="t.reason">{{ t.reason || '未知错误' }}</span>
          <button class="btn btn-xs btn-retry" @click="retry(t.id)" title="重试">↻</button>
          <button class="btn btn-xs" @click="openDownloadedFileDir(t.filename)" title="打开所在文件夹">📁</button>
          <button class="btn btn-xs" @click="cancel(t.id)" title="清除">✕</button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useDownloadStore, type TaskInfo } from '../stores/downloadStore'
import { fmtSize } from '../utils/formatters'

const store = useDownloadStore()

// All reactive state comes from the store (single source of truth).
const active = store.activeTasks
const history = store.historyTasks
const completed = store.completed
const failed = store.failed

// Actions delegate to store.
const pause = store.pauseDownload
const resume = store.resumeDownload
const retry = store.retryDownload
const cancel = store.cancelDownload
const clearHistory = store.clearHistory
const openDir = store.openDownloadDir
const openDownloadedFileDir = store.openDownloadedFileDir

// Manual refresh — re-fetch from server.
function doRefresh() { store.fetchAll() }

// ── Utility (pure functions, no state) ──
function fmtSpeed(n: number): string { if (n >= 1e6) return (n / 1e6).toFixed(1) + ' MB/s'; if (n >= 1e3) return (n / 1e3).toFixed(0) + ' KB/s'; return n + ' B/s' }
function eta(t: TaskInfo): string { if (!t.speed || t.speed <= 0 || !t.total) return ''; const remaining = t.total - t.written; if (remaining <= 0) return ''; const sec = Math.ceil(remaining / t.speed); if (sec < 60) return sec + '秒'; if (sec < 3600) return Math.floor(sec / 60) + '分' + (sec % 60) + '秒'; return Math.floor(sec / 3600) + '时' + Math.floor((sec % 3600) / 60) + '分' }
function timeAgo(ms: number): string { if (!ms) return ''; const sec = Math.floor((Date.now() - ms) / 1000); if (sec < 60) return '刚刚'; if (sec < 3600) return Math.floor(sec / 60) + '分钟前'; if (sec < 86400) return Math.floor(sec / 3600) + '小时前'; return Math.floor(sec / 86400) + '天前' }

onMounted(() => { store.fetchAll() })
</script>

<style scoped>
.dl-center { display: flex; flex-direction: column; gap: 20px; }

/* ─── Header ─── */
.page-head {
  display: flex; align-items: flex-start; justify-content: space-between; gap: 16px;
}
.head-left { display: flex; flex-direction: column; gap: 4px; }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.subtitle { font-size: 12px; color: var(--text-tertiary); font-weight: 450; }
.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

/* ─── Empty ─── */
.empty-state {
  display: flex; flex-direction: column; align-items: center; gap: 10px;
  padding: 60px 20px; color: var(--text-tertiary); text-align: center;
}
.empty-icon { font-size: 48px; opacity: 0.4; }
.empty-title { font-size: 16px; font-weight: 500; color: var(--text-secondary); }
.empty-hint { font-size: 12px; }

/* ─── Sections ─── */
.dl-section { display: flex; flex-direction: column; gap: 10px; }
.section-title {
  font-size: 13px; font-weight: 600; color: var(--text-secondary);
  display: flex; align-items: center; gap: 8px; margin: 0;
}
.count {
  font-size: 11px; font-weight: 500; color: var(--text-tertiary);
  background: var(--bg-elevated); padding: 1px 8px; border-radius: 10px;
}
.count-err { color: var(--danger); background: var(--danger-dim); }

/* ─── Active Download Cards ─── */
.dl-cards { display: flex; flex-direction: column; gap: 8px; }
.dl-card {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px;
}
.dl-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.dl-filename {
  font-size: 12px; font-weight: 500; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dl-progress-row { display: flex; align-items: center; gap: 10px; }
.dl-bar-track {
  flex: 1; height: 5px; border-radius: 3px;
  background: var(--bg-elevated); overflow: hidden;
}
.dl-bar-fill {
  height: 100%; border-radius: 3px;
  background: var(--accent); transition: width 0.3s ease;
}
.bar-running { background: linear-gradient(90deg, var(--accent), #5ac); animation: pulse 2s infinite; }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}
.dl-pct {
  font-size: 11px; font-weight: 600; color: var(--text-secondary);
  font-variant-numeric: tabular-nums; min-width: 32px; text-align: right;
}
.dl-meta {
  display: flex; gap: 14px; font-size: 11px; color: var(--text-tertiary);
}
.dl-status-paused { color: var(--warning); font-weight: 500; }
.dl-status-queued { color: var(--text-tertiary); font-weight: 500; }
.dl-status-retrying { color: var(--accent); font-weight: 500; font-size: 10px; }
.dl-actions { display: flex; gap: 4px; flex-shrink: 0; }
.btn-xs {
  width: 24px; height: 24px; padding: 0; font-size: 11px; line-height: 1;
  border: 1px solid var(--border-soft); border-radius: 4px;
  background: transparent; color: var(--text-secondary); cursor: pointer;
  display: inline-flex; align-items: center; justify-content: center;
}
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-go { color: var(--success); border-color: rgba(48,209,88,0.3); }
.btn-go:hover { background: rgba(48,209,88,0.1); }
.btn-retry { color: var(--accent); border-color: rgba(0,122,255,0.3); }
.btn-retry:hover { background: rgba(0,122,255,0.1); }
.btn-danger { color: var(--danger); border-color: rgba(255,69,58,0.3); }
.btn-danger:hover { background: rgba(255,69,58,0.1); }

/* ─── History Row List ─── */
.dl-list { display: flex; flex-direction: column; gap: 4px; }
.dl-row {
  display: flex; align-items: center; gap: 10px;
  padding: 7px 10px; border-radius: 6px; font-size: 12px;
}
.dl-row:hover { background: var(--bg-hover); }
.dl-row-ok { color: var(--text-secondary); }
.dl-row-err { color: var(--text-secondary); }
.dl-icon-ok { color: var(--success); font-size: 13px; width: 16px; text-align: center; flex-shrink: 0; }
.dl-icon-err { color: var(--danger); font-size: 13px; width: 16px; text-align: center; flex-shrink: 0; }
.dl-file { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.dl-size {
  font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums;
  flex-shrink: 0; width: 70px; text-align: right;
}
.dl-time {
  font-size: 11px; color: var(--text-tertiary); flex-shrink: 0; width: 60px; text-align: right;
}
.dl-reason {
  font-size: 11px; color: var(--danger); flex: 1;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
}
</style>
