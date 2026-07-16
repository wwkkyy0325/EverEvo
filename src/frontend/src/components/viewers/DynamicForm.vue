<template>
  <div class="dyn-form">
    <div v-for="(field, key) in schema" :key="key" class="dyn-field">
      <label class="dyn-label">{{ field.label }}<span v-if="field.required" class="dyn-required">*</span></label>
      <textarea v-if="field.type === 'textarea'" :value="values[key] || ''" @input="setVal(key, ($event.target as HTMLTextAreaElement).value)" :placeholder="field.placeholder || ''" :required="field.required" rows="4" class="dyn-textarea" />
      <input v-else-if="field.type === 'text'" type="text" :value="values[key] || ''" @input="setVal(key, ($event.target as HTMLInputElement).value)" :placeholder="field.placeholder || ''" :required="field.required" class="dyn-input" />
      <input v-else-if="field.type === 'number'" type="number" :value="values[key] || ''" @input="setVal(key, ($event.target as HTMLInputElement).value)" :placeholder="field.placeholder || ''" :required="field.required" class="dyn-input" />
      <select v-else-if="field.type === 'select'" :value="values[key] || ''" @change="setVal(key, ($event.target as HTMLSelectElement).value)" class="dyn-select">
        <option value="" disabled>请选择…</option>
        <option v-for="opt in field.options" :key="opt" :value="opt">{{ opt }}</option>
      </select>
      <div v-else-if="field.type === 'file'" class="dyn-file">
        <input type="file" :accept="field.placeholder || '*'" @change="onFile(key, $event)" class="dyn-file-input" />
        <span v-if="fileNames[key]" class="dyn-file-name">{{ fileNames[key] }}</span>
      </div>
    </div>
    <button class="btn btn-primary dyn-submit" @click="doSubmit" :disabled="busy">{{ busy ? '执行中…' : '执行' }}</button>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'

interface Field {
  label: string; type: string; placeholder?: string; required?: boolean
  default?: any; options?: string[]
}

const props = defineProps<{ schema?: Record<string, Field>; busy?: boolean }>()
const emit = defineEmits<{ submit: [params: Record<string, any>] }>()

const values = reactive<Record<string, any>>({})
const fileNames = reactive<Record<string, string>>({})
const fileData = reactive<Record<string, string>>({})

function reset() {
  Object.keys(values).forEach(k => delete values[k])
  Object.keys(fileNames).forEach(k => delete fileNames[k])
  Object.keys(fileData).forEach(k => delete fileData[k])
  if (!props.schema) return
  for (const [key, f] of Object.entries(props.schema)) {
    if (f.default) values[key] = f.default
  }
}

watch(() => props.schema, () => reset(), { immediate: true })

function setVal(key: string, val: any) { values[key] = val }
function onFile(key: string, e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  fileNames[key] = file.name
  const reader = new FileReader()
  reader.onload = () => {
    const b64 = reader.result as string
    const comma = b64.indexOf(',')
    fileData[key] = comma >= 0 ? b64.slice(comma + 1) : b64
  }
  reader.readAsDataURL(file)
}
function doSubmit() {
  if (!props.schema) return
  for (const [key, f] of Object.entries(props.schema)) {
    const v = f.type === 'file' ? fileData[key] : values[key]
    if (f.required && (!v || v === '')) return
  }
  emit('submit', { ...values, ...fileData })
  reset()
}
</script>

<style scoped>
.dyn-form { display: flex; flex-direction: column; gap: 14px; }
.dyn-field { display: flex; flex-direction: column; gap: 4px; }
.dyn-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.dyn-required { color: var(--danger); }
.dyn-input, .dyn-select { padding: 7px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-inset); color: var(--text-primary); font-size: 13px; font-family: var(--font-mono); }
.dyn-textarea { padding: 8px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-inset); color: var(--text-primary); font-size: 13px; font-family: var(--font-mono); resize: vertical; min-height: 80px; }
.dyn-file { display: flex; align-items: center; gap: 8px; }
.dyn-file-input { font-size: 12px; }
.dyn-file-name { font-size: 11px; color: var(--text-tertiary); font-family: var(--font-mono); }
.dyn-submit { align-self: flex-start; margin-top: 4px; }
.btn { padding: 6px 14px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
</style>
