<template>
  <div class="llm-skills">
    <!-- Toolbar -->
    <div class="skill-toolbar">
      <span class="skill-count">{{ skills.length }} 个能力{{ showAllDomains ? ' (全部领域)' : '' }} · {{ enabledCount }} 已启用 · {{ packageGroups.length }} 个包</span>
      <div class="skill-toolbar-actions">
        <label style="font-size:11px;color:var(--text-tertiary);cursor:pointer;display:flex;align-items:center;gap:4px;margin-right:4px;">
          <input type="checkbox" v-model="showAllDomains" style="cursor:pointer;" /> 全部领域
        </label>
        <button class="btn btn-sm" @click="doRefreshSkills">刷新</button>
        <button class="btn btn-sm btn-primary" @click="openSkillDialog()">新建 Skill</button>
        <button class="btn btn-sm btn-primary" @click="openMarket">浏览市场</button>
        <button class="btn btn-sm" @click="doExportSkills">导出</button>
        <label class="btn btn-sm" style="cursor:pointer">
          导入
          <input type="file" accept=".json" class="skill-file-input" @change="doImportSkills" />
        </label>
        <button class="btn btn-sm" @click="showNewPkg = true" v-if="!showNewPkg">+ 新建包</button>
      </div>
    </div>

    <!-- Package groups -->
    <div class="skill-list">
      <div v-for="grp in packageGroups" :key="grp.key"
        class="pkg-card glass-panel"
        :class="{ 'pkg-expanded': expandedPkg[grp.key], 'pkg-dragover': dragOverPkg === grp.key }"
        @dragover.prevent="onDragOver(grp.key)"
        @dragenter.prevent="onDragEnterPkg($event, grp.key)"
        @dragleave="onDragLeavePkg($event)"
        @drop="onDrop(grp.key)">
        <!-- Package header -->
        <div class="pkg-head" @click="togglePackage(grp.key)">
          <span class="pkg-expand-icon">{{ expandedPkg[grp.key] ? '▾' : '▸' }}</span>
          <span class="pkg-icon">{{ grp.key === 'everevo' ? '◎' : (grp.key ? '📦' : '⊡') }}</span>
          <div class="pkg-info">
            <!-- Inline rename or static name -->
            <template v-if="renamePkg === grp.key">
              <input class="pkg-rename-input" v-model="renamePkgVal"
                @keydown.enter="doRenamePkg" @keydown.escape="cancelRenamePkg"
                @click.stop @blur="cancelRenamePkg" autofocus />
            </template>
            <template v-else>
              <span class="pkg-name">{{ grp.label }}</span>
            </template>
            <span class="pkg-meta">{{ grp.skills.length }} 个能力 · {{ grp.enabledCount }} 已启用</span>
          </div>
          <div class="pkg-head-actions" @click.stop>
            <button v-if="grp.key && grp.key !== 'everevo'" class="pkg-edit-btn"
              @click="startRenamePkg(grp.key, grp.label)" title="重命名">✎</button>
            <button v-if="grp.key && grp.key !== 'everevo'" class="pkg-del-btn"
              @click="doDeletePackage(grp.key)" title="删除包">✕</button>
          </div>
        </div>
        <!-- Skill rows -->
        <div v-if="expandedPkg[grp.key]" class="pkg-body">
          <div v-for="s in grp.skills" :key="s.name"
            class="skill-row" :class="{ 'skill-row-on': s.enabled }"
            draggable="true"
            @dragstart="onDragStart($event, s)"
            @dragend="onDragEnd">
            <span class="skill-row-grip" title="拖拽移动">⠿</span>
            <span class="skill-row-icon">{{ s.icon || '⊞' }}</span>
            <div class="skill-row-info">
              <span class="skill-row-name">{{ s.title }}</span>
              <span class="skill-row-desc">{{ s.description }}</span>
            </div>
            <div class="skill-row-meta">
              <span class="skill-row-tag">{{ (s.tools || []).length }} 工具</span>
              <span class="skill-row-tag" v-if="s.mcpTools && s.mcpTools.length">{{ s.mcpTools.length }} MCP</span>
            </div>
            <div class="skill-row-actions">
              <button class="pbar-btn" @click.stop="openSkillDialog(s)" title="编辑">✎</button>
              <button class="pbar-btn pbar-btn-danger" @click.stop="doDeleteSkill(s)" title="删除">✕</button>
              <label class="skill-toggle-sm" @click.stop>
                <input type="checkbox" :checked="s.enabled" @change="toggleSkill(s)" />
              </label>
            </div>
          </div>
        </div>
      </div>

      <!-- Inline new-package row (Windows new-folder style) -->
      <div v-if="showNewPkg" class="pkg-card glass-panel new-pkg-card">
        <div class="pkg-head new-pkg-head">
          <span class="pkg-icon">📦</span>
          <input v-model="newPkgName" type="text" placeholder="输入包名称，回车创建…"
            class="new-pkg-inline-input" ref="newPkgInputRef"
            @keydown.enter="doCreatePackage"
            @keydown.escape="showNewPkg = false; newPkgName = ''"
            @blur="cancelNewPkg" />
        </div>
      </div>
    </div>

    <!-- Skill dialog (unchanged) -->
    <div v-if="skillDialog" class="overlay" @click.self="skillDialog = false">
      <div class="glass-panel dialog skill-dialog">
        <div class="sk-dialog-head">
          <div class="sk-icon-pick" @click="showSkillIconPick = !showSkillIconPick">
            <span class="sk-icon-preview">{{ skillForm.icon || '⊞' }}</span>
            <span class="sk-icon-arrow">▾</span>
            <div v-if="showSkillIconPick" class="sk-icon-dropdown" @click.stop>
              <div class="sk-icon-palette">
                <button v-for="ic in (props.iconPalette || defaultIconPalette)" :key="ic"
                  :class="['sk-icon-btn', { 'sk-icon-sel': skillForm.icon === ic }]"
                  @click="skillForm.icon = ic; showSkillIconPick = false">{{ ic }}</button>
              </div>
            </div>
          </div>
          <div class="sk-head-info">
            <h3>{{ editingSkill ? '编辑 Skill' : '新建 Skill' }}</h3>
            <input v-model="skillForm.title" type="text" placeholder="输入 Skill 标题…" class="sk-title-input" />
          </div>
          <button class="sk-dialog-close" @click="skillDialog = false">✕</button>
        </div>
        <div class="sk-body">
          <div class="sk-section">
            <div class="sk-section-title">基本信息</div>
            <div class="sk-row-2">
              <div class="sk-field">
                <label>标识名 <span class="sk-field-hint">— 唯一标识</span></label>
                <input v-model="skillForm.name" type="text" placeholder="my-skill" class="sk-input" :disabled="!!editingSkill" />
              </div>
              <div class="sk-field">
                <label>所属包 <span class="sk-field-hint">— 拖拽也可移动</span></label>
                <select v-model="skillForm.package" class="sk-input">
                  <option v-for="grp in packageGroups" :key="grp.key" :value="grp.key">{{ grp.label }}</option>
                </select>
              </div>
            </div>
            <div class="sk-row-2">
              <div class="sk-field">
                <label>分类</label>
                <input v-model="skillForm.category" type="text" placeholder="model / plugin / custom" class="sk-input" />
              </div>
              <div class="sk-field">
                <label>描述</label>
                <input v-model="skillForm.description" type="text" placeholder="简要描述…" class="sk-input" />
              </div>
            </div>
          </div>
          <div class="sk-section">
            <div class="sk-section-title">AI 行为定义</div>
            <div class="sk-field">
              <label>System Prompt</label>
              <textarea v-model="skillForm.systemPrompt" rows="4" placeholder="定义 AI 的角色、行为和约束…" class="sk-textarea"></textarea>
            </div>
          </div>
          <div class="sk-section">
            <div class="sk-section-title">工具与资源</div>
            <div class="sk-row-2">
              <div class="sk-field">
                <label>内部工具 <span class="sk-field-hint">— 每行一个</span></label>
                <textarea v-model="skillForm.toolsText" rows="3" placeholder="model_list&#10;kb_search" class="sk-textarea sk-textarea-sm"></textarea>
              </div>
              <div class="sk-field">
                <label>资源 URI</label>
                <textarea v-model="skillForm.resourcesText" rows="3" placeholder="everevo://models/list" class="sk-textarea sk-textarea-sm"></textarea>
              </div>
            </div>
          </div>
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
            <button class="btn" @click="skillDialog = false">取消</button>
            <button class="btn btn-primary" @click="doSaveSkill" :disabled="!skillForm.name || !skillForm.title">
              {{ editingSkill ? '保存修改' : '创建 Skill' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Market dialog -->
    <div v-if="showMarket" class="overlay" @click.self="showMarket = false">
      <div class="glass-panel dialog market-dialog">
        <div class="market-dialog-head">
          <h3>Skill 市场</h3>
          <div class="market-dialog-head-actions">
            <button class="btn btn-xs" @click="refreshMarket" :disabled="refreshingMarket">{{ refreshingMarket ? '刷新中…' : '⟳ 刷新' }}</button>
            <button class="prov-dialog-close" @click="showMarket = false">✕</button>
          </div>
        </div>
        <div class="market-list">
          <div v-if="!marketSkills.length" class="market-empty">
            <span>暂无可用的 Skill</span>
          </div>
          <div v-for="pkg in marketSkills" :key="pkg.name" class="market-item">
            <div class="market-item-left">
              <span class="market-icon">{{ pkg.icon || '⊞' }}</span>
              <div class="market-info">
                <div class="market-name">{{ pkg.title }}</div>
                <div class="market-desc">{{ pkg.description }}</div>
              </div>
            </div>
            <div class="market-item-right">
              <button v-if="!pkg.installed" class="btn btn-sm btn-primary" @click="installMarketSkill(pkg)" :disabled="installing === pkg.name">
                {{ installing === pkg.name ? '安装中…' : '安装' }}
              </button>
              <button v-else class="btn btn-sm btn-del" @click="uninstallMarketSkill(pkg)">卸载</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch, nextTick } from 'vue'
import { useToast } from '../../composables/useToast'
import { useActiveLibrary } from '../../composables/useActiveLibrary'
import { skillsApi } from '../../api/skills'
import { mcpApi } from '../../api/mcp'
import { memoryApi } from '../../api/memory'
import type { Skill, SkillPackage } from '../../api/skills'

const toast = useToast()
function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

const props = withDefaults(defineProps<{ iconPalette?: string[] }>(), {
  iconPalette: () => ['◎','◈','◇','□','◉','○','●','◆','◊','△','▲','▽','☆','★','⊕','⊗','⬡','⬢','☰','☷'],
})
const defaultIconPalette = ['◎','◈','◇','□','◉','○','●','◆','◊','△','▲','▽','☆','★','⊕','⊗','⬡','⬢','☰','☷']
const emit = defineEmits<{ (e: 'skills-changed'): void; (e: 'mcp-servers-changed'): void }>()

// ── Domain ──
const { activeLibraryId } = useActiveLibrary()
const showAllDomains = ref(false)
const domainLibs = ref<{ id: string; name: string }[]>([])

// ── State ──
const allSkills = ref<Skill[]>([])
// Filtered skills: either all, or only those matching active library (plus global skills with empty libraryId).
const skills = computed(() => {
  const list = allSkills.value || []
  if (showAllDomains.value || !activeLibraryId.value) return list
  return list.filter(s => !s.libraryId || s.libraryId === activeLibraryId.value)
})
const expandedPkg = reactive<Record<string, boolean>>({})

// New package
const showNewPkg = ref(false)
const newPkgName = ref('')
const newPkgInputRef = ref<HTMLInputElement | null>(null)
const STORAGE_KEY = 'everevo_known_packages'
const knownPackages = ref<string[]>(loadKnownPackages())

function loadKnownPackages(): string[] {
  try { return JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]') } catch { return [] }
}
function saveKnownPackages() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(knownPackages.value))
}

