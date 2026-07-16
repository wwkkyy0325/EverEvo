<template>
  <div class="wf-editor">
    <!-- ═══ Left: Workflow List ═══ -->
    <div class="wf-sidebar glass-panel">
      <div class="wf-list-head">
        <h3>工作流</h3>
        <button class="btn btn-sm btn-primary" @click="newWorkflow">+ 新建</button>
      </div>
      <div class="wf-list">
        <div v-if="!workflows.length" class="wf-empty">暂无工作流</div>
        <div v-for="w in workflows" :key="w.id"
          :class="['wf-item', { 'wf-sel': selectedId === w.id }]"
          @click="selectWorkflow(w.id)">
          <div class="wf-item-name">{{ w.name }}</div>
          <div class="wf-item-meta">{{ w.nodeCount }} 节点</div>
          <div class="wf-item-actions" @click.stop>
            <button class="btn btn-xs" @click="duplicateWorkflow(w.id)" title="复制">⧉</button>
            <button class="btn btn-xs" @click="exportWorkflow(w.id)" title="导出">⇧</button>
            <button class="btn btn-xs btn-del" @click="deleteWorkflow(w.id)" title="删除">✕</button>
          </div>
        </div>
      </div>
      <div class="wf-list-foot">
        <label class="btn btn-sm" style="cursor:pointer">
          导入 JSON
          <input type="file" accept=".json" class="wf-file-input" @change="importWorkflow" />
        </label>
      </div>
    </div>

    <!-- ═══ Center: Flow Canvas ═══ -->
    <div class="wf-canvas glass-panel">
      <template v-if="currentWorkflow">
      <!-- Name bar -->
      <div class="wf-canvas-head">
        <input v-model="currentWorkflow.name" class="wf-name-input" placeholder="工作流名称…" @change="markDirty" />
        <span v-if="dirty" class="wf-dirty-hint">已修改</span>
        <span class="wf-spacer"></span>
        <button class="btn btn-sm" @click="saveWorkflow" :disabled="!dirty">保存</button>
        <button class="btn btn-sm" @click="applyAutoLayout" :disabled="!flowNodes.length" title="自动排列节点">⇅ 排列</button>
        <span v-if="execState?.status === 'running'" class="wf-progress">执行中 {{ doneCount }}/{{ flowNodes.length - skippedCount }}{{ skippedCount ? ' · 跳过 ' + skippedCount : '' }}{{ runningProgress ? ' · ' + runningProgress : '' }}</span>
        <button v-if="execState?.status === 'running'" class="btn btn-sm btn-del" @click="stopExecution">■ 停止</button>
        <button v-else class="btn btn-sm btn-go" @click="runWorkflow">▶ 运行</button>
      </div>

      <!-- Vue Flow canvas -->
      <div class="wf-flow-wrap" ref="flowWrapRef"
        @dragover.prevent.capture="onFlowWrapDragOver"
        @drop.prevent.capture="onCanvasDrop">
        <VueFlow
          ref="vueFlowRef"
          v-model:nodes="flowNodes"
          v-model:edges="flowEdges"
          :default-viewport="{ x: 0, y: 0, zoom: 1 }"
          :min-zoom="0.2"
          :max-zoom="2"
          :snap-to-grid="true"
          :snap-grid="[20, 20]"
          :connection-line-style="{ stroke: 'var(--accent)', strokeWidth: 2 }"
          :default-edge-options="{ type: 'smoothstep', animated: false, style: { stroke: 'var(--border-soft)', strokeWidth: 2 } }"
          fit-view-on-init
          @node-click="onNodeClick"
          @pane-click="onPaneClick"
          @connect="onConnect"
          @edge-click="onEdgeClick"
          @node-drag-stop="onNodeDragStop"
        >
          <Background :gap="20" :pattern-color="'rgba(255,255,255,0.03)'" />
          <Controls position="bottom-right" />

          <!-- Custom node template -->
          <template #node-workflow="nodeProps">
            <div :class="['wf-flow-node', nodeClass(nodeProps.id)]"
              @dblclick="onNodeDblClick(nodeProps.id)">
              <!-- Type strip -->
              <div class="wf-flow-strip" :class="'strip-' + nodeProps.data.type"></div>
              <div class="wf-flow-body">
                <div class="wf-flow-head">
                  <span class="wf-flow-icon">{{ nodeProps.data.icon }}</span>
                  <span class="wf-flow-title">{{ nodeProps.data.label }}</span>
                  <span class="wf-flow-dot" :class="dotClass(nodeProps.id)"></span>
                </div>
                <div class="wf-flow-type">{{ nodeProps.data.typeLabel }}</div>
                <div class="wf-flow-progress" v-if="nodeRuns[nodeProps.id]?.status === 'running' && nodeRuns[nodeProps.id]?.progress">{{ nodeRuns[nodeProps.id].progress }}</div>
                <div class="wf-flow-error" v-if="nodeRuns[nodeProps.id]?.error" :title="nodeRuns[nodeProps.id].error">⚠ {{ nodeRuns[nodeProps.id].error }}</div>
                <div class="wf-flow-dur" v-if="nodeDuration(nodeProps.id)">⏱ {{ nodeDuration(nodeProps.id) }}</div>
              </div>
              <!-- Delete button -->
              <button class="wf-flow-del" @click.stop="removeFlowNode(nodeProps.id)" title="删除">✕</button>

              <!-- Vue Flow handles (connection points) -->
              <Handle type="target" :position="Position.Top"
                :style="{ background: 'var(--border-soft)', width: '10px', height: '10px', border: '2px solid var(--bg-elevated)' }" />
              <Handle type="source" :position="Position.Bottom"
                :style="{ background: 'var(--accent)', width: '10px', height: '10px', border: '2px solid var(--bg-elevated)' }" />
              <!-- Condition nodes get two source handles -->
              <Handle v-if="nodeProps.data.type === 'condition'"
                id="true" type="source" :position="Position.Right"
                :style="{ top: '30%', background: 'var(--success)', width: '8px', height: '8px' }"
                :title="nodeProps.data.trueLabel || 'True'" />
              <Handle v-if="nodeProps.data.type === 'condition'"
                id="false" type="source" :position="Position.Left"
                :style="{ top: '70%', background: 'var(--danger)', width: '8px', height: '8px' }"
                :title="nodeProps.data.falseLabel || 'False'" />
            </div>
          </template>

          <!-- Edge label for condition branches -->
          <template #edge-custom="edgeProps">
            <BaseEdge :path="(edgeProps as any).path" :style="(edgeProps as any).style" />
            <EdgeLabelRenderer>
              <div v-if="(edgeProps as any).data?.label" class="wf-edge-label"
                :style="{ transform: `translate(-50%, -50%) translate(${(edgeProps as any).labelX}px,${(edgeProps as any).labelY}px)` }">
                {{ (edgeProps as any).data.label }}
              </div>
            </EdgeLabelRenderer>
          </template>
        </VueFlow>
      </div>

      <!-- Execution result panel (shown when a run is not running) -->
      <div class="wf-result" v-if="execState && execState.status !== 'running'">
        <div class="wf-result-head">
          <span :class="['wf-result-tag', 'tag-' + execState.status]">{{ execState.status === 'done' ? '✓ 完成' : execState.status === 'error' ? '✗ 出错' : '已取消' }}</span>
          <span v-if="selectedNodeId && nodeRuns[selectedNodeId]" class="wf-result-node">节点：{{ nodeName(selectedNodeId) }} <span v-if="nodeDuration(selectedNodeId)" class="wf-result-dur">· {{ nodeDuration(selectedNodeId) }}</span></span>
          <span class="wf-spacer"></span>
          <button class="btn btn-xs" @click="execState = null" title="关闭">✕</button>
        </div>
        <pre v-if="execState.error" class="wf-result-error">{{ execState.error }}</pre>
        <pre v-else-if="selectedNodeId && nodeRuns[selectedNodeId]?.output != null" class="wf-result-out">{{ formatResult(nodeRuns[selectedNodeId].output) }}</pre>
        <pre v-else-if="execState.outputs && Object.keys(execState.outputs).length" class="wf-result-out">{{ formatResult(execState.outputs) }}</pre>
        <div v-else class="wf-result-empty">点击节点查看其输出</div>
      </div>

      <!-- Add node palette -->
      <div class="wf-palette">
        <button v-for="nt in paletteTypes" :key="nt.type" class="wf-palette-btn"
          draggable="true"
          @dragstart="onPaletteDragStart($event, nt.type)"
          @dragend="onPaletteDragEnd"
          @click="addFlowNode(nt.type)" :title="nt.desc + ' — 点击或拖入画布'">
          <span class="wf-palette-icon">{{ nt.icon }}</span>
          <span class="wf-palette-label">{{ nt.label }}</span>
        </button>
      </div>
      </template>
      <!-- Empty canvas state -->
      <div v-else class="wf-canvas-empty">
        <span>从左侧列表选择或新建一个工作流</span>
      </div>
    </div>

    <!-- ═══ Right: Config Panel ═══ -->
    <WorkflowNodeConfigPanel
      v-if="selectedNodeId && currentWorkflow"
      ref="configPanelRef"
      :node-id="selectedNodeId"
      :flow-nodes="flowNodes"
      :tools="availableTools"
      :agents="availableAgents"
      @close="selectedNodeId = null"
      @applied="onConfigApplied"
    />
    <div v-else-if="currentWorkflow" class="wf-config wf-config-empty glass-panel">
      <span>点击画布上的节点编辑配置</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { VueFlow, Position, Handle, useVueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { BaseEdge, EdgeLabelRenderer } from '@vue-flow/core'
import { useToast } from '../composables/useToast'
import WorkflowNodeConfigPanel from './WorkflowNodeConfigPanel.vue'
import { TYPE_META, flowToWorkflowNode, workflowToFlowNode, workflowEdgeToFlowEdge, flowEdgeToWorkflowEdge } from '../utils/workflow-mapper'
import { autoLayout } from '../utils/workflow-layout'
import { workflowApi } from '../api/workflow'
import { skillsApi } from '../api/skills'
import { agentsApi } from '../api/agents'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'

// ── Type metadata ──
const PALETTE_ORDER = ['input', 'llm', 'tool', 'agent', 'http', 'condition', 'code', 'loop', 'merge', 'delay', 'notify', 'custom', 'output']

// ── Toast ──
const toast = useToast()

// ── State ──
const workflows = ref<any[]>([])
const selectedId = ref<string | null>(null)
const currentWorkflow = ref<any>(null)
const dirty = ref(false)
const availableTools = ref<any[]>([])
const availableAgents = ref<any[]>([])

// Vue Flow state
const vueFlowRef = ref<any>(null)
const { fitView } = useVueFlow()
const flowWrapRef = ref<HTMLElement | null>(null)
// Carries the palette-dragged node type. We cannot rely solely on
// dataTransfer here: Wails/WebView2 drops custom MIME types, so getData()
// returns "" at drop time and the drop silently aborts.
const draggingType = ref<string | null>(null)
const flowNodes = ref<any[]>([])
const flowEdges = ref<any[]>([])
const paletteTypes = PALETTE_ORDER.map(t => ({ type: t, ...TYPE_META[t] }))

// Selection
const selectedNodeId = ref<string | null>(null)

// Config panel ref
const configPanelRef = ref<InstanceType<typeof WorkflowNodeConfigPanel> | null>(null)

// Execution
const execId = ref<string | null>(null)
const execState = ref<any>(null)
const nodeRuns = reactive<Record<string, any>>({})

let counter = 0

// Internal flags
let _restoring = false

// ── Computed ──
const doneCount = computed(() => {
  return Object.values(nodeRuns).filter((r: any) => r.status === 'done').length
})
const skippedCount = computed(() => {
  return Object.values(nodeRuns).filter((r: any) => r.status === 'skipped').length
})
// formatResult renders a node/exec result as readable text.
function formatResult(v: any): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  try { return JSON.stringify(v, null, 2) } catch (_) { return String(v) }
}
function nodeName(id: string): string {
  const n = flowNodes.value.find((fn: any) => fn.id === id)
  return n?.data?.label || id
}
// nodeDuration returns a finished node's run duration, or '' while running.
function nodeDuration(id: string): string {
  const r = nodeRuns[id]
  if (!r?.startedAt || !r.finishedAt) return ''
  const ms = r.finishedAt - r.startedAt
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`
}

// Progress text of the currently-running node, shown in the header.
const runningProgress = computed(() => {
  for (const id of Object.keys(nodeRuns)) {
    const r = nodeRuns[id]
    if (r.status === 'running' && r.progress) return r.progress
  }
  return ''
})

// Name of the per-run progress event we are currently subscribed to (cleaned up on reset/unmount).
let _progressEvt: string | null = null

// ── Watchers ──
watch(
  () => currentWorkflow.value?.nodes,
  () => { if (currentWorkflow.value && !_restoring) syncFlowToWorkflow() },
  { deep: true }
)
watch(
  () => currentWorkflow.value?.edges,
  () => { if (currentWorkflow.value && !_restoring) syncFlowToWorkflow() },
  { deep: true }
)

// ── Lifecycle ──

// Event handler refs (must be stable for EventsOff)
const _evtNodeStart = (d: any) => { nodeRuns[d.nodeId] = { status: 'running', output: null, error: '', progress: '', startedAt: Date.now() } }
const _evtNodeDone  = (d: any) => { const p = nodeRuns[d.nodeId]; nodeRuns[d.nodeId] = { status: 'done', output: d.output, error: '', progress: '', startedAt: p?.startedAt, finishedAt: Date.now() } }
const _evtNodeError = (d: any) => { const p = nodeRuns[d.nodeId]; nodeRuns[d.nodeId] = { status: 'error', output: null, error: d.error, progress: '', startedAt: p?.startedAt, finishedAt: Date.now() } }
const _evtNodeSkip  = (d: any) => { nodeRuns[d.nodeId] = { status: 'skipped', output: null, error: '', progress: '' } }
// Progress heartbeat: long nodes (LLM/agent) emit workflow-progress-<execId>
// every 2s with {nodeId, progress}. Merge into the running node so the canvas
// shows "正在生成…" live instead of going silent for ~30s.
const _evtProgress = (d: any) => {
  if (!d?.nodeId) return
  const cur = nodeRuns[d.nodeId]
  nodeRuns[d.nodeId] = { status: cur?.status || 'running', output: cur?.output ?? null, error: cur?.error || '', progress: d.progress || '' }
}
const _evtExecDone  = (d: any) => { execState.value = { status: 'done', outputs: d.outputs }; toast.show('success', '执行完成', '') }
const _evtExecError = (d: any) => { execState.value = { status: 'error', error: d.error }; toast.show('error', '执行出错', d.error || '') }
const _evtExecCancel = () => { execState.value = { status: 'cancelled' } }
// Live-sync: when a workflow mutates from another source (e.g. the LLM editing
// it via tool calls), refresh what the user is looking at so changes are visible.
const _evtWfChanged = async (d: any) => {
  const action = d?.action, id = d?.id as string
  await loadWorkflows()
  if (action === 'update' && id && id === selectedId.value && currentWorkflow.value && !dirty.value) {
    // Reload the viewed workflow to show the external change. Skip when the user
    // has unsaved edits so we don't clobber their work.
    try { currentWorkflow.value = await workflowApi.get(id); restoreFlowFromWorkflow() } catch (_) {}
  } else if (action === 'delete' && id === selectedId.value) {
    currentWorkflow.value = null; selectedId.value = null
    flowNodes.value = []; flowEdges.value = []
  } else if (action === 'create' && id && !selectedId.value) {
    // Auto-open a newly created workflow when nothing is selected, so the user
    // can watch it take shape as subsequent updates stream in.
    await selectWorkflow(id)
  }
}

onMounted(async () => {
  await loadWorkflows()
  await loadTools()
  await loadAgents()
  window.runtime.EventsOn('wf-node-start', _evtNodeStart)
  window.runtime.EventsOn('wf-node-done', _evtNodeDone)
  window.runtime.EventsOn('wf-node-error', _evtNodeError)
  window.runtime.EventsOn('wf-node-skip', _evtNodeSkip)
  window.runtime.EventsOn('wf-exec-done', _evtExecDone)
  window.runtime.EventsOn('wf-exec-error', _evtExecError)
  window.runtime.EventsOn('wf-exec-cancel', _evtExecCancel)
  window.runtime.EventsOn('workflow:changed', _evtWfChanged)
})

onBeforeUnmount(() => {
  const evts = ['wf-node-start','wf-node-done','wf-node-error','wf-node-skip','wf-exec-done','wf-exec-error','wf-exec-cancel','workflow:changed']
  evts.forEach(e => { try { window.runtime.EventsOff(e) } catch (_) {} })
  if (_progressEvt) { try { window.runtime.EventsOff(_progressEvt) } catch (_) {} ; _progressEvt = null }
})

// ── Sync workflow model -> flow canvas ──
function restoreFlowFromWorkflow() {
  if (!currentWorkflow.value) return
  _restoring = true
  const nodes = (currentWorkflow.value.nodes || []).map((n: any) => workflowToFlowNode(n, nodeRuns))
  const edges = (currentWorkflow.value.edges || []).map((e: any) => workflowEdgeToFlowEdge(e))
  // Any coordinate-less node (the backend stored none, or the LLM just added
  // one) triggers a full dagre layout so the canvas is always tidy.
  if (nodes.some((n: any) => !n.position)) {
    const pos = autoLayout(nodes, edges)
    nodes.forEach((n: any) => { n.position = pos[n.id] || { x: 0, y: 0 } })
  }
  flowNodes.value = nodes
  flowEdges.value = edges
  nextTick(() => { _restoring = false })
}

// Force a full dagre relayout of the current canvas (the "auto-arrange" button).
function applyAutoLayout() {
  if (!flowNodes.value.length) return
  const pos = autoLayout(flowNodes.value, flowEdges.value)
  flowNodes.value = flowNodes.value.map((n: any) => ({ ...n, position: pos[n.id] || n.position }))
  syncFlowToWorkflow()
  nextTick(() => { try { fitView() } catch (_) {} })
}

// Sync flow canvas -> workflow model (called on drag stop, connect, delete, etc.)
function syncFlowToWorkflow() {
  if (!currentWorkflow.value) return
  // Guard with _restoring: reassigning currentWorkflow.nodes/edges would otherwise
  // retrigger the deep watchers above, which call this function again — a recursive
  // update loop. Vue then aborts the render ("Maximum recursive updates exceeded"),
  // so the just-added node never appears. That was the real reason nodes could not be
  // added by click or drop.
  _restoring = true
  currentWorkflow.value.nodes = flowNodes.value.map(fn => flowToWorkflowNode(fn))
  currentWorkflow.value.edges = flowEdges.value.map(fe => flowEdgeToWorkflowEdge(fe))
  nextTick(() => { _restoring = false })
  markDirty()
}

// ── Workflow CRUD ──
async function loadWorkflows() {
  try { workflows.value = await workflowApi.list() || [] } catch (_) {}
}
async function selectWorkflow(id: string) {
  if (dirty.value && !await toast.confirm('未保存', '当前有未保存的修改，确定切换？')) return
  try {
    const wf = await workflowApi.get(id)
    currentWorkflow.value = wf
    selectedId.value = id
    selectedNodeId.value = null
    dirty.value = false
    resetExec()
    restoreFlowFromWorkflow()
    nextTick(() => { try { fitView() } catch (_) {} })
  } catch (e: any) { toast.show('error', '加载失败', e.message || e) }
}
async function newWorkflow() {
  if (dirty.value && !await toast.confirm('未保存', '当前有未保存的修改？')) return
  const name = '新工作流 ' + new Date().toLocaleTimeString('zh-CN')
  try {
    await workflowApi.create({ id: '', name, nodes: [], edges: [], variables: {}, createdAt: 0, updatedAt: 0 } as any)
    await loadWorkflows()
    const list = await workflowApi.list()
    const found = list.find((w: any) => w.name === name)
    if (found) await selectWorkflow(found.id)
  } catch (e: any) { toast.show('error', '创建失败', e.message || e) }
}
async function saveWorkflow() {
  if (!currentWorkflow.value) return
  syncFlowToWorkflow()
  try {
    await workflowApi.update(currentWorkflow.value.id, currentWorkflow.value)
    dirty.value = false
    await loadWorkflows()
    toast.show('success', '已保存', currentWorkflow.value.name)
  } catch (e: any) { toast.show('error', '保存失败', e.message || e) }
}
async function deleteWorkflow(id: string) {
  const wf = workflows.value.find(w => w.id === id)
  if (!await toast.confirm('删除工作流', '确定删除「' + (wf ? wf.name : id) + '」？')) return
  try {
    await workflowApi.remove(id)
    if (selectedId.value === id) { currentWorkflow.value = null; selectedId.value = null; flowNodes.value = []; flowEdges.value = [] }
    await loadWorkflows()
  } catch (e: any) { toast.show('error', '删除失败', e.message || e) }
}
async function duplicateWorkflow(id: string) {
  try { await workflowApi.duplicate(id); await loadWorkflows(); toast.show('success', '已复制') } catch (e: any) { toast.show('error', '复制失败', e.message || e) }
}
async function exportWorkflow(id: string) {
  syncFlowToWorkflow()
  try {
    const data = await workflowApi.export(id)
    const blob = new Blob([JSON.stringify(JSON.parse(data), null, 2)], { type: 'application/json' })
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = 'workflow.json'; a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) { toast.show('error', '导出失败', e.message || e) }
}
async function importWorkflow(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]; if (!file) return
  try {
    await workflowApi.import(JSON.parse(await file.text()))
    await loadWorkflows(); toast.show('success', '已导入')
  } catch (e: any) { toast.show('error', '导入失败', e.message || e) }
  ;(e.target as HTMLInputElement).value = ''
}

// ── Dirty flag ──
function markDirty() { dirty.value = true }

// ── Flow Canvas Events ──

// Drag from palette to canvas
function onPaletteDragStart(e: DragEvent, type: string) {
  draggingType.value = type
  if (e.dataTransfer) {
    // Standard MIME only; the real carrier is draggingType (see note above).
    e.dataTransfer.setData('text/plain', type)
    e.dataTransfer.effectAllowed = 'copy'
  }
}
function onPaletteDragEnd() {
  draggingType.value = null
}
function onFlowWrapDragOver(e: DragEvent) {
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
}

function onCanvasDrop(e: DragEvent) {
  const type = draggingType.value || e.dataTransfer?.getData('text/plain') || ''
  if (!type) return
  const vf = vueFlowRef.value
  const wrap = flowWrapRef.value
  if (!wrap) return
  const rect = wrap.getBoundingClientRect()
  const screenX = e.clientX - rect.left
  const screenY = e.clientY - rect.top
  let pos = { x: screenX, y: screenY }
  if (vf && typeof vf.project === 'function') {
    const projected = vf.project({ x: screenX, y: screenY })
    pos = { x: projected.x, y: projected.y }
  }
  addFlowNode(type, pos)
}

function addFlowNode(type: string, pos?: { x: number; y: number }) {
  if (!currentWorkflow.value) return
  const meta = TYPE_META[type]
  const id = 'n' + (++counter)
  let config: any = {}
  if (type === 'input') config = { fields: [] }
  else if (type === 'llm') config = { systemPrompt: '', userPrompt: '', tools: [], temperature: 0.7 }
  else if (type === 'tool') config = { toolName: '', params: {} }
  else if (type === 'http') config = { url: '', method: 'GET', headers: {}, body: '', outputField: 'response' }
  else if (type === 'condition') config = { expression: '', trueLabel: '是', falseLabel: '否' }
  else if (type === 'code') config = { template: '', outputField: 'output' }
  else if (type === 'loop') config = { sourceExpression: '', itemVariable: 'item', subNodeIDs: [], maxIterations: 100 }
  else if (type === 'agent') config = { agentId: '', userPrompt: '' }
  else if (type === 'merge') config = { waitFor: 'all', timeout: 30000 }
  else if (type === 'delay') config = { durationMs: 1000 }
  else if (type === 'notify') config = { title: '', message: '', level: 'info' }
  else if (type === 'custom') config = { script: '', language: 'go', timeout: 30000 }
  else if (type === 'output') config = { fields: [] }
  const node = {
    id,
    type: 'workflow',
    position: pos || { x: 100 + Math.random() * 200, y: 100 + Math.random() * 200 },
    data: {
      type, icon: meta.icon, label: meta.label, typeLabel: meta.label, desc: '', status: 'pending',
      config,
      trueLabel: 'True', falseLabel: 'False',
    },
    draggable: true, selectable: true,
  }
  flowNodes.value.push(node)
  syncFlowToWorkflow()
  selectedNodeId.value = id
}
function removeFlowNode(id: string) {
  flowNodes.value = flowNodes.value.filter(n => n.id !== id)
  flowEdges.value = flowEdges.value.filter(e => e.source !== id && e.target !== id)
  if (selectedNodeId.value === id) selectedNodeId.value = null
  syncFlowToWorkflow()
}
function onNodeClick({ node }: { node: any }) {
  selectedNodeId.value = node.id
}
function onNodeDblClick(id: string) {
  selectedNodeId.value = id
}
function onPaneClick() {
  selectedNodeId.value = null
}
function onConnect(connection: any) {
  const id = connection.source + '→' + (connection.sourceHandle || 'output') + '→' + connection.target
  if (flowEdges.value.find(e => e.id === id)) return // no duplicates
  flowEdges.value.push({
    id,
    source: connection.source,
    target: connection.target,
    sourceHandle: connection.sourceHandle || 'output',
    type: 'smoothstep',
    style: { stroke: 'var(--border-soft)', strokeWidth: 2 },
    data: { label: connection.sourceHandle === 'true' ? 'T' : connection.sourceHandle === 'false' ? 'F' : '' },
  })
  syncFlowToWorkflow()
}
function onEdgeClick({ edge }: { edge: any }) {
  flowEdges.value = flowEdges.value.filter(e => e.id !== edge.id)
  syncFlowToWorkflow()
}
function onNodeDragStop() {
  syncFlowToWorkflow()
}

// ── Node config (right panel) ──
function onConfigApplied(cfg: Record<string, any>) {
  const fn = flowNodes.value.find((n: any) => n.id === selectedNodeId.value)
  if (!fn) return
  fn.data.label = cfg.label
  fn.data.desc = cfg.desc
  if (cfg.config) fn.data.config = cfg.config
  if (cfg.trueLabel !== undefined) fn.data.trueLabel = cfg.trueLabel
  if (cfg.falseLabel !== undefined) fn.data.falseLabel = cfg.falseLabel
  syncFlowToWorkflow()
}

// ── Node display helpers ──
// nodeClass derives border styling from nodeRuns (the single source of truth,
// updated instantly by events) — no 300ms timer bridging a second data.status copy.
function nodeClass(nodeId: string) {
  const s = (nodeRuns[nodeId] || {}).status || 'pending'
  return { 'wf-node-running': s === 'running', 'wf-node-done': s === 'done', 'wf-node-error': s === 'error', 'wf-node-skipped': s === 'skipped' }
}
function dotClass(nodeId: string) {
  const s = (nodeRuns[nodeId] || {}).status
  return { 'dot-live': s === 'done', 'dot-dead': s === 'error', 'dot-pulse': s === 'running', 'dot-off': !s || s === 'pending' || s === 'skipped' }
}

// ── Load Tools ──
async function loadTools() {
  try { availableTools.value = await skillsApi.listTools() || [] } catch (_) {}
}

// ── Load Agents (for the agent node config dropdown) ──
async function loadAgents() {
  try { availableAgents.value = await agentsApi.list() || [] } catch (_) {}
}

// ── Execution ──
async function runWorkflow() {
  if (!currentWorkflow.value) return
  syncFlowToWorkflow()
  const v = await workflowApi.validate(currentWorkflow.value)
  if (v && !v.valid) { toast.show('error', '验证失败', (v.issues || []).join('\n')); return }
  if (dirty.value) await saveWorkflow()
  resetExec()
  execState.value = { status: 'running' }
  try {
    const inputs: Record<string, any> = {}
    currentWorkflow.value.nodes.filter((n: any) => n.type === 'input').forEach((n: any) => {
      ;(n.config.fields || []).forEach((f: any) => { if (f.name) inputs[f.name] = f.default })
    })
    execId.value = await workflowApi.execute(currentWorkflow.value.id, inputs)
  } catch (e: any) {
    execState.value = { status: 'error', error: e.message || e }
    toast.show('error', '启动失败', e.message || e)
  }
  // Subscribe to this run's progress heartbeat (dynamic event name).
  if (execId.value) {
    _progressEvt = `workflow-progress-${execId.value}`
    try { window.runtime.EventsOn(_progressEvt, _evtProgress) } catch (_) {}
  }
}
async function stopExecution() {
  if (!execId.value) return
  try { await workflowApi.cancel(execId.value) } catch (_) {}
  execState.value = { status: 'cancelled' }
}
function resetExec() {
  execId.value = null; execState.value = null
  Object.keys(nodeRuns).forEach(k => delete nodeRuns[k])
  if (_progressEvt) { try { window.runtime.EventsOff(_progressEvt) } catch (_) {} ; _progressEvt = null }
}
</script>

<style scoped>
.wf-editor { display: flex; gap: 12px; flex: 1; min-height: 0; height: 100%; }
.wf-editor > * { min-height: 0; }

/* ── Left Sidebar ── */
.wf-sidebar { width: 190px; flex-shrink: 0; padding: 12px; display: flex; flex-direction: column; gap: 8px; overflow-y: auto; }
.wf-list-head { display: flex; align-items: center; justify-content: space-between; }
.wf-list-head h3 { font-size: 14px; font-weight: 600; margin: 0; }
.wf-list { display: flex; flex-direction: column; gap: 3px; flex: 1; }
.wf-empty { font-size: 12px; color: var(--text-tertiary); text-align: center; padding: 20px 0; }
.wf-item { padding: 8px 10px; border-radius: var(--radius-sm); cursor: pointer; background: var(--bg-elevated); border: 1px solid transparent; transition: all var(--transition); }
.wf-item:hover { border-color: var(--border-soft); }
.wf-item.wf-sel { border-color: var(--accent); background: var(--accent-dim); }
.wf-item-name { font-size: 12px; font-weight: 600; }
.wf-item-meta { font-size: 10px; color: var(--text-tertiary); margin-top: 1px; }
.wf-item-actions { display: flex; gap: 2px; margin-top: 3px; opacity: 0; transition: opacity 0.15s; }
.wf-item:hover .wf-item-actions { opacity: 1; }
.wf-list-foot { padding-top: 6px; border-top: 1px solid var(--border-subtle); }
.wf-file-input { display: none; }

/* ── Center Canvas ── */
.wf-canvas { flex: 1; min-width: 0; display: flex; flex-direction: column; overflow: hidden; }
.wf-canvas-empty { display: flex; align-items: center; justify-content: center; color: var(--text-tertiary); font-size: 13px; }
.wf-canvas-head { display: flex; align-items: center; gap: 8px; padding: 8px 14px; border-bottom: 1px solid var(--border-subtle); flex-shrink: 0; }
.wf-name-input { flex: 1; max-width: 280px; padding: 5px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 14px; font-weight: 600; outline: none; }
.wf-name-input:focus { border-color: var(--accent); }
.wf-dirty-hint { font-size: 11px; color: var(--warning); }
.wf-progress { font-size: 12px; color: var(--accent); font-weight: 500; }
.wf-flow-wrap { flex: 1; min-height: 0; position: relative; }

/* ── Flow Node Styles ── */
.wf-flow-node {
  background: var(--bg-elevated); border: 1px solid var(--border-subtle);
  border-radius: var(--radius); min-width: 160px; max-width: 220px;
  display: flex; overflow: hidden; box-shadow: var(--shadow-card);
  transition: border-color 0.2s, box-shadow 0.2s; position: relative;
}
.wf-flow-node:hover { border-color: var(--border-soft); }
.wf-flow-node:hover .wf-flow-del { opacity: 1; }
.wf-flow-node.wf-node-running { border-color: var(--accent); box-shadow: 0 0 10px rgba(0,122,255,0.2); }
.wf-flow-node.wf-node-done { border-color: rgba(48,209,88,0.4); }
.wf-flow-node.wf-node-error { border-color: rgba(255,69,58,0.5); }
.wf-flow-node.wf-node-skipped { opacity: 0.45; }
.wf-flow-strip { width: 5px; flex-shrink: 0; }
.strip-input { background: var(--text-tertiary); }
.strip-llm { background: var(--accent); }
.strip-tool { background: var(--success); }
.strip-condition { background: var(--warning); }
.strip-code { background: #a855f7; }
.strip-loop { background: #f97316; }
.strip-agent { background: #14b8a6; }
.strip-output { background: var(--text-tertiary); }
.strip-http { background: #22d3ee; }
.strip-delay { background: #f59e0b; }
.strip-notify { background: #f97316; }
.strip-merge { background: #a78bfa; }
.strip-custom { background: #ec4899; }
.wf-flow-body { flex: 1; padding: 10px 12px; min-width: 0; }
.wf-flow-head { display: flex; align-items: center; gap: 6px; }
.wf-flow-icon { font-size: 15px; line-height: 1; }
.wf-flow-title { font-size: 12px; font-weight: 600; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.wf-flow-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.wf-flow-type { font-size: 10px; color: var(--text-tertiary); margin-top: 2px; }
.wf-flow-progress { font-size: 10px; color: var(--accent); margin-top: 3px; white-space: pre-wrap; max-height: 96px; overflow: hidden; }
.wf-flow-error { font-size: 10px; color: var(--danger); margin-top: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.wf-flow-dur { font-size: 9px; color: var(--text-tertiary); margin-top: 2px; }
/* Execution result panel */
.wf-result { border-top: 1px solid var(--border-soft); padding: 8px 12px; max-height: 160px; overflow: auto; background: var(--bg-elevated); }
.wf-result-head { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.wf-result-tag { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: var(--radius-sm); }
.wf-result-tag.tag-done { background: rgba(40,167,69,0.15); color: var(--success); }
.wf-result-tag.tag-error { background: rgba(220,53,69,0.15); color: var(--danger); }
.wf-result-tag.tag-cancelled { background: var(--border-soft); color: var(--text-secondary); }
.wf-result-node { font-size: 11px; color: var(--text-secondary); }
.wf-result-dur { color: var(--text-tertiary); }
.wf-result-error { margin: 0; font-family: var(--font-mono); font-size: 11px; color: var(--danger); white-space: pre-wrap; }
.wf-result-out { margin: 0; font-family: var(--font-mono); font-size: 11px; color: var(--text-secondary); white-space: pre-wrap; }
.wf-result-empty { font-size: 11px; color: var(--text-tertiary); }
.wf-flow-del { position: absolute; top: 4px; right: 4px; width: 18px; height: 18px; border: none; border-radius: 4px; background: transparent; color: var(--text-tertiary); font-size: 9px; cursor: pointer; display: flex; align-items: center; justify-content: center; opacity: 0; transition: all 0.15s; z-index: 10; }
.wf-flow-del:hover { background: var(--danger-dim); color: var(--danger); }

/* ── Edge Labels ── */
.wf-edge-label { font-size: 10px; font-weight: 600; color: var(--text-primary); background: var(--bg-elevated); border: 1px solid var(--border-soft); border-radius: 4px; padding: 1px 5px; pointer-events: none; }

/* ── Bottom Palette ── */
.wf-palette { display: flex; gap: 4px; padding: 6px 10px; border-top: 1px solid var(--border-subtle); flex-shrink: 0; overflow-x: auto; }
.wf-palette-btn { display: flex; align-items: center; gap: 4px; padding: 5px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); cursor: pointer; white-space: nowrap; font-size: 11px; color: var(--text-secondary); transition: all 0.12s; flex-shrink: 0; }
.wf-palette-btn:hover { border-color: var(--accent); color: var(--text-primary); background: var(--bg-hover); }
.wf-palette-icon { font-size: 14px; }
.wf-palette-label { font-weight: 500; }

/* ── Right Config ── */
.wf-config { width: 260px; flex-shrink: 0; padding: 12px 14px; overflow-y: auto; }
.wf-config-empty { display: flex; align-items: center; justify-content: center; color: var(--text-tertiary); font-size: 12px; }
.wf-cfg-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.wf-cfg-head h4 { font-size: 13px; font-weight: 600; margin: 0; }
.wf-cfg-close { width: 22px; height: 22px; border: none; border-radius: 5px; background: transparent; color: var(--text-tertiary); font-size: 12px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.wf-cfg-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.wf-cfg-group { display: flex; flex-direction: column; gap: 5px; margin-bottom: 10px; }
.wf-cfg-group label { font-size: 11px; font-weight: 500; color: var(--text-secondary); }
.wf-cfg-group .hint { font-weight: 400; color: var(--text-tertiary); }
.wf-cfg-row { display: flex; gap: 4px; align-items: center; }
.wf-cfg-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.wf-cfg-checks { max-height: 140px; overflow-y: auto; display: flex; flex-direction: column; gap: 2px; }
.wf-cfg-check { display: flex; align-items: center; gap: 6px; font-size: 11px; cursor: pointer; }

/* Dots */
.dot-live { background: var(--success); box-shadow: 0 0 5px rgba(48,209,88,0.5); }
.dot-dead { background: var(--danger); box-shadow: 0 0 4px rgba(255,69,58,0.45); }
.dot-pulse { background: var(--accent); box-shadow: 0 0 6px rgba(0,122,255,0.5); animation: wf-pulse 1s ease-in-out infinite; }
.dot-off { background: var(--text-tertiary); opacity: 0.4; }
@keyframes wf-pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }

/* Glue */
.field { padding: 6px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; outline: none; font-family: var(--font-mono); box-sizing: border-box; }
.field:focus { border-color: var(--accent); }
.wf-field { width: 100%; }
.wf-field-sm { flex: 1; }
.wf-textarea { resize: vertical; font-family: var(--font-mono); font-size: 11px; line-height: 1.5; width: 100%; }
.wf-spacer { flex: 1; }
.btn { padding: 5px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; white-space: nowrap; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 8px !important; font-size: 11px; }
.btn-xs { padding: 2px 5px; font-size: 10px; line-height: 1.3; border: 1px solid var(--border-soft); border-radius: 4px; background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer; }
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn-del { color: var(--danger); border-color: rgba(255,69,58,0.3); }
.btn-del:hover { background: var(--danger-dim); }
.btn-go { color: var(--success); border-color: rgba(48,209,88,0.3); }
.btn-go:hover { background: rgba(48,209,88,0.1); }
.btn:disabled { opacity: 0.4; cursor: default; }
</style>
