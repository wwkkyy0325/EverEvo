<template>
  <div v-if="modelValue" class="overlay">
    <div class="glass-panel prov-dialog">
      <div class="prov-dialog-head">
        <h3>{{ editingProv ? '编辑供应商' : '新建供应商' }}</h3>
        <button class="prov-dialog-close" @click="close">✕</button>
      </div>

      <!-- Preset selector (only for new) -->
      <div v-if="!editingProv" class="preset-section">
        <label class="preset-label">选择供应商类型</label>
        <div class="preset-grid">
          <button v-for="pr in presets" :key="pr.type"
            :class="['preset-btn', { 'preset-sel': provForm.type === pr.type }]"
            @click="applyPreset(pr)">
            <span class="preset-btn-icon">{{ pr.icon || '◎' }}</span>
            <span class="preset-btn-name">{{ pr.name }}</span>
          </button>
          <button :class="['preset-btn', { 'preset-sel': provForm.type === 'custom' }]"
            @click="provForm.type = 'custom'; provForm.models = []">
            <span class="preset-btn-icon">⊞</span>
            <span class="preset-btn-name">自定义</span>
          </button>
        </div>
      </div>

      <div class="prov-divider"></div>

      <!-- Form -->
      <div class="prov-form">
        <!-- Header: large icon + name / notes / website -->
        <div class="prov-header">
          <div class="prov-icon-area">
            <div class="prov-icon-trigger prov-icon-trigger-lg" @click="showIconDropdown = !showIconDropdown">
              <img v-if="isIconImage(provForm.icon)" :src="provForm.icon" class="prov-icon-preview-img-lg" />
              <span v-else class="prov-icon-preview-char-lg">{{ provForm.icon || '◎' }}</span>
              <span class="prov-icon-arrow">▾</span>
            </div>
            <div v-if="showIconDropdown" class="prov-icon-dropdown" @click.stop>
              <div class="prov-icon-dropdown-palette">
                <button v-for="ic in iconPalette" :key="ic"
                  :class="['prov-icon-btn', { 'prov-icon-sel': provForm.icon === ic }]"
                  @click="selectIcon(ic)">{{ ic }}</button>
              </div>
              <div class="prov-icon-dropdown-divider"></div>
              <label class="prov-icon-upload">
                <input type="file" accept="image/png,image/jpeg,image/gif,image/svg+xml,image/webp"
                  class="prov-icon-file-input" @change="onIconFileSelected" />
                📁 选择本地图片
              </label>
            </div>
          </div>
          <div class="prov-header-fields">
            <div class="prov-header-field">
              <label class="prov-label prov-label-inline">名称</label>
              <input v-model="provForm.name" type="text" placeholder="例如：我的 OpenAI" class="prov-field" />
            </div>
            <div class="prov-header-field">
              <label class="prov-label prov-label-inline">备注</label>
              <input v-model="provForm.notes" type="text" placeholder="例如：公司专用账号" class="prov-field" />
            </div>
            <div class="prov-header-field">
              <label class="prov-label prov-label-inline">官网</label>
              <input v-model="provForm.website" type="text" placeholder="https://platform.openai.com" class="prov-field" />
            </div>
          </div>
        </div>
        <div class="prov-form-row">
          <label class="prov-label">API Key</label>
          <template v-if="isLocalPreset">
            <div class="prov-field-row">
              <input type="text" value="本地模型无需 API Key" disabled class="prov-field prov-field-disabled" />
            </div>
            <span class="prov-hint">本地推理服务通过 localhost 直连，不需要鉴权</span>
          </template>
          <template v-else>
            <div class="prov-field-row">
              <input v-model="provForm.apiKey" :type="showKey ? 'text' : 'password'" placeholder="sk-…" class="prov-field" />
              <button class="prov-eye" @click="showKey = !showKey" :title="showKey ? '隐藏' : '显示'">
                {{ showKey ? '◉' : '○' }}
              </button>
            </div>
          </template>
        </div>
        <div class="prov-form-row">
          <label class="prov-label">API 格式</label>
          <select v-model="provForm.apiFormat" class="prov-field prov-field-sm">
            <option value="openai">OpenAI Chat Completions</option>
            <option value="anthropic">Anthropic Messages</option>
            <option value="openai-compat">OpenAI 兼容</option>
          </select>
        </div>
        <div class="prov-form-row">
          <label class="prov-label">请求地址</label>
          <input v-model="provForm.endpoint" type="text" :placeholder="defaultEndpoint(provForm.apiFormat)" class="prov-field" />
          <span class="prov-hint">基础地址，系统会自动追加 /chat/completions 或 /messages，勿以斜杠结尾</span>
        </div>
        <!-- Local model list status -->
        <div v-if="provForm.type === 'ollama' || provForm.type === 'llamacpp'" class="prov-form-row">
          <span v-if="detectingLocal" class="prov-hint">⏳ 正在从本地服务拉取模型列表…</span>
          <span v-else-if="provForm.models && provForm.models.length" class="prov-hint">✓ 已检测到 {{ provForm.models.length }} 个本地模型</span>
          <span v-else class="prov-hint" style="color:var(--warning)">⚠ 未检测到模型，请确认服务已启动</span>
        </div>
        <!-- DeepSeek model list status -->
        <div v-if="provForm.type === 'deepseek'" class="prov-form-row">
          <span v-if="fetchingDS" class="prov-hint">⏳ 正在从 DeepSeek 获取模型列表…</span>
          <span v-else-if="dsFetchError" class="prov-hint" style="color:var(--warning)">
            ⚠ {{ dsFetchError }}
            <button class="btn btn-xs" @click="doFetchDeepSeekModels" style="margin-left:6px">重试</button>
          </span>
          <span v-else-if="provForm.models && provForm.models.length" class="prov-hint">
            ✓ 已获取 {{ provForm.models.length }} 个 DeepSeek 模型
            <button class="btn btn-xs" @click="doFetchDeepSeekModels" style="margin-left:6px" :disabled="fetchingDS">刷新</button>
          </span>
          <span v-else-if="!provForm.apiKey" class="prov-hint">输入 API Key 后自动获取模型列表</span>
          <span v-else class="prov-hint" style="color:var(--warning)">⚠ 未获取到模型，请检查 API Key</span>
        </div>
        <!-- Model capabilities display -->
        <div class="prov-form-row">
          <div style="display:flex;align-items:center;gap:8px">
            <label class="prov-label" style="margin:0">模型能力</label>
            <button v-if="!probing" class="btn btn-xs btn-go" @click="probeCapabilities">🔬 实时检测</button>
            <button v-else class="btn btn-xs btn-del" @click="cancelProbe">■ 取消探测</button>
            <span class="prov-hint" style="margin:0">发送测试请求探测真实能力</span>
          </div>
        </div>
        <div v-if="activeModelCaps" class="prov-form-row">
          <div class="prov-caps-display">
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsVision }"
              :title="activeModelCaps.supportsVision ? '✓ 多模态：支持图像/视觉输入，可以识别和理解图片内容，适用于图文混合场景' : '✗ 不支持图像输入，仅处理纯文本'">
              👁 多模态
            </span>
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsTools }"
              :title="activeModelCaps.supportsTools ? '✓ 工具调用：支持 Function Calling / Tool Use，可调用外部 API、MCP 工具，是构建 Agent 的关键能力' : '✗ 不支持 Function Calling，无法作为 Agent 使用工具'">
              🔧 工具调用
            </span>
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsReasoning }"
              :title="activeModelCaps.supportsReasoning ? '✓ 深度推理：支持思维链 (CoT)，可进行多步逻辑推导、数学证明、自我纠错，推理过程中输出思考步骤' : '✗ 标准对话模式，不显式输出中间推理步骤'">
              🧠 推理
            </span>
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsStreaming }"
              :title="activeModelCaps.supportsStreaming ? '✓ 流式输出：支持 SSE 流式生成，边生成边显示，交互体验更好' : '✗ 仅完整响应模式，需等待全部生成完毕才能看到结果'">
              ⇢ 流式
            </span>
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsFIM }"
              :title="activeModelCaps.supportsFIM ? '✓ FIM 补全：支持 Fill-in-the-Middle 代码补全，可在光标位置插入生成内容（DeepSeek β 功能）' : '✗ 不支持 FIM 补全'">
              ⟷ FIM
            </span>
            <span class="prov-cap" :class="{ 'cap-on': activeModelCaps.supportsJSON }"
              :title="activeModelCaps.supportsJSON ? '✓ JSON 输出：支持原生 JSON 模式 (response_format)，保证输出为合法 JSON，适用于结构化数据提取' : '✗ 不支持原生 JSON 输出模式'">
              { } JSON
            </span>
            <span class="prov-cap ctx-cap" :class="{ 'cap-on': activeModelCaps.maxContextTokens > 0 }"
              :title="'上下文窗口：' + activeModelCaps.maxContextTokens.toLocaleString() + ' tokens — 决定单次对话能处理的文本上限（输入+输出），越大可处理的文档越长、对话轮次越多'">
              {{ activeModelCaps.maxContextTokens >= 1000 ? Math.round(activeModelCaps.maxContextTokens/1000) + 'K' : activeModelCaps.maxContextTokens }} 上下文
            </span>
          </div>
        </div>
        <!-- JSON Output slider toggle -->
        <div class="prov-form-row">
          <div class="prov-toggle-row">
            <label class="prov-toggle-label">JSON 结构化输出</label>
            <span class="prov-toggle-hint">强制模型返回合法 JSON（需要模型支持）</span>
            <label class="slide-switch">
              <input type="checkbox" v-model="provForm.enableJSONOutput" />
              <span class="slide-knob"></span>
            </label>
          </div>
        </div>
        <div class="prov-form-row">
          <label class="prov-label">默认模型</label>
          <div class="prov-field-row">
            <select v-if="provForm.models && provForm.models.length" v-model="provForm.model" class="prov-field">
              <option v-for="m in provForm.models" :key="m" :value="m">{{ m }}</option>
              <option value="__custom__">自定义…</option>
            </select>
            <input v-if="!provForm.models || !provForm.models.length || provForm.model === '__custom__'"
              v-model="provForm.modelCustom" type="text" placeholder="输入模型名…" class="prov-field" />
          </div>
        </div>

      </div>

      <div class="prov-dialog-foot">
        <div v-if="provMsg" class="msg" :class="provOk ? 'msg-ok' : 'msg-err'">{{ provMsg }}</div>
        <div class="prov-dialog-actions">
          <button class="btn" @click="close">取消</button>
          <button class="btn btn-primary" @click="saveProvider" :disabled="!provForm.name || !provForm.endpoint || (!provForm.apiKey && !isLocalPreset) || saving">
            {{ editingProv ? '保存' : '创建供应商' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { useToast } from '../../composables/useToast'
import { providersApi } from '../../api/providers'
import { getModelProfile } from '../../config/modelProfiles'

// ── Props ──
const props = defineProps<{
  modelValue: boolean
  editingProv: any
  presets: any[]
  iconPalette: string[]
}>()

// ── Emits ──
const emit = defineEmits<{
  (e: 'update:modelValue', val: boolean): void
  (e: 'saved'): void
  (e: 'silentSaved'): void
}>()

// ── Root instance for toast ──
const toast = useToast()

function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

// ── Form state ──
const provForm = reactive<Record<string, any>>({
  name: '', icon: '', notes: '', website: '',
  type: 'openai', apiFormat: 'openai',
  endpoint: '', apiKey: '',
  model: '', modelCustom: '', models: [] as string[],
  enableJSONOutput: true,
})

const showIconDropdown = ref(false)
const showKey = ref(false)
const provMsg = ref('')
const provOk = ref(false)
const detectingLocal = ref(false)
const probing = ref(false)
const fetchingDS = ref(false)    // DeepSeek model list loading
const dsFetchError = ref('')     // DeepSeek fetch error message

// ── Anti-freeze guards ──
const saving = ref(false)
let _detectCancelId = 0
let _probeTimer: ReturnType<typeof setTimeout> | null = null

// ── Computed ──
const isLocalPreset = computed(() => {
  return provForm.type === 'ollama' || provForm.type === 'llamacpp'
})

const activeModelCaps = computed(() => {
  try {
    if (!provForm.modelCapabilities) return null
    const m = provForm.model === '__custom__' ? provForm.modelCustom : provForm.model
    if (!m) return null
    const cap = provForm.modelCapabilities[m]
    if (!cap) return null
    if (!cap.maxContextTokens && !cap.supportsVision && !cap.supportsTools && !cap.supportsReasoning && !cap.supportsStreaming && !cap.supportsJSON && !cap.supportsFIM) return { _unprobed: true }
    return cap
  } catch (_) { return null }
})

// ── Watchers ──
watch(
  () => provForm.apiFormat,
  (fmt, old) => {
    if (old === undefined) return
    const defaults: Record<string, string> = {
      openai: 'https://api.openai.com/v1',
      anthropic: 'https://api.deepseek.com/anthropic',
      'openai-compat': 'https://api.openai.com/v1',
    }
    const knownEndpoints = ['https://api.openai.com/v1', 'https://api.deepseek.com/anthropic']
    if (!provForm.endpoint || knownEndpoints.includes(provForm.endpoint)) {
      provForm.endpoint = defaults[fmt] || ''
    }
  }
)

// Watch modelCustom → sync to model when no preset model list (custom input only)
watch(
  () => provForm.modelCustom,
  (val) => {
    if (!provForm.models.length && val) {
      provForm.model = val
    }
  }
)

// DeepSeek model list fetcher — shared by watcher, applyPreset, and manual refresh
async function doFetchDeepSeekModels() {
  const key = provForm.apiKey?.trim()
  if (!key || provForm.type !== 'deepseek') return
  fetchingDS.value = true; dsFetchError.value = ''
  try {
    const models = await providersApi.fetchDeepSeekModels(key)
    if (models && models.length) {
      const names = models.map((m: any) => m.id || m.name || '').filter(Boolean)
      if (names.length) {
        provForm.models = names
        if (!provForm.model || !names.includes(provForm.model)) {
          provForm.model = names[0]
        }
        t('info', 'DeepSeek 模型列表', '已获取 ' + names.length + ' 个模型')
      }
    } else {
      dsFetchError.value = 'API 未返回模型'
    }
  } catch (e: any) {
    dsFetchError.value = e?.message || '获取失败'
  }
  fetchingDS.value = false
}

// Watch DeepSeek preset: auto-fetch model list when API key is filled
watch(
  () => [provForm.type, provForm.apiKey] as const,
  ([typ, key]) => {
    if (typ === 'deepseek' && key && key.length > 10) {
      doFetchDeepSeekModels()
    }
  }
)

// Watch modelValue to initialise form when dialog opens
watch(
  () => props.modelValue,
  (visible) => {
    if (visible) {
      if (props.editingProv) {
        populateFromProvider(props.editingProv)
      } else {
        resetForm()
        if (props.presets && props.presets.length) applyPreset(props.presets[0])
      }
    }
  }
)

// ── Helpers ──
function isIconImage(icon: any) {
  return icon && typeof icon === 'string' && icon.startsWith('data:image/')
}

function fmtCtx(tokens: number) {
  if (!tokens) return '?'
  if (tokens >= 1000) return Math.round(tokens / 1000) + 'K'
  return tokens + ''
}

function defaultEndpoint(fmt: string) {
  if (fmt === 'anthropic') return 'https://api.deepseek.com/anthropic'
  return 'https://api.openai.com/v1'
}

function extractPort(endpoint: string) {
  if (!endpoint) return 0
  const m = endpoint.match(/:(\d+)/)
  return m ? parseInt(m[1]) : 0
}

// ── Form lifecycle ──
function resetForm() {
  provForm.name = ''
  provForm.icon = ''
  provForm.notes = ''
  provForm.website = ''
  provForm.type = 'openai'
  provForm.apiFormat = 'openai'
  provForm.endpoint = ''
  provForm.apiKey = ''
  provForm.model = ''
  provForm.modelCustom = ''
  provForm.models = []
  provForm.enableJSONOutput = true
  delete provForm.modelCapabilities
  provMsg.value = ''
  provOk.value = false
  showKey.value = false
  showIconDropdown.value = false
  detectingLocal.value = false
  probing.value = false
  fetchingDS.value = false
  dsFetchError.value = ''
  if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
  _detectCancelId++
}

function populateFromProvider(p: any) {
  const caps: Record<string, any> = {}
  if (p.modelCapabilities) {
    Object.keys(p.modelCapabilities).forEach(k => { caps[k] = Object.assign({}, p.modelCapabilities[k]) })
  }
  provForm.name = p.name
  provForm.icon = p.icon || ''
  provForm.notes = p.notes || ''
  provForm.website = p.website || ''
  provForm.type = p.type
  provForm.apiFormat = p.apiFormat || 'openai'
  provForm.endpoint = p.endpoint
  provForm.apiKey = p.apiKey
  provForm.models = [...(p.models || [])]
  // Preserve custom model name when model is not in the preset model list
  if (p.model && provForm.models.length > 0 && !provForm.models.includes(p.model)) {
    provForm.model = '__custom__'
    provForm.modelCustom = p.model
  } else if (p.model && !provForm.models.length) {
    provForm.model = p.model
    provForm.modelCustom = p.model
  } else {
    provForm.model = p.model || ''
    provForm.modelCustom = ''
  }
  provForm.enableJSONOutput = p.enableJSONOutput !== false
  provForm.modelCapabilities = caps
  provMsg.value = ''
  provOk.value = false
  showKey.value = false
  showIconDropdown.value = false
  detectingLocal.value = false
  probing.value = false
  if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
  _detectCancelId++
}

// ── Actions ──
function close() {
  emit('update:modelValue', false)
  probing.value = false
  detectingLocal.value = false
  if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
  _detectCancelId++
  provMsg.value = ''
  showKey.value = false
  showIconDropdown.value = false
}

function selectIcon(ic: string) {
  provForm.icon = ic
  showIconDropdown.value = false
}

function onIconFileSelected(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > 128 * 1024) { t('error', '图片过大', '图标图片请控制在 128KB 以内'); return }
  const reader = new FileReader()
  reader.onload = () => {
    provForm.icon = reader.result as string
    showIconDropdown.value = false
  }
  reader.readAsDataURL(file)
  ;(e.target as HTMLInputElement).value = ''
}

function applyPreset(pr: any) {
  provForm.type = pr.type
  provForm.name = pr.name
  provForm.icon = pr.icon || ''
  provForm.apiFormat = pr.apiFormat || 'openai'
  provForm.endpoint = pr.endpoint || ''
  provForm.models = [...(pr.models || [])]
  provForm.model = pr.models && pr.models.length ? pr.models[0] : ''
  provForm.modelCustom = ''
  if (pr.type === 'ollama' || pr.type === 'llamacpp') {
    nextTick(() => detectLocalModels())
  }
}

async function saveProvider() {
  if (saving.value) return
  const model = provForm.model === '__custom__' ? provForm.modelCustom : provForm.model
  const data = {
    name: provForm.name.trim(),
    icon: provForm.icon.trim(),
    notes: provForm.notes.trim(),
    website: provForm.website.trim(),
    type: provForm.type,
    apiFormat: provForm.apiFormat,
    endpoint: provForm.endpoint.trim(),
    apiKey: isLocalPreset.value ? '' : provForm.apiKey.trim(),
    model: model || provForm.modelCustom || '',
    models: provForm.models || [],
    modelCapabilities: provForm.modelCapabilities || {},
    enableJSONOutput: provForm.enableJSONOutput !== false,
    enabled: props.editingProv ? props.editingProv.enabled : true,
  }
  saving.value = true
  try {
    if (props.editingProv) {
      await providersApi.update(props.editingProv.id, data)
    } else {
      await providersApi.create(data)
    }
    emit('update:modelValue', false)
    emit('saved')
    t('success', props.editingProv ? '已更新' : '已创建', data.name)
  } catch (e: any) {
    provMsg.value = (props.editingProv ? '保存' : '创建') + '失败: ' + (e.message || e)
    provOk.value = false
  } finally {
    saving.value = false
  }
}

async function saveProviderSilent() {
  try {
    const wf = props.editingProv
    if (!wf) return
    const model = provForm.model === '__custom__' ? provForm.modelCustom : provForm.model
    const data = {
      name: provForm.name.trim(),
      icon: provForm.icon.trim(),
      notes: provForm.notes.trim(),
      website: provForm.website.trim(),
      type: provForm.type,
      apiFormat: provForm.apiFormat,
      endpoint: provForm.endpoint.trim(),
      apiKey: isLocalPreset.value ? '' : provForm.apiKey.trim(),
      model: model || provForm.modelCustom || '',
      models: provForm.models || [],
      modelCapabilities: provForm.modelCapabilities || {},
      enableJSONOutput: provForm.enableJSONOutput !== false,
      enabled: wf.enabled,
    }
    await providersApi.update(wf.id, data)
    emit('silentSaved') // refresh parent list without closing dialog
  } catch (e: any) {
    console.error('[ProviderDialog] silent save failed:', e?.message || e)
  }
}

function cancelProbe() {
  if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
  probing.value = false
  provMsg.value = '已取消探测'
  provOk.value = false
}

async function probeCapabilities() {
  const model = provForm.model === '__custom__' ? provForm.modelCustom : provForm.model
  if (!model) { t('warning', '请先选择/输入模型'); return }
  if (!provForm.endpoint) { t('warning', '请先填写请求地址'); return }

  if (_probeTimer) { clearTimeout(_probeTimer) }
  probing.value = true; provMsg.value = ''; provOk.value = false
  provMsg.value = '🔬 正在探测 ' + model + ' …'
  const token = ++_detectCancelId
  _probeTimer = setTimeout(() => {
    if (probing.value) { probing.value = false; provMsg.value = '探测超时（>60s），已自动取消'; provOk.value = false }
  }, 60000)
  try {
    const apiKey = isLocalPreset.value ? '' : (provForm.apiKey || '')
    const cap = await providersApi.probeCapability(
      provForm.endpoint, apiKey, model, provForm.apiFormat)
    if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
    if (_detectCancelId !== token) { probing.value = false; return }
    if (!provForm.modelCapabilities) provForm.modelCapabilities = {}
    // Merge with existing capability — preserve manually-configured context
    // when the probe returns 0 (llama.cpp / Ollama don't expose context via API).
    const existing = provForm.modelCapabilities[model]
    if (existing && existing.maxContextTokens > 0 && (!cap.maxContextTokens || cap.maxContextTokens === 0)) {
      cap.maxContextTokens = existing.maxContextTokens
    }
    provForm.modelCapabilities[model] = cap
    if (props.editingProv) await saveProviderSilent()
    // Look up the model profile for additional context (label, tuning params)
    const profile = getModelProfile(provForm.name, model)
    const ctxDisplay = cap.maxContextTokens || profile.contextWindow
    const tags = [
      cap.supportsVision ? '👁多模态' : '✗视觉',
      cap.supportsTools ? '🔧工具' : '✗工具',
      cap.supportsReasoning ? '🧠推理' : '✗推理',
      cap.supportsStreaming ? '⇢流式' : '✗流式',
      cap.supportsJSON ? '{}JSON' : '✗JSON',
      cap.supportsFIM ? '⟷FIM' : '✗FIM',
      fmtCtx(ctxDisplay) + '上下文'
    ]
    const profileHint = profile.label !== 'Unknown Model (conservative fallback)'
      ? ` [${profile.label}]` : ''
    provMsg.value = model + profileHint + ' 能力: ' + tags.join(' ')
    provOk.value = true
  } catch (e: any) {
    if (_probeTimer) { clearTimeout(_probeTimer); _probeTimer = null }
    if (_detectCancelId !== token) { probing.value = false; return }
    provMsg.value = '探测失败: ' + (e.message || e)
    provOk.value = false
  }
  probing.value = false
}

async function detectLocalModels() {
  detectingLocal.value = true
  const token = ++_detectCancelId
  const isOllama = provForm.type === 'ollama'
  const isLlamaCpp = provForm.type === 'llamacpp'
  const defaultPort = isOllama ? 11434 : (isLlamaCpp ? 8082 : 0)
  const altPorts = isOllama ? [11434, 11435, 8082] : (isLlamaCpp ? [8082, 8080, 8081, 11434] : [])

  let models: any[] | null = null; let usedPort: number | null = null
  const portsToTry = ([extractPort(provForm.endpoint) || defaultPort, ...altPorts] as number[])
    .filter((p, i, arr) => p > 0 && arr.indexOf(p) === i)

  for (const port of portsToTry) {
    if (_detectCancelId !== token) { detectingLocal.value = false; return }
    const base = isOllama ? 'http://127.0.0.1:' + port : 'http://127.0.0.1:' + port + '/v1'
    try {
      if (isOllama) {
        models = await providersApi.fetchOllamaModels(base)
      } else {
        models = await providersApi.fetchOpenAIModels(base, '')
      }
      if (models && models.length) { usedPort = port; break }
    } catch (_) {}
  }

  if (_detectCancelId !== token) { detectingLocal.value = false; return }
  if (models && models.length) {
    const names = models.map((m: any) => m.name)
    provForm.models = names
    const oldCaps = provForm.modelCapabilities || {}
    const merged: Record<string, any> = {}
    names.forEach((n: string) => { merged[n] = oldCaps[n] || null })
    provForm.modelCapabilities = merged
    if (names.length && !provForm.model) provForm.model = names[0]

    if (usedPort && usedPort !== extractPort(provForm.endpoint)) {
      const newEndpoint = isOllama
        ? 'http://127.0.0.1:' + usedPort + '/v1'
        : 'http://127.0.0.1:' + usedPort + '/v1'
      provForm.endpoint = newEndpoint
      t('info', '端口已更新', '检测到服务在端口 ' + usedPort)
    }
    t('success', '检测到 ' + names.length + ' 个模型', names.slice(0, 5).join(', ') + (names.length > 5 ? '…' : ''))
  } else {
    const hint = isOllama
      ? 'Ollama 默认端口 11434，请确认 ollama serve 已启动。已尝试端口: ' + portsToTry.join(', ')
      : 'llama.cpp 默认端口 8082，请确认 llama-server 已启动。已尝试端口: ' + portsToTry.join(', ')
    t('warning', '未检测到模型', hint)
  }
  detectingLocal.value = false
}
</script>

<style scoped>
.overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  backdrop-filter: blur(4px); display: flex; align-items: center;
  justify-content: center; z-index: 100;
}