// Auto-focus new-package input
watch(showNewPkg, async (v) => {
  if (v) { await nextTick(); newPkgInputRef.value?.focus() }
})

// Drag state
const dragSkill = ref<Skill | null>(null)
const dragOverPkg = ref('')

// Skill dialog
const skillDialog = ref(false)
const editingSkill = ref<any>(null)
const skillForm = reactive<Record<string, any>>({
  name:'', title:'', description:'', category:'', package:'', icon:'',
  toolsText:'', resourcesText:'', systemPrompt:'', mcpTools:[] as string[],
  libraryId: '',
})
const showSkillIconPick = ref(false)
const skillMsg = ref(''); const skillOk = ref(false)
const mcpServers = ref<any[]>([])
const showMarket = ref(false)
const marketSkills = ref<any[]>([])
const installing = ref<string | null>(null)
const refreshingMarket = ref(false)
const saving = ref(false)
let _mcpToolCache: Record<string, string[]> = {}

// ── Package grouping ──
const packageGroups = computed<SkillPackage[]>(() => {
  const map = new Map<string, Skill[]>()
  // Start with known empty packages
  for (const kp of knownPackages.value) {
    if (!map.has(kp)) map.set(kp, [])
  }
  for (const s of skills.value) {
    const pkg = s.package || ''
    if (!map.has(pkg)) map.set(pkg, [])
    map.get(pkg)!.push(s)
  }
  const groups: SkillPackage[] = []
  for (const [key, list] of map) {
    const label = key === '' ? '未分类' : (key === 'everevo' ? 'EverEvo 核心' : key)
    groups.push({ key, label, skills: list, enabledCount: list.filter(s => s.enabled).length })
  }
  groups.sort((a, b) => {
    if (a.key === 'everevo') return -1
    if (b.key === 'everevo') return 1
    if (a.key === '') return 1
    if (b.key === '') return -1
    return a.label.localeCompare(b.label)
  })
  return groups
})

