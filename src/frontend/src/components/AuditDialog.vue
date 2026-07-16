<template>
  <Teleport to="body">
    <div v-if="visible" class="audit-overlay" @click.self="reject">
      <div class="audit-dialog">
        <div class="audit-header">
          <span class="audit-icon">🔍</span>
          <span class="audit-title">审计确认</span>
          <span class="audit-badge" :class="req.tool === 'shell_exec' ? 'badge-exec' : 'badge-write'">
            {{ req.tool === 'shell_exec' ? '命令' : '写文件' }}
          </span>
        </div>

        <div class="audit-body">
          <div class="audit-field">
            <label>路径 / 命令</label>
            <code>{{ req.path }}</code>
          </div>
          <div class="audit-field">
            <label>内容预览 <span class="audit-size">({{ req.size }} 字节)</span></label>
            <pre class="audit-preview">{{ req.content.slice(0, 800) }}{{ req.content.length > 800 ? '…' : '' }}</pre>
          </div>
        </div>

        <div class="audit-actions">
          <label class="audit-permanent">
            <input type="checkbox" v-model="permanent" />
            <span>本次会话永久生效</span>
          </label>
          <div class="audit-buttons">
            <button class="btn btn-danger" @click="reject">拒绝</button>
            <button class="btn btn-primary" @click="approve">批准</button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { filectlApi, type AuditRequest } from '../api/filectl'

const visible = ref(false)
const permanent = ref(false)
const req = ref<AuditRequest>({ id: '', tool: '', path: '', content: '', size: 0, createdAt: 0 })

function show(r: AuditRequest) {
  req.value = r
  permanent.value = false
  visible.value = true
}

function approve() {
  filectlApi.resolveAudit(req.value.id, true, permanent.value)
  if (permanent.value) filectlApi.setMode('full')
  visible.value = false
}

function reject() {
  filectlApi.resolveAudit(req.value.id, false, permanent.value)
  if (permanent.value) filectlApi.setMode('readonly')
  visible.value = false
}

// Listen for audit requests from Go backend via Wails events.
function onAuditRequest(r: unknown) {
  show(r as AuditRequest)
}

onMounted(() => {
  window.runtime.EventsOn('filectl:audit-request', onAuditRequest)
})

onUnmounted(() => {
  window.runtime.EventsOff('filectl:audit-request', onAuditRequest)
})
</script>

<style scoped>
.audit-overlay {
  position: fixed; inset: 0; z-index: 9999;
  background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center;
}
.audit-dialog {
  background: #1a1a1e; border: 1px solid #3a3a4a; border-radius: 12px;
  width: 560px; max-width: 95vw; max-height: 85vh; overflow-y: auto;
  padding: 20px; color: #e0e0e0; box-shadow: 0 8px 32px rgba(0,0,0,0.5);
}
.audit-header { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
.audit-icon { font-size: 1.3em; }
.audit-title { font-weight: 700; font-size: 1.1em; }
.audit-badge { font-size: 0.7em; padding: 2px 8px; border-radius: 8px; }
.badge-exec { background: #2a2a1a; color: #aa5; }
.badge-write { background: #1a2a2a; color: #5aa; }

.audit-body { margin-bottom: 16px; }
.audit-field { margin-bottom: 10px; }
.audit-field label { display: block; font-size: 0.78em; color: #888; margin-bottom: 4px; }
.audit-field code { display: block; background: #0a0a0e; border: 1px solid #2a2a2e; border-radius: 5px; padding: 6px 10px; font-size: 0.82em; word-break: break-all; color: #ccc; }
.audit-size { font-weight: 400; color: #666; }
.audit-preview { background: #0a0a0e; border: 1px solid #2a2a2e; border-radius: 5px; padding: 10px; font-size: 0.78em; max-height: 250px; overflow-y: auto; white-space: pre-wrap; word-break: break-word; line-height: 1.4; color: #bbb; font-family: monospace; }
.audit-actions { display: flex; align-items: center; justify-content: space-between; }
.audit-permanent { display: flex; align-items: center; gap: 6px; font-size: 0.8em; color: #999; cursor: pointer; }
.audit-permanent input { accent-color: var(--accent, #7aa2f7); }
.audit-buttons { display: flex; gap: 8px; }

.btn { border: 1px solid #444; background: #1a1a1e; color: #ccc; padding: 6px 16px; border-radius: 5px; cursor: pointer; font-size: 0.85em; }
.btn:hover { background: #2a2a30; }
.btn-primary { background: var(--accent, #7aa2f7); border-color: var(--accent, #7aa2f7); color: #111; }
.btn-primary:hover { opacity: 0.85; }
.btn-danger { border-color: #5a2a2a; color: #f07070; }
.btn-danger:hover { background: #3a1a1a; }
</style>
