<template>
  <div class="ah">
    <header class="ah-head">
      <h2>活动历史</h2>
      <span class="ah-sub">所有 AI 工作的统一时间线 · 可查整个流程 · 重启不丢</span>
    </header>

    <section class="ah-filters glass-panel">
      <label>类型
        <select v-model="filter.kind" @change="reload">
          <option value="">全部</option>
          <option v-for="k in KINDS" :key="k.v" :value="k.v">{{ k.l }}</option>
        </select>
      </label>
      <label>来源
        <input v-model="filter.source" placeholder="agentId / execId" @keyup.enter="reload" />
      </label>
      <label>条数
        <select v-model.number="filter.limit" @change="reload">
          <option :value="100">100</option>
          <option :value="200">200</option>
          <option :value="500">500</option>
        </select>
      </label>
      <button class="btn" @click="reload">刷新</button>
      <button v-if="filter.kind || filter.source || filter.sessionId" class="btn btn-ghost" @click="clearFilters">清除筛选</button>
      <span class="ah-live" :class="live">● {{ live === 'live' ? '实时' : (live === 'idle' ? '已连接' : '连接中') }}</span>
    </section>

    <!-- Session "runs" (replay entry points) -->
    <section v-if="sessions.length" class="ah-runs">
      <div class="runs-title">运行（点击按会话回放）</div>
      <div class="runs-row">
        <button
          v-for="s in sessions" :key="s.id"
          class="run-chip" :class="{ active: filter.sessionId === s.id }"
          @click="replay(s.id)"
        >
          <span class="run-goal">{{ s.goal || s.id }}</span>
          <span class="run-meta">{{ s.status }} · {{ fmt(s.createdAt) }}</span>
        </button>
      </div>
    </section>

    <section v-if="filter.sessionId" class="ah-replaybar">
      正在回放会话 {{ filter.sessionId }} 的完整流程
      <button class="btn btn-ghost" @click="filter.sessionId = ''; reload()">退出回放</button>
    </section>

    <!-- Timeline -->
    <section class="ah-tl">
      <div v-if="rows.length === 0" class="empty">暂无活动记录</div>
      <div v-for="r in rows" :key="r.id" class="tl-row" :class="rowClass(r)" @click="toggle(r.id)">
        <span class="tl-time">{{ fmt(r.ts) }}</span>
        <span class="tl-tag" :class="r.kind">{{ KIND_LABEL[r.kind] || r.kind }}</span>
        <span class="tl-src" v-if="r.sourceName">@{{ r.sourceName || r.source }}</span>
        <span class="tl-summary">{{ r.summary }}</span>
        <pre v-if="open === r.id && r.payload" class="tl-payload">{{ r.payload }}</pre>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { collabApi, type ActivityRow, type CollabSession } from '../api/collab'

const KINDS = [
  { v: 'agent_start', l: 'Agent 开始' },
  { v: 'agent_done', l: 'Agent 完成' },
  { v: 'tool_call', l: '工具调用' },
  { v: 'workflow_start', l: '工作流开始' },
  { v: 'workflow_node', l: '工作流节点' },
  { v: 'workflow_done', l: '工作流结束' },
  { v: 'session', l: '协同会话' },
  { v: 'plan', l: '计划' },
  { v: 'blackboard', l: '黑板' },
]
const KIND_LABEL: Record<string, string> = {
  agent_start: 'AGENT▶', agent_done: 'AGENT✓', agent_message: 'MSG',
  tool_call: 'TOOL', workflow_start: 'WF▶', workflow_node: 'WF', workflow_done: 'WF✓',
  session: 'COLLAB', plan: 'PLAN', blackboard: 'BB', system: 'SYS', other: 'EVT', agent: 'AGENT',
}

const rows = ref<ActivityRow[]>([])
const sessions = ref<CollabSession[]>([])
const open = ref<string | null>(null)
const live = ref<'connecting' | 'idle' | 'live'>('connecting')
let liveTimer: ReturnType<typeof setTimeout> | null = null
const filter = reactive<{ kind: string; source: string; sessionId: string; limit: number }>({
  kind: '', source: '', sessionId: '', limit: 200,
})

onMounted(async () => {
  await reload()
  live.value = 'idle'
  window.runtime?.EventsOn?.('collab:event', () => {
    live.value = 'live'
    if (liveTimer) clearTimeout(liveTimer)
    liveTimer = setTimeout(() => { if (live.value === 'live') live.value = 'idle' }, 2500)
    // Throttled reload: a burst appends once after the stream settles.
    scheduleAppend()
  })
})
onBeforeUnmount(() => {
  window.runtime?.EventsOff?.('collab:event')
  if (liveTimer) clearTimeout(liveTimer)
  if (appendTimer) clearTimeout(appendTimer)
})

let appendTimer: ReturnType<typeof setTimeout> | null = null
function scheduleAppend() {
  if (appendTimer) return
  appendTimer = setTimeout(() => { appendTimer = null; void reload() }, 1500)
}