.msg {
  padding: 8px 12px; border-radius: var(--radius-sm);
  font-size: 12px; margin-top: 10px;
}
.msg-ok { background: var(--success-dim); color: var(--success); }
.msg-err { background: var(--danger-dim); color: var(--danger); }

/* Provider dialog */
.prov-dialog {
  width: 640px; max-width: 90vw; max-height: 85vh; overflow-y: auto;
  padding: 32px 36px 28px;
}
.prov-dialog-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px;
}
.prov-dialog-head h3 { font-size: 18px; font-weight: 600; margin: 0; }
.prov-dialog-close {
  width: 28px; height: 28px; border: none; border-radius: 6px;
  background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}
.prov-dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }

/* Preset section */
.preset-section { margin-bottom: 20px; }
.preset-label {
  display: block; font-size: 12px; font-weight: 500;
  color: var(--text-tertiary); margin-bottom: 10px;
}
.preset-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.preset-btn {
  display: flex; flex-direction: column; align-items: center; gap: 6px;
  padding: 14px 18px; min-width: 90px;
  border: 1px solid var(--border-soft); border-radius: 10px;
  background: var(--bg-elevated); color: var(--text-secondary);
  font-size: 12px; cursor: pointer; transition: all var(--transition);
}
.preset-btn:hover {
  border-color: var(--accent); color: var(--text-primary);
  transform: translateY(-1px);
}
.preset-btn.preset-sel {
  background: var(--accent-dim); border-color: var(--accent); color: var(--accent);
}
.preset-btn-icon { font-size: 20px; }
.preset-btn-name { font-weight: 500; }