const enabledCount = computed(() => skills.value.filter(s => s.enabled).length)
const mcpServersConnected = computed(() => (mcpServers.value || []).filter((s: any) => s.status === 'connected'))

// ── Lifecycle ──
onMounted(() => { loadSkills(); loadMCPServers() })

// ── Skills CRUD ──
async function loadSkills() {
  try { allSkills.value = await skillsApi.list() || [] } catch (e: any) { t('error','加载失败',e.message||e) }
  try { domainLibs.value = (await memoryApi.libraryList()) || [] } catch (_) {}
}
function doRefreshSkills() { loadSkills(); emit('skills-changed') }
async function toggleSkill(s: Skill) {
  try {
    await skillsApi.setEnabled(s.name, !s.enabled)
    s.enabled = !s.enabled
    emit('skills-changed')
  } catch (e: any) { t('error','操作失败',e.message||e) }
}
function togglePackage(key: string) { expandedPkg[key] = !expandedPkg[key] }

async function doExportSkills() {
  try {
    const data = await skillsApi.exportAll()
    // Wails deserialises json.RawMessage to a JS object, not a string
    const obj = typeof data === 'string' ? JSON.parse(data) : data
    const json = JSON.stringify(obj, null, 2)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a'); a.href = url; a.download = 'everevo-skills.json'; a.click()
    URL.revokeObjectURL(url)
    t('success', '已导出', 'everevo-skills.json')
  } catch (e: any) { t('error', '导出失败', e.message || e) }
}
async function doImportSkills(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  try {
    const text = await file.text()
    await skillsApi.importSkills(JSON.parse(text))
    await loadSkills(); emit('skills-changed')
    t('success', '已导入', file.name)
  } catch (e: any) { t('error', '导入失败', e.message || e) }
  ;(e.target as HTMLInputElement).value = ''
}

