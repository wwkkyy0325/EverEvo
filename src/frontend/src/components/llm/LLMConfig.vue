<template>
  <div class="llmcfg">
    <div class="llmcfg-head">
      <h2 class="llmcfg-title">大语言模型</h2>
      <div class="llmcfg-tabs">
        <button :class="['llmcfg-tab', { active: tab === 'llmcfg' }]" @click="tab = 'llmcfg'">⚙ 模型配置</button>
        <button :class="['llmcfg-tab', { active: tab === 'skills' }]" @click="tab = 'skills'">⊞ 能力清单</button>
        <button :class="['llmcfg-tab', { active: tab === 'mcp' }]" @click="tab = 'mcp'">⇌ MCP</button>
        <button :class="['llmcfg-tab', { active: tab === 'agent' }]" @click="tab = 'agent'">◉ 代理</button>
        <button :class="['llmcfg-tab', { active: tab === 'feishu' }]" @click="tab = 'feishu'">⊹ 飞书</button>
      </div>
    </div>

    <!-- ═══ Tab: llmcfg (Provider Management — core feature, inline) ═══ -->
    <div v-if="tab === 'llmcfg'" class="llmcfg-body">
      <!-- CC Switch -- active provider status -->
      <div class="ccswitch-card glass-panel" :class="{ 'ccswitch-on': activeProvider }">
        <div class="ccswitch-left">
          <div class="ccswitch-icon">{{ activeProvider ? '◎' : '○' }}</div>
          <div class="ccswitch-info">
            <div class="ccswitch-title">大语言模型</div>
            <div class="ccswitch-desc">
              {{ activeProvider ? activeProvider.name + ' / ' + activeProvider.model : '未配置供应商' }}
            </div>
          </div>
        </div>
        <label class="ccswitch-toggle">
          <input type="checkbox" :checked="!!activeProvider" @change="toggleLLM" />
          <span class="ccswitch-knob"></span>
        </label>
      </div>

      <!-- Extraction model (memory fact/graph extraction; "" → active) -->
      <div class="ccswitch-card glass-panel">
        <div class="ccswitch-left">
          <div class="ccswitch-icon">🧠</div>
          <div class="ccswitch-info">
            <div class="ccswitch-title">记忆抽取模型</div>
            <div class="ccswitch-desc">事实/图谱抽取使用的模型，可选更便宜的；留空跟随活动模型</div>
          </div>
        </div>
        <select :value="extractionId" style="min-width:180px;padding:6px 8px;border:1px solid var(--border-soft);border-radius:var(--radius-sm);background:var(--bg-elevated);color:var(--text-primary);font-size:12px"
          @change="setExtraction(($event.target as HTMLSelectElement).value)">
          <option value="">默认（活动模型）</option>
          <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }} / {{ p.model }}</option>
        </select>
      </div>

      <!-- Provider bars -->
      <div v-if="providers.length" class="provider-list">
        <div v-for="p in providers" :key="p.id"
          class="provider-bar glass-panel"
          :class="{ 'is-active': p.id === activeId && p.enabled, 'is-enabled': p.enabled }">
          <div class="pbar-left">
            <span class="pbar-icon pbar-icon-clickable"
              @click.stop="toggleIconPicker(p)" title="点击更换图标">
              <img v-if="isIconImage(p.icon)" :src="p.icon" class="pbar-icon-img" />
              <span v-else>{{ p.icon || '◎' }}</span>
            </span>
            <!-- Inline icon picker popover -->
            <div v-if="iconPickerFor === p.id" class="icon-popover" @click.stop>
              <div class="icon-popover-palette">
                <button v-for="ic in iconPalette" :key="ic"
                  :class="['icon-popover-btn', { 'icon-popover-sel': (p.icon || '◎') === ic }]"
                  @click="setProviderIcon(p, ic)">{{ ic }}</button>
              </div>
              <div class="icon-popover-divider"></div>
              <label class="icon-popover-upload">
                <input type="file" accept="image/png,image/jpeg,image/gif,image/svg+xml,image/webp"
                  class="icon-popover-file-input" @change="onPopoverIconFile($event, p)" />
                📁 本地图片
              </label>
              <button class="icon-popover-close" @click="iconPickerFor = ''">✕</button>
            </div>
            <div class="pbar-info">
              <div class="pbar-name">{{ p.name }}</div>
              <div class="pbar-meta">
                <span class="pbar-model">{{ p.model }}</span>
                <span class="pbar-sep">·</span>
                <span v-if="p.id === activeId && p.enabled" class="pbar-status-dot pbar-live-dot"></span>
                <span v-else class="pbar-status-dot pbar-off-dot"></span>
                <span class="pbar-endpoint">{{ p.endpoint }}</span>
              </div>
              <!-- Expanded detail row (visible on hover) -->
              <div class="pbar-detail">
                <span v-if="providerCapsMap[p.id] && providerCapsMap[p.id].hasCaps" class="pbar-caps">
                  <span v-if="providerCapsMap[p.id].vision" class="cap-tag cap-vision" title="多模态">👁</span>
                  <span v-if="providerCapsMap[p.id].tools" class="cap-tag cap-tools" title="工具调用">🔧</span>
                  <span v-if="providerCapsMap[p.id].reasoning" class="cap-tag cap-reason" title="深度推理">🧠</span>
                  <span v-if="providerCapsMap[p.id].streaming" class="cap-tag cap-stream" title="流式输出">⇢</span>
                  <span v-if="providerCapsMap[p.id].json" class="cap-tag cap-json" title="JSON 输出">{ }</span>
                  <span v-if="providerCapsMap[p.id].fim" class="cap-tag cap-fim" title="FIM 补全">⟷</span>
                  <span class="cap-tag cap-ctx" :title="providerCapsMap[p.id].ctx + ' tokens'">{{ providerCapsMap[p.id].ctxFmt }}</span>
                </span>
                <span v-if="p.type === 'deepseek'" class="pbar-balance">
                  <span v-if="loadingBalance[p.id]" class="pbar-balance-loading">⏳</span>
                  <span v-else class="pbar-balance-amount">{{ formatBalance(balanceMap[p.id]) }}</span>
                  <button class="pbar-balance-refresh" @click.stop="queryBalance(p)" title="刷新余额">↻</button>
                </span>
              </div>
            </div>
          </div>
          <div class="pbar-actions">
            <button v-if="p.id === activeId && p.enabled"
              class="pbar-activate pbar-activate-on" disabled>已启用</button>
            <button v-else
              class="pbar-activate" @click="activateProvider(p)">启用</button>
            <button class="pbar-btn" @click="editProvider(p)" title="编辑">✎</button>
            <button class="pbar-btn" @click="testProvider(p)" title="测试连接">↻</button>
            <button class="pbar-btn pbar-btn-danger" @click="deleteProvider(p)" title="删除">✕</button>
            <button class="pbar-btn" @click="copyProvider(p)" title="复制">⧉</button>
          </div>
        </div>
      </div>

      <!-- Empty state -->
      <div v-if="!providers.length" class="provider-empty">
        <span>暂无供应商</span>
        <span class="hint">点击下方按钮添加一个 LLM 供应商</span>
      </div>

      <button class="btn btn-primary" @click="openNewProvider()">+ 新建供应商</button>

      <!-- Provider Create / Edit Dialog -->
      <ProviderDialog v-model="provDialog" :editing-prov="editingProv" :presets="presets" :icon-palette="iconPalette" @saved="onProviderSaved" @silent-saved="loadChatCfg" />
    </div>

    <!-- ═══ Tab: Skills ═══ -->
    <div v-if="tab === 'skills'" class="llmcfg-body">
      <LLMSkills :icon-palette="iconPalette" @skills-changed="onSkillsChanged" @mcp-servers-changed="onMCPServersChanged" />
    </div>

    <!-- ═══ Tab: MCP ═══ -->
    <div v-if="tab === 'mcp'" class="llmcfg-body">
      <LLMMCP />
    </div>

    <!-- ═══ Tab: Agent ═══ -->
    <div v-if="tab === 'agent'" class="llmcfg-body">
      <LLMAgent />
    </div>

    <!-- ═══ Tab: Feishu ═══ -->
    <div v-if="tab === 'feishu'" class="llmcfg-body">
      <LLMFeishu />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useToast } from '../../composables/useToast'
