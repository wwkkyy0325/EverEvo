<template>
  <div v-if="modelValue" class="overlay" @click.self="close">
    <div class="glass-panel dialog skill-dialog">
      <!-- Header -->
      <div class="sk-dialog-head">
        <div class="sk-icon-pick" @click="showSkillIconPick = !showSkillIconPick">
          <span class="sk-icon-preview">{{ skillForm.icon || '⊞' }}</span>
          <span class="sk-icon-arrow">▾</span>
          <div v-if="showSkillIconPick" class="sk-icon-dropdown" @click.stop>
            <div class="sk-icon-palette">
              <button v-for="ic in iconPalette" :key="ic"
                :class="['sk-icon-btn', { 'sk-icon-sel': skillForm.icon === ic }]"
                @click="skillForm.icon = ic; showSkillIconPick = false">{{ ic }}</button>
            </div>
          </div>
        </div>
        <div class="sk-head-info">
          <h3>{{ editingSkill ? '编辑 Skill' : '新建 Skill' }}</h3>
          <input v-model="skillForm.title" type="text" placeholder="输入 Skill 标题…" class="sk-title-input" />
        </div>
        <button class="sk-dialog-close" @click="close">✕</button>
      </div>

      <div class="sk-body">
        <!-- Section: Basic Info -->
        <div class="sk-section">
          <div class="sk-section-title">基本信息</div>
          <div class="sk-row-2">
            <div class="sk-field">
              <label>标识名 <span class="sk-field-hint">— 唯一标识，创建后不可修改</span></label>
              <input v-model="skillForm.name" type="text" placeholder="my-skill" class="sk-input" :disabled="!!editingSkill" />
            </div>
            <div class="sk-field">
              <label>分类</label>
              <input v-model="skillForm.category" type="text" placeholder="model / plugin / custom" class="sk-input" />
            </div>
          </div>
          <div class="sk-field">
            <label>描述</label>
            <input v-model="skillForm.description" type="text" placeholder="简要描述此 Skill 的用途和能力…" class="sk-input" />
          </div>
        </div>

        <!-- Section: AI Behaviour -->
        <div class="sk-section">
          <div class="sk-section-title">AI 行为定义</div>
          <div class="sk-field">
            <label>System Prompt <span class="sk-field-hint">— 定义 AI 的角色、行为和约束</span></label>
            <textarea v-model="skillForm.systemPrompt" rows="5" placeholder="例如：你是一个资深代码审查员。审查代码时关注安全漏洞、性能和代码风格…" class="sk-textarea"></textarea>
          </div>
        </div>

        <!-- Section: Tools & Resources -->
        <div class="sk-section">
          <div class="sk-section-title">工具与资源</div>
          <div class="sk-row-2">
            <div class="sk-field">
              <label>内部工具 <span class="sk-field-hint">— 每行一个工具名</span></label>
              <textarea v-model="skillForm.toolsText" rows="3" placeholder="model_list&#10;kb_search&#10;plugin_list" class="sk-textarea sk-textarea-sm"></textarea>
            </div>
            <div class="sk-field">
              <label>资源 URI <span class="sk-field-hint">— 每行一个</span></label>
              <textarea v-model="skillForm.resourcesText" rows="3" placeholder="everevo://models/list&#10;everevo://kb/list" class="sk-textarea sk-textarea-sm"></textarea>
            </div>
          </div>
        </div>

        <!-- Section: External MCP Tools -->
        <div v-if="mcpServersConnected.length" class="sk-section">
          <div class="sk-section-title">外部 MCP 工具</div>
          <div class="sk-mcp-list">
            <div v-for="srv in mcpServersConnected" :key="srv.id" class="sk-mcp-group">
              <div class="sk-mcp-group-name">{{ srv.name }}</div>
              <div class="sk-mcp-tools">
                <label v-for="toolName in getMCPToolNames(srv)" :key="toolName" class="sk-mcp-check">
                  <input type="checkbox" :value="toolName" v-model="skillForm.mcpTools" />
                  <span>{{ toolName.replace('mcp__' + srv.id + '__', '') }}</span>
                </label>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="sk-foot">
        <div v-if="skillMsg" class="msg" :class="skillOk ? 'msg-ok' : 'msg-err'">{{ skillMsg }}</div>
        <div class="sk-foot-actions">
          <button class="btn" @click="close">取消</button>
          <button class="btn btn-primary" @click="doSaveSkill" :disabled="!skillForm.name || !skillForm.title">
            {{ editingSkill ? '保存修改' : '创建 Skill' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { useToast } from '../../composables/useToast'
import { skillsApi } from '../../api/skills'
import { mcpApi } from '../../api/mcp'

const props = defineProps<{
  modelValue: boolean
  editingSkill: any
  mcpServersConnected: any[]
  iconPalette: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved'): void
}>()

const toast = useToast()

function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

const skillForm = reactive<Record<string, any>>({
  name: '', title: '', description: '', category: '', icon: '',
  toolsText: '', resourcesText: '', systemPrompt: '', mcpTools: [] as string[],
})

const showSkillIconPick = ref(false)
const skillMsg = ref('')
const skillOk = ref(false)
const saving = ref(false)
const mcpToolCache: Record<string, string[]> = {}

function close() {
  emit('update:modelValue', false)
}

function getMCPToolNames(srv: any): string[] {
  return mcpToolCache[srv.id] || []
}

async function doSaveSkill() {
  if (saving.value) return
  const build = () => ({
    name: skillForm.name.trim(),
    title: skillForm.title.trim(),
    description: skillForm.description.trim(),
    category: skillForm.category.trim(),
    icon: skillForm.icon.trim(),
    tools: skillForm.toolsText.split('\n').map((s: string) => s.trim()).filter(Boolean),
    resources: skillForm.resourcesText.split('\n').map((s: string) => s.trim()).filter(Boolean),
    prompts: [],
    systemPrompt: skillForm.systemPrompt.trim(),
    mcpTools: skillForm.mcpTools || [],
    enabled: props.editingSkill ? props.editingSkill.enabled : true,
  })
  saving.value = true
  try {
    if (props.editingSkill) {
      await skillsApi.update(props.editingSkill.name, build())
      t('success', '已更新', build().title)
    } else {
      await skillsApi.create(build())
      t('success', '已创建', build().title)
    }
    close()
    emit('saved')
  } catch (e: any) {
    skillMsg.value = (props.editingSkill ? '更新' : '创建') + '失败: ' + (e.message || e)
    skillOk.value = false
  } finally {
    saving.value = false
  }
}

watch(() => props.modelValue, async (val) => {
  if (!val) return
  skillMsg.value = ''
  showSkillIconPick.value = false

  // Load MCP tool cache from connected servers
  const connected = props.mcpServersConnected || []
  for (const key of Object.keys(mcpToolCache)) delete mcpToolCache[key]
  for (const srv of connected) {
    try {
      const tools = await mcpApi.getServerTools(srv.id)
      mcpToolCache[srv.id] = (tools || []).map((t: any) => t.name)
    } catch (_) { mcpToolCache[srv.id] = [] }
  }

  // Populate form from editingSkill (or reset for new)
  const skill = props.editingSkill
  if (skill) {
    skillForm.name = skill.name
    skillForm.title = skill.title
    skillForm.description = skill.description
    skillForm.category = skill.category
    skillForm.icon = skill.icon || ''
    skillForm.toolsText = (skill.tools || []).join('\n')
    skillForm.resourcesText = (skill.resources || []).join('\n')
    skillForm.systemPrompt = skill.systemPrompt || ''
    skillForm.mcpTools = skill.mcpTools ? [...skill.mcpTools] : []
  } else {
    skillForm.name = ''
    skillForm.title = ''
    skillForm.description = ''
    skillForm.category = ''
    skillForm.icon = ''
    skillForm.toolsText = ''
    skillForm.resourcesText = ''
    skillForm.systemPrompt = ''
    skillForm.mcpTools = []
  }
})
</script>

<style scoped>
/* ── Overlay ── */
.overlay {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.5); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
  z-index: 100;
}

/* ── Message ── */
.msg {
  padding: 8px 12px; border-radius: var(--radius-sm);
  font-size: 12px; margin-top: 10px;
}
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }

