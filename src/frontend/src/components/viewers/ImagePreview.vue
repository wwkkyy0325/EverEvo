<template>
  <div class="img-preview">
    <div v-if="label" class="img-label">{{ label }}</div>
    <div v-if="!src" class="img-empty">无图片</div>
    <img v-else :src="resolvedSrc" :alt="alt || 'preview'" class="img-img" @error="errored = true" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const props = defineProps<{ src?: string; alt?: string; label?: string }>()
const errored = ref(false)

const resolvedSrc = computed(() => {
  if (!props.src) return ''
  if (props.src.startsWith('data:')) return props.src
  return 'data:image/png;base64,' + props.src
})
</script>

<style scoped>
.img-preview { margin-top: 4px; }
.img-label { font-size: 11px; font-weight: 500; color: var(--text-tertiary); margin-bottom: 6px; }
.img-empty { padding: 20px; text-align: center; color: var(--text-tertiary); font-size: 12px; }
.img-img { max-width: 100%; max-height: 300px; border-radius: var(--radius-sm); border: 1px solid var(--border-subtle); object-fit: contain; }
</style>