import { useDataChanged } from '../../composables/useDataChanged'
import { useChatStore } from '../../stores/chatStore'
import { providersApi } from '../../api/providers'
import { systemApi } from '../../api/system'
import LLMSkills from './LLMSkills.vue'
import LLMMCP from './LLMMCP.vue'
import LLMAgent from './LLMAgent.vue'
import LLMFeishu from './LLMFeishu.vue'
import ProviderDialog from './ProviderDialog.vue'

// ── Toast / Chat Store ──
const toast = useToast()
const chat = useChatStore()

// ── State ──
const tab = ref('llmcfg')

// LLM Providers
const providers = ref<any[]>([])
const presets = ref<any[]>([])
const activeId = ref('')
const extractionId = ref('') // memory extraction provider ("" → active)

// Provider dialog
const provDialog = ref(false)
const editingProv = ref<any>(null)

const iconPalette = ['◎', '◈', '◇', '□', '◉', '○', '●', '◆', '◊', '△', '▲', '▽', '☆', '★', '⊕', '⊗', '⬡', '⬢', '☰', '☷']

const iconPickerFor = ref('')

// ── Balance state (DeepSeek) ──
const balanceMap = ref<Record<string, any>>({})
const loadingBalance = ref<Record<string, boolean>>({})
let balanceTimer: ReturnType<typeof setInterval> | null = null

