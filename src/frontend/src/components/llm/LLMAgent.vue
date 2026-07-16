<template>
  <div>
    <!-- ═══ Section A: Local Agent Service ═══ -->
    <div class="agent-section">
      <div class="agent-section-head">
        <div class="agent-section-icon">◉</div>
        <div class="agent-section-info">
          <div class="agent-section-title">本机 Agent 服务</div>
          <div class="agent-section-desc">启动后将本机 Agent 能力以 A2A 协议暴露，供本地或远端 Agent 调用。提供 agent-card 发现端点（/.well-known/agent-card.json）</div>
        </div>
      </div>

      <div class="agent-local-cards">
        <!-- Status card -->
        <div class="glass-panel agent-status-card">
          <div class="agent-status-row">
            <span class="be-dot" :class="serverOk ? 'dot-live' : 'dot-dead'"></span>
            <span class="agent-status-label">{{ serverOk ? '运行中' : '已停止' }}</span>
            <span v-if="serverOk && serverUrl" class="agent-status-url">{{ serverUrl }}</span>
          </div>
          <div v-if="serverErr" class="agent-error">{{ serverErr }}</div>
          <div class="agent-actions">
            <button v-if="!serverOk" class="btn btn-primary" @click="startLocalServer">启动服务</button>
            <button v-else class="btn" @click="stopLocalServer">停止</button>
            <button v-if="serverOk" class="btn btn-sm" @click="restartLocalServer">重启</button>
            <button v-if="serverOk && serverUrl" class="btn btn-sm" @click="copyAgentUrl">复制地址</button>
          </div>
        </div>

        <!-- Port + config card -->
        <div class="glass-panel agent-port-card">
          <div class="agent-port-row">
            <label class="agent-port-label">Agent 名称</label>
            <input v-model="cfgForm.name" type="text" class="agent-port-input agent-port-input-wide" placeholder="EverEvo Agent" />
          </div>
          <div class="agent-port-row">
            <label class="agent-port-label">服务端口</label>
            <div class="agent-port-row-inner">
              <input v-model.number="cfgForm.port" type="number" class="agent-port-input" placeholder="19801" />
              <button class="btn btn-sm btn-primary" @click="saveLocalConfig" :disabled="saving">保存并重启</button>
            </div>
          </div>
          <div class="agent-port-row">
            <label class="agent-port-label">签名密钥（可选）</label>
            <input v-model="cfgForm.secret" type="text" class="agent-port-input agent-port-input-wide" placeholder="留空不校验；设后入站 task 须签名" />
          </div>
        </div>
      </div>

      <!-- Access guide -->
      <div class="glass-panel agent-guide-card">
        <div class="agent-guide-toggle" @click="showGuide = !showGuide">
          <span>{{ showGuide ? '▾' : '▸' }} 接入指南 — 如何调用本机 Agent</span>
        </div>
        <div v-if="showGuide" class="agent-guide-body">
          <div class="agent-guide-item">
            <strong>curl 测试</strong>
            <pre>curl {{ serverUrl || 'http://127.0.0.1:19801' }}/.well-known/agent-card.json