/* ── Skill dialog ── */
.skill-dialog {
  width: 720px; max-width: 92vw; max-height: 88vh; overflow-y: auto;
  padding: 0; display: flex; flex-direction: column;
}

/* ── Dialog header ── */
.sk-dialog-head {
  display: flex; align-items: center; gap: 18px;
  padding: 28px 32px 22px;
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
}
.sk-dialog-close {
  width: 28px; height: 28px; border: none; border-radius: 6px;
  background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; align-self: flex-start;
}
.sk-dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }

.sk-icon-pick { position: relative; flex-shrink: 0; cursor: pointer; }
.sk-icon-preview {
  display: flex; align-items: center; justify-content: center;
  width: 56px; height: 56px; border-radius: 14px;
  background: var(--bg-elevated); border: 1.5px solid var(--border-soft);
  font-size: 26px; transition: all 0.15s;
}
.sk-icon-pick:hover .sk-icon-preview {
  border-color: var(--accent); background: var(--accent-dim);
}
.sk-icon-arrow {
  font-size: 9px; color: var(--text-tertiary);
  position: absolute; bottom: -3px; right: -3px;
  background: var(--bg-elevated); border-radius: 50%;
  width: 18px; height: 18px; display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border-soft);
}
.sk-icon-dropdown {
  position: absolute; left: 0; top: 100%; margin-top: 8px; padding: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  border-radius: 12px; box-shadow: 0 12px 32px rgba(0,0,0,0.5); z-index: 60;
}
.sk-icon-palette { display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px; }
.sk-icon-btn {
  width: 34px; height: 34px; border-radius: 8px;
  border: 1px solid var(--border-soft); background: var(--bg-elevated);
  font-size: 16px; cursor: pointer; display: flex; align-items: center; justify-content: center;
  color: var(--text-secondary); transition: all 0.12s;
}
.sk-icon-btn:hover { border-color: var(--accent); color: var(--text-primary); transform: scale(1.08); }
.sk-icon-btn.sk-icon-sel { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); }