.prov-divider {
  height: 1px; background: var(--border-subtle); margin-bottom: 20px;
}

/* Form */
.prov-form { display: flex; flex-direction: column; gap: 18px; }
.prov-form-row { display: flex; flex-direction: column; gap: 6px; }
.prov-label {
  font-size: 12px; font-weight: 500; color: var(--text-secondary);
}
.prov-field {
  width: 100%; padding: 9px 12px; box-sizing: border-box;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary);
  font-size: 13px; font-family: var(--font-mono); outline: none;
}
.prov-field:focus { border-color: var(--accent); }
.prov-field-disabled {
  opacity: 0.45; cursor: not-allowed; background: var(--bg-inset, rgba(0,0,0,0.15));
  color: var(--text-tertiary); font-style: italic; border-style: dashed;
}
.prov-field-sm { max-width: 260px; }
.prov-field-row { display: flex; gap: 8px; align-items: center; }
.prov-eye {
  width: 30px; height: 30px; border: 1px solid var(--border-soft); border-radius: 6px;
  background: var(--bg-elevated); color: var(--text-tertiary); font-size: 14px;
  cursor: pointer; display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; padding: 0; transition: all 0.15s;
}
.prov-eye:hover { border-color: var(--accent); color: var(--text-primary); }

/* Icon dropdown */
.prov-icon-area { position: relative; }
.prov-icon-trigger {
  display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px;
  border: 1px solid var(--border-soft); border-radius: 6px;
  background: var(--bg-elevated); cursor: pointer; user-select: none;
  transition: border-color var(--transition);
}
.prov-icon-trigger:hover { border-color: var(--accent); }
.prov-icon-preview-char {
  font-size: 18px; width: 24px; text-align: center; line-height: 1;
}
.prov-icon-preview-img {
  width: 24px; height: 24px; border-radius: 4px; object-fit: contain;
}
.prov-icon-arrow {
  font-size: 10px; color: var(--text-tertiary);
  transition: transform 0.15s;
}
.prov-icon-dropdown {
  position: absolute; left: 0; top: 100%; z-index: 50;
  margin-top: 6px; padding: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  min-width: 200px;
}
.prov-icon-dropdown-palette {
  display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px;
}
.prov-icon-btn {
  width: 32px; height: 32px; border-radius: 6px;
  border: 1px solid var(--border-soft); background: var(--bg-elevated);
  font-size: 16px; cursor: pointer; display: flex; align-items: center; justify-content: center;
  transition: all var(--transition); color: var(--text-secondary);
}
.prov-icon-btn:hover { border-color: var(--accent); color: var(--text-primary); }
.prov-icon-btn.prov-icon-sel {
  background: var(--accent-dim); border-color: var(--accent);
}
.prov-icon-dropdown-divider {
  height: 1px; background: var(--border-soft); margin: 10px 0;
}
.prov-icon-upload {
  display: flex; align-items: center; gap: 6px; padding: 7px 10px;
  border-radius: 6px; font-size: 12px; color: var(--text-secondary);
  cursor: pointer; transition: all 0.12s;
}
.prov-icon-upload:hover { background: var(--bg-hover); color: var(--text-primary); }
.prov-icon-file-input { display: none; }
.prov-hint { font-size: 11px; color: var(--text-tertiary); margin-top: 2px; }