async function reload() {
  try {
    const [list, sess] = await Promise.all([
      collabApi.listActivity(filter.kind, filter.sessionId, filter.source, 0, filter.limit),
      collabApi.listSessions(),
    ])
    rows.value = list || []
    sessions.value = sess || []
  } catch (_) {
    /* ignore */
  }
}

function replay(sessionId: string) {
  filter.sessionId = sessionId
  filter.kind = ''
  void reload()
}
function clearFilters() {
  filter.kind = ''; filter.source = ''; filter.sessionId = ''
  void reload()
}
function toggle(id: string) { open.value = open.value === id ? null : id }
function rowClass(r: ActivityRow) {
  if (r.kind.includes('error')) return 'err'
  if (r.kind === 'agent_done' || r.kind === 'workflow_done') return 'ok'
  return ''
}
function fmt(ts: number | string): string {
  if (!ts) return ''
  const n = typeof ts === 'string' ? Date.parse(ts) : ts
  if (!n) return ''
  return new Date(n).toLocaleString('zh-CN', { hour12: false })
}
</script>

<style scoped>
.ah { padding: 24px; max-width: 1100px; margin: 0 auto; color: #e0e0e0; }
.ah-head { display: flex; align-items: baseline; gap: 14px; margin-bottom: 16px; }
.ah-head h2 { margin: 0; font-size: 1.4em; }
.ah-sub { font-size: 0.82em; color: #777; }

.ah-filters { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; padding: 14px 16px; margin-bottom: 16px; }
.ah-filters label { display: flex; flex-direction: column; gap: 4px; font-size: 0.78em; color: #999; }
.ah-filters select, .ah-filters input { background: #1a1a1e; border: 1px solid #333; border-radius: 6px; color: #e0e0e0; padding: 6px 10px; font-size: 0.9em; min-width: 120px; }
.btn { padding: 7px 14px; border: 1px solid #444; border-radius: 6px; background: #252528; color: #ccc; cursor: pointer; font-size: 0.86em; }
.btn:hover { background: #333; }
.btn-ghost { background: none; border-color: #333; color: #888; }
.ah-live { margin-left: auto; font-size: 0.76em; color: #888; }
.ah-live.live { color: #5c5; }
.ah-live.idle { color: #7cb3ff; }
.ah-live.connecting { color: #d29922; }

.ah-runs { margin-bottom: 14px; }
.runs-title { font-size: 0.78em; color: #777; margin-bottom: 6px; }
.runs-row { display: flex; gap: 8px; flex-wrap: wrap; }
.run-chip { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; max-width: 220px; background: #1a2230; border: 1px solid #2a3a4a; border-radius: 8px; padding: 8px 12px; cursor: pointer; text-align: left; }
.run-chip:hover { border-color: #4a6a8a; }
.run-chip.active { border-color: #7cb3ff; background: #1e3a5f; }
.run-goal { font-size: 0.82em; color: #d0d0d0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
.run-meta { font-size: 0.68em; color: #777; }

.ah-replaybar { display: flex; align-items: center; justify-content: space-between; gap: 10px; background: #1e3a5f; border: 1px solid #2a4a6a; border-radius: 6px; padding: 8px 14px; font-size: 0.82em; color: #7cb3ff; margin-bottom: 14px; }

.ah-tl { display: flex; flex-direction: column; gap: 2px; }
.empty { color: #555; font-style: italic; padding: 32px 0; text-align: center; }
.tl-row { display: flex; align-items: baseline; gap: 8px; padding: 7px 10px; border-radius: 5px; cursor: pointer; flex-wrap: wrap; border-bottom: 1px solid rgba(255,255,255,0.03); }
.tl-row:hover { background: rgba(255,255,255,0.03); }
.tl-time { color: #555; font-family: monospace; font-size: 0.74em; flex-shrink: 0; width: 150px; }
.tl-tag { font-size: 0.72em; font-weight: 600; padding: 1px 6px; border-radius: 3px; background: #2a2a2e; color: #888; flex-shrink: 0; min-width: 56px; text-align: center; }
.tl-tag.tool_call { background: #1a2a3a; color: #6fb3d9; }
.tl-tag.agent_start, .tl-tag.agent_done { background: #1e3a5f; color: #7cb3ff; }
.tl-tag.workflow_start, .tl-tag.workflow_node, .tl-tag.workflow_done { background: #2a223a; color: #b09cf0; }
.tl-tag.session { background: #3a2a1a; color: #d29922; }
.tl-tag.plan { background: #2a1a3a; color: #b07cf0; }
.tl-tag.blackboard { background: #1a3a2a; color: #4a9; }
.tl-src { color: #d29922; font-size: 0.8em; flex-shrink: 0; }
.tl-summary { color: #bbb; font-size: 0.84em; flex: 1; min-width: 200px; }
.tl-row.err .tl-tag { background: #3a1a1a; color: #f07070; }
.tl-row.ok .tl-tag { background: #1a3a1a; color: #5c5; }
.tl-payload { flex-basis: 100%; background: #141418; border: 1px solid #2a2a2e; border-radius: 4px; padding: 8px; margin: 4px 0 0; font-size: 0.74em; color: #9a9a9a; white-space: pre-wrap; word-break: break-word; max-height: 240px; overflow-y: auto; }
</style>
