<template>
  <div class="glass-panel card">
    <div class="row-top">
      <div class="model-info">
        <div class="model-type" :class="'type-' + model.type">{{ typeIcon }}</div>
        <div class="model-meta">
          <div class="model-name">{{ model.name }}</div>
          <div class="model-id">{{ model.id }}</div>
        </div>
      </div>
      <span class="tag engine-tag" :class="'engine-' + (model.engineStatus || 'unknown')" :title="engineTooltip">
        <span class="engine-dot" :class="engineDotClass"></span>{{ engineLabel }}
      </span>
      <span class="tag" :class="stateClass">{{ stateText }}</span>
      <div class="top-actions">
        <button v-if="showRun" class="btn btn-sm run-toggle" @click="showRunPanel = !showRunPanel">
          {{ showRunPanel ? '收起' : '运行' }}
        </button>
        <button class="btn btn-sm" @click="emit('unload', model.id)">卸载</button>
      </div>
    </div>
    <div v-if="showRun && showRunPanel" class="row-run">
      <input v-model="runInput" type="text" class="run-input" placeholder="输入文本，回车运行..." @keyup.enter="doRun" />
      <button class="btn btn-primary" @click="doRun" :disabled="running">
        {{ running ? '运行中…' : '运行' }}
      </button>
    </div>
    <div v-if="runResult !== null" class="run-result">
      <span class="result-label">输出</span>
      <span class="result-value">{{ runResult }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { modelsApi } from '../api/models'
import { systemApi } from '../api/system'

interface Model {
  id: string; name: string; type: string; state: string
  engine?: string; engineStatus?: string
}

const props = defineProps<{ model: Model }>()
const emit = defineEmits<{ unload: [id: string] }>()

const showRun = ref(false)
const showRunPanel = ref(false)
const runInput = ref('')
const running = ref(false)
const runResult = ref<string | null>(null)

const STATE: Record<string, string> = {
  idle: '空闲', loading: '加载中', ready: '就绪', error: '异常', running: '运行中',
}
const STATE_CLASS: Record<string, string> = {
  idle: 'tag-muted', loading: 'tag-warn', ready: 'tag-ok', error: 'tag-warn', running: 'tag-accent',
}
const ENGINE_LABELS: Record<string, string> = {
  onnx: 'ONNX Runtime', llama: 'llama.cpp', 'safetensors-metadata': 'SafeTensors 元数据', none: '无引擎', placeholder: '占位',
}
const ENGINE_TOOLTIPS: Record<string, string> = {
  live: '真实推理可用', 'metadata-only': '仅读取张量元数据，真实推理需下游引擎',
  unavailable: '引擎 DLL 未找到，当前为占位回显模式', unsupported: '此格式不被原生支持，需转换为 ONNX 或 GGUF',
}

const stateText = computed(() => STATE[props.model.state] || props.model.state)
const stateClass = computed(() => STATE_CLASS[props.model.state] || 'tag-muted')
const typeIcon = computed(() => {
  const icons: Record<string, string> = { placeholder: '◇', onnx: '◆', gguf: '⬡', safetensors: '⊞', pytorch: '△' }
  return icons[props.model.type] || '●'
})
const engineLabel = computed(() => {
  if (props.model.engine === 'none') return '需转换'
  const label = ENGINE_LABELS[props.model.engine || ''] || props.model.engine || '未知引擎'
  if (props.model.engineStatus === 'unavailable') return label + ' 未加载'
  if (props.model.engineStatus === 'metadata-only') return '仅元数据'
  return label
})
const engineTooltip = computed(() => ENGINE_TOOLTIPS[props.model.engineStatus || ''] || '')
const engineDotClass = computed(() => {
  const map: Record<string, string> = { live: 'dot-live', 'metadata-only': 'dot-warn', unavailable: 'dot-dead', unsupported: 'dot-dead' }
  return map[props.model.engineStatus || ''] || 'dot-dead'
})

watch(() => props.model.state, (s) => {
  if (s === 'ready' || s === 'running') showRun.value = true
}, { immediate: true })

async function doRun() {
  if (!runInput.value || running.value) return
  running.value = true; runResult.value = null
  try {
    await systemApi.logToTerminal(`[界面] ModelCard.doRun id=${props.model.id} input=${runInput.value}`)
    runResult.value = await modelsApi.runModel(props.model.id, runInput.value)
  } catch (e: any) {
    runResult.value = '错误: ' + (typeof e === 'string' ? e : (e?.message || String(e)))
  }
  running.value = false
}
</script>

<style scoped>
.card { overflow: hidden; }
.row-top { display: flex; align-items: center; gap: 14px; padding: 14px 18px; }
.model-info { display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1; }
.model-type { width: 36px; height: 36px; display: flex; align-items: center; justify-content: center; border-radius: var(--radius-sm); font-size: 16px; background: rgba(255,255,255,0.05); flex-shrink: 0; }
.type-placeholder { color: var(--accent); }
.type-onnx { color: var(--success); }
.type-gguf { color: #4fc3f7; }
.type-safetensors { color: var(--warning); }
.type-pytorch { color: var(--accent); }
.model-meta { min-width: 0; }
.model-name { font-size: 14px; font-weight: 550; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.model-id { font-size: 12px; color: var(--text-tertiary); font-family: var(--font-mono); }
.top-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.btn-sm { padding: 5px 14px; font-size: 12px; }
.engine-tag { display: inline-flex; align-items: center; gap: 5px; cursor: default; }
.engine-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.dot-live { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5); }
.dot-warn { background: var(--warning); box-shadow: 0 0 3px rgba(255,159,10,0.4); }
.dot-dead { background: var(--danger); box-shadow: 0 0 3px rgba(255,69,58,0.4); }
.engine-live { background: rgba(48,209,88,0.08); color: var(--success); }
.engine-metadata-only { background: rgba(255,159,10,0.08); color: var(--warning); }
.engine-unavailable { background: rgba(255,69,58,0.08); color: var(--danger); }
.engine-unsupported { background: rgba(255,69,58,0.06); color: var(--danger); }
.row-run { display: flex; align-items: center; gap: 10px; padding: 0 18px 14px; }
.run-input { flex: 1; min-width: 0; padding: 8px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: rgba(255,255,255,0.04); color: var(--text-primary); font-size: 13px; font-family: var(--font); outline: none; transition: border-color var(--transition); }
.run-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.run-input::placeholder { color: var(--text-tertiary); }
.run-result { display: flex; gap: 10px; padding: 12px 18px; border-top: 1px solid var(--border-subtle); background: rgba(255,255,255,0.02); font-size: 12px; }
.result-label { font-weight: 600; color: var(--text-secondary); flex-shrink: 0; }
.result-value { color: var(--text-primary); font-family: var(--font-mono); word-break: break-all; }
</style>