// ── Anti-freeze guards ──
const saving = ref(false)

// ── Computed ──
const activeProvider = computed(() => {
  return providers.value.find(p => p.id === activeId.value && p.enabled) || null
})

const providerCapsMap = computed(() => {
  const map: Record<string, any> = {}
  for (const p of providers.value) {
    try {
      if (p.modelCapabilities && p.model) {
        const c = p.modelCapabilities[p.model]
        if (c && (c.maxContextTokens || c.supportsVision || c.supportsTools || c.supportsReasoning || c.supportsStreaming || c.supportsJSON || c.supportsFIM)) {
          map[p.id] = {
            hasCaps: true,
            vision: !!c.supportsVision,
            tools: !!c.supportsTools,
            reasoning: !!c.supportsReasoning,
            streaming: !!c.supportsStreaming,
            json: !!c.supportsJSON,
            fim: !!c.supportsFIM,
            ctx: typeof c.maxContextTokens === 'number' ? c.maxContextTokens : 0,
            ctxFmt: (typeof c.maxContextTokens === 'number' && c.maxContextTokens >= 1000)
              ? Math.round(c.maxContextTokens / 1000) + 'K' : String(c.maxContextTokens || '?'),
          }
          continue
        }
      }
    } catch (_) {}
    map[p.id] = { hasCaps: false, vision: false, tools: false, reasoning: false, streaming: false, json: false, fim: false, ctx: 0, ctxFmt: '?' }
  }
  return map
})

// ── Lifecycle ──
onBeforeUnmount(() => {
  stopBalancePolling()
})

onMounted(async () => {
  await loadChatCfg()
  // Initial balance query for DeepSeek providers
  for (const p of providers.value) {
    if (p.type === 'deepseek' && p.enabled) queryBalance(p)
  }
  startBalancePolling()
})

// ── Real-time refresh — reload when a provider is mutated anywhere ──
useDataChanged('providers:changed', () => { loadChatCfg() })

// ── LLM Config / Provider CRUD (inline — core feature) ──
async function loadChatCfg() {
  try {
    presets.value = await providersApi.listPresets() || []
    providers.value = await providersApi.list() || []
    const cfg = await systemApi.getConfig()
    if (cfg && cfg.llm) {
      activeId.value = cfg.llm.activeProvider || ''
      extractionId.value = cfg.llm.extractionProvider || ''
    }
    chat.providers = providers.value
    chat.activeId = activeId.value
  } catch (_) {}
}

// Set the memory-extraction provider ("" → fall back to active).
async function setExtraction(id: string) {
  extractionId.value = id
  try { await providersApi.setExtractionProvider(id) } catch (_) {}
}

// ── Balance query ──
async function queryBalance(p: any) {
  if (p.type !== 'deepseek' || !p.apiKey) return
  loadingBalance.value = { ...loadingBalance.value, [p.id]: true }
  try {
    const info = await providersApi.queryBalance(p.id)
    balanceMap.value = { ...balanceMap.value, [p.id]: info }
  } catch (_) { /* ignore */ }
  loadingBalance.value = { ...loadingBalance.value, [p.id]: false }
}

function startBalancePolling() {
  stopBalancePolling()
  balanceTimer = setInterval(() => {
    for (const p of providers.value) {
      if (p.type === 'deepseek' && p.enabled) queryBalance(p)
    }
  }, 5 * 60 * 1000) // every 5 minutes
}