curl -X POST {{ serverUrl || 'http://127.0.0.1:19801' }} \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":"1","method":"tasks/send","params":{"id":"test-1","message":{"role":"user","parts":[{"kind":"text","text":"Hello"}]}}}'</pre>
          </div>
          <div class="agent-guide-item">
            <strong>另一个 EverEvo 实例</strong>
            <p>在远端 Agent 中添加此地址即可连接和调用</p>
          </div>
          <div class="agent-guide-item">
            <strong>第三方 A2A 客户端</strong>
            <p>任意实现了 A2A JSON-RPC 协议的客户端均可连接，调用 <code>tasks/send</code> 发送任务</p>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══ Section B: Remote Agents ═══ -->
    <div class="agent-section">
      <div class="agent-section-head">
        <div class="agent-section-icon">⇢</div>
        <div class="agent-section-info">
          <div class="agent-section-title">远端 Agent 连接</div>
          <div class="agent-section-desc">连接本地或远端的其他 A2A Agent，将其能力集成到 EverEvo。可一键创建为 Skill</div>
        </div>
      </div>

      <!-- Connected agent list -->
      <div class="mcpsrv-list" v-if="remoteAgents.length">
        <div v-for="ra in remoteAgents" :key="ra.id" class="glass-panel mcpsrv-card">
          <div class="mcpsrv-card-left">
            <span class="be-dot" :class="ra.status === 'connected' ? 'dot-live' : (ra.status === 'error' ? 'dot-dead' : 'dot-off')"></span>
            <div class="mcpsrv-info">
              <div class="mcpsrv-name">{{ ra.name }}</div>
              <div class="mcpsrv-meta">
                <span class="mcpsrv-status-tag" :class="'st-' + ra.status">{{ statusLabel(ra.status) }}</span>
                <span class="mcpsrv-transport">A2A JSON-RPC</span>
                <span v-if="ra.card" class="mcpsrv-toolcount">{{ ra.card.name }} v{{ ra.card.version }}</span>
              </div>
              <div class="mcpsrv-cmdline">
                <code>{{ ra.url }}</code>
              </div>
              <div v-if="ra.card && ra.card.description" class="agent-card-desc">{{ ra.card.description }}</div>
              <div v-if="ra.card && ra.card.skills && ra.card.skills.length" class="agent-card-skills">
                <span class="agent-skill-tag" v-for="sk in ra.card.skills" :key="sk.id">{{ sk.name }}</span>
              </div>
              <div v-if="ra.error" class="mcpsrv-error">{{ ra.error }}</div>
            </div>
          </div>
          <div class="mcpsrv-actions">
            <button v-if="ra.status === 'disconnected' || ra.status === 'error'" class="btn btn-xs btn-go" @click="connectRemote(ra)" :disabled="connecting[ra.id]">
              {{ connecting[ra.id] ? '连接中…' : (ra.status === 'error' ? '重试' : '连接') }}
            </button>
            <button v-else class="btn btn-xs" @click="disconnectRemote(ra)">断开</button>
            <button v-if="ra.status === 'connected'" class="btn btn-xs btn-primary" @click="createSkillForAgent(ra)">+ 创建 Skill</button>
            <button v-if="ra.status === 'connected'" class="btn btn-xs" @click="testAgent(ra)">测试</button>
            <button class="agent-icon-btn" @click="editRemote(ra)" title="编辑">✎</button>
            <button class="agent-icon-btn agent-icon-btn-danger" @click="deleteRemote(ra)" title="删除">✕</button>
          </div>
        </div>
      </div>
      <div v-else class="mcpsrv-empty">
        <span>暂无远端 Agent</span>
        <span class="hint">添加本地或远程 A2A Agent，将其能力集成到 EverEvo</span>
      </div>

      <!-- Add new agent form -->
      <div class="mcpsrv-add-section glass-panel">
        <h4>{{ editingRemote ? '编辑远端 Agent' : '添加远端 Agent' }}</h4>
        <div class="mcpsrv-form">
          <div class="mcpsrv-form-row">
            <label>名称</label>
            <input v-model="remoteForm.name" type="text" placeholder="例如：本地 Worker Agent" class="field mcpsrv-field" />
          </div>
          <div class="mcpsrv-form-row">
            <label>URL</label>
            <input v-model="remoteForm.url" type="text" placeholder="http://127.0.0.1:19801" class="field mcpsrv-field" />
          </div>
          <div class="mcpsrv-form-row">
            <label>签名密钥</label>
            <input v-model="remoteForm.secret" type="text" placeholder="对端 secret（与对端 server 一致，可留空）" class="field mcpsrv-field" />
          </div>
          <div class="mcpsrv-form-actions">
            <button v-if="editingRemote" class="btn btn-sm" @click="cancelEditRemote">取消</button>
            <button class="btn btn-sm btn-primary" @click="saveRemote" :disabled="!remoteForm.name || !remoteForm.url">
              {{ editingRemote ? '保存' : '添加并连接' }}
            </button>
          </div>
          <div v-if="remoteMsg" class="msg" :class="remoteOk ? 'msg-ok' : 'msg-err'">{{ remoteMsg }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { useToast } from '../../composables/useToast'
import { useDataChanged } from '../../composables/useDataChanged'
import { agentApi } from '../../api/agent'
import type { A2AConfig, RemoteAgent, AgentServerStatus } from '../../api/agent'

const toast = useToast()
function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

// ── Section A: Local Server ──
const serverOk = ref(false)
const serverUrl = ref('')
const serverErr = ref('')
const showGuide = ref(false)

const cfgForm = reactive<A2AConfig>({
  enabled: false,
  name: 'EverEvo Agent',
  description: '',
  version: '0.1.0',
  port: 19801,
  secret: '',
})

const saving = ref(false)

// ── Section B: Remote Agents ──
const remoteAgents = ref<RemoteAgent[]>([])
const connecting = ref<Record<string, boolean>>({})

const editingRemote = ref<RemoteAgent | null>(null)
const remoteForm = reactive({ name: '', url: '', secret: '' })
const remoteMsg = ref('')
const remoteOk = ref(false)

let _pollTimer: ReturnType<typeof setTimeout> | null = null

// ── Lifecycle ──
onBeforeUnmount(() => {
  if (_pollTimer) { clearTimeout(_pollTimer); _pollTimer = null }
})

onMounted(async () => {
  await loadAll()
  // Poll for connecting agents
  const poll = (remaining: number) => {
    if (remaining <= 0) { _pollTimer = null; return }
    _pollTimer = setTimeout(async () => {
      _pollTimer = null
      await loadRemoteAgents()
      const hasConnecting = remoteAgents.value.some((a: any) => a.status === 'connecting')
      if (hasConnecting) poll(remaining - 1)
    }, 2000)
  }
  poll(5)
})

useDataChanged('agent:changed', () => { loadAll() })

async function loadAll() {
  await Promise.all([loadLocalServer(), loadRemoteAgents()])
}

// ── Local Server ──
async function loadLocalServer() {
  try {
    const cfg = await agentApi.getConfig()
    cfgForm.enabled = cfg.enabled
    cfgForm.name = cfg.name || 'EverEvo Agent'
    cfgForm.description = cfg.description || ''
    cfgForm.version = cfg.version || '0.1.0'
    cfgForm.port = cfg.port || 19801
    cfgForm.secret = cfg.secret || ''
    const st = await agentApi.getServerStatus()
    serverOk.value = !!(st && st.running)
    serverUrl.value = (st && st.url) ? st.url : ''
  } catch (e: any) {
    serverErr.value = '状态查询失败: ' + (e.message || e)
  }
}

async function startLocalServer() {
  serverErr.value = ''
  try {
    await agentApi.startServer()
    await new Promise(r => setTimeout(r, 400))
    await loadLocalServer()
    if (serverOk.value) {
      cfgForm.enabled = true
      await agentApi.updateConfig(cfgForm)
      t('success', 'Agent 服务已启动', serverUrl.value)
    }
  } catch (e: any) {
    serverErr.value = '启动失败: ' + (e.message || e)
    t('error', '启动失败', e.message || e)
  }
}

async function stopLocalServer() {
  serverErr.value = ''
  try {
    await agentApi.stopServer()
    await loadLocalServer()
    cfgForm.enabled = false
    await agentApi.updateConfig(cfgForm)
    t('info', 'Agent 服务已停止')
  } catch (e: any) { t('error', '停止失败', e.message || e) }
}

async function restartLocalServer() {
  await agentApi.stopServer().catch(() => {})
  await new Promise(r => setTimeout(r, 400))
  await startLocalServer()
}

function copyAgentUrl() {
  if (!serverUrl.value) return
  navigator.clipboard.writeText(serverUrl.value).then(() => t('success', '已复制', serverUrl.value)).catch(() => {})
}

async function saveLocalConfig() {
  saving.value = true
  try {
    cfgForm.enabled = serverOk.value
    await agentApi.updateConfig(cfgForm)
    await restartLocalServer()
    t('success', '配置已保存并重启')
  } catch (e: any) { t('error', '保存失败', e.message || e) } finally { saving.value = false }
}

// ── Remote Agents ──
async function loadRemoteAgents() {
  try { remoteAgents.value = await agentApi.listRemoteAgents() || [] } catch (_) {}
}

function statusLabel(st: string) {
  const m: Record<string, string> = {
    connected: '已连接', connecting: '连接中', disconnected: '未连接', error: '错误',
  }
  return m[st] || st
}

async function connectRemote(ra: RemoteAgent) {
  connecting.value = { ...connecting.value, [ra.id]: true }
  try {
    await agentApi.connectRemoteAgent(ra.id)
    await loadRemoteAgents()
    t('success', '已连接', ra.name)
  } catch (e: any) { t('error', '连接失败', e.message || e) }
  connecting.value = { ...connecting.value, [ra.id]: false }
}

async function disconnectRemote(ra: RemoteAgent) {
  try {
    await agentApi.disconnectRemoteAgent(ra.id)
    await loadRemoteAgents()
    t('info', '已断开', ra.name)
  } catch (e: any) { t('error', '断开失败', e.message || e) }
}

async function deleteRemote(ra: RemoteAgent) {
  if (!await toast.confirm('删除 Agent', '确定删除「' + ra.name + '」？')) return
  try {
    await agentApi.removeRemoteAgent(ra.id)
    await loadRemoteAgents()
    t('success', '已删除', ra.name)
  } catch (e: any) { t('error', '删除失败', e.message || e) }
}

async function saveRemote() {
  if (!remoteForm.name.trim() || !remoteForm.url.trim()) return
  try {
    if (editingRemote.value) {
      await agentApi.updateRemoteAgent(editingRemote.value.id, remoteForm.name.trim(), remoteForm.url.trim(), remoteForm.secret.trim())
    } else {
      await agentApi.addRemoteAgent(remoteForm.name.trim(), remoteForm.url.trim(), remoteForm.secret.trim())
      await loadRemoteAgents()
      // Auto-connect the newly added agent
      const added = remoteAgents.value.find(a => a.url === remoteForm.url.trim() && a.status === 'disconnected')
      if (added) {
        await connectRemote(added)
      }
    }
    remoteMsg.value = ''
    remoteOk.value = true
    editingRemote.value = null
    remoteForm.name = ''
    remoteForm.url = ''
    remoteForm.secret = ''
    t('success', editingRemote.value ? '已更新' : '已添加')
  } catch (e: any) {
    remoteMsg.value = '操作失败: ' + (e.message || e)
    remoteOk.value = false
  }
}

function editRemote(ra: RemoteAgent) {
  editingRemote.value = ra
  remoteForm.name = ra.name
  remoteForm.url = ra.url
  remoteForm.secret = ra.secret || ''
  remoteMsg.value = ''
}

function cancelEditRemote() {
  editingRemote.value = null
  remoteForm.name = ''
  remoteForm.url = ''
  remoteForm.secret = ''
  remoteMsg.value = ''
}

async function createSkillForAgent(ra: RemoteAgent) {
  try {
    await agentApi.createAgentSkill(ra.id, '')
    t('success', 'Skill 已创建', 'Agent「' + ra.name + '」已转换为 Skill，可在能力清单中管理')
  } catch (e: any) {
    t('error', '创建失败', e.message || e)
  }
}

async function testAgent(ra: RemoteAgent) {
  try {
    const result = await agentApi.sendTask(ra.id, 'Hello, this is a connection test.')
    const text = result?.text || result?.status || JSON.stringify(result)
    t('success', '测试成功', text.substring(0, 100))
  } catch (e: any) {
    t('error', '测试失败', e.message || e)
  }
}
</script>

<style scoped>
/* ── Section ── */
.agent-section {
  display: flex; flex-direction: column; gap: 14px;
}
.agent-section + .agent-section { margin-top: 28px; }
.agent-section-head {
  display: flex; align-items: flex-start; gap: 14px;
  padding-bottom: 12px; border-bottom: 1px solid var(--border-subtle);
}
.agent-section-icon {
  width: 40px; height: 40px; border-radius: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; color: var(--accent); flex-shrink: 0;
}
.agent-section-info { display: flex; flex-direction: column; gap: 3px; }
.agent-section-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.agent-section-desc { font-size: 12px; color: var(--text-tertiary); line-height: 1.4; }

/* Local server cards row */
.agent-local-cards {
  display: grid; grid-template-columns: 1fr 220px; gap: 12px;
}
.agent-status-card {
  padding: 16px 18px; display: flex; flex-direction: column; gap: 12px;
}
.agent-status-row { display: flex; align-items: center; gap: 10px; }
.agent-status-label { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.agent-status-url {
  font-size: 12px; font-family: var(--font-mono); color: var(--accent);
  background: var(--bg-inset); padding: 2px 8px; border-radius: 4px;
}
.agent-error { padding: 8px 12px; font-size: 12px; color: var(--danger); background: var(--danger-dim); border-radius: var(--radius-sm); }
.agent-actions { display: flex; gap: 8px; }

.agent-port-card {
  padding: 16px 18px; display: flex; flex-direction: column; gap: 10px;
}
.agent-port-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.agent-port-row { display: flex; flex-direction: column; gap: 6px; }
.agent-port-row-inner { display: flex; gap: 8px; align-items: center; }
.agent-port-input {
  width: 80px; padding: 6px 8px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 13px; font-family: var(--font-mono); text-align: center; outline: none;
}
.agent-port-input:focus { border-color: var(--accent); }
.agent-port-input-wide { width: 100%; text-align: left; }

/* Access guide -- collapsible */
.agent-guide-card { padding: 14px 18px; }
.agent-guide-toggle {
  display: flex; align-items: center; gap: 10px;
  cursor: pointer; user-select: none; font-size: 13px; font-weight: 500;
  color: var(--text-secondary); transition: color 0.15s;
}
.agent-guide-toggle:hover { color: var(--text-primary); }
.agent-guide-body { margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border-subtle); }
.agent-guide-item { margin-bottom: 12px; font-size: 12px; }
.agent-guide-item:last-child { margin-bottom: 0; }
.agent-guide-item strong { color: var(--text-primary); }
.agent-guide-item p { color: var(--text-secondary); margin: 4px 0; }
.agent-guide-item code { font-size: 11px; font-family: var(--font-mono); padding: 1px 5px; background: var(--bg-inset); border-radius: 3px; color: var(--accent); }
.agent-guide-item pre { margin: 6px 0; padding: 10px 12px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 11px; line-height: 1.5; font-family: var(--font-mono); overflow-x: auto; white-space: pre-wrap; }

/* ── Remote Agent cards ── */
.agent-card-desc { font-size: 11px; color: var(--text-secondary); margin-top: 3px; }
.agent-card-skills { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 3px; }
.agent-skill-tag {
  font-size: 9px; padding: 1px 5px; border-radius: 2px;
  background: var(--bg-inset); color: var(--text-tertiary); font-family: var(--font-mono);
}

/* ── Shared dots / status styles (.glass-panel comes from global CSS, same as LLMMCP) ── */
.be-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-live { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5); }
.dot-dead { background: var(--danger); box-shadow: 0 0 3px rgba(255,69,58,0.4); }
.dot-off { background: var(--text-tertiary); opacity: 0.5; }

