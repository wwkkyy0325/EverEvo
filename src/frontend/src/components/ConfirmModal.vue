<template>
  <transition name="confirm-fade">
    <div v-if="visible" class="confirm-overlay" @click.self="dismiss(false)">
      <div class="confirm-dialog glass-panel">
        <div class="confirm-icon">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="8" x2="12" y2="12"/>
            <line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
        </div>
        <h3 class="confirm-title">{{ title }}</h3>
        <p class="confirm-msg">{{ message }}</p>
        <div class="confirm-actions">
          <button class="btn confirm-cancel" @click="dismiss(false)">取消</button>
          <button class="btn confirm-ok" ref="okBtnRef" @click="dismiss(true)">{{ okLabel }}</button>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
import { ref, nextTick } from 'vue'

const visible = ref(false)
const title = ref('')
const message = ref('')
const okLabel = ref('确定')
let resolvePromise: ((value: boolean) => void) | null = null

const okBtnRef = ref<HTMLButtonElement | null>(null)

function show(t: string, m: string, ok = '确定'): Promise<boolean> {
  title.value = t
  message.value = m
  okLabel.value = ok
  visible.value = true
  return new Promise(resolve => {
    resolvePromise = resolve
    nextTick(() => okBtnRef.value?.focus())
  })
}

function dismiss(ok: boolean) {
  visible.value = false
  if (resolvePromise) {
    resolvePromise(ok)
    resolvePromise = null
  }
}

defineExpose({ show })
</script>

<style scoped>
.confirm-overlay {
  position: fixed; inset: 0; z-index: 1000;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.confirm-dialog {
  width: 380px; max-width: 90vw;
  padding: 28px 28px 22px;
  text-align: center;
  box-shadow: var(--shadow-dialog);
}
.confirm-icon {
  width: 48px; height: 48px; margin: 0 auto 14px;
  border-radius: 50%; background: var(--warning-dim);
  display: flex; align-items: center; justify-content: center;
  color: var(--warning);
}
.confirm-title { font-size: 15px; font-weight: 600; margin-bottom: 8px; color: var(--text-primary); }
.confirm-msg { font-size: 13px; color: var(--text-secondary); line-height: 1.6; margin-bottom: 22px; white-space: pre-line; }
.confirm-actions { display: flex; gap: 10px; justify-content: center; }
.confirm-cancel { min-width: 80px; padding: 7px 18px; background: var(--bg-elevated); border: 1px solid var(--border-soft); color: var(--text-secondary); }
.confirm-cancel:hover { background: var(--bg-hover); color: var(--text-primary); }
.confirm-ok { min-width: 80px; padding: 7px 18px; background: var(--danger); border: 1px solid var(--danger); color: #fff; }
.confirm-ok:hover { background: #ff5a4f; border-color: #ff5a4f; }

.confirm-fade-enter-active, .confirm-fade-leave-active { transition: opacity 0.18s ease; }
.confirm-fade-enter-active .confirm-dialog, .confirm-fade-leave-active .confirm-dialog { transition: transform 0.18s cubic-bezier(0.22,0.61,0.36,1), opacity 0.18s ease; }
.confirm-fade-enter-from, .confirm-fade-leave-to { opacity: 0; }
.confirm-fade-enter-from .confirm-dialog { transform: scale(0.94); opacity: 0; }
.confirm-fade-leave-to .confirm-dialog { transform: scale(0.94); opacity: 0; }

.btn { display: inline-flex; align-items: center; justify-content: center; border-radius: var(--radius-sm); font-family: var(--font); font-size: 13px; font-weight: 500; cursor: pointer; transition: all var(--transition); white-space: nowrap; }
</style>