function stopBalancePolling() {
  if (balanceTimer) { clearInterval(balanceTimer); balanceTimer = null }
}

function formatBalance(info: any): string {
  if (!info || !info.balanceInfos || !info.balanceInfos.length) return '--'
  const b = info.balanceInfos[0]
  const total = parseFloat(b.totalBalance)
  if (isNaN(total)) return '--'
  return b.currency + ' ' + total.toFixed(2)
}

function toggleIconPicker(p: any) {
  iconPickerFor.value = iconPickerFor.value === p.id ? '' : p.id
}

async function setProviderIcon(p: any, icon: string) {
  p.icon = icon
  iconPickerFor.value = ''
  try { await providersApi.update(p.id, p) } catch (_) {}
}

function isIconImage(icon: any) {
  return icon && typeof icon === 'string' && icon.startsWith('data:image/')
}

async function onPopoverIconFile(e: Event, p: any) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > 128 * 1024) { toast.show('error', '图片过大', '图标图片请控制在 128KB 以内'); return }
  const reader = new FileReader()
  reader.onload = async () => {
    p.icon = reader.result as string
    iconPickerFor.value = ''
    try { await providersApi.update(p.id, p) } catch (_) {}
  }
  reader.readAsDataURL(file)
  ;(e.target as HTMLInputElement).value = ''
}

function openNewProvider() {
  editingProv.value = null
  provDialog.value = true
}

function editProvider(p: any) {
  editingProv.value = p
  provDialog.value = true
}

async function onProviderSaved() {
  provDialog.value = false
  await loadChatCfg()
}

async function activateProvider(p: any) {
  if (saving.value) return
  saving.value = true
  try {
    p.enabled = true
    await providersApi.update(p.id, p)
    await providersApi.setActive(p.id)
    await loadChatCfg()
    toast.show('success', '已切换至', p.name)
  } catch (e: any) { toast.show('error', '切换失败', e.message || e) } finally { saving.value = false }
}

function toggleLLM() {
  if (activeProvider.value) {
    const p = activeProvider.value
    p.enabled = false
    providersApi.update(p.id, p).then(() => loadChatCfg()).catch(() => {})
  } else if (providers.value.length) {
    const p = providers.value[0]
    p.enabled = true
    providersApi.update(p.id, p).then(() => {
      providersApi.setActive(p.id).then(() => loadChatCfg()).catch(() => {})
    }).catch(() => {})
  }
}

async function testProvider(p: any) {
  try {
    const result = await providersApi.testConnection(p.id)
    toast.show('success', '测试通过', result)
  } catch (e: any) { toast.show('error', '测试失败', e.message || e) }
}

async function deleteProvider(p: any) {
  if (saving.value) return
  if (!await toast.confirm('删除供应商', '确定删除「' + p.name + '」？')) return
  saving.value = true
  try {
    await providersApi.remove(p.id)
    await loadChatCfg()
    toast.show('success', '已删除', p.name)
  } catch (e: any) { toast.show('error', '删除失败', e.message || e) } finally { saving.value = false }
}

async function copyProvider(p: any) {
  const copy = { ...p, name: p.name + ' (副本)', notes: '', id: undefined, createdAt: undefined }
  try {
    await providersApi.create(copy)
    await loadChatCfg()
    toast.show('success', '已复制', copy.name)
  } catch (e: any) { toast.show('error', '复制失败', e.message || e) }
}

// ── Tab event handlers ──
function onSkillsChanged() {
  // Skills were modified — refresh chatStore if needed
  chat.loadSkills().catch(() => {})
}

function onMCPServersChanged() {
  // MCP servers were modified from skills tab (e.g., market install)
  // No action needed here; LLMMCP reloads its own data
}
</script>

