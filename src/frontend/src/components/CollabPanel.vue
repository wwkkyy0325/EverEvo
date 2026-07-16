<template>
  <div class="collab-panel">
    <header class="collab-header">
      <h2>协同工作台</h2>
      <button class="btn btn-primary" @click="showCreate = !showCreate">+ 新建协同</button>
    </header>

    <!-- Create session -->
    <section v-if="showCreate" class="collab-section glass-panel">
      <h3>创建协同会话</h3>
      <input v-model="newGoal" placeholder="协同目标（要解决的问题）" />
      <input v-model="newOrchestrator" placeholder="主控 agent ID（默认 Evo）" />
      <div class="hint">成员可在创建后通过 collab_dispatch 动态加入</div>
      <button class="btn btn-primary" @click="handleCreate" :disabled="!newGoal.trim()">创建</button>
    </section>

    <!-- Session list -->
    <section v-if="sessions.length === 0" class="empty">暂无协同会话</section>
    <section v-else>
      <div v-for="s in sessions" :key="s.id" class="collab-card glass-panel">
        <div class="collab-card-head">
          <span class="collab-goal">{{ s.goal }}</span>
          <span class="collab-status" :class="s.status">{{ statusLabel(s.status) }}</span>
        </div>
        <div class="collab-card-meta">
          <span>主控: {{ s.orchestratorId }}</span>
          <span>成员: {{ s.members.length }}</span>
          <span>{{ formatTime(s.createdAt) }}</span>
        </div>
        <div class="collab-card-members">
          <span v-for="m in s.members" :key="m.agentId" class="member-chip">{{ m.agentId }} ({{ m.role }})</span>
        </div>
        <div class="collab-card-actions">
          <button class="btn btn-sm" @click="loadBlackboard(s.blackboardId, s.id)">黑板</button>
          <button class="btn btn-sm btn-danger" @click="handleComplete(s.id)">结束</button>
        </div>
      </div>
    </section>

    <!-- Blackboard viewer -->
    <div v-if="bbSession" class="modal-overlay" @click.self="bbSession = null">
      <div class="modal">
        <h3>共享黑板</h3>
        <div v-if="bbEntries.length === 0" class="empty">黑板为空</div>
        <div v-for="e in bbEntries" :key="e.key" class="bb-entry">
          <span class="bb-key">{{ e.key }}</span>
          <span class="bb-kind" :class="e.kind">{{ e.kind }}</span>
          <span class="bb-author">@{{ e.author }}</span>
          <pre class="bb-value">{{ e.value }}</pre>
        </div>
        <button @click="bbSession = null">关闭</button>
      </div>
    </div>

    <div v-if="message" class="message">{{ message }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { collabApi, type CollabSession, type BlackboardEntry } from '../api/collab'

const sessions = ref<CollabSession[]>([])
const showCreate = ref(false)
const newGoal = ref('')
const newOrchestrator = ref('Evo')
const message = ref('')
const bbSession = ref<string | null>(null)
const bbEntries = ref<BlackboardEntry[]>([])

onMounted(loadSessions)

async function loadSessions() {
  try {
    sessions.value = await collabApi.listSessions()
  } catch (e: any) {
    showMessage('加载失败: ' + (e?.message || e))
  }
}

function showMessage(msg: string) {
  message.value = msg
  setTimeout(() => { message.value = '' }, 6000)
}

async function handleCreate() {
  try {
    const res = await collabApi.create(newGoal.value, newOrchestrator.value, [])
    showMessage('协同会话已创建: ' + res.sessionId)
    newGoal.value = ''
    showCreate.value = false
    await loadSessions()
  } catch (e: any) {
    showMessage('创建失败: ' + (e?.message || e))
  }
}

async function handleComplete(id: string) {
  if (!confirm('结束此协同会话？黑板将被清除。')) return
  try {
    await collabApi.complete(id)
    showMessage('已结束')
    await loadSessions()
  } catch (e: any) {
    showMessage('失败: ' + (e?.message || e))
  }
}

async function loadBlackboard(blackboardId: string, sessionId: string) {
  try {
    bbEntries.value = await collabApi.bbList(sessionId)
    bbSession.value = sessionId
  } catch (e: any) {
    showMessage('加载黑板失败: ' + (e?.message || e))
  }
}

function statusLabel(s: string) {
  return { active: '进行中', paused: '暂停', completed: '已完成' }[s] || s
}

function formatTime(ts: string) {
  if (!ts) return ''
  return new Date(ts).toLocaleString('zh-CN')
}
</script>

<style scoped>
.collab-panel { padding: 24px; max-width: 900px; margin: 0 auto; color: #e0e0e0; }
.collab-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.collab-header h2 { margin: 0; font-size: 1.4em; }
.collab-section { padding: 16px; margin-bottom: 20px; }
.collab-section h3 { margin: 0 0 12px; }
.collab-section input { width: 100%; padding: 8px 12px; margin-bottom: 8px; background: #1a1a1e; border: 1px solid #333; border-radius: 6px; color: #e0e0e0; }
.hint { font-size: 0.82em; color: #666; }
.empty { color: #555; font-style: italic; padding: 24px 0; text-align: center; }
.collab-card { padding: 14px 16px; margin-bottom: 12px; }
.collab-card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.collab-goal { font-weight: 600; font-size: 1.05em; }
.collab-status { padding: 2px 10px; border-radius: 10px; font-size: 0.75em; }
.collab-status.active { background: #1a3a1a; color: #5c5; }
.collab-status.completed { background: #2a2a2a; color: #888; }
.collab-card-meta { display: flex; gap: 16px; font-size: 0.82em; color: #777; margin-bottom: 8px; }
.collab-card-members { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.member-chip { background: #1e3a5f; color: #7cb3ff; padding: 2px 8px; border-radius: 10px; font-size: 0.78em; }
.collab-card-actions { display: flex; gap: 8px; }
button { padding: 6px 14px; border: 1px solid #444; border-radius: 5px; background: #252528; color: #ccc; cursor: pointer; font-size: 0.88em; }
button:hover { background: #333; }
.btn-primary { background: #1e4a2e; border-color: #2a6a3a; color: #7cff9c; }
.btn-danger { background: #3a1a1a; border-color: #5a2a2a; color: #f07070; }
.btn-sm { padding: 4px 10px; font-size: 0.82em; }
.message { padding: 10px 14px; background: #1a2a3a; color: #7cb3ff; border-radius: 6px; margin-top: 12px; font-size: 0.9em; }
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: #1e1e22; border: 1px solid #333; border-radius: 10px; padding: 20px; max-width: 600px; width: 90%; max-height: 80vh; overflow-y: auto; }
.modal h3 { margin: 0 0 12px; }
.bb-entry { border-bottom: 1px solid #2a2a2e; padding: 10px 0; }
.bb-key { font-weight: 600; color: #7cb3ff; }
.bb-kind { padding: 1px 6px; border-radius: 8px; font-size: 0.72em; margin-left: 8px; background: #2a2a2a; }
.bb-author { font-size: 0.78em; color: #777; margin-left: 8px; }
.bb-value { background: #141418; padding: 8px; border-radius: 4px; margin: 6px 0 0; font-size: 0.85em; white-space: pre-wrap; word-break: break-word; }
</style>
