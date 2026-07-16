<template>
  <div class="mcpsrv-form-root">
    <div class="mcpsrv-add-section glass-panel">
      <h4>{{ editingSrv ? '编辑 Server' : '添加 MCP Server' }}</h4>
      <div class="mcpsrv-form">
        <div class="mcpsrv-form-row">
          <label>名称</label>
          <input v-model="form.name" type="text" placeholder="例如：Filesystem" class="field mcpsrv-field" />
        </div>
        <div class="mcpsrv-form-row">
          <label>传输方式</label>
          <select v-model="form.transport" class="field mcpsrv-field-sm">
            <option value="stdio">本地命令 (stdio)</option>
            <option value="http">远程 HTTP</option>
          </select>
        </div>
        <template v-if="form.transport === 'stdio'">
          <div class="mcpsrv-form-row">
            <label>命令</label>
            <input v-model="form.command" type="text" placeholder="npx" class="field mcpsrv-field" />
          </div>
          <div class="mcpsrv-form-row">
            <label>参数（空格分隔）</label>
            <input v-model="form.argsText" type="text" :placeholder="'-y @anthropic/mcp-server-filesystem .'" class="field mcpsrv-field" />
          </div>
        </template>
        <template v-else>
          <div class="mcpsrv-form-row">
            <label>URL</label>
            <input v-model="form.url" type="text" placeholder="https://mcp.example.com/mcp" class="field mcpsrv-field" />
          </div>
        </template>
        <div class="mcpsrv-form-actions">
          <button v-if="editingSrv" class="btn btn-sm" @click="cancel">取消</button>
          <button class="btn btn-sm btn-primary" @click="save" :disabled="!form.name || saving">保存并连接</button>
        </div>
        <div v-if="msg" class="msg" :class="ok ? 'msg-ok' : 'msg-err'">{{ msg }}</div>
      </div>
    </div>

    <div v-if="recommends.length" class="mcpsrv-recommend glass-panel">
      <h4>推荐 Server</h4>
      <div class="mcpsrv-rec-grid">
        <div v-for="rec in recommends" :key="rec.key" class="mcpsrv-rec-card" @click="fillFromRecommend(rec)">
          <div class="mcpsrv-rec-name">{{ rec.name }}</div>
          <div class="mcpsrv-rec-desc">{{ rec.description }}</div>
          <span class="mcpsrv-rec-cat">{{ rec.category }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useToast } from '../../composables/useToast'
import { mcpApi, type MCPServer } from '../../api/mcp'

const toast = useToast()

function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

// ── Props ──
const props = defineProps<{
  editingSrv: Record<string, any> | null
  recommends: Record<string, any>[]
}>()

// ── Emits ──
const emit = defineEmits<{
  (e: 'saved'): void
  (e: 'cancel'): void
}>()

// ── Form state ──
const form = reactive({
  name: '',
  transport: 'stdio',
  command: '',
  argsText: '',
  url: '',
})

const msg = ref('')
const ok = ref(false)
const saving = ref(false)

// ── Reset ──
function resetForm() {
  form.name = ''
  form.transport = 'stdio'
  form.command = ''
  form.argsText = ''
  form.url = ''
  msg.value = ''
}

// ── Watcher: populate form when editingSrv changes ──
watch(
  () => props.editingSrv,
  (srv) => {
    resetForm()
    if (srv) {
      form.name = srv.name || ''
      form.transport = srv.transport || 'stdio'
      form.command = srv.command || ''
      form.argsText = (srv.args || []).join(' ')
      form.url = srv.url || ''
    }
  },
  { immediate: true }
)

// ── Save ──
async function save() {
  if (saving.value) return
  const args = form.argsText ? form.argsText.split(/\s+/).filter(Boolean) : []
  const cfg: Partial<MCPServer> = {
    id: props.editingSrv ? props.editingSrv.id : '',
    name: form.name.trim(),
    transport: form.transport as 'stdio' | 'http',
    command: form.command.trim(),
    args,
    url: form.url.trim(),
    status: 'disconnected',
  }

  saving.value = true
  try {
    if (props.editingSrv) {
      await mcpApi.removeServer(cfg.id)
    }
    await mcpApi.addServer(cfg)
    // Connect after adding
    const servers = await mcpApi.listServers() || []
    const added = servers.find((s: any) => s.name === cfg.name)
    if (added) {
      await mcpApi.connectServer(added.id)
    }
    msg.value = ''
    ok.value = true
    resetForm()
    emit('saved')
    t('success', '已添加并连接', cfg.name)
  } catch (e: any) {
    msg.value = '保存失败: ' + (e.message || e)
    ok.value = false
  } finally {
    saving.value = false
  }
}

// ── Cancel ──
function cancel() {
  resetForm()
  emit('cancel')
}

// ── Quick-fill from recommended server ──
function fillFromRecommend(rec: Record<string, any>) {
  resetForm()
  form.name = rec.name || ''
  form.transport = rec.transport || 'stdio'
  form.command = rec.transport === 'stdio' ? (rec.command || '') : ''
  form.argsText = rec.transport === 'stdio' && rec.args ? rec.args.join(' ') : ''
  form.url = rec.url || ''
}
</script>

<style scoped>
/* ── Utility classes ── */
.field {
  padding: 7px 10px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
  color: var(--text-primary);
  font-size: 12px;
  outline: none;
  font-family: var(--font-mono);
}
.field:focus { border-color: var(--accent); }

.msg {
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  font-size: 12px;
  margin-top: 10px;
}
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }

.btn {
  padding: 6px 12px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
}
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }

/* ── Form root ── */
.mcpsrv-form-root {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* ── Add / Edit form ── */
.mcpsrv-add-section {
  padding: 18px 20px;
  border: 1px solid var(--border-subtle);
}
.mcpsrv-add-section h4 { font-size: 14px; font-weight: 600; margin: 0 0 14px; }

.mcpsrv-form { display: flex; flex-direction: column; gap: 10px; }
.mcpsrv-form-row { display: flex; align-items: center; gap: 10px; }
.mcpsrv-form-row label {
  width: 72px;
  flex-shrink: 0;
  font-size: 12px;
  color: var(--text-secondary);
  text-align: right;
}
.mcpsrv-field { flex: 1; padding: 7px 10px; min-width: 0; }
.mcpsrv-field-sm { width: 200px; flex: none; }
.mcpsrv-form-actions { display: flex; gap: 8px; padding-left: 82px; }

/* ── Recommended servers ── */
.mcpsrv-recommend { padding: 18px 20px; }
.mcpsrv-recommend h4 {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 12px;
  color: var(--text-secondary);
}
.mcpsrv-rec-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px;
}
.mcpsrv-rec-card {
  padding: 12px 14px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  cursor: pointer;
  transition: all 0.15s;
  background: var(--bg-elevated);
}
.mcpsrv-rec-card:hover { border-color: var(--accent); background: var(--bg-hover); }
.mcpsrv-rec-name { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }
.mcpsrv-rec-desc { font-size: 11px; color: var(--text-secondary); line-height: 1.4; margin-bottom: 6px; }
.mcpsrv-rec-cat { font-size: 10px; color: var(--text-tertiary); font-family: var(--font-mono); }
</style>
