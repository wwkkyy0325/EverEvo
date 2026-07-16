<template>
  <div v-if="error" class="rp-error">{{ error }}</div>
  <div v-else-if="!result" />
  <div v-else class="rp-results">
    <div v-for="(field, key) in schema" :key="key" class="rp-item">
      <ImagePreview v-if="field.type === 'image-base64'" :src="result[key]" :label="field.label" />
      <TextOutput v-else :value="result[key]" :type="field.type" :label="field.label" />
    </div>
  </div>
</template>

<script setup lang="ts">
import ImagePreview from './ImagePreview.vue'
import TextOutput from './TextOutput.vue'

defineProps<{
  schema?: Record<string, { type: string; label: string }>
  result?: Record<string, any> | null
  error?: string
}>()
</script>

<style scoped>
.rp-error { margin-top: 8px; padding: 8px 12px; font-size: 12px; color: var(--danger); background: var(--danger-dim); border-radius: var(--radius-sm); }
.rp-results { display: flex; flex-direction: column; gap: 10px; margin-top: 8px; }
</style>
