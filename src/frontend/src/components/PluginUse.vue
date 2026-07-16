<template>
  <div class="pu">
    <div class="pu-head"><button class="btn btn-sm" @click="emit('back')">← 返回</button><span class="pu-name">{{ spec.name }}</span><span class="tag tag-muted">v{{ spec.version }}</span></div>
    <div class="pu-methods"><button v-for="m in spec.methods" :key="m" class="pu-method-btn" :class="{ active: method === m }" @click="selectMethod(m)">{{ m }}</button></div>
    <DynamicForm v-if="currentSchema" :key="method" :schema="currentSchema" :busy="busy" @submit="execute" />
    <div v-else class="pu-noschema">该方法无输入参数，点击执行直接调用<button class="btn btn-sm btn-primary" @click="execute({})" :disabled="busy" style="margin-left:8px">{{ busy ? '执行中…' : '执行' }}</button></div>
    <ResultPanel v-if="result || error" :schema="currentOutputSchema" :result="result" :error="error" />
    <details v-if="rawResponse" class="pu-raw"><summary>原始响应 (JSON)</summary><pre>{{ rawResponse }}</pre></details>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import DynamicForm from './viewers/DynamicForm.vue'
import ResultPanel from './viewers/ResultPanel.vue'
import { pluginsApi } from '../api/plugins'

const props = defineProps<{ spec: { name: string; version: string; methods: string[]; inputSchema?: Record<string, any>; outputSchema?: Record<string, any> } }>()
const emit = defineEmits<{ back: [] }>()

const method = ref('')
const busy = ref(false)
const result = ref<any>(null)
const error = ref<string | null>(null)
const rawResponse = ref<string | null>(null)

const currentSchema = computed(() => {
  if (!props.spec.inputSchema || !method.value) return null
  return props.spec.inputSchema[method.value] || null
})
const currentOutputSchema = computed(() => {
  if (!props.spec.outputSchema || !method.value) return null
  return props.spec.outputSchema[method.value] || null
})

// Default to first custom (non-infra) method
const methods = props.spec.methods || []
const custom = methods.filter((m: string) => m !== 'info' && m !== 'health')
method.value = custom[0] || methods[0] || ''

function selectMethod(m: string) { method.value = m; result.value = null; error.value = null; rawResponse.value = null }
async function execute(params: Record<string, any>) {
  busy.value = true; result.value = null; error.value = null; rawResponse.value = null
  try {
    const res = await pluginsApi.execute(props.spec.name, method.value, params || {})
    rawResponse.value = JSON.stringify(res, null, 2)
    result.value = res
  } catch (e: any) { error.value = typeof e === 'string' ? e : (e?.message || String(e)) }
  busy.value = false
}
</script>

<style scoped>
.pu { display: flex; flex-direction: column; gap: 16px; }
.pu-head { display: flex; align-items: center; gap: 10px; }
.pu-name { font-size: 18px; font-weight: 600; }
.pu-methods { display: flex; gap: 4px; flex-wrap: wrap; }
.pu-method-btn { padding: 4px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-secondary); font-size: 12px; font-family: var(--font-mono); cursor: pointer; transition: all var(--transition); }
.pu-method-btn:hover { background: var(--bg-hover); }
.pu-method-btn.active { background: var(--accent); border-color: var(--accent); color: #fff; }
.pu-noschema { font-size: 13px; color: var(--text-tertiary); display: flex; align-items: center; padding: 12px 0; }
.pu-raw { margin-top: 8px; }
.pu-raw summary { font-size: 11px; color: var(--text-tertiary); cursor: pointer; }
.pu-raw pre { margin: 6px 0 0; padding: 10px 12px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 11px; font-family: var(--font-mono); line-height: 1.5; color: var(--text-secondary); white-space: pre-wrap; word-break: break-all; max-height: 200px; overflow-y: auto; }
.tag { display: inline-block; padding: 1px 7px; border-radius: 4px; font-size: 11px; font-weight: 500; }
.tag-muted { background: var(--bg-inset); color: var(--text-tertiary); }
.btn { padding: 6px 14px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
</style>
