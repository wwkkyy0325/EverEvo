<template>
  <div class="zone-panel">
    <header class="zone-header">
      <h2>运行区管理</h2>
      <span class="current-badge" v-if="currentZone">
        当前: {{ currentZone.name }} ({{ typeLabel(currentZone.type) }})
      </span>
    </header>

    <!-- Create Experiment -->
    <section class="zone-section">
      <h3>创建实验区</h3>
      <div class="create-row">
        <input
          v-model="newExpName"
          placeholder="实验区名称，例如 fix-config"
          @keyup.enter="handleCreate"
        />
        <button @click="handleCreate" :disabled="!newExpName.trim() || loading">
          创建并启动
        </button>
      </div>
      <p class="hint">从生产区复制配置和数据，分配独立端口后启动新窗口。</p>
    </section>

    <!-- Zone List -->
    <section class="zone-section">
      <h3>所有运行区</h3>
      <div v-if="zones.length === 0" class="empty">暂无运行区</div>
      <div v-for="z in zones" :key="z.name" class="zone-card" :class="zoneCardClass(z)">
        <div class="zone-card-header">
          <span class="zone-name">{{ z.name }}</span>
          <span class="zone-type-badge" :class="z.type">{{ typeLabel(z.type) }}</span>
          <span v-if="z.pid > 0" class="zone-status running">运行中 (PID {{ z.pid }})</span>
          <span v-else class="zone-status stopped">已停止</span>
        </div>
        <div class="zone-card-meta">
          <span v-if="z.parent">来源: {{ z.parent }}</span>
          <span>MCP: {{ z.mcpPort }}</span>
          <span>A2A: {{ z.a2aPort }}</span>
          <span>{{ formatTime(z.createdAt) }}</span>
        </div>
        <div class="zone-card-actions">
          <button
            v-if="z.type === 'experiment' && z.pid === 0"
            @click="handleLaunch(z.name)"
            :disabled="loading"
          >启动</button>
          <button
            v-if="z.type === 'experiment' && z.pid > 0"
            class="btn-warn"
            @click="handleStop(z.name)"
            :disabled="loading"
          >停止</button>
          <button
            v-if="z.type === 'experiment'"
            class="btn-primary"
            @click="handleMerge(z.name)"
            :disabled="loading"
          >合并到生产区</button>
          <button
            v-if="z.type === 'experiment'"
            class="btn-danger"
            @click="handleDiscard(z.name)"
            :disabled="loading"
          >丢弃</button>
          <button
            v-if="z.type === 'backup'"
            @click="handleRestore(z.name)"
            :disabled="loading"
          >恢复</button>
          <button
            v-if="z.type === 'backup'"
            class="btn-danger"
            @click="handleDeleteBackup(z.name)"
            :disabled="loading"
          >删除</button>
        </div>
      </div>
    </section>

    <!-- Message -->
    <div v-if="message" class="message" :class="messageType">{{ message }}</div>

    <!-- Merge Confirmation Modal -->
    <div v-if="mergeTarget" class="modal-overlay" @click.self="mergeTarget = null">
      <div class="modal">
        <h3>确认合并</h3>
        <p>将实验区 <strong>{{ mergeTarget }}</strong> 合并到生产区？</p>
        <ul>
          <li>生产区当前数据会先自动备份</li>
          <li>实验期间对生产区的任何改动 <strong>将会丢失</strong></li>
          <li>合并后建议重启生产区实例</li>
        </ul>
        <div class="modal-actions">
          <button @click="mergeTarget = null">取消</button>
          <button class="btn-primary" @click="confirmMerge" :disabled="loading">确认合并</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { zoneApi, type Zone } from '../api/zone'

const currentZone = ref<Zone | null>(null)
const zones = ref<Zone[]>([])
const newExpName = ref('')
const loading = ref(false)
const message = ref('')
const messageType = ref<'info' | 'error'>('info')
const mergeTarget = ref<string | null>(null)

onMounted(async () => {
  try {
    currentZone.value = await zoneApi.getCurrent()
    zones.value = await zoneApi.list()
  } catch (e: any) {
    showMessage('加载运行区列表失败: ' + (e?.message || e), 'error')
  }
})

async function refresh() {
  try {
    zones.value = await zoneApi.list()
  } catch (e: any) {
    showMessage('刷新失败: ' + (e?.message || e), 'error')
  }
}

function showMessage(msg: string, type: 'info' | 'error' = 'info') {
  message.value = msg
  messageType.value = type
  setTimeout(() => { message.value = '' }, 8000)
}