// ── Package management ──
const renamePkg = ref('')           // which package key is being renamed
const renamePkgVal = ref('')        // inline rename input value

function startRenamePkg(key: string, label: string) {
  renamePkg.value = key
  renamePkgVal.value = label
}
function doRenamePkg() {
  const oldKey = renamePkg.value
  const newKey = renamePkgVal.value.trim()
  if (!newKey || newKey === oldKey) { renamePkg.value = ''; return }
  // Update all skills in this package
  for (const s of skills.value) {
    if (s.package === oldKey) {
      skillsApi.moveSkill(s.name, newKey).catch(() => {})
      s.package = newKey
    }
  }
  if (expandedPkg[oldKey]) { expandedPkg[newKey] = true; delete expandedPkg[oldKey] }
  // Update knownPackages if old key was a known empty package
  const kpIdx = knownPackages.value.indexOf(oldKey)
  if (kpIdx >= 0) { knownPackages.value[kpIdx] = newKey; saveKnownPackages() }
  renamePkg.value = ''
  emit('skills-changed')
  t('info', '包已重命名', oldKey + ' → ' + newKey)
}
function cancelRenamePkg() { renamePkg.value = '' }

function cancelNewPkg() {
  // Delay to allow Enter key to fire first
  setTimeout(() => {
    if (showNewPkg.value) { showNewPkg.value = false; newPkgName.value = '' }
  }, 100)
}
function doCreatePackage() {
  const name = newPkgName.value.trim()
  if (!name) { showNewPkg.value = false; return }
  if (knownPackages.value.includes(name) || packageGroups.value.some(g => g.key === name)) {
    t('warning', '包名已存在', name)
    return
  }
  showNewPkg.value = false; newPkgName.value = ''
  knownPackages.value.push(name)
  saveKnownPackages()
  expandedPkg[name] = true
}
async function doDeletePackage(key: string) {
  if (!await toast.confirm('删除包', '确定删除包「' + key + '」？包内 Skill 将移至未分类。')) return
  for (const s of skills.value) {
    if (s.package === key) {
      try { await skillsApi.moveSkill(s.name, '') } catch (_) {}
      s.package = ''
    }
  }
  knownPackages.value = knownPackages.value.filter(k => k !== key)
  saveKnownPackages()
  await loadSkills(); emit('skills-changed')
}