<style scoped>
/* ── Container Layout ── */
.llmcfg { display: flex; flex-direction: column; gap: 18px; flex: 1; min-height: 0; }
.llmcfg-head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; flex-shrink: 0; }
.llmcfg-title { font-size: 22px; font-weight: 600; }
.llmcfg-tabs { display: flex; gap: 4px; }
.llmcfg-tab {
  padding: 6px 14px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-secondary); font-size: 12px; cursor: pointer; transition: all var(--transition);
}
.llmcfg-tab:hover { background: var(--bg-hover); color: var(--text-primary); }
.llmcfg-tab.active { background: var(--accent); border-color: var(--accent); color: #fff; }
.llmcfg-body { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; padding-right: 8px; }

/* ── CC Switch ── */
.ccswitch-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 20px; margin-bottom: 16px;
  transition: all 0.25s ease;
  border: 1px solid var(--border-subtle);
}
.ccswitch-card.ccswitch-on {
  border-color: rgba(48,209,88,0.18);
  background: linear-gradient(135deg, rgba(48,209,88,0.03) 0%, transparent 50%);
}
.ccswitch-left { display: flex; align-items: center; gap: 14px; }
.ccswitch-icon {
  width: 36px; height: 36px; border-radius: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  display: flex; align-items: center; justify-content: center;
  font-size: 17px; color: var(--text-tertiary); transition: all 0.25s ease;
}
.ccswitch-on .ccswitch-icon { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); }
.ccswitch-info { display: flex; flex-direction: column; gap: 2px; }
.ccswitch-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.ccswitch-desc { font-size: 12px; color: var(--text-tertiary); }
.ccswitch-on .ccswitch-desc { color: var(--success); }
.ccswitch-toggle {
  position: relative; display: inline-block; width: 46px; height: 26px; flex-shrink: 0; cursor: pointer;
}
.ccswitch-toggle input { display: none; }
.ccswitch-knob {
  position: absolute; inset: 0; border-radius: 13px;
  background: var(--bg-inset); border: 1px solid var(--border-soft); transition: all 0.22s ease;
}
.ccswitch-knob::after {
  content: ''; position: absolute; top: 2px; left: 2px; width: 20px; height: 20px;
  border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,0.2);
  transition: transform 0.22s cubic-bezier(0.22, 0.61, 0.36, 1);
}
.ccswitch-toggle input:checked + .ccswitch-knob { background: var(--success); border-color: var(--success); }
.ccswitch-toggle input:checked + .ccswitch-knob::after { transform: translateX(20px); }

