<template>
  <div>
    <!-- Section A: EverEvo MCP Service -->
    <div class="mcp-section">
      <div class="mcp-section-head">
        <div class="mcp-section-icon">&#9678;</div>
        <div class="mcp-section-info">
          <div class="mcp-section-title">EverEvo MCP &#26381;&#21153;</div>
          <div class="mcp-section-desc">&#36719;&#20214;&#21551;&#21160;&#26102;&#33258;&#21160;&#36816;&#34892;&#65292;&#23558;&#27169;&#22411;&#31649;&#29702;&#12289;&#30693;&#35782;&#24211;&#31561;&#33021;&#21147;&#20197; MCP &#21327;&#35758;&#26292;&#38706;&#32473;&#22806;&#37096; AI &#24037;&#20855;&#65288;Claude Desktop&#12289;Cursor &#31561;&#65289;</div>
        </div>
      </div>

      <div class="mcp-local-cards">
        <!-- Status card -->
        <div class="glass-panel mcp-status-card">
          <div class="mcp-status-row">
            <span class="be-dot" :class="mcpOk ? 'dot-live' : 'dot-dead'"></span>
            <span class="mcp-status-label">{{ mcpOk ? '&#36816;&#34892;&#20013;' : '&#24050;&#20572;&#27490;' }}</span>
            <span v-if="mcpOk && mcpUrl" class="mcp-status-url">{{ mcpUrl }}</span>
          </div>
          <div v-if="mcpErr" class="mcp-error">{{ mcpErr }}</div>
          <div class="mcp-actions">
            <button v-if="!mcpOk" class="btn btn-primary" @click="startMCP">&#21551;&#21160;&#26381;&#21153;</button>
            <button v-else class="btn" @click="stopMCP">&#20572;&#27490;</button>
            <button v-if="mcpOk" class="btn btn-sm" @click="restartMCP">&#37325;&#21551;</button>
            <button v-if="mcpOk && mcpUrl" class="btn btn-sm" @click="copyMCPUrl">&#22797;&#21046;&#22320;&#22336;</button>
          </div>
        </div>

        <!-- Port config card -->
        <div class="glass-panel mcp-port-card">
          <label class="mcp-port-label">&#26381;&#21153;&#31471;&#21475;</label>
          <div class="mcp-port-row">
            <input type="number" v-model.number="mcpPort" class="mcp-port-input" placeholder="19800" />
            <button class="btn btn-sm btn-primary" @click="saveMCPPort">&#20445;&#23384;&#24182;&#37325;&#21551;</button>
          </div>
        </div>
      </div>

    </div>

    <!-- Section B: External Integration -->
    <div class="mcp-section">
      <div class="mcp-section-head">
        <div class="mcp-section-icon">&#8652;</div>
        <div class="mcp-section-info">
          <div class="mcp-section-title">&#22806;&#37096;&#38598;&#25104;</div>
          <div class="mcp-section-desc">&#25509;&#20837;&#31532;&#19977;&#26041; MCP Server &#25193;&#23637;&#33021;&#21147;&#65292;&#25110;&#23558; EverEvo &#20197; MCP &#21327;&#35758;&#26292;&#38706;&#32473;&#22806;&#37096; AI &#24037;&#20855;&#65288;Claude Desktop / Cursor &#31561;&#65289;</div>
        </div>
      </div>

      <!-- Access guide -->
      <div class="glass-panel mcp-guide-card">
        <div class="mcp-guide-toggle" @click="showMCPGuide = !showMCPGuide">
          <span>{{ showMCPGuide ? '&#9662;' : '&#9656;' }} &#25509;&#20837;&#25351;&#21335; — Claude Desktop / Cursor / VS Code</span>
        </div>
        <div v-if="showMCPGuide" class="mcp-guide-body">
          <div class="mcp-guide-item">
            <strong>Claude Desktop</strong>
            <p>&#32534;&#36753; <code>claude_desktop_config.json</code>&#65306;</p>
            <pre>{
  "mcpServers": {
    "everevo": {
      "url": "{{ mcpUrl || 'http://127.0.0.1:19800/mcp' }}"
    }
  }
}</pre>
          </div>
          <div class="mcp-guide-item">
            <strong>Cursor / Continue / VS Code</strong>
            <p>&#22312; MCP &#37197;&#32622;&#20013;&#28155;&#21152;&#65306;<code>{{ mcpUrl || 'http://127.0.0.1:19800/mcp' }}</code></p>
          </div>
          <div class="mcp-guide-item">
            <strong>&#26412;&#24212;&#29992; AI &#32842;&#22825;</strong>
            <p>&#20999;&#25442;&#21040;&#12300;AI &#32842;&#22825;&#12301;&#26631;&#31614;&#39029;&#21363;&#21487;&#33258;&#21160;&#36830;&#25509;&#20869;&#32622;&#33021;&#21147;</p>
          </div>
        </div>
      </div>

      <!-- Connected server list -->
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px;">
        <h4 style="margin:0;">外部 MCP Server</h4>
        <label style="font-size:11px;color:var(--text-tertiary);cursor:pointer;display:flex;align-items:center;gap:4px;">
          <input type="checkbox" v-model="showAllDomains" style="cursor:pointer;" />
          显示全部领域
        </label>
      </div>
      <div class="mcpsrv-list" v-if="mcpServers.length">
        <div v-for="srv in mcpServers" :key="srv.id" class="glass-panel mcpsrv-card">
          <div class="mcpsrv-card-left">
            <span class="be-dot" :class="srv.status === 'connected' ? 'dot-live' : (srv.status === 'error' ? 'dot-dead' : 'dot-off')"></span>
            <div class="mcpsrv-info">
              <div class="mcpsrv-name">{{ srv.name }} <span v-if="srv.libraryId && libName(srv.libraryId)" class="domain-tag">&#x1F4DA; {{ libName(srv.libraryId) }}</span></div>
              <div class="mcpsrv-meta">
                <span class="mcpsrv-status-tag" :class="'st-' + srv.status">{{ statusLabel(srv.status) }}</span>
                <span v-if="srv.toolCount" class="mcpsrv-toolcount">{{ srv.toolCount }} &#24037;&#20855;</span>
                <span class="mcpsrv-transport">{{ srv.transport === 'stdio' ? '&#26412;&#22320;&#23376;&#36827;&#31243;' : '&#36828;&#31243; HTTP' }}</span>
              </div>
              <!-- Show the actual command line for stdio servers -->
              <div v-if="srv.transport === 'stdio' && srv.command" class="mcpsrv-cmdline">
                <code>{{ srv.command }} {{ (srv.args || []).join(' ') }}</code>
              </div>
              <div v-else-if="srv.transport === 'http' && srv.url" class="mcpsrv-cmdline">
                <code>{{ srv.url }}</code>
              </div>
              <div v-if="srv.error" class="mcpsrv-error">{{ srv.error }}</div>
            </div>
          </div>
          <div class="mcpsrv-actions">
            <button v-if="srv.status === 'disconnected' || srv.status === 'error'" class="btn btn-xs btn-go" @click="connectMCP(srv.id)">{{ srv.status === 'error' ? '&#37325;&#35797;' : '&#36830;&#25509;' }}</button>
            <button v-else class="btn btn-xs" @click="disconnectMCP(srv.id)">&#26029;&#24320;</button>
            <button v-if="srv.status === 'connected'" class="btn btn-xs" @click="refreshMCPTools(srv.id)">&#21047;&#26032;&#24037;&#20855;</button>
            <button class="btn btn-xs btn-del" @click="removeMCPServer(srv.id)">&#21024;&#38500;</button>
          </div>
        </div>
      </div>
      <div v-else class="mcpsrv-empty">
        <span>&#26242;&#26080;&#31532;&#19977;&#26041; MCP Server</span>
        <span class="hint">&#28155;&#21152;&#31038;&#21306; MCP Server&#65292;EverEvo &#20316;&#20026;&#32479;&#19968;&#35843;&#24230;&#20013;&#24515;&#33258;&#21160;&#35843;&#29992;&#20854;&#24037;&#20855;</span>
      </div>

      <!-- Add new server form -->
      <div class="mcpsrv-add-section glass-panel">
        <h4>{{ editingSrv ? '&#32534;&#36753; Server' : '&#28155;&#21152; MCP Server' }}</h4>
        <div class="mcpsrv-form">
          <div class="mcpsrv-form-row">
            <label>&#21517;&#31216;</label>
            <input v-model="srvForm.name" type="text" placeholder="&#20363;&#22914;&#65306;Filesystem" class="field mcpsrv-field" />
          </div>
          <div class="mcpsrv-form-row">
            <label>&#20256;&#36755;&#26041;&#24335;</label>
            <select v-model="srvForm.transport" class="field mcpsrv-field-sm">
              <option value="stdio">&#26412;&#22320;&#21629;&#20196; (stdio)</option>
              <option value="http">&#36828;&#31243; HTTP</option>
            </select>
          </div>
          <template v-if="srvForm.transport === 'stdio'">
            <div class="mcpsrv-form-row">
              <label>&#21629;&#20196;</label>
              <input v-model="srvForm.command" type="text" placeholder="npx" class="field mcpsrv-field" />
            </div>
            <div class="mcpsrv-form-row">
              <label>&#21442;&#25968;&#65288;&#31354;&#26684;&#20998;&#38548;&#65289;</label>
              <input v-model="srvForm.argsText" type="text" :placeholder="'-y @anthropic/mcp-server-filesystem .'" class="field mcpsrv-field" />
            </div>
          </template>
          <template v-else>
            <div class="mcpsrv-form-row">
              <label>URL</label>
              <input v-model="srvForm.url" type="text" placeholder="https://mcp.example.com/mcp" class="field mcpsrv-field" />
            </div>
          </template>
          <div class="mcpsrv-form-actions">
            <button v-if="editingSrv" class="btn btn-sm" @click="editingSrv = null; resetSrvForm()">&#21462;&#28040;</button>
            <button class="btn btn-sm btn-primary" @click="saveMCPServer" :disabled="!srvForm.name">&#20445;&#23384;&#24182;&#36830;&#25509;</button>
          </div>
          <div v-if="srvMsg" class="msg" :class="srvOk ? 'msg-ok' : 'msg-err'">{{ srvMsg }}</div>
        </div>
      </div>

      <!-- Recommended servers -->
      <div class="mcpsrv-recommend glass-panel" v-if="recommends.length">
        <h4>&#25512;&#33616; Server</h4>
        <div class="mcpsrv-rec-grid">
          <div v-for="rec in recommends" :key="rec.key" class="mcpsrv-rec-card" @click="installRecommend(rec)">
            <div class="mcpsrv-rec-name">{{ rec.name }}</div>
            <div class="mcpsrv-rec-desc">{{ rec.description }}</div>
            <span class="mcpsrv-rec-cat">{{ rec.category }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { useToast } from '../../composables/useToast'
import { useDataChanged } from '../../composables/useDataChanged'
import { useActiveLibrary } from '../../composables/useActiveLibrary'
import { mcpApi, type MCPServer } from '../../api/mcp'
import { systemApi } from '../../api/system'
import { memoryApi } from '../../api/memory'

// ── Toast ──
const toast = useToast()

function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

// ── Domain ──
const { activeLibraryId } = useActiveLibrary()
const showAllDomains = ref(false)
const domainLibs = ref<{ id: string; name: string }[]>([])
function libName(id: string) { return domainLibs.value.find(l => l.id === id)?.name || '' }

// ── State ──
const mcpOk = ref(false)
const mcpUrl = ref('')
const mcpPort = ref(19800)
const mcpErr = ref('')

const allServers = ref<any[]>([])
// Filtered servers: either all, or only those matching active library.
const mcpServers = computed(() => {
  const list = allServers.value || []
  if (showAllDomains.value || !activeLibraryId.value) return list
  return list.filter((s: any) => !s.libraryId || s.libraryId === activeLibraryId.value)
})
const recommends = ref<any[]>([])
const editingSrv = ref<any>(null)
const srvForm = reactive({ name: '', transport: 'stdio', command: '', argsText: '', url: '' })
const srvMsg = ref('')
const srvOk = ref(false)
const showMCPGuide = ref(false)

// ── Anti-freeze guards ──
const saving = ref(false)
let _pollTimer: ReturnType<typeof setTimeout> | null = null
let _mcpRetryTimer: ReturnType<typeof setTimeout> | null = null

// ── Real-time refresh ──
useDataChanged('mcp:changed', () => { loadMCPServers() })

// ── Lifecycle ──
onBeforeUnmount(() => {
  if (_pollTimer) { clearTimeout(_pollTimer); _pollTimer = null }
  if (_mcpRetryTimer) { clearTimeout(_mcpRetryTimer); _mcpRetryTimer = null }
})

onMounted(async () => {
  loadMCP()
  loadMCPServers()
  loadRecommends()
  _mcpRetryTimer = setTimeout(() => { loadMCP(); _mcpRetryTimer = null }, 1500)
  const pollMCP = (remaining: number) => {
    if (remaining <= 0) { _pollTimer = null; return }
    _pollTimer = setTimeout(async () => {
      _pollTimer = null
      await loadMCPServers()
      loadMCP()
      const hasConnecting = mcpServers.value.some((s: any) => s.status === 'connecting')
      if (hasConnecting) pollMCP(remaining - 1)
    }, 2000)
  }
  pollMCP(5)
})

// ── EverEvo MCP Service ──
async function loadMCP() {
  try {
    const st = await mcpApi.getStatus()
    mcpOk.value = !!(st && st.running)
    mcpUrl.value = (st && st.url) ? st.url : ''
  } catch (e: any) { mcpErr.value = '&#29366;&#24577;&#26597;&#35810;&#22833;&#36133;: ' + (e.message || e) }
  try {
    const cfg = await systemApi.getConfig()
    if (cfg && cfg.llm) mcpPort.value = cfg.llm.mcpPort || 19800
  } catch (_) {}
}

async function startMCP() {
  mcpErr.value = ''
  try {
    await mcpApi.startServer()
    await new Promise(r => setTimeout(r, 400))
    await loadMCP()
    if (mcpOk.value) t('success', 'EverEvo MCP &#26381;&#21153;&#24050;&#21551;&#21160;', mcpUrl.value)
  } catch (e: any) { mcpErr.value = '&#21551;&#21160;&#22833;&#36133;: ' + (e.message || e); t('error', 'MCP &#21551;&#21160;&#22833;&#36133;', e.message || e) }
}

async function stopMCP() {
  mcpErr.value = ''
  try {
    await mcpApi.stopServer()
    await loadMCP()
    t('info', 'EverEvo MCP &#26381;&#21153;&#24050;&#20572;&#27490;')
  } catch (e: any) { t('error', 'MCP &#20572;&#27490;&#22833;&#36133;', e.message || e) }
}

async function restartMCP() {
  await mcpApi.stopServer().catch(() => {})
  await new Promise(r => setTimeout(r, 400))
  await startMCP()
}

function copyMCPUrl() {
  if (!mcpUrl.value) return
  navigator.clipboard.writeText(mcpUrl.value).then(() => t('success', '&#24050;&#22797;&#21046;', mcpUrl.value)).catch(() => {})
}

async function saveMCPPort() {
  try {
    await mcpApi.setPort(mcpPort.value)
    await new Promise(r => setTimeout(r, 400))
    await loadMCP()
    t('success', 'MCP &#31471;&#21475;&#24050;&#26356;&#26032;', '&#31471;&#21475;: ' + mcpPort.value)
  } catch (e: any) { t('error', '&#20445;&#23384;&#22833;&#36133;', e.message || e) }
}

// ── MCP Server management ──
async function loadMCPServers() {
  try { allServers.value = await mcpApi.listServers() || [] } catch (_) {}
  try { domainLibs.value = (await memoryApi.libraryList()) || [] } catch (_) {}
}

async function loadRecommends() {
  try { recommends.value = await mcpApi.listRecommends() || [] } catch (_) {}
}

function statusLabel(st: string) {
  const m: Record<string, string> = { connected: '&#24050;&#36830;&#25509;', connecting: '&#36830;&#25509;&#20013;', disconnected: '&#26410;&#36830;&#25509;', error: '&#38169;&#35823;' }
  return m[st] || st
}

function resetSrvForm() {
  srvForm.name = ''
  srvForm.transport = 'stdio'
  srvForm.command = ''
  srvForm.argsText = ''
  srvForm.url = ''
}

async function saveMCPServer() {
  if (saving.value) return
  const args = srvForm.argsText ? srvForm.argsText.split(/\s+/).filter(Boolean) : []
  const cfg: Partial<MCPServer> = {
    id: editingSrv.value ? editingSrv.value.id : '',
    name: srvForm.name.trim(),
    transport: srvForm.transport as 'stdio' | 'http',
    command: srvForm.command.trim(),
    args: args,
    url: srvForm.url.trim(),
    status: 'disconnected',
    libraryId: activeLibraryId.value || (editingSrv.value ? editingSrv.value.libraryId : ''),
  }
  saving.value = true
  try {
    if (editingSrv.value) {
      await mcpApi.removeServer(cfg.id)
    }
    await mcpApi.addServer(cfg)
    await loadMCPServers()
    const added = mcpServers.value.find((s: any) => s.name === cfg.name)
    if (added) {
      await mcpApi.connectServer(added.id)
      await loadMCPServers()
    }
    srvMsg.value = ''; srvOk.value = true
    editingSrv.value = null; resetSrvForm()
    t('success', '&#24050;&#28155;&#21152;&#24182;&#36830;&#25509;', cfg.name)
  } catch (e: any) {
    srvMsg.value = '&#20445;&#23384;&#22833;&#36133;: ' + (e.message || e); srvOk.value = false
  } finally {
    saving.value = false
  }
}

async function connectMCP(id: string) {
  const idx = mcpServers.value.findIndex((s: any) => s.id === id)
  if (idx >= 0) { mcpServers.value[idx].status = 'connecting'; mcpServers.value[idx].error = '' }
  try {
    await mcpApi.connectServer(id)
    await loadMCPServers()
    t('success', '&#24050;&#36830;&#25509;')
  } catch (e: any) {
    if (idx >= 0) mcpServers.value[idx].status = 'error'
    await loadMCPServers()
    t('error', '&#36830;&#25509;&#22833;&#36133;', e.message || e)
  }
}

async function disconnectMCP(id: string) {
  try {
    await mcpApi.disconnectServer(id)
    await loadMCPServers()
    t('success', '&#24050;&#26029;&#24320;')
  } catch (e: any) { t('error', '&#26029;&#24320;&#22833;&#36133;', e.message || e) }
}

async function refreshMCPTools(id: string) {
  try {
    await mcpApi.disconnectServer(id)
    await new Promise(r => setTimeout(r, 300))
    await mcpApi.connectServer(id)
    await loadMCPServers()
    t('success', '&#24037;&#20855;&#24050;&#21047;&#26032;')
  } catch (e: any) { t('error', '&#21047;&#26032;&#22833;&#36133;', e.message || e) }
}

async function removeMCPServer(id: string) {
  if (!await toast.confirm('&#21024;&#38500; Server', '&#30830;&#23450;&#21024;&#38500;&#27492; MCP Server&#65311;&#20851;&#32852;&#30340;&#24037;&#20855;&#23558;&#19981;&#21487;&#29992;&#12290;')) return
  try {
    await mcpApi.removeServer(id)
    await loadMCPServers()
    t('success', '&#24050;&#21024;&#38500;')
  } catch (e: any) { t('error', '&#21024;&#38500;&#22833;&#36133;', e.message || e) }
}

function installRecommend(rec: any) {
  resetSrvForm()
  editingSrv.value = null
  srvForm.name = rec.name
  srvForm.transport = rec.transport
  srvForm.command = rec.transport === 'stdio' ? rec.command : ''
  srvForm.argsText = rec.transport === 'stdio' && rec.args ? rec.args.join(' ') : ''
  srvForm.url = rec.url || ''
}
</script>

<style scoped>
/* ── MCP Section ── */
.mcp-section {
  display: flex; flex-direction: column; gap: 14px;
}
.mcp-section + .mcp-section { margin-top: 28px; }
.mcp-section-head {
  display: flex; align-items: flex-start; gap: 14px;
  padding-bottom: 12px; border-bottom: 1px solid var(--border-subtle);
}
.mcp-section-icon {
  width: 40px; height: 40px; border-radius: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; color: var(--accent); flex-shrink: 0;
}
.mcp-section-info { display: flex; flex-direction: column; gap: 3px; }
.mcp-section-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.mcp-section-desc { font-size: 12px; color: var(--text-tertiary); line-height: 1.4; }

/* Local server cards row */
.mcp-local-cards {
  display: grid; grid-template-columns: 1fr 200px; gap: 12px;
}
.mcp-status-card {
  padding: 16px 18px; display: flex; flex-direction: column; gap: 12px;
}
.mcp-status-row { display: flex; align-items: center; gap: 10px; }
.mcp-status-label { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.mcp-status-url {
  font-size: 12px; font-family: var(--font-mono); color: var(--accent);
  background: var(--bg-inset); padding: 2px 8px; border-radius: 4px;
}
.mcp-error { padding: 8px 12px; font-size: 12px; color: var(--danger); background: var(--danger-dim); border-radius: var(--radius-sm); }
.mcp-actions { display: flex; gap: 8px; }

.mcp-port-card {
  padding: 16px 18px; display: flex; flex-direction: column; gap: 10px;
}
.mcp-port-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.mcp-port-row { display: flex; gap: 8px; align-items: center; }
.mcp-port-input {
  width: 80px; padding: 6px 8px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 13px; font-family: var(--font-mono); text-align: center; outline: none;
}
.mcp-port-input:focus { border-color: var(--accent); }

/* Access guide -- collapsible */
.mcp-guide-card { padding: 14px 18px; }
.mcp-guide-toggle {
  display: flex; align-items: center; gap: 10px;
  cursor: pointer; user-select: none; font-size: 13px; font-weight: 500;
  color: var(--text-secondary); transition: color 0.15s;
}
.mcp-guide-toggle:hover { color: var(--text-primary); }
.mcp-guide-hint { font-size: 11px; color: var(--text-tertiary); font-weight: 400; }
.mcp-guide-body { margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border-subtle); }
.mcp-guide-item { margin-bottom: 12px; font-size: 12px; }
.mcp-guide-item:last-child { margin-bottom: 0; }
.mcp-guide-item strong { color: var(--text-primary); }
.mcp-guide-item p { color: var(--text-secondary); margin: 4px 0; }
.mcp-guide-item code { font-size: 11px; font-family: var(--font-mono); padding: 1px 5px; background: var(--bg-inset); border-radius: 3px; color: var(--accent); }
.mcp-guide-item pre { margin: 6px 0; padding: 10px 12px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 11px; font-family: var(--font-mono); overflow-x: auto; white-space: pre-wrap; }

/* ── External MCP servers ── */
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
.mcpsrv-actions { display: flex; gap: 4px; flex-shrink: 0; }
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
.mcpsrv-field-sm { width: 200px; flex: none; }
.mcpsrv-form-actions { display: flex; gap: 8px; padding-left: 82px; }

.mcpsrv-recommend { padding: 18px 20px; }
.mcpsrv-recommend h4 { font-size: 13px; font-weight: 600; margin: 0 0 12px; color: var(--text-secondary); }
.mcpsrv-rec-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 8px; }
.mcpsrv-rec-card {
  padding: 12px 14px; border: 1px solid var(--border-soft); border-radius: var(--radius);
  cursor: pointer; transition: all 0.15s;
  background: var(--bg-elevated);
}
.mcpsrv-rec-card:hover { border-color: var(--accent); background: var(--bg-hover); }
.mcpsrv-rec-name { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }
.mcpsrv-rec-desc { font-size: 11px; color: var(--text-secondary); line-height: 1.4; margin-bottom: 6px; }
.mcpsrv-rec-cat { font-size: 10px; color: var(--text-tertiary); font-family: var(--font-mono); }

/* ── Shared UI elements (needed by this component) ── */
.dot-off { background: var(--text-tertiary); opacity: 0.5; }
.be-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-live { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5); }
.dot-dead { background: var(--danger); box-shadow: 0 0 3px rgba(255,69,58,0.4); }

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
</style>
