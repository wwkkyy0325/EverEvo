<template>
  <div class="workbench">
    <!-- ═══ Main: full-bleed collaboration graph ═══ -->
    <div class="wb-stage">
      <VueFlow
        v-if="graphNodes.length"
        :nodes="graphNodes"
        :edges="graphEdges"
        :fit-view-on-init="true"
        :min-zoom="0.3"
        :max-zoom="1.8"
        :default-edge-options="{ type: 'smoothstep', animated: true, style: { stroke: '#4a90d9', strokeWidth: 2 } }"
      >
        <Background :gap="22" :pattern-color="'rgba(255,255,255,0.04)'" />
        <Controls position="bottom-right" />
        <template #node-agent="props">
          <div class="agnode" :class="props.data.state" @click="selected = props.id">
            <div class="agnode-icon">{{ props.data.role === 'orchestrator' ? '◉' : '⬡' }}</div>
            <div class="agnode-body">
              <div class="agnode-name">{{ props.data.label }}</div>
              <div class="agnode-doing">{{ props.data.doing || '空闲' }}</div>
            </div>
            <span class="agnode-dot"></span>
          </div>
        </template>
        <template #node-wf="props">
          <div class="wfnode" :class="props.data.status" @click="selected = props.id">
            <div class="wfnode-icon">⚙</div>
            <div class="wfnode-body">
              <div class="wfnode-name">{{ props.data.name }}</div>
              <div class="wfnode-progress">{{ props.data.progress || '运行中…' }}</div>
            </div>
            <span class="wfnode-tag">工作流</span>
          </div>
        </template>
      </VueFlow>
      <div v-else class="wb-empty-big">
        <div class="wb-empty-icon">🔀</div>
        <div>暂无协作活动</div>
        <div class="hint">AI 用 collab_create / collab_dispatch 后，agent 作为节点、派发作为连线实时显示；运行中的工作流也会作为节点出现</div>
      </div>

      <!-- live badge: 3-state (connecting → idle → live) -->
      <div class="wb-live" :class="connState">● {{ connLabel }}</div>
    </div>

    <!-- ═══ Float 1: Plan board (top-left) ═══ -->
    <div class="float-panel float-plan" :class="{ collapsed: !showPlan }">
      <div class="float-head" @click="showPlan = !showPlan">
        <span class="float-title">📋 任务计划 <span v-if="plans.length" class="float-count">{{ plans.length }}</span></span>
        <span class="float-toggle">{{ showPlan ? '▾' : '▸' }}</span>
      </div>
      <div v-if="showPlan" class="float-body">
        <div v-if="plans.length === 0" class="float-empty">暂无计划<br><span class="hint">plan_create 拆解任务后显示</span></div>
        <div v-for="p in plans" :key="p.id" class="plan-card">
          <div class="plan-head">
            <div class="plan-goal-wrap">
              <div class="plan-goal">{{ p.goal }}</div>
              <div class="plan-meta">@{{ p.author }} · {{ p.steps.filter(s => s.status === 'done').length }}/{{ p.steps.length }}</div>
            </div>
            <div class="plan-ring" :style="ringStyle(planPct(p))"><span>{{ planPct(p) }}%</span></div>
          </div>
          <div class="plan-steps">
            <div v-for="s in p.steps" :key="s.index" class="step" :class="s.status">
              <span class="step-check" @click="toggleStep(p, s)">
                <span v-if="s.status === 'done'">✓</span>
                <span v-else-if="s.status === 'in_progress'" class="spin">◐</span>
                <span v-else-if="s.status === 'skipped'">⊘</span>
                <span v-else class="step-num">{{ s.index + 1 }}</span>
              </span>
              <div class="step-content">
                <div class="step-title">{{ s.title }}</div>
                <div v-if="s.note || s.agentId" class="step-note">
                  <span v-if="s.agentId" class="step-agent">@{{ nameOf(s.agentId) }}</span>
                  <span v-if="s.note">{{ s.note }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══ Float 2: Event stream (right) ═══ -->
    <div class="float-panel float-stream" :class="{ collapsed: !showStream, drawer: !!selected }">
      <div class="float-head" @click="showStream = !showStream">
        <span class="float-title">📡 事件流 <span class="float-count">{{ events.length }}</span></span>
        <span class="float-actions">
          <button v-if="showStream" class="float-clear" @click.stop="events = []">清空</button>
          <span class="float-toggle">{{ showStream ? '▸' : '◂' }}</span>
        </span>
      </div>
      <div v-if="showStream" class="float-body stream-body">
        <div v-if="events.length === 0" class="float-empty">等待事件…</div>
        <div v-for="(e, i) in events" :key="i" class="event-row" :class="eventClass(e)">
          <span class="ev-time">{{ e.time }}</span>
          <span class="ev-tag" :class="eventTag(e)">{{ eventTag(e) }}</span>
          <span class="ev-source" v-if="e.source">@{{ nameOf(e.source) }}</span>
          <span class="ev-desc">{{ eventDesc(e) }}</span>
        </div>
      </div>
    </div>

    <!-- ═══ Agent/workflow detail drawer ═══ -->
    <div v-if="selected" class="drawer" @click.self="selected = null">
      <div class="drawer-card glass">
        <div class="drawer-head">
          <span class="drawer-title">{{ drawerTitle }}</span>
          <button class="float-clear" @click="selected = null">✕</button>
        </div>
        <div class="drawer-body">
          <div v-if="drawerEvents.length === 0" class="float-empty">暂无该对象的详细活动</div>
          <div v-for="(e, i) in drawerEvents" :key="i" class="event-row" :class="eventClass(e)">
            <span class="ev-time">{{ e.time }}</span>
            <span class="ev-tag" :class="eventTag(e)">{{ eventTag(e) }}</span>
            <span class="ev-desc">{{ eventDesc(e) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { VueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { collabApi, type CollabSession, type Plan, type PlanStep } from '../api/collab'
import { agentsApi } from '../api/agents'

type AgentState = 'idle' | 'busy' | 'done' | 'error'

interface AgentNode { id: string; name: string; state: AgentState; doing: string; task: string; role?: string }
interface WfNode { id: string; name: string; status: string; progress: string; total: number }
interface EvRow { time: string; topic: string; source: string; type: string; payload: any }

// Connection state: 'connecting' until first API/event confirms the backend is
// wired; 'idle' when connected but no recent activity; 'live' while events flow.
const connState = ref<'connecting' | 'idle' | 'live'>('connecting')
const connLabel = computed(() => ({ connecting: '连接中', idle: '已连接·空闲', live: '实时' }[connState.value]))
let liveTimer: ReturnType<typeof setTimeout> | null = null
function pulseLive() {
  connState.value = 'live'
  if (liveTimer) clearTimeout(liveTimer)
  liveTimer = setTimeout(() => { if (connState.value === 'live') connState.value = 'idle' }, 3000)
}

const showPlan = ref(true)
const showStream = ref(true)
const plans = ref<Plan[]>([])
const sessions = ref<CollabSession[]>([])
const idToName = ref<Record<string, string>>({})
const agents = ref<Record<string, AgentNode>>({})
const workflows = ref<Record<string, WfNode>>({})
const comms = ref<Record<string, { from: string; to: string; label: string }>>({})
const events = ref<EvRow[]>([])
const selected = ref<string | null>(null)
function nameOf(id: string): string {
  if (!id) return ''
  return idToName.value[id] || agents.value[id]?.name || id
}

function ensureAgent(id: string, role?: string): AgentNode {
  if (!agents.value[id]) {
    agents.value[id] = { id, name: nameOf(id), state: 'idle', doing: '', task: '' }
  }
  if (role) agents.value[id].role = role
  agents.value[id].name = nameOf(id)
  return agents.value[id]
}

const graphNodes = computed(() => {
  const out: any[] = []
  const agList = Object.values(agents.value)
  agList.forEach((a, i) => {
    const col = i % 3
    const row = Math.floor(i / 3)
    out.push({
      id: a.id, type: 'agent', position: { x: 120 + col * 280, y: 80 + row * 170 },
      data: { label: a.name, state: a.state, doing: a.doing, task: a.task, role: a.role },
      draggable: true,
    })
  })
  Object.values(workflows.value).forEach((w, i) => {
    out.push({
      id: w.id, type: 'wf', position: { x: 1000, y: 80 + i * 150 },
      data: { name: w.name, status: w.status, progress: w.progress },
      draggable: true,
    })
  })
  return out
})

const graphEdges = computed(() => {
  const edges: any[] = []
  for (const s of sessions.value) {
    ensureAgent(s.orchestratorId, 'orchestrator')
    for (const m of s.members) {
      ensureAgent(m.agentId, m.role)
      if (m.agentId !== s.orchestratorId) {
        edges.push({
          id: `mem-${s.orchestratorId}-${m.agentId}`,
          source: s.orchestratorId, target: m.agentId,
          animated: agents.value[m.agentId]?.state === 'busy',
          style: { stroke: '#3a5a7a', strokeWidth: 1.5 },
        })
      }
    }
  }
  for (const k in comms.value) {
    const c = comms.value[k]
    edges.push({
      id: `comms-${k}`, source: c.from, target: c.to, animated: true, label: c.label,
      style: { stroke: '#d29922', strokeWidth: 2 },
    })
  }
  return edges
})

onMounted(() => {
  // Initial snapshot — subsequent updates are entirely event-driven via the
  // collab:event subscription (routeEvent handles plan.* / collab.* /
  // agent.* / workflow.*), so no polling interval is needed.
  refreshAll().then(() => { if (connState.value === 'connecting') connState.value = 'idle' }).catch(() => {})
  window.runtime?.EventsOn?.('collab:event', (envelope: any) => {
    pulseLive()
    const ev = envelope?.data || {}
    const topic = ev.topic || envelope?.topic || ''
    if (topic === 'collab.ready') {
      if (connState.value === 'connecting') connState.value = 'idle'
      return
    }
    const source = ev.source || ''
    const type = ev.type || ''
    const payload = ev.payload
    pushEvent(topic, source, type, payload)
    routeEvent(topic, source, type, payload)
  })
})
onBeforeUnmount(() => {
  window.runtime?.EventsOff?.('collab:event')
  if (liveTimer) clearTimeout(liveTimer)
})

async function refreshAll() {
  await Promise.all([refreshPlans(), refreshSessions(), refreshAgents()])
}
async function refreshPlans() { try { plans.value = await collabApi.planList() } catch (_) {} }
async function refreshSessions() { try { sessions.value = await collabApi.listSessions() } catch (_) {} }
async function refreshAgents() {
  try {
    const list = await agentsApi.list()
    const map: Record<string, string> = {}
    for (const a of list || []) map[a.id] = a.name
    idToName.value = map
    // refresh display names already shown
    for (const id in agents.value) agents.value[id].name = nameOf(id)
  } catch (_) {}
}

function routeEvent(topic: string, source: string, type: string, payload: any) {
  const m = asMap(payload)
  if (topic.startsWith('agent.')) {
    const a = ensureAgent(source)
    if (type === 'start') { a.state = 'busy'; a.task = str(m.task); a.doing = trunc(str(m.task) || '执行中', 40) }
    else if (type === 'done') { a.state = 'done'; a.doing = str(m.result) ? '完成：' + trunc(str(m.result), 30) : '完成' }
    else if (type === 'message') { a.state = 'busy'; a.doing = '通信中' }
  } else if (topic.startsWith('tool.')) {
    const a = ensureAgent(source)
    a.state = 'busy'
    const tool = str(m.tool) || type
    a.doing = '调用 ' + tool
    const parsed = tryParseArgs(m.args)
    if (isCommsTool(tool) && parsed?.targetAgentId) {
      ensureAgent(parsed.targetAgentId)
      const key = `${source}->${parsed.targetAgentId}`
      comms.value = { ...comms.value, [key]: { from: source, to: parsed.targetAgentId, label: trunc(commsLabel(tool, parsed), 28) } }
    }
  } else if (topic.startsWith('blackboard.') && source) {
    const a = ensureAgent(source); a.doing = '写黑板'; a.state = 'busy'
  } else if (topic.startsWith('plan.')) {
    refreshPlans()
  } else if (topic.startsWith('collab.')) {
    refreshSessions()
  } else if (topic === 'wf-exec-start') {
    workflows.value = { ...workflows.value, [m.execId]: { id: m.execId, name: str(m.workflowName) || str(m.workflowId) || str(m.execId), status: 'running', progress: '', total: Number(m.totalNodes) || 0 } }
  } else if (topic.startsWith('wf-node-')) {
    const w = workflows.value[m.execId]
    if (w) { w.progress = str(m.title) + ' ' + topic.replace('wf-node-', ''); w.status = 'running'; workflows.value = { ...workflows.value } }
  } else if (topic.startsWith('workflow-progress-')) {
    const eid = topic.replace('workflow-progress-', '')
    const w = workflows.value[eid]
    if (w && m.progress) { w.progress = str(m.progress); workflows.value = { ...workflows.value } }
  } else if (topic.startsWith('wf-exec-')) {
    const w = workflows.value[m.execId]
    if (w) { w.status = topic.includes('done') ? 'done' : (topic.includes('error') ? 'error' : 'done'); workflows.value = { ...workflows.value } }
  }
}

function pushEvent(topic: string, source: string, type: string, payload: any) {
  events.value.unshift({ time: new Date().toLocaleTimeString('zh-CN', { hour12: false }), topic, source, type, payload })
  if (events.value.length > 300) events.value.pop()
}

async function toggleStep(p: Plan, s: PlanStep) {
  const next = s.status === 'done' ? 'pending' : 'done'
  try { await collabApi.planStepUpdate(p.id, s.index, next, '', 'user') } catch (_) {}
}

function planPct(p: Plan): number {
  if (!p.steps.length) return 0
  return Math.round((p.steps.filter(s => s.status === 'done' || s.status === 'skipped').length / p.steps.length) * 100)
}
function ringStyle(pct: number) {
  const deg = Math.round(pct / 100 * 360)
  return { background: `conic-gradient(#4a9 ${deg}deg, #2a2a2e 0deg)` }
}

// ─── helpers ───
function asMap(v: any): Record<string, any> {
  if (!v) return {}
  if (typeof v === 'object') return v
  try { const j = JSON.parse(String(v)); return typeof j === 'object' && j ? j : {} } catch { return {} }
}
function str(v: any): string { return v == null ? '' : String(v) }
function trunc(s: string, n: number): string { s = (s || '').trim(); return s.length <= n ? s : s.slice(0, n) + '…' }
function tryParseArgs(s: any): Record<string, any> | undefined {
  const raw = str(s); if (!raw) return undefined
  try { const j = JSON.parse(raw); return typeof j === 'object' && j ? j : undefined } catch { return undefined }
}
const COMMS_TOOLS = new Set(['collab_dispatch', 'collab_dispatch_async', 'agent_message'])
function isCommsTool(t: string): boolean { return COMMS_TOOLS.has(t) }
function commsLabel(tool: string, args: Record<string, any>): string {
  if (tool === 'agent_message') return '消息：' + str(args.message)
  return '派发：' + str(args.task)
}

// ─── event display ───
const drawerTitle = computed(() => {
  if (!selected.value) return ''
  if (workflows.value[selected.value]) return '⚙ ' + workflows.value[selected.value].name
  return nameOf(selected.value)
})
const drawerEvents = computed(() => {
  if (!selected.value) return []
  const id = selected.value
  return events.value.filter(e => e.source === id || asMap(e.payload).execId === id)
})
function eventClass(e: EvRow) {
  const t = e.topic || ''
  if (t.includes('error') || t.includes('failed')) return 'ev-err'
  if (t.includes('done') || t.includes('completed')) return 'ev-ok'
  return ''
}
function eventTag(e: EvRow): string {
  const t = e.topic || ''
  if (t.startsWith('plan.')) return 'PLAN'
  if (t.startsWith('agent.')) return t.includes('message') ? 'MSG' : 'AGENT'
  if (t.startsWith('tool.')) return 'TOOL'
  if (t.startsWith('blackboard.')) return 'BB'
  if (t.startsWith('wf-') || t.startsWith('workflow-')) return 'WF'
  if (t.startsWith('collab.')) return 'COLLAB'
  return 'EVT'
}
function eventDesc(e: EvRow): string {
  const p = asMap(e.payload)
  if (p.key) return `${p.key} = ${trunc(str(p.value), 50)}`
  if (p.tool) return `${p.tool}` + (p.ok === false ? ' ✗' : '')
  if (p.index !== undefined) return `步骤 ${p.index} → ${p.status || e.type}`
  if (p.workflowName) return p.workflowName
  if (p.title) return `${p.title}`
  if (p.task) return trunc(p.task, 60)
  if (p.message) return trunc(p.message, 60)
  return e.type || ''
}
</script>

<style scoped>
.workbench {
  position: relative;
  width: 100%;
  height: 100%;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}
.wb-stage { position: absolute; inset: 0; min-height: 60vh; background: rgba(10,10,14,0.45); }

/* ── Main stage ── */
.wb-live { position: absolute; top: 12px; left: 50%; transform: translateX(-50%); font-size: 0.76em; color: #888; background: rgba(20,20,24,0.85); padding: 3px 12px; border-radius: 12px; z-index: 5; pointer-events: none; transition: color 0.3s; }
.wb-live.connecting { color: #d29922; }
.wb-live.idle { color: #7cb3ff; }
.wb-live.live { color: #5c5; }
.wb-live.live::first-letter { animation: pulse 1.2s infinite; }
.wb-empty-big { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; color: #555; gap: 8px; }
.wb-empty-icon { font-size: 2.6em; opacity: 0.4; }
.wb-empty-big .hint { font-size: 0.82em; color: #444; max-width: 380px; text-align: center; line-height: 1.5; }

/* ── Floating panels (overlay on the stage) ── */
.float-panel {
  position: absolute; z-index: 20;
  background: rgba(22,22,28,0.92); backdrop-filter: blur(10px);
  border: 1px solid #2e2e36; border-radius: 10px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.5);
  display: flex; flex-direction: column; max-height: calc(100% - 24px);
  overflow: hidden;
}
.float-plan { top: 12px; left: 12px; width: 320px; }
.float-stream { top: 12px; right: 12px; width: 340px; bottom: 12px; }
.float-plan.collapsed { width: auto; }
.float-stream.collapsed { width: auto; bottom: auto; }
.float-stream.drawer { bottom: 12px; max-height: 40%; }

.float-head { display: flex; align-items: center; justify-content: space-between; padding: 9px 12px; cursor: pointer; border-bottom: 1px solid #2a2a30; flex-shrink: 0; user-select: none; }
.float-head:hover { background: rgba(255,255,255,0.03); }
.float-title { font-size: 0.84em; font-weight: 600; color: #c8c8c8; display: flex; align-items: center; gap: 6px; }
.float-count { font-size: 0.78em; background: #2a3a4a; color: #7cb3ff; padding: 1px 7px; border-radius: 8px; font-weight: 500; }
.float-actions { display: flex; align-items: center; gap: 8px; }
.float-toggle { color: #888; font-size: 0.82em; }
.float-clear { background: none; border: 1px solid #333; color: #888; font-size: 0.7em; padding: 1px 8px; border-radius: 4px; cursor: pointer; }
.float-clear:hover { color: #ccc; }
.float-body { overflow-y: auto; padding: 8px; flex: 1; min-height: 0; }
.float-empty { color: #555; font-size: 0.82em; text-align: center; padding: 20px 8px; font-style: italic; line-height: 1.6; }
.float-empty .hint { font-size: 0.9em; color: #444; }

.float-panel.collapsed .float-body { display: none; }
.float-panel.collapsed .float-head { border-bottom: none; }

/* ── Plan cards ── */
.plan-card { background: rgba(255,255,255,0.025); border: 1px solid #2a2a30; border-radius: 8px; padding: 12px; margin-bottom: 10px; }
.plan-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 10px; }
.plan-goal-wrap { flex: 1; min-width: 0; }
.plan-goal { font-weight: 600; font-size: 0.9em; }
.plan-meta { font-size: 0.72em; color: #777; margin-top: 2px; }
.plan-ring { width: 44px; height: 44px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 0.76em; font-weight: 600; position: relative; flex-shrink: 0; }
.plan-ring::after { content: ''; position: absolute; inset: 4px; border-radius: 50%; background: #1c1c22; }
.plan-ring span { position: relative; z-index: 1; }
.plan-steps { display: flex; flex-direction: column; gap: 6px; }
.step { display: flex; align-items: flex-start; gap: 10px; padding: 6px 8px; border-radius: 6px; background: rgba(255,255,255,0.015); }
.step:hover { background: rgba(255,255,255,0.04); }
.step-check { width: 22px; height: 22px; border: 2px solid #444; border-radius: 6px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer; color: #4a9; font-weight: 600; flex-shrink: 0; font-size: 0.78em; transition: all 0.2s; }
.step-num { color: #666; font-size: 0.76em; }
.step.done .step-check { background: #2a5a3a; border-color: #4a9; }
.step.done .step-title { color: #888; text-decoration: line-through; }
.step.in_progress .step-check { border-color: #d29922; color: #d29922; }
.step.in_progress .step-title { color: #d29922; }
.spin { animation: spin 1.5s linear infinite; display: inline-block; }
@keyframes spin { to { transform: rotate(360deg); } }
.step.skipped .step-title { color: #555; text-decoration: line-through; }
.step-content { flex: 1; min-width: 0; }
.step-title { font-size: 0.84em; }
.step-note { font-size: 0.72em; color: #777; margin-top: 2px; display: flex; gap: 6px; }
.step-agent { color: #7cb3ff; }

/* ── Stream rows ── */
.stream-body { padding: 4px 0; }
.event-row { display: flex; align-items: baseline; gap: 6px; padding: 4px 12px; font-size: 0.74em; border-bottom: 1px solid rgba(255,255,255,0.03); }
.ev-time { color: #555; font-family: monospace; flex-shrink: 0; }
.ev-tag { font-size: 0.82em; font-weight: 600; padding: 1px 5px; border-radius: 3px; background: #2a2a2e; color: #888; flex-shrink: 0; }
.ev-tag.PLAN { background: #2a1a3a; color: #b07cf0; }
.ev-tag.AGENT { background: #1e3a5f; color: #7cb3ff; }
.ev-tag.MSG { background: #3a2a1a; color: #d8a878; }
.ev-tag.TOOL { background: #1a2a3a; color: #6fb3d9; }
.ev-tag.BB { background: #1a3a2a; color: #4a9; }
.ev-tag.WF { background: #2a223a; color: #b09cf0; }
.ev-tag.COLLAB { background: #3a2a1a; color: #d29922; }
.ev-source { color: #d29922; font-size: 0.92em; flex-shrink: 0; }
.ev-desc { color: #aaa; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.event-row.ev-err .ev-tag { background: #3a1a1a; color: #f07070; }
.event-row.ev-ok .ev-tag { background: #1a3a1a; color: #5c5; }

/* ── Agent node (Vue Flow custom node) ── */
.agnode { display: flex; align-items: center; gap: 10px; padding: 10px 14px; min-width: 170px; background: #1c1c22; border: 1px solid #333; border-radius: 10px; position: relative; cursor: pointer; }
.agnode:hover { border-color: #555; }
.agnode.busy { border-color: #d29922; box-shadow: 0 0 12px rgba(210,153,34,0.3); }
.agnode.done { border-color: #4a9; }
.agnode.error { border-color: #f07070; }
.agnode-icon { font-size: 1.4em; color: #7cb3ff; }
.agnode.busy .agnode-icon { color: #d29922; }
.agnode-body { flex: 1; min-width: 0; }
.agnode-name { font-weight: 600; font-size: 0.88em; }
.agnode-doing { font-size: 0.74em; color: #888; margin-top: 2px; }
.agnode-dot { position: absolute; top: 7px; right: 7px; width: 8px; height: 8px; border-radius: 50%; background: #555; }
.agnode.busy .agnode-dot { background: #d29922; box-shadow: 0 0 6px rgba(210,153,34,0.8); animation: pulse 1.2s infinite; }
.agnode.done .agnode-dot { background: #4a9; }
@keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.35; } }

/* ── Workflow node ── */
.wfnode { display: flex; align-items: center; gap: 10px; padding: 10px 14px; min-width: 180px; background: #1a1822; border: 1px solid #4a3a6a; border-radius: 10px; position: relative; cursor: pointer; }
.wfnode:hover { border-color: #6a5a8a; }
.wfnode.done { border-color: #4a9; }
.wfnode.error { border-color: #f07070; }
.wfnode-icon { font-size: 1.4em; color: #b09cf0; }
.wfnode.running .wfnode-icon { animation: spin 2s linear infinite; display: inline-block; }
.wfnode-body { flex: 1; min-width: 0; }
.wfnode-name { font-weight: 600; font-size: 0.86em; }
.wfnode-progress { font-size: 0.72em; color: #888; margin-top: 2px; }
.wfnode-tag { position: absolute; top: -9px; right: 8px; font-size: 0.6em; background: #2a223a; color: #b09cf0; padding: 1px 6px; border-radius: 6px; }

/* ── Detail drawer ── */
.drawer { position: absolute; inset: 0; z-index: 40; display: flex; align-items: flex-end; justify-content: center; padding: 16px; background: rgba(0,0,0,0.35); }
.drawer-card { width: 100%; max-width: 560px; max-height: 60%; display: flex; flex-direction: column; background: #1c1c22; border: 1px solid #333; border-radius: 10px; overflow: hidden; }
.drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-bottom: 1px solid #2a2a30; }
.drawer-title { font-weight: 600; font-size: 0.92em; color: #d0d0d0; }
.drawer-body { overflow-y: auto; padding: 6px 0; }
</style>