// ── Drag & drop ──
function onDragStart(e: DragEvent, s: Skill) {
  dragSkill.value = s
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', s.name)
  }
}
function onDragEnd() { dragSkill.value = null; dragOverPkg.value = '' }
function onDragOver(key: string) { dragOverPkg.value = key }
function onDragEnterPkg(e: DragEvent, key: string) {
  // Only set when entering from outside the package (not from child elements)
  const el = e.currentTarget as HTMLElement
  if (e.relatedTarget && el.contains(e.relatedTarget as Node)) return
  dragOverPkg.value = key
}
function onDragLeavePkg(e: DragEvent) {
  // Only clear when leaving the package (not entering a child)
  const el = e.currentTarget as HTMLElement
  if (e.relatedTarget && el.contains(e.relatedTarget as Node)) return
  dragOverPkg.value = ''
}
async function onDrop(targetPkg: string) {
  dragOverPkg.value = ''
  const s = dragSkill.value
  if (!s || s.package === targetPkg) return
  try {
    await skillsApi.moveSkill(s.name, targetPkg)
    s.package = targetPkg
    emit('skills-changed')
    t('info', s.title, '已移至 ' + (targetPkg === 'everevo' ? 'EverEvo 核心' : targetPkg))
  } catch (e: any) { t('error','移动失败',e.message||e) }
  dragSkill.value = null
}

// ── Skill dialog ──
async function openSkillDialog(skill?: any) {
  skillMsg.value = ''; showSkillIconPick.value = false
  if (!mcpServers.value.length) await loadMCPServers()
  _mcpToolCache = {}
  for (const srv of (mcpServers.value||[]).filter((s:any) => s.status==='connected')) {
    try { _mcpToolCache[srv.id] = ((await mcpApi.getServerTools(srv.id))||[]).map((t:any) => t.name) } catch (_) { _mcpToolCache[srv.id] = [] }
  }
  if (skill) {
    editingSkill.value = skill
    skillForm.name = skill.name; skillForm.title = skill.title
    skillForm.description = skill.description; skillForm.category = skill.category
    skillForm.package = skill.package || ''
    skillForm.icon = skill.icon || ''
    skillForm.toolsText = (skill.tools||[]).join('\n')
    skillForm.resourcesText = (skill.resources||[]).join('\n')
    skillForm.systemPrompt = skill.systemPrompt || ''
    skillForm.mcpTools = skill.mcpTools ? [...skill.mcpTools] : []
  } else {
    editingSkill.value = null
    skillForm.name = ''; skillForm.title = ''; skillForm.description = ''
    skillForm.category = ''; skillForm.package = 'everevo'
    skillForm.icon = ''
    skillForm.toolsText = ''; skillForm.resourcesText = ''; skillForm.systemPrompt = ''
    skillForm.mcpTools = []; skillForm.libraryId = activeLibraryId.value
  }
  skillDialog.value = true
}
function getMCPToolNames(srv: any): string[] { return (_mcpToolCache && _mcpToolCache[srv.id]) || [] }
async function doSaveSkill() {
  if (saving.value) return
  const build = () => ({
    name: skillForm.name.trim(), title: skillForm.title.trim(),
    description: skillForm.description.trim(), category: skillForm.category.trim(),
    package: skillForm.package || '', icon: skillForm.icon.trim(),
    tools: skillForm.toolsText.split('\n').map((s:string)=>s.trim()).filter(Boolean),
    resources: skillForm.resourcesText.split('\n').map((s:string)=>s.trim()).filter(Boolean),
    prompts: [], systemPrompt: skillForm.systemPrompt.trim(),
    mcpTools: skillForm.mcpTools || [],
    libraryId: skillForm.libraryId || activeLibraryId.value,
    enabled: editingSkill.value ? editingSkill.value.enabled : true,
  })
  saving.value = true
  try {
    if (editingSkill.value) { await skillsApi.update(editingSkill.value.name, build()); t('success','已更新',build().title) }
    else { await skillsApi.create(build()); t('success','已创建',build().title) }
    skillDialog.value = false; await loadSkills(); emit('skills-changed')
  } catch (e: any) { skillMsg.value = (editingSkill.value?'更新':'创建')+'失败: '+(e.message||e); skillOk.value = false }
  finally { saving.value = false }
}
async function doDeleteSkill(s: Skill) {
  if (!await toast.confirm('删除 Skill','确定删除「'+s.title+'」？')) return
  try { await skillsApi.remove(s.name); await loadSkills(); emit('skills-changed'); t('success','已删除',s.title) }
  catch (e: any) { t('error','删除失败',e.message||e) }
}
async function doResetSkills() {
  if (!await toast.confirm('重置 Skills','确定恢复所有 Skill 到默认状态？自定义 Skill 将丢失。')) return
  try { await skillsApi.reset(); await loadSkills(); emit('skills-changed'); t('success','已重置') }
  catch (e: any) { t('error','重置失败',e.message||e) }
}