/* Model capability display */
.prov-caps-display {
  display: flex; gap: 6px; flex-wrap: wrap; overflow: visible;
}
.prov-cap {
  display: inline-flex; align-items: center; gap: 3px;
  padding: 4px 10px; border-radius: 6px;
  font-size: 11px; color: var(--text-tertiary);
  background: var(--bg-inset, rgba(0,0,0,0.2));
  border: 1px solid var(--border-subtle);
  transition: all 0.15s;
}
.prov-cap.cap-on {
  color: var(--text-primary); border-color: var(--border-soft);
}
.prov-cap { cursor: help; }
.prov-cap.cap-on:hover { border-color: var(--accent); }
.ctx-cap { font-family: var(--font-mono); }

/* Provider header (icon + name/notes/website) */
.prov-header { display: flex; gap: 20px; align-items: center; }
.prov-icon-trigger-lg {
  display: flex; align-items: center; justify-content: center; gap: 4px;
  width: 80px; height: 80px; padding: 0;
  border: 1px solid var(--border-soft); border-radius: var(--radius-lg);
  background: var(--bg-elevated); cursor: pointer; user-select: none;
  transition: border-color var(--transition); flex-shrink: 0;
}
.prov-icon-trigger-lg:hover { border-color: var(--accent); }
.prov-icon-preview-char-lg { font-size: 38px; line-height: 1; }
.prov-icon-preview-img-lg {
  width: 58px; height: 58px; border-radius: 10px; object-fit: contain;
}
.prov-header-fields {
  flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 8px;
}
.prov-header-field { display: flex; align-items: center; gap: 8px; }
.prov-label-inline { width: 40px; flex-shrink: 0; font-size: 12px; }

