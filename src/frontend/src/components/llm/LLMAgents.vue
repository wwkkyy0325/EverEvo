<template>
  <div class="llm-agents">
    <!-- Toolbar -->
    <div class="agents-toolbar glass-panel">
      <div class="agents-toolbar-info">
        <span class="agents-count">{{ agents.length }} 个 Agent{{ showAllDomains ? ' (全部领域)' : '' }}</span>
        <span class="agents-hint">本地智能体人格 —— 主 Agent 可 <code>agent_create</code> / <code>agent_run</code> 调用，也可在聊天中切换</span>
      </div>
      <div class="agents-toolbar-actions">
        <label style="font-size:11px;color:var(--text-tertiary);cursor:pointer;display:flex;align-items:center;gap:4px;margin-right:4px;">
          <input type="checkbox" v-model="showAllDomains" style="cursor:pointer;" /> 全部领域
        </label>
        <button class="btn btn-sm" @click="loadAgents">刷新</button>
        <button class="btn btn-sm btn-primary" @click="openCreate">+ 新建 Agent</button>
      </div>
    </div>

    <!-- Agent cards -->
    <div class="agents-list">
      <div v-for="ag in agents" :key="ag.id" class="agent-card glass-panel">
        <div class="agc-main">
          <div class="agc-icon">{{ ag.icon || '◉' }}</div>
          <div class="agc-info">
            <div class="agc-title-row">
              <span class="agc-name">{{ ag.name }}</span>
              <span v-if="ag.isDefault" class="agc-badge agc-badge-default">默认主 Agent</span>
            </div>
            <div class="agc-desc">{{ ag.description || '（无描述）' }}</div>
            <div class="agc-meta">
              <span v-if="libraryName(ag.libraryId)" class="agc-tag" title="所属领域库">📚 {{ libraryName(ag.libraryId) }}</span>
              <span class="agc-tag" :title="'使用模型'">{{ modelLabel(ag) }}</span>
              <span class="agc-tag">{{ capabilityLabel(ag) }}</span>
              <span v-if="ag.temperature != null" class="agc-tag">temp {{ ag.temperature }}</span>
              <span v-if="ag.maxTokens" class="agc-tag">{{ ag.maxTokens }} tok</span>
            </div>
          </div>
        </div>
        <div class="agc-actions">
          <button class="agc-btn" @click="openEdit(ag)" title="编辑">✎</button>
          <button class="agc-btn" @click="duplicateAgent(ag)" title="复制">⧉</button>
          <button class="agc-btn agc-btn-danger" @click="deleteAgent(ag)" :disabled="ag.isDefault" :title="ag.isDefault ? '默认 Agent 不可删除' : '删除'">✕</button>
        </div>
      </div>
      <div v-if="!agents.length" class="agents-empty">
        <span>暂无 Agent</span>
        <span class="hint">点击「新建 Agent」创建一个智能体人格</span>
      </div>
    </div>

    <!-- Create / Edit dialog -->
    <div v-if="dialogOpen" class="overlay" @click.self="closeDialog">
      <div class="glass-panel dialog agent-dialog">
        <div class="ag-dialog-head">
          <div class="ag-icon-pick" @click="showIconPick = !showIconPick">
            <span class="ag-icon-preview">{{ form.icon || '◉' }}</span>
            <span class="ag-icon-arrow">▾</span>
            <div v-if="showIconPick" class="ag-icon-dropdown" @click.stop>
              <div class="ag-icon-palette">
                <button v-for="ic in iconPalette" :key="ic"
                  :class="['ag-icon-btn', { 'ag-icon-sel': form.icon === ic }]"
                  @click="form.icon = ic; showIconPick = false">{{ ic }}</button>
              </div>
            </div>
          </div>
          <div class="ag-head-info">
            <h3>{{ editing ? '编辑 Agent' : '新建 Agent' }}</h3>
            <input v-model="form.name" type="text" placeholder="Agent 名称，如「翻译专家」" class="ag-title-input" />
          </div>
          <button class="ag-dialog-close" @click="closeDialog">✕</button>
        </div>

        <div class="ag-body">
          <!-- Description -->
          <div class="ag-section">
            <label class="ag-label">描述</label>
            <input v-model="form.description" type="text" placeholder="一句话说明这个 Agent 的用途" class="field ag-input" />
          </div>

          <!-- Domain library binding -->
          <div class="ag-section">
            <label class="ag-label">所属领域库 <span class="ag-label-hint">— 该 Agent 为哪个领域库服务</span></label>
            <select v-model="form.libraryId" class="field ag-input">
              <option value="">核心领域（默认）</option>
              <option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option>
            </select>
          </div>

          <!-- System prompt -->
          <div class="ag-section">
            <label class="ag-label">系统提示词 <span class="ag-label-hint">— 定义 Agent 的人格、能力和行为准则</span></label>
            <textarea v-model="form.systemPrompt" rows="5" placeholder="你是一个……" class="field ag-textarea"></textarea>
          </div>

          <!-- Model -->
          <div class="ag-section ag-section-grid2">
            <div class="ag-field">
              <label class="ag-label">供应商 <span class="ag-label-hint">— 留空用全局活动供应商</span></label>
              <select v-model="form.providerId" class="field ag-input">
                <option value="">（继承全局）</option>
                <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }}</option>
              </select>
            </div>
            <div class="ag-field">
              <label class="ag-label">模型 <span class="ag-label-hint">— 覆盖供应商默认模型（可选）</span></label>
              <input v-model="form.model" type="text" :placeholder="providerDefaultModelPlaceholder" class="field ag-input" />
            </div>
            <div class="ag-field">
              <label class="ag-label">Temperature <span class="ag-label-hint">— 留空=默认</span></label>
              <input v-model="form.temperature" type="number" step="0.1" min="0" max="2" placeholder="如 0.7" class="field ag-input" />
            </div>
            <div class="ag-field">
              <label class="ag-label">Max tokens <span class="ag-label-hint">— 留空=默认</span></label>
              <input v-model="form.maxTokens" type="number" step="1" min="0" placeholder="如 2048" class="field ag-input" />
            </div>
          </div>

          <!-- Capabilities -->
          <div class="ag-section">
            <label class="ag-cap-toggle">
              <input type="checkbox" v-model="form.inheritSkills" />
              <span>继承全局所有已启用能力（推荐主 Agent 勾选）</span>
            </label>
          </div>
          <div v-if="!form.inheritSkills" class="ag-section">
            <label class="ag-label">授予的能力域 <span class="ag-label-hint">— 只勾选这个 Agent 能用的 Skill</span></label>
            <div class="ag-skill-grid">
              <label v-for="s in skills" :key="s.name" class="ag-skill-chk" :class="{ 'ag-skill-on': form.skills.includes(s.name) }">
                <input type="checkbox" :value="s.name" v-model="form.skills" />
                <span class="ag-skill-icon">{{ s.icon || '⊞' }}</span>
                <span class="ag-skill-title">{{ s.title }}</span>
              </label>
              <div v-if="!skills.length" class="ag-subtle">（暂无能力域）</div>
            </div>

            <label class="ag-label ag-label-mt">额外工具 <span class="ag-label-hint">— 每行一个工具名（如 model_list）</span></label>
            <textarea v-model="form.toolsText" rows="2" placeholder="model_list&#10;system_info" class="field ag-textarea ag-textarea-sm"></textarea>

            <label class="ag-label ag-label-mt">外部 MCP 工具 <span class="ag-label-hint">— 每行一个（mcp__ 前缀）</span></label>
            <textarea v-model="form.mcpToolsText" rows="2" placeholder="mcp__server__tool" class="field ag-textarea ag-textarea-sm"></textarea>
          </div>
        </div>

        <div class="ag-dialog-foot">
          <span v-if="formError" class="ag-form-error">{{ formError }}</span>
          <div class="ag-dialog-foot-actions">
            <button class="btn btn-sm" @click="closeDialog">取消</button>
            <button class="btn btn-sm btn-primary" @click="saveAgent" :disabled="saving || !form.name.trim()">
              {{ saving ? '保存中…' : (editing ? '保存' : '创建') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useToast } from '../../composables/useToast'
import { useDataChanged } from '../../composables/useDataChanged'
import { useActiveLibrary } from '../../composables/useActiveLibrary'
import { agentsApi } from '../../api/agents'
import { providersApi } from '../../api/providers'
import { skillsApi } from '../../api/skills'
import { memoryApi } from '../../api/memory'
import type { LocalAgent } from '../../api/agents'

const toast = useToast()
function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

interface SkillLike { name: string; title: string; icon?: string }
interface ProviderLike { id: string; name: string; model: string; enabled?: boolean }

const { activeLibraryId } = useActiveLibrary()
const allAgents = ref<LocalAgent[]>([])
const showAllDomains = ref(false)
const agents = computed(() => {
  const list = allAgents.value || []
  if (showAllDomains.value || !activeLibraryId.value) return list
  return list.filter(a => !a.libraryId || a.libraryId === activeLibraryId.value || a.isDefault)
})
const providers = ref<ProviderLike[]>([])
const skills = ref<SkillLike[]>([])

const iconPalette = ['◉', '◎', '◈', '◇', '□', '○', '●', '◆', '⊕', '⊗', '★', '☆', '♟', '⚙', '⊹', '⌘']

// ── Dialog state ──
const dialogOpen = ref(false)
const editing = ref<LocalAgent | null>(null)
const showIconPick = ref(false)
const saving = ref(false)
const formError = ref('')

const form = reactive({
  id: '',
  name: '',
  description: '',
  icon: '◉',
  systemPrompt: '',
  providerId: '',
  model: '',
  inheritSkills: false,
  skills: [] as string[],
  toolsText: '',
  mcpToolsText: '',
  temperature: '' as string | number,
  maxTokens: '' as string | number,
  libraryId: '',
})

const libraries = ref<any[]>([])
async function loadLibraries() { try { libraries.value = await memoryApi.libraryList() || [] } catch (_) { libraries.value = [] } }

const providerDefaultModelPlaceholder = computed(() => {
  if (!form.providerId) return '继承活动供应商模型'
  const p = providers.value.find(x => x.id === form.providerId)
  return p ? `默认 ${p.model}` : ''
})

// ── Lifecycle ──
onMounted(async () => {
  await Promise.all([loadAgents(), loadOptions(), loadLibraries()])
})
useDataChanged('agents:changed', () => { loadAgents() })

async function loadAgents() {
  try { allAgents.value = await agentsApi.list() || [] } catch (e: any) {
    t('error', '加载失败', e.message || String(e))
  }
}

async function loadOptions() {
  try {
    const [ps, sks] = await Promise.all([
      providersApi.list().catch(() => []),
      skillsApi.list().catch(() => []),
    ])
    providers.value = (ps || []).filter((p: ProviderLike) => p.enabled)
    skills.value = (sks || []) as SkillLike[]
  } catch (_) { /* ignore */ }
}

// ── Display helpers ──
function modelLabel(ag: LocalAgent): string {
  if (ag.providerId) {
    const p = providers.value.find(x => x.id === ag.providerId)
    return (p ? p.name : ag.providerId) + (ag.model ? ' / ' + ag.model : '')
  }
  return ag.model ? ('全局 / ' + ag.model) : '继承全局模型'
}

function capabilityLabel(ag: LocalAgent): string {
  if (ag.inheritSkills) return '继承全部能力'
  const n = (ag.skills || []).length + (ag.tools || []).length + (ag.mcpTools || []).length
  return n ? `${n} 项能力` : '无工具'
}

function libraryName(id?: string): string {
  if (!id) return ''
  return libraries.value.find(l => l.id === id)?.name || ''
}

// ── Dialog open/close ──
function resetForm() {
  form.id = ''
  form.name = ''
  form.description = ''
  form.icon = '◉'
  form.systemPrompt = ''
  form.providerId = ''
  form.model = ''
  form.inheritSkills = false
  form.skills = []
  form.toolsText = ''
  form.mcpToolsText = ''
  form.temperature = ''
  form.maxTokens = ''
  form.libraryId = ''
  formError.value = ''
}

function openCreate(libraryId = '') {
  editing.value = null
  resetForm()
  form.libraryId = libraryId || activeLibraryId.value
  showIconPick.value = false
  dialogOpen.value = true
}

function openEdit(ag: LocalAgent) {
  editing.value = ag
  resetForm()
  form.id = ag.id
  form.name = ag.name
  form.description = ag.description || ''
  form.icon = ag.icon || '◉'
  form.systemPrompt = ag.systemPrompt || ''
  form.providerId = ag.providerId || ''
  form.model = ag.model || ''
  form.inheritSkills = !!ag.inheritSkills
  form.skills = [...(ag.skills || [])]
  form.toolsText = (ag.tools || []).join('\n')
  form.mcpToolsText = (ag.mcpTools || []).join('\n')
  form.temperature = ag.temperature != null ? ag.temperature : ''
  form.maxTokens = ag.maxTokens ? ag.maxTokens : ''
  form.libraryId = ag.libraryId || ''
  showIconPick.value = false
  dialogOpen.value = true
}

function closeDialog() {
  dialogOpen.value = false
}

// ── Save ──
function buildPayload(): Partial<LocalAgent> {
  const linesToArray = (s: string) =>
    s.split('\n').map(x => x.trim()).filter(Boolean)

  const tempNum = form.temperature === '' || form.temperature === null ? null : Number(form.temperature)
  const maxNum = form.maxTokens === '' || form.maxTokens === null ? 0 : Number(form.maxTokens)

  return {
    name: form.name.trim(),
    description: form.description.trim(),
    icon: form.icon,
    systemPrompt: form.systemPrompt,
    providerId: form.providerId,
    model: form.model.trim(),
    inheritSkills: form.inheritSkills,
    skills: form.inheritSkills ? [] : form.skills,
    tools: form.inheritSkills ? [] : linesToArray(form.toolsText as string),
    mcpTools: form.inheritSkills ? [] : linesToArray(form.mcpToolsText as string),
    temperature: (tempNum === null || isNaN(tempNum as number)) ? null : tempNum as number,
    maxTokens: (isNaN(maxNum) ? 0 : maxNum) as number,
    libraryId: form.libraryId,
  }
}

async function saveAgent() {
  if (!form.name.trim()) { formError.value = '请填写名称'; return }
  if (!form.systemPrompt.trim() && !form.inheritSkills) {
    // allow empty system prompt but warn softly — not blocking
  }
  saving.value = true
  formError.value = ''
  try {
    const payload = buildPayload()
    if (editing.value) {
      await agentsApi.update(editing.value.id, { ...editing.value, ...payload } as LocalAgent)
      t('success', '已保存', form.name)
    } else {
      await agentsApi.create(payload)
      t('success', '已创建', form.name)
    }
    dialogOpen.value = false
    await loadAgents()
  } catch (e: any) {
    formError.value = '保存失败: ' + (e.message || String(e))
  } finally {
    saving.value = false
  }
}

async function deleteAgent(ag: LocalAgent) {
  if (ag.isDefault) return
  if (!await toast.confirm('删除 Agent', '确定删除「' + ag.name + '」？')) return
  try {
    await agentsApi.remove(ag.id)
    t('success', '已删除', ag.name)
    await loadAgents()
  } catch (e: any) { t('error', '删除失败', e.message || String(e)) }
}

async function duplicateAgent(ag: LocalAgent) {
  try {
    const { id, isDefault, createdAt, updatedAt, ...rest } = ag
    await agentsApi.create({ ...rest, name: ag.name + ' (副本)' })
    t('success', '已复制', ag.name + ' (副本)')
    await loadAgents()
  } catch (e: any) { t('error', '复制失败', e.message || String(e)) }
}

defineExpose({ openCreate, openEdit, dialogOpen })
</script>

<style scoped>
.llm-agents { display: flex; flex-direction: column; gap: 12px; }

/* Toolbar */
.agents-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; gap: 12px;
}
.agents-toolbar-info { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.agents-count { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.agents-hint { font-size: 11px; color: var(--text-tertiary); }
.agents-hint code { font-family: var(--font-mono); background: var(--bg-inset); padding: 1px 5px; border-radius: 3px; color: var(--accent); }
.agents-toolbar-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* List */
.agents-list { display: flex; flex-direction: column; gap: 8px; }
.agent-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; gap: 12px; border: 1px solid var(--border-subtle);
}
.agc-main { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }
.agc-icon {
  width: 38px; height: 38px; border-radius: 10px; flex-shrink: 0;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; color: var(--accent);
}
.agc-info { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.agc-title-row { display: flex; align-items: center; gap: 8px; }
.agc-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.agc-badge { font-size: 10px; padding: 1px 7px; border-radius: 8px; font-weight: 600; }
.agc-badge-default { background: var(--accent-dim); color: var(--accent); }
.agc-desc { font-size: 11px; color: var(--text-secondary); line-height: 1.4; }
.agc-meta { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 1px; }
.agc-tag {
  font-size: 10px; padding: 1px 6px; border-radius: 3px;
  background: var(--bg-inset); color: var(--text-tertiary); font-family: var(--font-mono);
}
.agc-actions { display: flex; gap: 4px; flex-shrink: 0; }
.agc-btn {
  width: 28px; height: 28px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer;
  display: flex; align-items: center; justify-content: center; font-size: 12px; transition: all 0.15s;
}
.agc-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
.agc-btn:disabled { opacity: 0.3; cursor: default; }
.agc-btn-danger:hover:not(:disabled) { color: var(--danger); border-color: rgba(255,69,58,0.4); background: rgba(255,69,58,0.08); }

.agents-empty {
  text-align: center; padding: 36px 0; color: var(--text-tertiary); font-size: 13px;
  display: flex; flex-direction: column; gap: 4px;
}
.agents-empty .hint { font-size: 11px; opacity: 0.7; }

/* Dialog */
.overlay {
  position: fixed; inset: 0; z-index: 100;
  background: rgba(0,0,0,0.5); backdrop-filter: blur(2px);
  display: flex; align-items: center; justify-content: center; padding: 24px;
}
.agent-dialog {
  width: 620px; max-width: 100%; max-height: 88vh; display: flex; flex-direction: column;
  background: var(--bg-glass);
}
.ag-dialog-head {
  display: flex; align-items: center; gap: 12px;
  padding: 16px 18px; border-bottom: 1px solid var(--border-subtle);
}
.ag-icon-pick { position: relative; cursor: pointer; }
.ag-icon-preview {
  width: 42px; height: 42px; border-radius: 10px; display: flex; align-items: center; justify-content: center;
  background: var(--bg-elevated); border: 1px solid var(--border-soft); font-size: 20px; color: var(--accent);
}
.ag-icon-arrow { position: absolute; right: -4px; bottom: -2px; font-size: 10px; color: var(--text-tertiary); }
.ag-icon-dropdown {
  position: absolute; top: 100%; left: 0; z-index: 10; margin-top: 6px;
  padding: 8px; background: var(--bg-elevated); border: 1px solid var(--border-soft);
  border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,0.4);
}
.ag-icon-palette { display: grid; grid-template-columns: repeat(6, 1fr); gap: 4px; }
.ag-icon-btn {
  width: 30px; height: 30px; border-radius: 6px; border: 1px solid var(--border-soft);
  background: var(--bg-elevated); font-size: 15px; cursor: pointer; color: var(--text-secondary);
}
.ag-icon-btn:hover { border-color: var(--accent); color: var(--text-primary); }
.ag-icon-sel { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); }
.ag-head-info { flex: 1; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.ag-head-info h3 { margin: 0; font-size: 15px; font-weight: 600; }
.ag-title-input {
  width: 100%; padding: 7px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary); font-size: 13px; outline: none;
}
.ag-title-input:focus { border-color: var(--accent); }
.ag-dialog-close {
  width: 28px; height: 28px; border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-tertiary); font-size: 13px; cursor: pointer; flex-shrink: 0;
}
.ag-dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }

