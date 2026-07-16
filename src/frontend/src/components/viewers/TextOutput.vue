<template>
  <div class="text-out">
    <div v-if="label" class="text-out-label">{{ label }}</div>
    <pre v-if="type === 'json'" class="text-out-json">{{ pretty }}</pre>
    <div v-else-if="type === 'number'" class="text-out-number">{{ value }}</div>
    <pre v-else class="text-out-plain">{{ value }}</pre>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  value?: any
  type?: string
  label?: string
}>()

const pretty = computed(() => {
  const v = props.value
  if (v == null) return ''
  if (typeof v === 'string') {
    try { return JSON.stringify(JSON.parse(v), null, 2) } catch (_) { return v }
  }
  return JSON.stringify(v, null, 2)
})
</script>

<style scoped>
.text-out { margin-top: 4px; }
.text-out-label { font-size: 11px; font-weight: 500; color: var(--text-tertiary); margin-bottom: 4px; }
.text-out-plain, .text-out-json { margin: 0; padding: 10px 12px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 12px; font-family: var(--font-mono); line-height: 1.5; color: var(--text-primary); white-space: pre-wrap; word-break: break-all; max-height: 240px; overflow-y: auto; }
.text-out-number { padding: 6px 10px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 16px; font-family: var(--font-mono); color: var(--accent); font-weight: 600; }
</style>