.mcpsrv-list { display: flex; flex-direction: column; gap: 8px; }
.mcpsrv-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 18px; gap: 14px;
}
.mcpsrv-card-left { display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0; }
.mcpsrv-info { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.mcpsrv-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.mcpsrv-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.mcpsrv-status-tag { font-size: 10px; font-weight: 600; padding: 1px 8px; border-radius: 8px; }
.st-connected { background: var(--success-dim); color: var(--success); }
.st-connecting { background: rgba(255,159,10,0.12); color: var(--warning); }
.st-disconnected { background: var(--bg-inset); color: var(--text-tertiary); }
.st-error { background: var(--danger-dim); color: var(--danger); }
.mcpsrv-toolcount { font-size: 10px; color: var(--accent); font-family: var(--font-mono); }
.mcpsrv-transport { font-size: 10px; color: var(--text-tertiary); }
.mcpsrv-cmdline { margin-top: 4px; }
.mcpsrv-cmdline code {
  font-size: 10px; font-family: var(--font-mono); color: var(--text-tertiary);
  background: var(--bg-inset); padding: 2px 8px; border-radius: 4px;
  display: inline-block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.mcpsrv-error {
  font-size: 10px; color: var(--danger); margin-top: 4px;
  padding: 6px 8px; background: var(--danger-dim); border-radius: 4px;
  line-height: 1.4; max-height: 80px; overflow-y: auto; white-space: pre-wrap; word-break: break-all;
}
.mcpsrv-actions { display: flex; align-items: center; gap: 4px; flex-shrink: 0; flex-wrap: nowrap; }
.mcpsrv-empty {
  text-align: center; padding: 28px 0; color: var(--text-tertiary);
  font-size: 13px; display: flex; flex-direction: column; gap: 4px;
}
.mcpsrv-empty .hint { font-size: 11px; opacity: 0.7; }

.mcpsrv-add-section {
  padding: 18px 20px;
  border: 1px solid var(--border-subtle);
}
.mcpsrv-add-section h4 { font-size: 14px; font-weight: 600; margin: 0 0 14px; }
.mcpsrv-form { display: flex; flex-direction: column; gap: 10px; }
.mcpsrv-form-row { display: flex; align-items: center; gap: 10px; }
.mcpsrv-form-row label { width: 72px; flex-shrink: 0; font-size: 12px; color: var(--text-secondary); text-align: right; }
.mcpsrv-field { flex: 1; padding: 7px 10px; min-width: 0; }
.mcpsrv-form-actions { display: flex; gap: 8px; padding-left: 82px; }

.msg { padding: 8px 12px; border-radius: var(--radius-sm); font-size: 12px; margin-top: 10px; }
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }

.field { padding: 7px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; outline: none; font-family: var(--font-mono); }
.field:focus { border-color: var(--accent); }

.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-xs {
  padding: 2px 7px; font-size: 11px; line-height: 1.3;
  border: 1px solid var(--border-soft); border-radius: 4px;
  background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer;
}
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
.btn-go { color: var(--success); border-color: rgba(48,209,88,0.3); }
.btn-go:hover { background: rgba(48,209,88,0.1); }
.btn-del { color: var(--danger); border-color: rgba(255,69,58,0.3); }
.btn-del:hover { background: rgba(255,69,58,0.1); }

/* ── Inline icon buttons (align with LLMConfig / LLMSkills .pbar-btn) ── */
.agent-icon-btn {
  width: 26px; height: 26px; display: flex; align-items: center; justify-content: center;
  border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-tertiary); font-size: 12px; cursor: pointer;
  transition: all 0.15s ease; flex-shrink: 0;
}
.agent-icon-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.agent-icon-btn-danger:hover { color: var(--danger); background: rgba(255,69,58,0.08); }
</style>