async function handleCreate() {
  const name = newExpName.value.trim()
  if (!name) return
  loading.value = true
  try {
    await zoneApi.createExperiment(name)
    showMessage('实验区 ' + name + ' 已创建')
    await zoneApi.launch(name)
    showMessage('实验区 ' + name + ' 已启动')
    newExpName.value = ''
    await refresh()
  } catch (e: any) {
    showMessage('创建失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

async function handleLaunch(name: string) {
  loading.value = true
  try {
    await zoneApi.launch(name)
    showMessage(name + ' 已启动')
    await refresh()
  } catch (e: any) {
    showMessage('启动失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

async function handleStop(name: string) {
  loading.value = true
  try {
    await zoneApi.stop(name)
    showMessage(name + ' 已停止')
    await refresh()
  } catch (e: any) {
    showMessage('停止失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

function handleMerge(name: string) {
  mergeTarget.value = name
}

async function confirmMerge() {
  const name = mergeTarget.value
  if (!name) return
  loading.value = true
  mergeTarget.value = null
  try {
    await zoneApi.merge(name)
    showMessage(name + ' 已合并到生产区，建议重启生产区实例')
    await refresh()
  } catch (e: any) {
    showMessage('合并失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

async function handleDiscard(name: string) {
  if (!confirm(`确定要删除实验区 "${name}" 吗？此操作不可恢复。`)) return
  loading.value = true
  try {
    await zoneApi.discard(name)
    showMessage(name + ' 已删除')
    await refresh()
  } catch (e: any) {
    showMessage('删除失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

async function handleRestore(name: string) {
  if (!confirm(`确定从 "${name}" 恢复？当前生产区会先备份。`)) return
  loading.value = true
  try {
    await zoneApi.restoreBackup(name)
    showMessage('已从 ' + name + ' 恢复')
    await refresh()
  } catch (e: any) {
    showMessage('恢复失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

async function handleDeleteBackup(name: string) {
  if (!confirm(`确定要删除备份 "${name}" 吗？`)) return
  loading.value = true
  try {
    await zoneApi.deleteBackup(name)
    showMessage(name + ' 已删除')
    await refresh()
  } catch (e: any) {
    showMessage('删除失败: ' + (e?.message || e), 'error')
  } finally {
    loading.value = false
  }
}

function typeLabel(t: string) {
  return { production: '生产区', experiment: '实验区', backup: '备份' }[t] || t
}

function zoneCardClass(z: Zone) {
  return {
    'card-production': z.type === 'production',
    'card-experiment': z.type === 'experiment',
    'card-backup': z.type === 'backup',
  }
}

function formatTime(ts: string) {
  if (!ts) return ''
  return new Date(ts).toLocaleString('zh-CN')
}
</script>

<style scoped>
.zone-panel {
  padding: 24px;
  max-width: 900px;
  margin: 0 auto;
  color: #e0e0e0;
}

.zone-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 24px;
}

.zone-header h2 {
  margin: 0;
  font-size: 1.4em;
}

.current-badge {
  background: #1e3a5f;
  color: #7cb3ff;
  padding: 4px 12px;
  border-radius: 12px;
  font-size: 0.85em;
}

.zone-section {
  margin-bottom: 28px;
}

.zone-section h3 {
  margin: 0 0 12px 0;
  font-size: 1.05em;
  color: #aaa;
}

.create-row {
  display: flex;
  gap: 8px;
}

.create-row input {
  flex: 1;
  padding: 8px 12px;
  background: #1a1a1e;
  border: 1px solid #333;
  border-radius: 6px;
  color: #e0e0e0;
  font-size: 0.95em;
}

.create-row input:focus {
  outline: none;
  border-color: #4a90d9;
}

.hint {
  margin: 6px 0 0 0;
  font-size: 0.82em;
  color: #666;
}

.empty {
  color: #555;
  font-style: italic;
  padding: 16px 0;
}

/* Zone cards */
.zone-card {
  background: #1a1a1e;
  border: 1px solid #2a2a2e;
  border-radius: 8px;
  padding: 14px 16px;
  margin-bottom: 10px;
}

.zone-card.card-production {
  border-left: 3px solid #4a90d9;
}

.zone-card.card-experiment {
  border-left: 3px solid #f0a040;
}

.zone-card.card-backup {
  border-left: 3px solid #666;
}

.zone-card-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.zone-name {
  font-weight: 600;
  font-size: 1.05em;
}

.zone-type-badge {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 0.75em;
}

.zone-type-badge.production { background: #1e3a5f; color: #7cb3ff; }
.zone-type-badge.experiment { background: #3a2a10; color: #f0a040; }
.zone-type-badge.backup { background: #2a2a2a; color: #999; }

.zone-status {
  font-size: 0.82em;
  padding: 2px 8px;
  border-radius: 8px;
}

.zone-status.running { background: #1a3a1a; color: #5c5; }
.zone-status.stopped { background: #2a2a2a; color: #888; }

.zone-card-meta {
  display: flex;
  gap: 16px;
  font-size: 0.82em;
  color: #777;
  margin-bottom: 10px;
}

.zone-card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* Buttons */
button {
  padding: 6px 14px;
  border: 1px solid #444;
  border-radius: 5px;
  background: #252528;
  color: #ccc;
  cursor: pointer;
  font-size: 0.88em;
  transition: all 0.15s;
}

button:hover:not(:disabled) {
  background: #333;
  border-color: #555;
}

button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.btn-primary {
  background: #1e4a2e;
  border-color: #2a6a3a;
  color: #7cff9c;
}

.btn-primary:hover:not(:disabled) {
  background: #2a5a3a;
}

.btn-warn {
  background: #3a2a10;
  border-color: #5a4020;
  color: #f0a040;
}

.btn-warn:hover:not(:disabled) {
  background: #4a3a1a;
}

.btn-danger {
  background: #3a1a1a;
  border-color: #5a2a2a;
  color: #f07070;
}

.btn-danger:hover:not(:disabled) {
  background: #4a2a2a;
}

/* Message */
.message {
  padding: 10px 14px;
  border-radius: 6px;
  font-size: 0.9em;
  margin-top: 12px;
}

.message.info {
  background: #1a2a3a;
  color: #7cb3ff;
  border: 1px solid #2a4a6a;
}

.message.error {
  background: #3a1a1a;
  color: #f07070;
  border: 1px solid #5a2a2a;
}

/* Modal */
.modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(0,0,0,0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal {
  background: #1e1e22;
  border: 1px solid #333;
  border-radius: 10px;
  padding: 24px;
  max-width: 420px;
  width: 90%;
}

.modal h3 { margin: 0 0 12px 0; }

.modal ul {
  margin: 8px 0 16px;
  padding-left: 20px;
  font-size: 0.88em;
  color: #aaa;
}

.modal li { margin-bottom: 4px; }

.modal-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
</style>