.ag-body { padding: 16px 18px; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; }
.ag-section { display: flex; flex-direction: column; gap: 6px; }
.ag-section-grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.ag-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.ag-label-hint { font-weight: 400; color: var(--text-tertiary); }
.ag-label-mt { margin-top: 10px; }
.ag-field { display: flex; flex-direction: column; gap: 6px; }
.ag-input { width: 100%; padding: 7px 10px; }
.ag-textarea { width: 100%; padding: 8px 10px; resize: vertical; font-family: var(--font); line-height: 1.5; }
.ag-textarea-sm { font-family: var(--font-mono); font-size: 11px; }

.ag-cap-toggle {
  display: flex; align-items: center; gap: 8px; cursor: pointer;
  font-size: 12px; color: var(--text-secondary); user-select: none;
}
.ag-cap-toggle input { accent-color: var(--accent); }

.ag-skill-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 6px; }
.ag-skill-chk {
  display: flex; align-items: center; gap: 6px; padding: 6px 8px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  cursor: pointer; font-size: 12px; color: var(--text-secondary);
}
.ag-skill-chk:hover { background: var(--bg-hover); }
.ag-skill-chk input { accent-color: var(--accent); }
.ag-skill-chk.ag-skill-on { border-color: var(--accent); background: var(--accent-dim); color: var(--text-primary); }
.ag-skill-icon { font-size: 13px; }
.ag-skill-title { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ag-subtle { font-size: 11px; color: var(--text-tertiary); }

.ag-dialog-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  padding: 12px 18px; border-top: 1px solid var(--border-subtle);
}
.ag-form-error { font-size: 11px; color: var(--danger); }
.ag-dialog-foot-actions { display: flex; gap: 8px; }

/* Shared field/button styling (matches LLMAgent.vue) */
.field { border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; outline: none; }
.field:focus { border-color: var(--accent); }
.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
</style>
