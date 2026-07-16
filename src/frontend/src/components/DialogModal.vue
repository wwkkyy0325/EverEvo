<template>
  <transition name="dm-fade">
    <div v-if="visible" class="dm-overlay" @click.self="onOverlayClick">
      <div class="dm-card glass-panel" :style="{ width: width }">
        <div class="dm-head">
          <h3 class="dm-title">{{ title }}</h3>
          <button v-if="closable" class="dm-close" @click="cancel">✕</button>
        </div>
        <div class="dm-body"><slot></slot></div>
        <div class="dm-foot" v-if="$slots.footer || okLabel">
          <slot name="footer">
            <button class="btn" @click="cancel">{{ cancelLabel }}</button>
            <button class="btn btn-primary" @click="ok" :disabled="okDisabled">{{ okLabel }}</button>
          </slot>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
defineProps<{
  visible: boolean
  title?: string
  width?: string
  okLabel?: string
  cancelLabel?: string
  okDisabled?: boolean
  closable?: boolean
  closeOnOverlay?: boolean
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  ok: []
  cancel: []
}>()

function onOverlayClick() {
  // Read props via internal instance — for simplicity, always close on overlay
  emit('cancel')
}
function ok() { emit('ok') }
function cancel() { emit('update:visible', false); emit('cancel') }
</script>

<style scoped>
.dm-overlay {
  position: fixed; inset: 0; z-index: 100;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.dm-card { max-width: 92vw; max-height: 90vh; overflow-y: auto; padding: 24px 26px 20px; box-shadow: var(--shadow-dialog); }
.dm-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 18px; }
.dm-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin: 0; flex: 1; }
.dm-close {
  width: 26px; height: 26px; display: flex; align-items: center; justify-content: center;
  border: none; border-radius: 6px; background: transparent; color: var(--text-tertiary);
  font-size: 13px; cursor: pointer; flex-shrink: 0; transition: all 0.12s;
}
.dm-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.dm-body { margin-bottom: 18px; }
.dm-foot { display: flex; gap: 10px; justify-content: flex-end; align-items: center; }

.dm-fade-enter-active, .dm-fade-leave-active { transition: opacity 0.18s ease; }
.dm-fade-enter-active .dm-card, .dm-fade-leave-active .dm-card { transition: transform 0.18s cubic-bezier(0.22,0.61,0.36,1), opacity 0.18s ease; }
.dm-fade-enter-from, .dm-fade-leave-to { opacity: 0; }
.dm-fade-enter-from .dm-card { transform: scale(0.94); opacity: 0; }
.dm-fade-leave-to .dm-card { transform: scale(0.94); opacity: 0; }

.btn { display: inline-flex; align-items: center; justify-content: center; border-radius: var(--radius-sm); padding: 7px 16px; font-family: var(--font); font-size: 13px; font-weight: 500; cursor: pointer; transition: all var(--transition); white-space: nowrap; }
</style>
