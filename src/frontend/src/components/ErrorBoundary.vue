<template>
  <div v-if="error" class="err-boundary">
    <div class="err-boundary-icon">⚠</div>
    <h3>页面出错了</h3>
    <p class="err-boundary-msg">{{ error.message || '未知错误' }}</p>
    <button class="btn btn-primary" @click="reset">重试</button>
  </div>
  <slot v-else />
</template>

<script setup lang="ts">
import { ref, onErrorCaptured } from 'vue'

const error = ref<Error | null>(null)

onErrorCaptured((err: Error, _instance, _info) => {
  error.value = err
  console.error('[ErrorBoundary]', _info, err)
  return false // prevent propagation
})

function reset() {
  error.value = null
}
</script>

<style scoped>
.err-boundary {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 12px; padding: 60px 24px; text-align: center; min-height: 200px;
}
.err-boundary-icon { font-size: 40px; opacity: 0.6; }
.err-boundary h3 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.err-boundary-msg {
  font-size: 13px; color: var(--text-tertiary); max-width: 400px;
  font-family: var(--font-mono); word-break: break-all;
}
</style>