/* Footer */
.prov-dialog-foot {
  margin-top: 24px; display: flex; flex-direction: column; gap: 10px;
}
.prov-dialog-actions { display: flex; justify-content: flex-end; gap: 10px; }

/* Shared buttons (scoped copies so the dialog is self-contained) */
.btn {
  padding: 6px 12px; border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm); background: var(--bg-elevated);
  color: var(--text-primary); font-size: 12px; cursor: pointer;
}
.btn:hover { background: var(--bg-hover); }
.btn-xs {
  padding: 2px 7px; font-size: 11px; line-height: 1.3;
  border: 1px solid var(--border-soft); border-radius: 4px;
  background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer;
}
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-primary {
  background: var(--accent); border-color: var(--accent); color: #fff;
}
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
.btn-go { color: var(--success); border-color: rgba(48,209,88,0.3); }
.btn-go:hover { background: rgba(48,209,88,0.1); }
.btn-del { color: var(--danger); border-color: rgba(255,69,58,0.3); }
.btn-del:hover { background: rgba(255,69,58,0.1); }

.glass-panel {
  background: var(--bg-glass); backdrop-filter: blur(12px);
  border: 1px solid var(--border-glass); border-radius: var(--radius-lg);
}

/* ── JSON toggle row ── */
.prov-toggle-row {
  display: flex; align-items: center; gap: 12px;
}
.prov-toggle-label {
  font-size: 13px; font-weight: 500; color: var(--text-primary);
}
.prov-toggle-hint {
  font-size: 11px; color: var(--text-tertiary); flex: 1;
}

/* ── Slide switch ── */
.slide-switch {
  position: relative; display: inline-block; width: 44px; height: 24px;
  flex-shrink: 0; cursor: pointer;
}
.slide-switch input { display: none; }
.slide-knob {
  position: absolute; inset: 0; border-radius: 12px;
  background: var(--bg-inset); border: 1px solid var(--border-soft);
  transition: all 0.22s ease;
}
.slide-knob::after {
  content: ''; position: absolute; top: 2px; left: 2px; width: 18px; height: 18px;
  border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,0.2);
  transition: transform 0.22s cubic-bezier(0.22, 0.61, 0.36, 1);
}
.slide-switch input:checked + .slide-knob {
  background: var(--accent); border-color: var(--accent);
}
.slide-switch input:checked + .slide-knob::after {
  transform: translateX(20px);
}
</style>