// ── MCP ──
async function loadMCPServers() { try { mcpServers.value = await mcpApi.listServers()||[] } catch (_) {} }

// ── Market ──
async function openMarket() { showMarket.value = true; try { marketSkills.value = await skillsApi.listMarket()||[] } catch (_) {} }
async function installMarketSkill(pkg: any) {
  installing.value = pkg.name
  try { await skillsApi.installMarket(pkg); t('success','已安装 '+pkg.title); showMarket.value = false; await loadSkills(); await loadMCPServers(); emit('skills-changed'); emit('mcp-servers-changed') }
  catch (e: any) { t('error','安装失败',e.message||e) }
  installing.value = null
}
async function refreshMarket() {
  refreshingMarket.value = true
  try { marketSkills.value = await skillsApi.refreshMarket()||[] }
  catch (e: any) { t('error','刷新失败',e.message||e); try { marketSkills.value = await skillsApi.listMarket()||[] } catch (_) {} }
  refreshingMarket.value = false
}
async function uninstallMarketSkill(pkg: any) {
  if (!await toast.confirm('卸载 Skill','确定卸载「'+pkg.title+'」？')) return
  try { await skillsApi.uninstallMarket(pkg.name); await loadSkills(); emit('skills-changed'); t('success','已卸载 '+pkg.title) }
  catch (e: any) { t('error','卸载失败',e.message||e) }
}
</script>

<style scoped>
.overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 100; }
.msg { padding: 8px 12px; border-radius: var(--radius-sm); font-size: 12px; margin-top: 10px; }
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }
.glass-panel { background: var(--bg-glass); backdrop-filter: blur(12px); border: 1px solid var(--border-glass); border-radius: var(--radius-lg); }

.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn-danger { background: transparent; border-color: rgba(255,69,58,0.3); color: var(--danger); }
.btn:disabled { opacity: 0.4; cursor: default; }
.btn-xs { padding: 2px 7px; font-size: 11px; border: 1px solid var(--border-soft); border-radius: 4px; background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer; }
.btn-del { color: var(--danger); border-color: rgba(255,69,58,0.3); }

/* Toolbar */
.skill-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.skill-count { font-size: 13px; color: var(--text-tertiary); }
.skill-toolbar-actions { display: flex; gap: 6px; }
.skill-file-input { display: none; }

/* Inline new-package card */
.new-pkg-card { border-style: dashed; border-color: var(--border-soft); opacity: 0.7; background: transparent; }
.new-pkg-card:hover { border-color: var(--accent); opacity: 1; }
.new-pkg-head { cursor: default; }
.new-pkg-head:hover { background: transparent; }
.new-pkg-inline-input {
  width: 100%; padding: 2px 6px; border: none; border-radius: 2px;
  background: transparent; color: var(--text-primary); font-size: 12px;
  font-weight: 600; outline: none; font-family: var(--font-mono);
}
.new-pkg-inline-input::placeholder { color: var(--text-tertiary); font-weight: 400; }

