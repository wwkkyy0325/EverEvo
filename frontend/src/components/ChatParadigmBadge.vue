<template>
  <div v-if="paradigm" class="cpb-badge" @click="expanded = !expanded" :title="paradigm.name + ' — 点击展开详情与反馈'">
    <span class="cpb-icon">{{ paradigm.icon || '🧠' }}</span>
    <span class="cpb-name">{{ paradigm.name }}</span>
    <span class="cpb-conf" :style="{ color: confidenceColor }">{{ compositePct }}%</span>
    <span v-if="expanded" class="cpb-expand">▲</span>
    <span v-else class="cpb-expand">▼</span>
    <div v-if="expanded" class="cpb-body">
      <div class="cpb-desc">{{ paradigm.description }}</div>
      <div class="cpb-feedback-grid">
        <div class="cpb-fb-dim">
          <span class="cpb-fb-label">匹配度</span>
          <span class="cpb-fb-val" :style="{ color: dimColor(match) }">{{ pct(match) }}%</span>
        </div>
        <div class="cpb-fb-dim">
          <span class="cpb-fb-label">执行度</span>
          <span class="cpb-fb-val" :style="{ color: dimColor(exec) }">{{ pct(exec) }}%</span>
        </div>
        <div class="cpb-fb-dim">
          <span class="cpb-fb-label">结果度</span>
          <span class="cpb-fb-val" :style="{ color: dimColor(outcome) }">{{ pct(outcome) }}%</span>
        </div>
      </div>
      <div class="cpb-fb-reason">
        <input v-model="reason" class="field cpb-reason-input" placeholder="为什么？匹配不准/执行不到位/结果不好？" @keyup.enter="doFeedback" />
      </div>
      <div class="cpb-actions">
        <button class="btn btn-xs" @click.stop="quickFeedback(true)">👍 整体有效</button>
        <button class="btn btn-xs" @click.stop="quickFeedback(false)">👎 整体无效</button>
        <button class="btn btn-xs btn-primary" @click.stop="doFeedback" :disabled="!reason.trim()">反馈</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { memoryApi } from '../api/memory'

interface ParadigmInfo {
  id: string; name: string; icon?: string; description?: string; methodology?: string
  match?: number; exec?: number; outcome?: number
}

const props = defineProps<{ paradigm: ParadigmInfo | null }>()
const expanded = ref(false)
const match = ref(props.paradigm?.match ?? 0.5)
const exec = ref(props.paradigm?.exec ?? 0.5)
const outcome = ref(props.paradigm?.outcome ?? 0.5)
const reason = ref('')

const compositePct = computed(() => {
  const m = match.value; const e = exec.value; const o = outcome.value
  return ((m * 0.15 + e * 0.45 + o * 0.40) * 100).toFixed(0)
})
const confidenceColor = computed(() => {
  const c = Number(compositePct.value); if (c >= 80) return '#5abf7f'; if (c >= 50) return '#d4a853'; return '#f07070'
})
function pct(v: number) { return (v * 100).toFixed(0) }
function dimColor(v: number) { if (v >= 0.8) return '#5abf7f'; if (v >= 0.5) return '#d4a853'; return '#f07070' }

async function doFeedback() {
  if (!props.paradigm?.id || !reason.value.trim()) return
  try {
    await memoryApi.paradigmFeedback(props.paradigm.id, match.value, exec.value, outcome.value, reason.value.trim())
    reason.value = ''
  } catch (_) {}
}

async function quickFeedback(success: boolean) {
  if (!props.paradigm?.id) return
  const v = success ? 1.0 : 0.2
  try {
    await memoryApi.paradigmFeedback(props.paradigm.id, v, v, v, success ? '整体有效' : '整体无效')
  } catch (_) {}
}
</script>

<style scoped>
.cpb-badge { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 6px; background: rgba(120,180,220,0.08); border: 1px solid rgba(120,180,220,0.15); font-size: 0.78em; cursor: pointer; user-select: none; margin-top: 6px; max-width: fit-content; }
.cpb-badge:hover { background: rgba(120,180,220,0.15); }
.cpb-icon { font-size: 1em; }
.cpb-name { color: #ccc; font-weight: 500; }
.cpb-conf { font-size: 0.85em; }
.cpb-expand { font-size: 0.7em; color: #666; margin-left: 4px; }
.cpb-body { padding: 10px 0 0 0; border-top: 1px solid rgba(255,255,255,0.06); margin-top: 6px; width: 100%; }
.cpb-desc { font-size: 0.85em; color: #aaa; margin-bottom: 6px; }
.cpb-feedback-grid { display: flex; gap: 12px; margin-bottom: 6px; }
.cpb-fb-dim { display: flex; flex-direction: column; align-items: center; }
.cpb-fb-label { font-size: 0.7em; color: #666; }
.cpb-fb-val { font-size: 0.9em; font-weight: 600; }
.cpb-fb-reason { margin-bottom: 6px; }
.cpb-reason-input { width: 100%; font-size: 0.78em; padding: 3px 6px; }
.cpb-actions { display: flex; gap: 6px; }
.field { background: #121215; border: 1px solid #333; border-radius: 5px; padding: 4px 8px; color: #e0e0e0; }
.btn { border: 1px solid #444; background: #1a1a1e; color: #ccc; padding: 4px 12px; border-radius: 5px; cursor: pointer; font-size: 0.82em; }
.btn:hover { background: #2a2a30; }
.btn-primary { background: var(--accent, #7aa2f7); border-color: var(--accent, #7aa2f7); color: #111; }
.btn-xs { padding: 2px 7px; font-size: 0.72em; }
</style>
