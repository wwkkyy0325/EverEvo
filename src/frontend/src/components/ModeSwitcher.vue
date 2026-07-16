<template>
  <button class="mode-btn" :class="mode" @click="cycle" :title="tooltip()">
    <span class="mode-icon">{{ icon() }}</span>
  </button>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { filectlApi, type FileControlMode } from '../api/filectl'

const mode = ref<FileControlMode>('full')

const MODES: FileControlMode[] = ['full', 'audit', 'readonly']

function icon() {
  switch (mode.value) {
    case 'readonly': return '🔒'
    case 'audit': return '🔍'
    case 'full': return '🔓'
  }
}

function tooltip() {
  switch (mode.value) {
    case 'readonly': return '只读模式：LLM 只能读不能写。点击切换'
    case 'audit': return '审计模式：写操作需你确认。点击切换'
    case 'full': return '完全控制：LLM 自由读写。点击切换'
  }
}

async function cycle() {
  const idx = MODES.indexOf(mode.value)
  const next = MODES[(idx + 1) % MODES.length]
  try {
    await filectlApi.setMode(next)
    mode.value = next
  } catch { /* ignore */ }
}

async function refresh() {
  try {
    const r = await filectlApi.getMode()
    const m = (r?.mode || 'full') as FileControlMode
    mode.value = m
  } catch { /* backend not ready */ }
}

onMounted(refresh)
defineExpose({ refresh })
</script>

<style scoped>
.mode-btn {
  background: transparent; border: 1px solid transparent; border-radius: 4px;
  cursor: pointer; padding: 2px 4px; font-size: 13px; line-height: 1;
  transition: all .15s; opacity: 0.7;
}
.mode-btn:hover { opacity: 1; }
.mode-btn.full { border-color: #2a4a2a; background: #1a2a1a20; }
.mode-btn.audit { border-color: #4a4a2a; background: #2a2a1a20; }
.mode-btn.readonly { border-color: #4a2a2a; background: #2a1a1a20; }
</style>