/* Package cards — OpenCode-inspired minimal dark style */
.skill-list { display: flex; flex-direction: column; gap: 1px; }
.pkg-card {
  padding: 0; transition: border-color 0.15s ease;
  border: 1px solid var(--border-subtle); border-radius: var(--radius);
  background: var(--bg-elevated); overflow: hidden;
}
.pkg-card:hover { border-color: var(--border-soft); }
.pkg-card.pkg-dragover {
  border-color: var(--accent);
  box-shadow: inset 0 0 0 1px rgba(59,130,246,0.3);
  background: rgba(59,130,246,0.03);
}
.pkg-head {
  display: flex; align-items: center; gap: 8px; padding: 8px 12px;
  cursor: pointer; user-select: none; transition: background 0.1s;
}
.pkg-head:hover { background: var(--bg-hover); }
.pkg-expand-icon { font-size: 10px; color: var(--text-tertiary); width: 12px; flex-shrink: 0; font-family: var(--font-mono); }
.pkg-icon { font-size: 16px; flex-shrink: 0; opacity: 0.8; }
.pkg-info { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; }
.pkg-name { font-size: 12px; font-weight: 600; color: var(--text-primary); font-family: var(--font-mono); }
.pkg-meta { font-size: 10px; color: var(--text-tertiary); font-family: var(--font-mono); }
.pkg-head-actions { display: flex; gap: 2px; }
.pkg-edit-btn, .pkg-del-btn {
  width: 22px; height: 22px; border: none; border-radius: 3px; background: transparent;
  color: var(--text-tertiary); font-size: 11px; cursor: pointer; display: flex; align-items: center; justify-content: center;
  opacity: 0; transition: all 0.1s;
}
.pkg-head:hover .pkg-edit-btn,
.pkg-head:hover .pkg-del-btn { opacity: 0.6; }
.pkg-edit-btn:hover,
.pkg-del-btn:hover { opacity: 1 !important; background: var(--bg-hover); }
.pkg-del-btn:hover { color: var(--danger); background: rgba(255,69,58,0.1); }
.pkg-rename-input {
  padding: 1px 4px; font-size: 12px; font-weight: 600; width: 140px;
  border: 1px solid var(--accent); border-radius: 3px;
  background: var(--bg-elevated); color: var(--text-primary); outline: none; font-family: var(--font-mono);
}
.pkg-body { border-top: 1px solid var(--border-subtle); }