/* ── Provider bars ── */
.provider-list { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.provider-bar {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 14px; transition: all 0.25s ease;
  border: 1px solid var(--border-subtle); border-radius: var(--radius-lg);
  position: relative; overflow: hidden;
}
/* Active state: blue ring + glow */
.provider-bar.is-active {
  border-color: rgba(59,130,246,0.5);
  box-shadow: 0 0 0 1px rgba(59,130,246,0.3), 0 0 16px rgba(59,130,246,0.08);
  background: linear-gradient(135deg, rgba(59,130,246,0.04) 0%, transparent 60%);
}
.provider-bar.is-enabled:not(.is-active) { border-color: var(--border-soft); }
.provider-bar:not(.is-enabled) { opacity: 0.55; }

.pbar-left { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; position: relative; }
.pbar-icon {
  font-size: 26px; width: 40px; height: 40px; text-align: center; flex-shrink: 0; opacity: 0.9;
  display: flex; align-items: center; justify-content: center; border-radius: var(--radius-sm);
}
.pbar-icon-clickable { cursor: pointer; transition: all 0.15s; }
.pbar-icon-clickable:hover { background: var(--bg-hover); opacity: 1; }
.pbar-icon-img { width: 30px; height: 30px; border-radius: 5px; object-fit: contain; }

/* Inline icon popover */
.icon-popover {
  position: absolute; left: 0; top: 100%; z-index: 50;
  margin-top: 6px; padding: 8px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  display: flex; flex-direction: column; gap: 4px;
}
.icon-popover-palette { display: grid; grid-template-columns: repeat(5, 1fr); gap: 3px; }
.icon-popover-btn {
  width: 30px; height: 30px; border-radius: 6px;
  border: 1px solid var(--border-soft); background: var(--bg-elevated);
  font-size: 15px; cursor: pointer; display: flex; align-items: center; justify-content: center;
  transition: all 0.12s; color: var(--text-secondary);
}
.icon-popover-btn:hover { border-color: var(--accent); color: var(--text-primary); }
.icon-popover-btn.icon-popover-sel { background: var(--accent-dim); border-color: var(--accent); }
.icon-popover-close {
  position: absolute; top: 6px; right: 6px;
  width: 22px; height: 22px; border: none; border-radius: 4px;
  background: transparent; color: var(--text-tertiary); font-size: 11px; cursor: pointer;
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.icon-popover-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.icon-popover-divider { height: 1px; background: var(--border-soft); flex-shrink: 0; margin: 2px 0; }
.icon-popover-upload {
  display: flex; align-items: center; gap: 4px; padding: 5px 8px; border-radius: 4px;
  font-size: 11px; color: var(--text-secondary); cursor: pointer; white-space: nowrap;
  text-decoration: none;
}
.icon-popover-upload:hover { background: var(--bg-hover); color: var(--text-primary); }
.icon-popover-file-input { display: none; }

/* Info area */
.pbar-info { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.pbar-name {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.pbar-meta {
  display: flex; align-items: center; gap: 6px; font-size: 11px;
  white-space: nowrap; overflow: hidden;
}
.pbar-model {
  color: var(--accent); font-family: var(--font-mono);
  max-width: 160px; overflow: hidden; text-overflow: ellipsis;
}
.pbar-sep { color: var(--text-tertiary); opacity: 0.4; flex-shrink: 0; }
.pbar-status-dot {
  width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0;
}
.pbar-live-dot { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.6); }
.pbar-off-dot { background: var(--text-tertiary); opacity: 0.5; }
.pbar-endpoint {
  color: var(--text-tertiary); font-family: var(--font-mono);
  overflow: hidden; text-overflow: ellipsis; flex: 1; min-width: 0;
}

/* Detail row: always visible, locked height — no hover jitter */
.pbar-detail {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  min-height: 18px;
}
.pbar-caps { display: flex; gap: 2px; align-items: center; }
.cap-tag { font-size: 11px; opacity: 0.7; cursor: default; }
.cap-tag:hover { opacity: 1; }
.cap-vision { color: #60a5fa; }
.cap-tools { color: #4ade80; }
.cap-reason { color: #f59e0b; }
.cap-stream { color: #a78bfa; }
.cap-json { color: #f59e0b; }
.cap-fim { color: #22d3ee; }
.cap-ctx { color: var(--text-tertiary); font-size: 10px; font-family: var(--font-mono); }

.pbar-balance {
  display: flex; align-items: center; gap: 3px; font-size: 10px;
  color: var(--accent); font-family: var(--font-mono);
}
.pbar-balance-loading { opacity: 0.5; }
.pbar-balance-amount { cursor: default; }
.pbar-balance-refresh {
  background: none; border: none; color: var(--text-tertiary);
  font-size: 11px; cursor: pointer; padding: 0 1px; line-height: 1;
}
.pbar-balance-refresh:hover { color: var(--accent); }

/* Actions — activate always visible, rest hidden until hover */
.pbar-actions { display: flex; gap: 4px; flex-shrink: 0; align-items: center; }
.pbar-activate {
  padding: 4px 0; font-size: 12px; font-weight: 500; width: 64px; text-align: center;
  border: 1px solid var(--accent); border-radius: 20px;
  background: transparent; color: var(--accent); cursor: pointer;
  white-space: nowrap; transition: all 0.2s ease;
  line-height: 1.4;
}
.pbar-activate:hover { background: var(--accent); color: #fff; }
.pbar-activate-on {
  border-color: var(--border-soft); color: var(--text-tertiary);
  background: transparent; cursor: default; opacity: 0.7;
}
.pbar-activate-on:hover { background: transparent; color: var(--text-tertiary); }

/* Small icon buttons — hidden until hover */
.pbar-btn {
  width: 26px; height: 26px; display: flex; align-items: center; justify-content: center;
  border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-tertiary); font-size: 12px; cursor: pointer;
  transition: all 0.15s ease; opacity: 0;
}
.provider-bar:hover .pbar-btn { opacity: 0.5; }
.pbar-btn:hover { opacity: 1 !important; background: var(--bg-hover); color: var(--text-primary); }
.pbar-btn-danger:hover { color: var(--danger); background: rgba(255,69,58,0.08); }

.provider-empty { text-align: center; padding: 24px 0; color: var(--text-tertiary); font-size: 13px; display: flex; flex-direction: column; gap: 4px; }
.provider-empty .hint { font-size: 11px; opacity: 0.7; }

/* ── Shared UI buttons (used in llmcfg tab) ── */
.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
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

/* ── Glass panel (used by inline cards) ── */
.glass-panel {
  background: var(--bg-glass); backdrop-filter: blur(12px);
  border: 1px solid var(--border-glass); border-radius: var(--radius-lg);
}
</style>