<template>
  <transition-group name="toast" tag="div" class="toast-container">
    <div v-for="t in toasts" :key="t.id" class="toast" :class="'toast-' + t.type" @click="emit('remove', t.id)">
      <span class="toast-icon">{{ icon(t.type) }}</span>
      <div class="toast-body">
        <span class="toast-title">{{ t.title }}</span>
        <span v-if="t.desc" class="toast-desc">{{ t.desc }}</span>
      </div>
      <button class="toast-close" @click="emit('remove', t.id)">✕</button>
    </div>
  </transition-group>
</template>

<script setup lang="ts">
defineProps<{ toasts: Array<{ id: number; type: string; title: string; desc?: string }> }>()
const emit = defineEmits<{ remove: [id: number] }>()

const icons: Record<string, string> = { success: '✓', error: '✕', warning: '⚠', info: 'ℹ' }
function icon(type: string): string { return icons[type] || 'ℹ' }
</script>

<style scoped>
.toast-container {
  position: fixed; top: 16px; right: 16px; z-index: 9999;
  display: flex; flex-direction: column; gap: 10px;
  pointer-events: none;
}
.toast {
  pointer-events: auto; cursor: pointer;
  display: flex; align-items: flex-start; gap: 12px;
  min-width: 300px; max-width: 420px;
  padding: 14px 16px;
  border-radius: 12px;
  background: rgba(40, 40, 42, 0.92);
  backdrop-filter: blur(24px) saturate(140%);
  -webkit-backdrop-filter: blur(24px) saturate(140%);
  border: 1px solid rgba(255,255,255,0.08);
  box-shadow: 0 8px 32px rgba(0,0,0,0.4);
}
.toast-icon {
  width: 24px; height: 24px; border-radius: 50%; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 13px; font-weight: 700; margin-top: 1px;
}
.toast-success .toast-icon { background: var(--success-dim); color: var(--success); }
.toast-error   .toast-icon { background: var(--danger-dim); color: var(--danger); }
.toast-warning .toast-icon { background: var(--warning-dim); color: var(--warning); }
.toast-info    .toast-icon { background: var(--accent-dim); color: var(--accent); }

.toast-body { flex: 1; display: flex; flex-direction: column; gap: 2px; }
.toast-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.toast-desc { font-size: 11px; color: var(--text-tertiary); word-break: break-word; }
.toast-close {
  width: 20px; height: 20px; flex-shrink: 0;
  border: none; border-radius: 5px; background: transparent;
  color: var(--text-tertiary); font-size: 11px; cursor: pointer;
  transition: all var(--transition);
}
.toast-close:hover { background: var(--bg-hover); color: var(--text-secondary); }

/* 动画 */
.toast-enter-active { transition: all 0.35s cubic-bezier(0.22, 0.61, 0.36, 1); }
.toast-leave-active { transition: all 0.25s ease; position: absolute; right: 16px; }
.toast-enter-from { opacity: 0; transform: translateX(60px) scale(0.95); }
.toast-leave-to { opacity: 0; transform: translateX(60px); }
.toast-move { transition: transform 0.3s ease; }
</style>