.sk-head-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 10px; }
.sk-head-info h3 { font-size: 18px; font-weight: 600; margin: 0; color: var(--text-primary); }
.sk-title-input {
  width: 100%; padding: 9px 14px; box-sizing: border-box;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 15px; font-weight: 500; outline: none; font-family: var(--font);
  transition: border-color 0.15s;
}
.sk-title-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.sk-title-input::placeholder { color: var(--text-tertiary); }

/* ── Dialog body ── */
.sk-body {
  padding: 24px 32px;
  display: flex; flex-direction: column; gap: 24px;
  flex: 1; overflow-y: auto;
}

/* ── Sections ── */
.sk-section {
  display: flex; flex-direction: column; gap: 14px;
}
.sk-section-title {
  font-size: 11px; font-weight: 600; color: var(--text-tertiary);
  text-transform: uppercase; letter-spacing: 0.06em;
  padding-bottom: 8px; border-bottom: 1px solid var(--border-subtle);
}

/* ── Fields ── */
.sk-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.sk-field label {
  font-size: 12px; font-weight: 500; color: var(--text-secondary);
}
.sk-field-hint { font-weight: 400; color: var(--text-tertiary); }

.sk-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }

/* ── Inputs ── */
.sk-input {
  width: 100%; padding: 9px 12px; box-sizing: border-box;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 13px; font-family: var(--font-mono); outline: none;
  transition: border-color 0.15s;
}
.sk-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.sk-input:disabled { opacity: 0.5; cursor: not-allowed; }
.sk-input::placeholder { color: var(--text-tertiary); }

.sk-textarea {
  width: 100%; padding: 10px 12px; box-sizing: border-box;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 12px; font-family: var(--font-mono); outline: none;
  resize: vertical; line-height: 1.6;
  transition: border-color 0.15s;
}
.sk-textarea:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.sk-textarea::placeholder { color: var(--text-tertiary); }
.sk-textarea-sm { min-height: 72px; }

/* ── MCP tools ── */
.sk-mcp-list {
  display: flex; flex-direction: column; gap: 12px;
  max-height: 200px; overflow-y: auto; padding: 4px 0;
}
.sk-mcp-group { display: flex; flex-direction: column; gap: 4px; }
.sk-mcp-group-name {
  font-size: 11px; font-weight: 600; color: var(--accent);
  margin-bottom: 4px; padding-bottom: 4px;
  border-bottom: 1px solid var(--border-subtle);
}
.sk-mcp-tools {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 2px;
}
.sk-mcp-check {
  display: flex; align-items: center; gap: 7px;
  font-size: 12px; color: var(--text-secondary); cursor: pointer;
  padding: 4px 6px; border-radius: 5px; transition: background 0.1s;
}
.sk-mcp-check:hover { background: var(--bg-hover); }
.sk-mcp-check input { accent-color: var(--accent); cursor: pointer; width: 14px; height: 14px; }

/* ── Dialog footer ── */
.sk-foot {
  padding: 16px 32px 22px;
  border-top: 1px solid var(--border-subtle);
  flex-shrink: 0;
}
.sk-foot .msg { margin-bottom: 12px; }
.sk-foot-actions { display: flex; justify-content: flex-end; gap: 10px; }
</style>