/* Skill rows */
.skill-row {
  display: flex; align-items: center; gap: 6px;
  padding: 6px 12px 6px 8px;
  transition: background 0.08s; border-bottom: 1px solid var(--border-subtle); cursor: grab;
}
.skill-row:last-child { border-bottom: none; }
.skill-row:hover { background: var(--bg-hover); }
.skill-row-on { border-left: 2px solid var(--accent); }
.skill-row:active { cursor: grabbing; }
.skill-row-grip { font-size: 12px; color: var(--text-tertiary); cursor: grab; flex-shrink: 0; opacity: 0; transition: opacity 0.1s; font-family: var(--font-mono); }
.skill-row:hover .skill-row-grip { opacity: 0.4; }
.skill-row-icon { font-size: 15px; flex-shrink: 0; width: 20px; text-align: center; opacity: 0.7; }
.skill-row-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 0; }
.skill-row-name { font-size: 11px; font-weight: 600; color: var(--text-primary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); }
.skill-row-desc { font-size: 10px; color: var(--text-tertiary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.skill-row-meta { display: flex; gap: 3px; flex-shrink: 0; }
.skill-row-tag { font-size: 9px; padding: 0 5px; border-radius: 2px; background: var(--bg-inset); color: var(--text-tertiary); font-family: var(--font-mono); line-height: 1.6; }
.skill-row-actions { display: flex; align-items: center; gap: 1px; flex-shrink: 0; }

/* Integrated icon buttons */
.pbar-btn {
  width: 22px; height: 22px; display: flex; align-items: center; justify-content: center;
  border: none; border-radius: 3px; background: transparent;
  color: var(--text-tertiary); font-size: 11px; cursor: pointer;
  transition: all 0.1s ease; opacity: 0;
}
.skill-row:hover .pbar-btn { opacity: 0.5; }
.pbar-btn:hover { opacity: 1 !important; background: var(--bg-hover); color: var(--text-primary); }
.pbar-btn-danger:hover { color: var(--danger); background: rgba(255,69,58,0.1); }
.skill-toggle-sm { display: flex; align-items: center; cursor: pointer; flex-shrink: 0; margin-left: 1px; }
.skill-toggle-sm input { width: 12px; height: 12px; accent-color: var(--accent); cursor: pointer; }

/* Dialog styles (condensed) */
.skill-dialog { width: 720px; max-width: 92vw; max-height: 88vh; overflow-y: auto; padding: 0; display: flex; flex-direction: column; }
.sk-dialog-head { display: flex; align-items: center; gap: 18px; padding: 28px 32px 22px; border-bottom: 1px solid var(--border-subtle); flex-shrink: 0; }
.sk-dialog-close { width: 28px; height: 28px; border: none; border-radius: 6px; background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer; display: flex; align-items: center; justify-content: center; flex-shrink: 0; align-self: flex-start; }
.sk-dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.sk-icon-pick { position: relative; flex-shrink: 0; cursor: pointer; }
.sk-icon-preview { display: flex; align-items: center; justify-content: center; width: 56px; height: 56px; border-radius: 14px; background: var(--bg-elevated); border: 1.5px solid var(--border-soft); font-size: 26px; transition: all 0.15s; }
.sk-icon-pick:hover .sk-icon-preview { border-color: var(--accent); background: var(--accent-dim); }
.sk-icon-arrow { font-size: 9px; color: var(--text-tertiary); position: absolute; bottom: -3px; right: -3px; background: var(--bg-elevated); border-radius: 50%; width: 18px; height: 18px; display: flex; align-items: center; justify-content: center; border: 1px solid var(--border-soft); }
.sk-icon-dropdown { position: absolute; left: 0; top: 100%; margin-top: 8px; padding: 10px; background: var(--bg-elevated); border: 1px solid var(--border-soft); border-radius: 12px; box-shadow: 0 12px 32px rgba(0,0,0,0.5); z-index: 60; }
.sk-icon-palette { display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px; }
.sk-icon-btn { width: 34px; height: 34px; border-radius: 8px; border: 1px solid var(--border-soft); background: var(--bg-elevated); font-size: 16px; cursor: pointer; display: flex; align-items: center; justify-content: center; color: var(--text-secondary); transition: all 0.12s; }
.sk-icon-btn:hover { border-color: var(--accent); color: var(--text-primary); transform: scale(1.08); }
.sk-icon-btn.sk-icon-sel { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); }
.sk-head-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 10px; }
.sk-head-info h3 { font-size: 18px; font-weight: 600; margin: 0; }
.sk-title-input { width: 100%; padding: 9px 14px; box-sizing: border-box; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 15px; font-weight: 500; outline: none; font-family: var(--font); transition: border-color 0.15s; }
.sk-title-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-dim); }
.sk-body { padding: 24px 32px; display: flex; flex-direction: column; gap: 24px; flex: 1; overflow-y: auto; }
.sk-section { display: flex; flex-direction: column; gap: 14px; }
.sk-section-title { font-size: 11px; font-weight: 600; color: var(--text-tertiary); text-transform: uppercase; letter-spacing: 0.06em; padding-bottom: 8px; border-bottom: 1px solid var(--border-subtle); }
.sk-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.sk-field label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.sk-field-hint { font-weight: 400; color: var(--text-tertiary); }
.sk-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.sk-input { width: 100%; padding: 9px 12px; box-sizing: border-box; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 13px; font-family: var(--font-mono); outline: none; }
.sk-input:focus { border-color: var(--accent); }
.sk-input:disabled { opacity: 0.5; cursor: not-allowed; }
.sk-textarea { width: 100%; padding: 10px 12px; box-sizing: border-box; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; font-family: var(--font-mono); outline: none; resize: vertical; line-height: 1.6; }
.sk-textarea:focus { border-color: var(--accent); }
.sk-textarea-sm { min-height: 72px; }
.sk-mcp-list { display: flex; flex-direction: column; gap: 12px; max-height: 200px; overflow-y: auto; }
.sk-mcp-group { display: flex; flex-direction: column; gap: 4px; }
.sk-mcp-group-name { font-size: 11px; font-weight: 600; color: var(--accent); margin-bottom: 4px; padding-bottom: 4px; border-bottom: 1px solid var(--border-subtle); }
.sk-mcp-tools { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 2px; }
.sk-mcp-check { display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--text-secondary); cursor: pointer; padding: 4px 6px; border-radius: 5px; }
.sk-mcp-check:hover { background: var(--bg-hover); }
.sk-mcp-check input { accent-color: var(--accent); cursor: pointer; width: 14px; height: 14px; }
.sk-foot { padding: 16px 32px 22px; border-top: 1px solid var(--border-subtle); flex-shrink: 0; }
.sk-foot-actions { display: flex; justify-content: flex-end; gap: 10px; }

/* Market */
.market-dialog { width: 600px; max-width: 90vw; max-height: 75vh; overflow-y: auto; padding: 24px 28px; }
.market-dialog-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.market-dialog-head h3 { font-size: 16px; font-weight: 600; margin: 0; }
.market-dialog-head-actions { display: flex; align-items: center; gap: 8px; }
.market-list { display: flex; flex-direction: column; gap: 8px; }
.market-empty { text-align: center; padding: 32px 0; color: var(--text-tertiary); font-size: 13px; }
.market-item { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 16px; border: 1px solid var(--border-soft); border-radius: var(--radius); background: var(--bg-elevated); }
.market-item-left { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }
.market-icon { font-size: 24px; flex-shrink: 0; }
.market-info { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.market-name { font-size: 13px; font-weight: 600; }
.market-desc { font-size: 11px; color: var(--text-secondary); }
.market-item-right { flex-shrink: 0; }
.prov-dialog-close { width: 28px; height: 28px; border: none; border-radius: 6px; background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.prov-dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }
</style>
