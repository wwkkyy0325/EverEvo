<template>
  <div
    class="chat-panel"
    :class="{ 'chat-compact': compact, 'chat-drop-active': dragOver }"
    @dragover.prevent="onDragOver"
    @dragleave.prevent="onDragLeave"
    @drop.prevent="onDrop"
  >
    <!-- Drop overlay -->
    <div v-if="dragOver" class="chat-drop-overlay">
      <span class="chat-drop-icon">📂</span>
      <span class="chat-drop-text">释放文件以添加到对话</span>
      <span class="chat-drop-hint">支持 .txt, .md, .pdf, .csv, .json, .log…</span>
    </div>

    <!-- Header (hidden in compact mode — wrapper provides its own) -->
    <div v-if="!compact" class="chat-panel-header">
      <div class="chat-panel-header-left">
        <span class="chat-panel-header-dot"></span>
        <span class="chat-panel-header-model">{{ providerLabel }}</span>
      </div>
      <button class="chat-think-toggle" :class="{ active: chat.thinkMode }"
              @click="chat.thinkMode = !chat.thinkMode"
              :title="chat.thinkMode ? '思考模式已开启（英文草稿）' : '开启思考模式'">
        🧠
      </button>
    </div>

    <!-- Session bar -->
    <div class="chat-session-bar">
      <span class="chat-session-name" @click="onRenameSession()" :title="'点击重命名: ' + (currentSessionTitle || '新对话')">
        {{ currentSessionTitle || '新对话' }}
      </span>
      <div class="chat-session-spacer"></div>
      <button ref="historyBtnRef" class="chat-bar-btn" :class="{ active: showHistory }" @click.stop="openHistory" title="历史记录">📋</button>
      <button class="chat-bar-btn" @click="chat.createSession()" :disabled="chat.busy" title="新对话">＋</button>
    </div>

    <!-- Popovers (teleported to body to escape overflow:hidden) -->
    <Teleport to="body">
      <!-- History -->
      <div v-if="showHistory" class="chat-popover-backdrop" @click="showHistory = false"></div>
      <Transition name="popover-fade">
        <div v-if="showHistory" class="chat-history-popover" :style="historyPopoverStyle">
          <div class="chat-popover-head">
            <span>历史记录</span>
            <button class="chat-bar-btn" @click="showHistory = false" title="关闭" style="width:22px;height:22px;font-size:12px;">✕</button>
          </div>
          <div class="chat-history-list">
            <div v-for="s in chat.sessions" :key="s.id" class="chat-history-item"
                 :class="{ active: s.id === chat.currentSessionId }"
                 @click="onHistorySelect(s.id)">
              <span class="chat-history-title">{{ s.title || '新对话' }}</span>
              <span class="chat-history-time">{{ fmtTime(s.updatedAt) }}</span>
              <button class="chat-history-del" @click.stop="onHistoryDelete(s.id)" title="删除">✕</button>
            </div>
          </div>
        </div>
      </Transition>
      <!-- Settings -->
      <div v-if="showSettings" class="chat-popover-backdrop" @click="showSettings = false"></div>
      <Transition name="popover-fade">
        <div v-if="showSettings" class="chat-settings-popover" :style="settingsPopoverStyle">
          <div class="chat-popover-head">
            <span>设置</span>
            <button class="chat-bar-btn" @click="showSettings = false" title="关闭" style="width:22px;height:22px;font-size:12px;">✕</button>
          </div>
          <div class="chat-settings-body">
            <div class="chat-setting-row">
              <span class="chat-setting-label">Agent</span>
              <select class="chat-agent-select" v-model="agentModel" :disabled="chat.busy">
                <option value="">默认主 Agent</option>
                <option v-for="a in chat.visibleAgents" :key="a.id" :value="a.id">{{ a.name }}</option>
              </select>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Messages -->
    <div class="chat-panel-box" ref="chatBox" @scroll="onScroll">
      <!-- Load-earlier indicator -->
      <!-- Load-earlier indicator: hidden while compressed history is partially revealed,
           to avoid clashing with the boundary button. Shows when no compression is
           active, or after all compressed history has been expanded. -->
      <div v-if="chat.hasMoreMessages && (chat.compressionMarker < 0 || chat.historyVisibleCount >= chat.historyMessages.length)" class="chat-load-earlier">
        <span v-if="loadingMore" class="chat-loading-spin">◌</span>
        <span v-else>↑ 上滑加载更早的对话（{{ chat.messages.length }} / {{ chat.totalMessageCount }}）</span>
      </div>

      <div v-if="!chat.messages.length" class="chat-empty">
        <span>💬</span>
        <p>用自然语言操作 EverEvo</p>
        <p class="chat-empty-hint">拖拽文件到此处，直接让 AI 帮你分析</p>
        <div class="chat-sug">
          <button v-for="s in chat.suggestedPrompts" :key="s" class="btn btn-sm" @click="chat.sendMessage(s)">{{ s }}</button>
        </div>
      </div>

      <template v-for="(m, i) in chat.messages" :key="m.id || i">
        <!-- History messages (before marker): progressively revealed as user scrolls up -->
        <template v-if="chat.compressionMarker >= 0 && i < chat.compressionMarker && i < chat.compressionMarker - chat.historyVisibleCount"></template>
        <!-- Toggle button + summary at the marker boundary -->
        <template v-else-if="chat.compressionMarker >= 0 && i === chat.compressionMarker">
          <button class="chat-history-boundary-btn"
                  @click="onToggleHistory">
            {{ chat.historyVisibleCount > 0
              ? '▾ 收起压缩前历史 (' + chat.historyRoundCount + ' 轮, ' + chat.historyVisibleCount + '/' + chat.historyMessages.length + ' 条可见)'
              : '▸ 查看压缩前对话 (' + chat.historyRoundCount + ' 轮, ' + chat.historyMessages.length + ' 条) — 点击或上滑加载' }}
          </button>
          <!-- Compression summary card — teal accent, structured sections -->
          <div class="chat-meta-block chat-meta-compress">
            <div class="chat-meta-head"
                 @click="chat.expandedTool[i + '-compress'] = !chat.expandedTool[i + '-compress']">
              <span class="chat-meta-label">上下文压缩</span>
              <span class="chat-meta-badge">{{ chat.compressedRoundCount }} 轮</span>
              <span class="chat-meta-toggle">{{ chat.expandedTool[i + '-compress'] === true ? '收起 ▴' : '展开 ▾' }}</span>
            </div>
            <div v-if="chat.expandedTool[i + '-compress'] === true" class="chat-meta-body chat-compress-body"
                 v-html="chat.renderCompression(m.content.replace(/^[━⚠].*\n*/, ''))"></div>
          </div>
        </template>
        <template v-else>
        <!-- User message — box with blue left border -->
        <div v-if="m.role === 'user'" class="msg-row chat-user-msg" :class="{ 'chat-history-msg': i < chat.compressionMarker }">
          <div class="chat-user-text" v-html="chat.chatRender(m.content)"></div>
          <!-- File cards attached to this message -->
          <div v-if="chat.messageFiles[i]?.length" class="chat-file-cards">
            <div v-for="f in chat.messageFiles[i]" :key="f.name" class="chat-file-card"
                 :title="f.path" @click="openFileDir(f.path)">
              <span class="chat-file-card-icon">{{ fileIcon(f.type) }}</span>
              <div class="chat-file-card-info">
                <span class="chat-file-card-name">{{ f.name }}</span>
                <span class="chat-file-card-meta">{{ f.type.toUpperCase() }} · {{ fmtSize(f.size) }}
                  <span v-if="f.isScanned" class="chat-file-card-badge">扫描件</span>
                </span>
              </div>
              <button class="chat-file-card-btn" @click.stop="openFileDir(f.path)" title="打开文件所在目录">📂</button>
            </div>
          </div>
        </div>

        <!-- Other system message (warning, truncation notice, etc.) -->
        <div v-else-if="m.role === 'system'"
             class="msg-row"
             style="text-align:center;padding:6px 0;color:var(--text-tertiary);font-size:11px;opacity:0.7;">
          <span v-html="chat.chatRender(m.content)"></span>
        </div>

        <!-- Assistant message — bare, no bubble -->
        <div v-else-if="m.role === 'assistant'" class="msg-row chat-asst-msg">
          <!-- Reasoning — collapsible full-width block -->
          <div v-if="chat.reasoningContent[i]" class="chat-meta-block chat-meta-think">
            <div class="chat-meta-head" @click="chat.expandedTool[i + '-reasoning'] = !chat.expandedTool[i + '-reasoning']">
              <span class="chat-meta-label">思考过程</span>
              <span class="chat-meta-toggle">{{ chat.expandedTool[i + '-reasoning'] ? '收起 ▴' : '展开 ▾' }}</span>
            </div>
            <div v-if="chat.expandedTool[i + '-reasoning']" class="chat-meta-body">
              {{ chat.reasoningContent[i] }}
            </div>
          </div>

          <div class="chat-asst-text" v-html="chat.chatRender(m.content)"></div>

          <!-- Paradigm footnote — subtle marker at bottom-right -->
          <div v-if="paradigmForMsg(i)" class="paradigm-footnote">
            【{{ paradigmForMsg(i)!.name }}】
          </div>

          <!-- Tool calls — terminal-style blocks with live execution state -->
          <div v-for="(tc, j) in m.toolCalls" :key="tc.name"
               class="chat-meta-block chat-meta-tool"
               :class="toolBlockClass(m, j)">
            <div class="chat-meta-head" @click="toolResultReady(m, j) && (chat.expandedTool[i + '-' + j] = !chat.expandedTool[i + '-' + j])">
              <span class="chat-meta-label terminal-label">
                <span v-if="!toolResultReady(m, j)" class="terminal-spin">◐</span>
                <span v-else-if="m.toolResults?.[j]?.result?.success === false">✗</span>
                <span v-else>✓</span>
              </span>
              <span class="chat-meta-tool-name">{{ tc.name }}</span>
              <span v-if="m.toolResults?.[j]?.args && Object.keys(m.toolResults[j].args).length" class="terminal-args-inline">{{ fmtToolArgsBrief(m.toolResults[j].args) }}</span>
              <span v-if="!toolResultReady(m, j)" class="terminal-status terminal-status-busy">{{ elapsedTool(m, j) }}</span>
              <span v-else-if="m.toolResults?.[j]?.result?.success === false" class="terminal-status terminal-status-err">失败</span>
              <span v-else class="terminal-status terminal-status-ok">完成</span>
              <span v-if="toolResultReady(m, j)" class="chat-meta-toggle">{{ chat.expandedTool[i + '-' + j] ? '收起 ▴' : '展开 ▾' }}</span>
            </div>
            <div v-if="toolResultReady(m, j) && chat.expandedTool[i + '-' + j] && m.toolResults?.[j]" class="terminal-body">
              <div class="terminal-line terminal-args"><span class="terminal-prompt">$</span> {{ tc.name }} <span class="terminal-args-json">{{ fmtToolArgs(m.toolResults[j].args) }}</span></div>
              <div class="terminal-line terminal-output" :class="{ 'terminal-error': m.toolResults[j].result?.success === false }">
                <pre>{{ JSON.stringify(m.toolResults[j].result, null, 2) }}</pre>
              </div>
            </div>
          </div>
        </div>
        </template><!-- end v-else: only show messages after compression marker -->
      </template>

      <!-- Thinking / tool-in-progress indicator -->
      <div v-if="chat.busy" class="chat-busy-row">
        <div class="chat-busy-anim">
          <span class="chat-busy-dot"></span>
          <span class="chat-busy-dot"></span>
          <span class="chat-busy-dot"></span>
        </div>
        <span class="chat-busy-msg">{{ busyMsg }}</span>
      </div>
    </div>

    <!-- File chips — attached files waiting to be sent -->
    <div v-if="chat.pendingFiles.length" class="chat-files-row">
      <div v-for="f in chat.pendingFiles" :key="f.name" class="chat-file-chip" :title="f.name + ' (' + fmtSize(f.size) + ')'">
        <span class="chat-file-icon">{{ fileIcon(f.type) }}</span>
        <span class="chat-file-name">{{ f.name }}</span>
        <span class="chat-file-size">{{ fmtSize(f.size) }}</span>
        <button class="chat-file-remove" @click="chat.removeFile(f.name)" :disabled="chat.busy" title="移除">✕</button>
      </div>
    </div>

    <!-- Async task panel -->
    <Transition name="fade">
      <AsyncPanel v-if="showAsyncPanel" @resume="onResumeAsync" style="margin-bottom: 8px;" />
    </Transition>
    <!-- Input area (textarea + bottom bar in rounded border box) -->
    <div class="chat-input-section" :class="{ 'effort-max': chat.thinkEffort === 'max' }">
    <div class="chat-input-area">
      <div class="chat-textarea-wrap" :data-replicated-value="chat.inputText">
        <textarea
          ref="textareaRef"
          v-model="chat.inputText"
          class="chat-panel-textarea"
          :placeholder="chat.pendingFiles.length ? '输入问题（可选，留空则让 AI 自行分析文件）…' : '输入指令，或拖拽文件到此处…'"
          @keydown.enter.exact.prevent="doSend"
          @keydown.shift.enter.prevent="chat.inputText += '\n'"
          :disabled="chat.busy"
          @paste="onPaste"
        ></textarea>
      </div>
    </div>

    <!-- Bottom bar -->
    <div class="chat-bottom-bar">
      <input ref="fileInputRef" type="file" multiple style="display:none" @change="onFileSelect" />
      <button class="chat-bar-btn" @click="triggerUpload" title="上传文件" :disabled="chat.busy">＋</button>
      <button ref="settingsBtnRef" class="chat-bar-btn" :class="{ active: showSettings }" @click.stop="showSettings = !showSettings" title="设置">⚙</button>
      <!-- Context bar + compress -->
      <div class="chat-ctx-wrap">
        <div class="chat-ctx-bar" :class="'ctx-' + chat.contextLevel"
             :title="'System: ' + fmtTokens(chat.contextBreakdown.system) + ' | Tools: ' + fmtTokens(chat.contextBreakdown.tools) + ' | Memory: ' + fmtTokens(chat.contextBreakdown.memory) + ' | Messages: ' + fmtTokens(chat.contextBreakdown.messages) + ' | Reserved: ' + fmtTokens(chat.contextBreakdown.reserved) + '\nUsable: ' + fmtTokens(chat.contextBreakdown.usable) + ' / Model limit: ' + fmtTokens(chat.contextLimit) + ' (' + chat.contextLimitPct + '%)'">
          <div class="chat-ctx-fill" :style="{ width: chat.contextBarPct + '%' }"></div>
        </div>
        <span class="chat-ctx-label" :class="'ctx-label-' + chat.contextLevel">{{ chat.contextPct }}%</span>
      </div>
      <button class="chat-bar-btn" :class="{ active: compressing }"
              @click="runFullPipeline" :disabled="compressing || chat.busy"
              :title="compressing ? '压缩中…' : '压缩上下文 + 全处理管线'">
        <span v-if="compressing" class="chat-spin-icon">◌</span>
        <span v-else style="font-size:12px;">↻</span>
      </button>
      <ModeSwitcher />
      <div class="chat-bar-spacer"></div>
      <!-- async task badge -->
      <button v-if="asyncStore.tasks.length" class="chat-bar-btn"
              :class="{ active: showAsyncPanel }" @click="showAsyncPanel = !showAsyncPanel"
              :title="'后台任务: ' + asyncStore.activeCount + ' 进行中'">
        ⟳<span v-if="asyncStore.activeCount" style="font-size:10px;color:#e0c040;">{{ asyncStore.activeCount }}</span>
      </button>
      <span class="chat-think-label">{{ thinkLevelLabel }}</span>
      <div class="chat-think-dots" ref="thinkDotsRef"
           @mousedown="onThinkDragStart" @mousemove="onThinkDragMove" @mouseup="onThinkDragEnd" @mouseleave="onThinkDragEnd">
        <span v-for="(c, i) in THINK_COLORS" :key="i" class="chat-think-dot"
              :class="{ active: i === thinkLevel }"
              :style="{ background: c }"
              :title="THINK_LEVELS[i]"
              @mousedown.stop="setThinkLevel(String(i))"></span>
      </div>
      <button class="chat-bar-btn chat-send-btn" @click="chat.busy ? stopGeneration() : doSend()"
              :disabled="!chat.busy && !chat.inputText.trim() && !chat.pendingFiles.length"
              :title="chat.busy ? '停止生成' : '发送消息'">
        <span v-if="chat.busy" class="chat-stop-icon">⬛</span>
        <span v-else class="chat-send-icon">↑</span>
      </button>
    </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useChatStore, type PendingFile } from '../stores/chatStore'
import { useDataChanged } from '../composables/useDataChanged'
import { knowledgeApi } from '../api/knowledge'
import { memoryApi } from '../api/memory'
import { parseParadigmMarker } from '../utils/paradigm'
import { fmtSize } from '../utils/formatters'
import ModeSwitcher from "./ModeSwitcher.vue"
import AsyncPanel from './AsyncPanel.vue'
import { useAsyncStore, type AsyncTask } from '../stores/asyncStore'
import { useToast } from '../composables/useToast'

defineProps<{ compact?: boolean }>()

const chat = useChatStore()

// Extract paradigm name from @paradigm{...} marker in assistant message
function paradigmForMsg(idx: number): { name: string } | null {
  const m = chat.messages[idx]
  if (!m || m.role !== 'assistant') return null
  const info = parseParadigmMarker(m.content)
  return info ? { name: info.name } : null
}
const toast = useToast()
const asyncStore = useAsyncStore()
const showAsyncPanel = ref(false)
const now = ref(Date.now()) // reactive clock — drives live elapsed-time display
const chatBox = ref<HTMLElement | null>(null)
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
const dragOver = ref(false)
const showSettings = ref(false)
const showHistory = ref(false)
const historyBtnRef = ref<HTMLElement | null>(null)
const settingsBtnRef = ref<HTMLElement | null>(null)
const historyPopoverStyle = computed(() => {
  if (!historyBtnRef.value) return {}
  const r = historyBtnRef.value.getBoundingClientRect()
  return { position: 'fixed' as const, top: (r.bottom + 4) + 'px', right: (window.innerWidth - r.right) + 'px' }
})
const settingsPopoverStyle = computed(() => {
  if (!settingsBtnRef.value) return { position: 'fixed' as const, top: '0', right: '0' }
  const r = settingsBtnRef.value.getBoundingClientRect()
  return { position: 'fixed' as const, bottom: (window.innerHeight - r.top + 4) + 'px', right: (window.innerWidth - r.right) + 'px' }
})
// Think level: 0=off 1=low 2=medium 3=high 4=max
const THINK_LEVELS = ['off', 'low', 'medium', 'high', 'max'] as const
const THINK_COLORS = ['#666', '#4a9', '#ca8', '#48f', '#f55']
const thinkLevel = ref(3) // default: high
function setThinkLevel(v: string) {
  const lv = parseInt(v)
  thinkLevel.value = lv
  if (lv === 0) {
    chat.thinkMode = false
    chat.thinkEffort = 'high'
  } else {
    chat.thinkMode = true
    chat.thinkEffort = lv >= 4 ? 'max' : 'high'
  }
}
const thinkLevelLabel = computed(() => THINK_LEVELS[thinkLevel.value])
const thinkDotsRef = ref<HTMLElement | null>(null)
let thinkDragging = false

function getThinkLevelFromX(clientX: number): number {
  if (!thinkDotsRef.value) return thinkLevel.value
  const dots = thinkDotsRef.value.querySelectorAll('.chat-think-dot')
  let best = thinkLevel.value
  let bestDist = Infinity
  dots.forEach((dot, i) => {
    const r = dot.getBoundingClientRect()
    const cx = r.left + r.width / 2
    const dist = Math.abs(clientX - cx)
    if (dist < bestDist) { bestDist = dist; best = i }
  })
  return best
}
function onThinkDragStart(e: MouseEvent) {
  thinkDragging = true
  setThinkLevel(String(getThinkLevelFromX(e.clientX)))
}
function onThinkDragMove(e: MouseEvent) {
  if (!thinkDragging) return
  setThinkLevel(String(getThinkLevelFromX(e.clientX)))
}
function onThinkDragEnd() {
  thinkDragging = false
}

let dragCounter = 0

const currentSessionTitle = computed(() => {
  const s = chat.sessions.find(x => x.id === chat.currentSessionId)
  return s?.title || ''
})

function fmtTime(ts: number) {
  if (!ts) return ''
  // ts is Unix milliseconds — if it looks like seconds (10 digits), convert.
  const ms = ts < 1e12 ? ts * 1000 : ts
  const d = new Date(ms)
  const now = Date.now()
  const diff = now - ms
  if (diff < 60000) return '刚刚'
  if (diff < 3600000) return Math.floor(diff / 60000) + '分钟前'
  if (diff < 86400000) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  if (diff < 604800000) return Math.floor(diff / 86400000) + '天前'
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

async function openHistory() {
  showHistory.value = !showHistory.value
  if (showHistory.value) {
    // Refresh sessions to get latest updated_at ordering.
    try { chat.sessions = await memoryApi.sessionList() || [] } catch (_) {}
  }
}

async function onHistorySelect(id: string) {
  showHistory.value = false
  await chat.selectSession(id)
}

async function onHistoryDelete(id: string) {
  await chat.deleteSession(id)
}

function triggerUpload() {
  fileInputRef.value?.click()
}

async function onFileSelect(e: Event) {
  const files = (e.target as HTMLInputElement).files
  if (files?.length) await processFiles(files);
  (e.target as HTMLInputElement).value = ''
}

function fmtTokens(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

const compressing = ref(false)
async function runFullPipeline() {
  if (compressing.value || chat.busy) return
  compressing.value = true
  const prevTokens = chat.contextTokens
  try {
    // force=true: user explicitly requested — bypass busy guard and run at any level.
    const result = await chat.maybeCompressContext(true)
    // Also sync with backend extraction pipeline.
    try { await window.go.app.App.MemoryForceExtract() } catch (_) {}

    const afterTokens = chat.contextTokens
    const saved = prevTokens - afterTokens
    switch (result) {
      case 'compressed':
        toast.show('success', '上下文已压缩', '从 ' + fmtTokens(prevTokens) + ' 降至 ' + fmtTokens(afterTokens) + '（节省 ' + fmtTokens(Math.max(0, saved)) + ' tokens）')
        break
      case 'truncated':
        toast.show('warning', '上下文已截断', '硬截断已触发 — 最早的消息已被移除（节省 ' + fmtTokens(Math.max(0, saved)) + ' tokens）')
        break
      case 'precompute':
        toast.show('info', '背景压缩已触发', '摘要将在后台生成，完成后自动应用（当前 ' + chat.contextPct + '%）')
        break
      case 'warned':
        toast.show('info', '已注入压缩提示', '已提醒 AI 简洁回复（当前 ' + chat.contextPct + '%）')
        break
      default:
        if (chat.contextPct < 50) {
          toast.show('info', '无需压缩', '上下文使用率仅 ' + chat.contextPct + '%，低于 50% 阈值')
        } else {
          toast.show('info', '全处理管线已触发', '上下文压缩 + 事实提取 + 图谱更新')
        }
    }
  } catch (e: any) {
    toast.show('error', '压缩失败', e?.message || String(e))
  } finally {
    compressing.value = false
  }
}

function stopGeneration() {
  chat.stopRequested = true
  if (chat.currentStreamId) {
    try { window.go.app.App.ChatStreamCancel(chat.currentStreamId) } catch (_) {}
  }
}

// Resume an async task: inject its result into the current conversation.
async function onResumeAsync(task: AsyncTask) {
  const go = (window as any).go?.app?.App
  if (!go?.ResumeAsyncTask || !chat.currentSessionId) return
  try {
    const res = await go.ResumeAsyncTask(task.id, chat.currentSessionId)
    if (res?.resumeHint) {
      // Append the resume hint as a system message to prompt the LLM.
      chat.inputText = res.resumeHint
      showAsyncPanel.value = false
    }
  } catch (e: any) {
    toast.show('error', '恢复任务失败', e?.message || String(e))
  }
}

// Two-way binding to the store's selected agent persona.
const agentModel = computed({
  get: () => chat.selectedAgentId,
  set: (v: string) => chat.selectAgent(v),
})
// Whether the user is parked at the bottom of the transcript. Auto-scroll
// only follows generation while this is true; scrolling up pauses it.
const atBottom = ref(true)

const providerLabel = computed(() => {
  const p = chat.activeProvider
  return p ? (p.name + ' / ' + p.model) : 'LLM Chat'
})

// ── File handling ──

function fileIcon(type: string): string {
  const map: Record<string, string> = { pdf: '📕', txt: '📄', md: '📝', csv: '📊', json: '📋', xml: '📋', yaml: '⚙️', yml: '⚙️', log: '📜' }
  return map[type] || '📎'
}

function onDragOver(e: DragEvent) {
  // Only activate when files are being dragged (not text selections)
  if (e.dataTransfer?.types.includes('Files')) {
    dragOver.value = true
  }
}

function onDragLeave(e: DragEvent) {
  // Counter-based to avoid flicker when dragging over child elements
  dragOver.value = false
}

async function onDrop(e: DragEvent) {
  dragOver.value = false
  const files = e.dataTransfer?.files
  if (!files?.length) return
  await processFiles(files)
}

async function onPaste(e: ClipboardEvent) {
  // Check for inline image data first (screenshots, copied images).
  // clipboardData.files does NOT include clipboard images on most platforms,
  // so we must inspect clipboardData.items for image/* MIME types.
  const items = e.clipboardData?.items
  if (items) {
    const imageBlobs: Array<{ blob: File; ext: string }> = []
    for (let i = 0; i < items.length; i++) {
      if (items[i].type.startsWith('image/')) {
        const blob = items[i].getAsFile()
        if (blob) {
          const ext = items[i].type.split('/')[1] || 'png'
          imageBlobs.push({ blob, ext })
        }
      }
    }
    if (imageBlobs.length > 0) {
      e.preventDefault()
      // Generate timestamp-based filenames for each pasted image.
      const ts = Date.now()
      for (let i = 0; i < imageBlobs.length; i++) {
        const { blob, ext } = imageBlobs[i]
        const file = new File([blob], `paste-${ts}-${i}.${ext}`, { type: blob.type })
        await uploadFile(file)
      }
      return
    }
  }

  // Fall through: clipboard files (e.g., copied file in Explorer).
  const files = e.clipboardData?.files
  if (!files?.length) return
  e.preventDefault()
  await processFiles(files)
}

async function processFiles(fileList: FileList) {
  for (let i = 0; i < fileList.length; i++) {
    const file = fileList[i]
    await uploadFile(file)
  }
}

/** Upload a single file (from drag-drop, paste, or clipboard image). */
async function uploadFile(file: File) {
  const ext = (file.name.split('.').pop() || '').toLowerCase()
  try {
    // All files go through SaveChatFile: save to disk → get path + preview
    // → LLM uses read_file tool to access content on demand.
    const buf = await file.arrayBuffer()
    const bytes = new Uint8Array(buf)
    let b64 = ''
    for (let j = 0; j < bytes.length; j++) {
      b64 += String.fromCharCode(bytes[j])
    }
    b64 = btoa(b64)

    const info = await knowledgeApi.saveChatFile(b64, file.name)
    if (info.isScanned) {
      toast.show('warning', '扫描件 PDF', file.name + ' 文字无法提取，上传路径供参考')
    }
    const isImage = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg'].includes(ext)
    if (isImage && !info.preview) {
      toast.show('info', '图片已上传', file.name + ' — AI 可通过 read_media_file 工具查看')
    }
    chat.addFile({
      name: file.name,
      type: ext || 'bin',
      size: file.size,
      path: info.path,
      preview: info.preview || '',
      isScanned: info.isScanned,
    })
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    toast.show('error', '上传失败', file.name + ': ' + msg)
  }
}

function doSend() {
  chat.sendMessage(chat.inputText)
}

/** Open the containing folder of a file path in the system file manager. */
function openFileDir(filePath: string) {
  try {
    // Wails runtime: open the parent directory
    const dir = filePath.replace(/[/\\][^/\\]*$/, '')
    window.runtime.BrowserOpenURL('file:///' + dir.replace(/\\/g, '/'))
  } catch (_) {
    // Fallback: show the path
    toast.show('info', '文件路径', filePath)
  }
}

// ── Tool call display helpers ──

function toolResultReady(m: any, j: number): boolean {
  return m.toolResults?.[j]?.result !== null && m.toolResults?.[j]?.result !== undefined
}
function elapsedTool(m: any, j: number): string {
  const startedAt = m.toolResults?.[j]?.startedAt as number | undefined
  if (!startedAt) return '执行中…'
  const sec = Math.round((now.value - startedAt) / 1000)
  if (sec < 60) return `执行中… ${sec}s`
  return `执行中… ${Math.floor(sec / 60)}m ${sec % 60}s`
}
function toolBlockClass(m: any, j: number) {
  const r = m.toolResults?.[j]?.result
  if (r === null || r === undefined) return 'terminal-running'
  if (r?.success === false) return 'terminal-err'
  return 'terminal-done'
}
function fmtToolArgs(args: any): string {
  if (!args || Object.keys(args).length === 0) return ''
  const s = JSON.stringify(args)
  return s.length > 120 ? s.slice(0, 120) + '…' : s
}
function fmtToolArgsBrief(args: any): string {
  if (!args || Object.keys(args).length === 0) return ''
  const keys = Object.keys(args)
  const firstKey = keys[0]
  const val = JSON.stringify(args[firstKey])
  const brief = `${firstKey}=${val.length > 30 ? val.slice(0, 30) + '…' : val}`
  if (keys.length > 1) return brief + ` +${keys.length - 1}`
  return brief
}

// ── Tool emojis / quips ──

const TOOL_EMOJI: Record<string, string> = {
  read_file: '📄', write_file: '✏️', list_directory: '📁', search_files: '🔍',
  kb_search: '🔎', kb_add_texts: '📝', kb_create: '📚',
  model_list: '📋', model_load: '📦', model_run: '🏃', model_unload: '🗑️',
  download_file: '⬇️', download_package: '📦',
  plugin_start: '▶️', plugin_stop: '⏹️', plugin_install: '📥',
  workflow_execute: '⚡', workflow_create: '🆕',
  catalog_search: '🔍', guide_search: '📖',
  system_info: '🖥️', proxy_test: '🌐',
  mcp_connect_server: '🔗', mcp_list_servers: '📡',
}
function toolEmoji(name: string) { return TOOL_EMOJI[name] || '🔧' }

const TOOL_QUIPS: Record<string, string> = {
  read_file: '正在翻阅文件...', write_file: '正在落笔书写...', list_directory: '正在翻看目录...', search_files: '正在四处搜寻文件...',
  kb_search: '正在翻阅知识库...', kb_add_texts: '正在整理知识...', kb_create: '正在创建知识库...',
  model_list: '正在清点模型库存...', model_load: '正在唤醒模型...', model_run: '正在让模型干活...', model_unload: '正在让模型休息...',
  download_file: '正在下载文件，稍等片刻...', download_package: '正在打包下载...',
  plugin_start: '正在启动插件...', plugin_stop: '正在关闭插件...', plugin_install: '正在安装插件...',
  workflow_execute: '正在执行工作流...', workflow_create: '正在编排工作流...',
  catalog_search: '正在逛模型市场...', guide_search: '正在翻攻略...',
  system_info: '正在查看系统状态...', proxy_test: '正在测试网络通路...',
  mcp_connect_server: '正在建立连接...', mcp_list_servers: '正在扫描可用服务...',
}
function toolQuip(name: string) { return TOOL_QUIPS[name] || `正在调用 ${name}，马上就好...` }

const busyMsg = computed(() => {
  // Find the last assistant message with pending tool calls
  const msgs = chat.messages
  for (let i = msgs.length - 1; i >= 0; i--) {
    const m = msgs[i]
    if (m.role === 'assistant' && m.toolCalls?.length) {
      // Find the first tool call without a result yet
      const results = m.toolResults || []
      for (const tc of m.toolCalls) {
        const done = results.some(r => r.name === tc.name && r.result !== null && r.result !== undefined)
        if (!done) return toolQuip(tc.name)
      }
      return '正在处理工具结果...'
    }
  }
  return '正在思考...'
})

watch(() => chat.messages.length, () => nextTick(() => scrollChat()))
// Track last message content during streaming so scroll follows generation
watch(() => {
  const msgs = chat.messages
  const last = msgs.length ? msgs[msgs.length - 1] : null
  return last ? (last.content || '') + '__' + (last.toolResults ? last.toolResults.length : 0) : ''
}, () => nextTick(() => scrollChat()))
watch(() => chat.busy, (val) => { if (val) { atBottom.value = true; nextTick(() => scrollChat()) } })
const _nowTimer = setInterval(() => { now.value = Date.now() }, 1000)
onMounted(() => {
  chat.loadConfig()
  chat.loadSkills()
  chat.loadAgents()
  chat.loadSessions()
  nextTick(() => { chat.setChatBoxRef(chatBox.value); scrollChat() })
})
onBeforeUnmount(() => { clearInterval(_nowTimer) })

// Reload agent list when personas change elsewhere (e.g. management page).
useDataChanged('agents:changed', () => { chat.loadAgents() })
// Refresh sessions when memory changes elsewhere (create/rename/delete).
useDataChanged('memory:changed', () => { chat.loadSessions() })

function onSessionChange(e: Event) {
  chat.selectSession((e.target as HTMLSelectElement).value)
}
async function onRenameSession() {
  const id = chat.currentSessionId
  if (!id) return
  const cur = chat.sessions.find(s => s.id === id)
  const title = window.prompt('会话标题', cur?.title || '新对话')
  if (title === null) return
  await chat.renameSession(id, title.trim() || '新对话')
}
async function onDeleteSession() {
  const id = chat.currentSessionId
  if (!id || chat.sessions.length <= 1) return
  if (!window.confirm('删除当前会话？此操作不可撤销。')) return
  await chat.deleteSession(id)
}

const loadingMore = ref(false)

/** Save scroll position, then after DOM update restore to keep the viewport stable. */
async function saveScrollThenReveal(chatStore: any) {
  const el = chatBox.value
  if (!el) return
  const prevSH = el.scrollHeight
  const prevST = el.scrollTop
  // Double-wait: nextTick for Vue DOM update, RAF for browser layout.
  await nextTick()
  await new Promise(r => requestAnimationFrame(r))
  if (!el) return
  el.scrollTop = prevST + (el.scrollHeight - prevSH)
}

/** Click handler for the "view compressed history" button. */
function onToggleHistory() {
  const el = chatBox.value
  if (!el) return
  if (chat.historyVisibleCount > 0) {
    // Collapse: scroll the marker boundary to the top of the viewport.
    chat.hideCompressedHistory()
    nextTick().then(() => {
      const btn = el.querySelector('.chat-history-boundary-btn') as HTMLElement
      if (btn) btn.scrollIntoView({ block: 'start', behavior: 'instant' })
    })
    return
  }
  // Expand: reveal first batch, keep scroll stable.
  const prevSH = el.scrollHeight
  const prevST = el.scrollTop
  chat.revealMoreHistory()
  nextTick().then(() => {
    requestAnimationFrame(() => {
      if (el) el.scrollTop = prevST + (el.scrollHeight - prevSH)
    })
  })
}

function onScroll() {
  const el = chatBox.value
  if (!el) return
  atBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  // Trigger load-more when scrolling near top (within 60px).
  // When load-more is available, it takes priority over progressive reveal —
  // newly loaded messages naturally expand the compressed history.
  if (el.scrollTop < 60 && chat.hasMoreMessages && !loadingMore.value) {
    loadingMore.value = true
    chat.loadEarlierMessages().finally(() => { loadingMore.value = false })
  } else if (chat.compressionMarker >= 0 && chat.historyVisibleCount > 0
      && chat.historyVisibleCount < chat.historyMessages.length
      && el.scrollTop < 80) {
    // Progressive history reveal: only when load-more is exhausted.
    const prevSH = el.scrollHeight
    chat.revealMoreHistory()
    nextTick().then(() => {
      requestAnimationFrame(() => {
        if (el) el.scrollTop = el.scrollTop + (el.scrollHeight - prevSH)
      })
    })
  }
}
function scrollChat() {
  if (!atBottom.value) return
  try { if (chatBox.value) chatBox.value.scrollTop = chatBox.value.scrollHeight } catch (_) {}
}
</script>

<style scoped>
.chat-panel {
  display: flex; flex-direction: column;
  height: 100%; min-height: 0; overflow: hidden;
  position: relative;
}

/* ── Drop overlay ── */
.chat-drop-active { outline: 2px dashed var(--accent); outline-offset: -2px; }
.chat-drop-overlay {
  position: absolute; inset: 0; z-index: 10;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px;
  background: rgba(0,0,0,0.75);
  backdrop-filter: blur(6px);
  border-radius: var(--radius-sm);
  pointer-events: none;
}
.chat-drop-icon { font-size: 40px; }
.chat-drop-text { font-size: 16px; font-weight: 600; color: var(--text-primary); }
.chat-drop-hint { font-size: 12px; color: var(--text-tertiary); }

/* ── Header ── */
.chat-panel-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 14px; flex-shrink: 0;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
}
.chat-panel-header-left { display: flex; align-items: center; gap: 8px; }
.chat-panel-header-dot {
  width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0;
  background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5);
}
.chat-panel-header-model { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.chat-think-toggle {
  margin-left: auto; width: 28px; height: 28px; padding: 0; border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm); background: transparent; font-size: 14px; cursor: pointer;
  opacity: 0.5; transition: all 0.15s; display: flex; align-items: center; justify-content: center;
}
.chat-think-toggle:hover { opacity: 0.8; }
.chat-think-toggle.active { opacity: 1; background: var(--accent-dim); border-color: var(--accent); }

/* ── Session bar ── */
.chat-session-bar {
  display: flex; align-items: center; gap: 4px;
  padding: 6px 10px; flex-shrink: 0;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-elevated); position: relative;
}
.chat-session-name {
  font-size: 12px; font-weight: 550; color: var(--text-primary);
  cursor: pointer; padding: 2px 6px; border-radius: 4px; max-width: 200px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.chat-session-name:hover { background: var(--bg-hover); }
.chat-session-spacer { flex: 1; }

/* Popovers (shared) */
.chat-popover-backdrop { position: fixed; inset: 0; z-index: 18; }
.chat-popover-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 10px; border-bottom: 1px solid var(--border-soft);
  font-size: 12px; font-weight: 600; color: var(--text-primary);
  position: sticky; top: 0; background: var(--bg-elevated); z-index: 1;
}
.chat-history-popover {
  z-index: 20; min-width: 240px; max-width: 320px; max-height: 320px; overflow-y: auto;
  background: var(--bg-elevated); border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  box-shadow: 0 8px 24px rgba(0,0,0,0.5);
}
.popover-fade-enter-active { transition: all 0.15s ease-out; }
.popover-fade-leave-active { transition: all 0.1s ease-in; }
.popover-fade-enter-from, .popover-fade-leave-to { opacity: 0; transform: translateY(-4px); }
.chat-history-list { display: flex; flex-direction: column; }
.chat-history-item {
  display: flex; align-items: center; gap: 8px; padding: 8px 10px;
  cursor: pointer; border-bottom: 1px solid var(--border-subtle); transition: background 0.1s;
}
.chat-history-item:hover { background: var(--bg-hover); }
.chat-history-item.active { background: var(--accent-dim); }
.chat-history-title { flex: 1; font-size: 12px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chat-history-time { font-size: 10px; color: var(--text-tertiary); flex-shrink: 0; }
.chat-history-del {
  width: 18px; height: 18px; padding: 0; border: none; border-radius: 3px;
  background: transparent; color: var(--text-tertiary); font-size: 11px; cursor: pointer; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.chat-history-del:hover { background: var(--danger-dim); color: var(--danger); }

/* ── Messages area ── */
.chat-panel-box {
  flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 10px;
  padding: 14px 14px 16px; scrollbar-gutter: stable;
  overflow-anchor: none; /* prevent browser scroll-anchoring from fighting manual scrollTop */
}

/* ── File chips ── */
.chat-files-row {
  display: flex; flex-wrap: wrap; gap: 4px; align-items: center;
  padding: 6px 14px; flex-shrink: 0;
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
}
.chat-file-chip {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 4px 8px 4px 6px;
  background: var(--accent-dim); border: 1px solid var(--accent);
  border-radius: var(--radius-sm);
  font-size: 11px; color: var(--accent);
}
.chat-file-icon { font-size: 14px; flex-shrink: 0; }
.chat-file-name { font-weight: 550; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chat-file-size { font-size: 9px; color: var(--text-tertiary); }
.chat-file-remove {
  width: 16px; height: 16px; padding: 0; border: none; border-radius: 3px;
  background: transparent; color: var(--accent); font-size: 11px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  opacity: 0.6; transition: all 0.15s;
}
.chat-file-remove:hover { opacity: 1; background: var(--danger-dim); color: var(--danger); }

/* ── Context bar ── */
.chat-ctx-wrap {
  display: flex; flex-direction: row; align-items: center; gap: 5px; flex-shrink: 0;
}
.chat-ctx-bar {
  width: 64px; height: 4px; border-radius: 2px;
  background: var(--bg-inset); overflow: hidden;
}
.chat-ctx-fill {
  height: 100%; border-radius: 2px; transition: width 0.5s ease;
  background: var(--success);
}
.ctx-warn .chat-ctx-fill     { background: #d29922; }
.ctx-critical .chat-ctx-fill { background: var(--danger); }
.ctx-danger .chat-ctx-fill   { background: var(--danger); animation: ctx-pulse 0.8s ease-in-out infinite alternate; }
@keyframes ctx-pulse { from { opacity: 1; } to { opacity: 0.5; } }
.chat-ctx-label { font-size: 9px; color: var(--text-tertiary); font-family: var(--font-mono); line-height: 1; min-width: 32px; text-align: right; }
.ctx-label-warn     { color: #d29922; }
.ctx-label-critical { color: var(--danger); }
.ctx-label-danger   { color: var(--danger); font-weight: 700; }

/* ── Input section (textarea + bottom bar) ── */
.chat-input-section {
  flex-shrink: 0; margin: 8px 12px 10px;
  border: 1.5px solid var(--accent); border-radius: 12px;
  background: var(--bg-elevated); transition: border-color 0.25s;
}
.chat-input-section.effort-max { border-color: var(--danger); }
.chat-input-area { padding: 8px 10px 0; }
.chat-textarea-wrap {
  display: grid;
  min-height: 28px; /* matches button height in bottom bar */
  max-height: 88px; /* 4 rows × ~22px */
  overflow-y: auto;
}
.chat-textarea-wrap::after {
  content: attr(data-replicated-value) " ";
  white-space: pre-wrap; visibility: hidden;
  grid-area: 1 / 1;
  padding: 4px 4px; border: none;
  font-family: var(--font); font-size: 13px; line-height: 1.55;
  word-break: break-word;
}
.chat-panel-textarea {
  grid-area: 1 / 1;
  width: 100%; padding: 4px 4px; border: none; border-radius: 0;
  background: transparent; color: var(--text-primary); font-family: var(--font);
  font-size: 13px; resize: none; outline: none;
  line-height: 1.55; overflow: hidden;
}

/* ── Settings popover ── */
.chat-settings-popover {
  z-index: 20; min-width: 220px; max-width: 280px;
  background: var(--bg-elevated); border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  box-shadow: 0 8px 24px rgba(0,0,0,0.5);
}
.chat-settings-body { padding: 10px 12px; display: flex; flex-direction: column; gap: 8px; }
.chat-setting-row { display: flex; align-items: center; gap: 10px; }
.chat-setting-label { font-size: 11px; color: var(--text-secondary); min-width: 56px; }
.chat-setting-toggle { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-primary); cursor: pointer; }
.chat-setting-hint { font-size: 10px; color: var(--text-tertiary); min-width: 28px; text-align: right; }

/* Sliding switch */
.chat-switch { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.chat-switch input { display: none; }
.chat-switch-track {
  width: 36px; height: 20px; border-radius: 10px; position: relative;
  background: var(--bg-inset); border: 1px solid var(--border-soft);
  transition: background 0.2s, border-color 0.2s;
}
.chat-switch input:checked + .chat-switch-track {
  background: var(--accent); border-color: var(--accent);
}
.chat-switch.effort-high .chat-switch-track {
  background: var(--accent); border-color: var(--accent);
}
.chat-switch.effort-high .chat-switch-knob { left: 2px; right: auto; }
.chat-switch.effort-max input:checked + .chat-switch-track {
  background: var(--danger); border-color: var(--danger);
}
.chat-switch-disabled { opacity: 0.4; pointer-events: none; }
.chat-disabled .chat-setting-label { opacity: 0.4; }
.chat-switch-knob {
  position: absolute; top: 2px; left: 2px;
  width: 14px; height: 14px; border-radius: 50%;
  background: #fff; transition: transform 0.2s;
}
.chat-switch input:checked + .chat-switch-track .chat-switch-knob {
  transform: translateX(16px);
}
.chat-switch-label { font-size: 11px; color: var(--text-secondary); }
.chat-switch-sm .chat-switch-track { width: 28px; height: 16px; border-radius: 8px; }
.chat-switch-sm .chat-switch-knob { width: 12px; height: 12px; top: 1px; left: 1px; }
.chat-switch-sm input:checked + .chat-switch-track .chat-switch-knob { transform: translateX(12px); }
.chat-switch-sm .chat-switch-label { font-size: 10px; }
.chat-effort-btn {
  padding: 3px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: transparent; color: var(--text-secondary); font-size: 11px; cursor: pointer;
  font-family: var(--font-mono); transition: all 0.12s;
}
.chat-effort-btn:hover { background: var(--bg-hover); }
.chat-effort-btn.active { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); font-weight: 600; }
.chat-agent-select {
  flex: 1; min-width: 120px; padding: 3px 6px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-secondary); font-size: 11px; outline: none; cursor: pointer;
}
.chat-agent-select:focus { border-color: var(--accent); color: var(--text-primary); }

/* ── Bottom bar ── */
.chat-bottom-bar {
  display: flex; align-items: center; gap: 6px; padding: 6px 10px 8px; flex-shrink: 0;
  border-top: 1px solid var(--border-subtle);
}
.chat-bar-btn {
  width: 28px; height: 28px; padding: 0; border: none; border-radius: 6px;
  background: transparent; color: var(--text-secondary); font-size: 15px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.12s; flex-shrink: 0;
}
.chat-bar-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.chat-bar-btn.active { background: var(--accent-dim); color: var(--accent); }
.chat-bar-btn:disabled { opacity: 0.3; cursor: default; }
.chat-spin-icon { animation: spin 1s linear infinite; display: inline-block; font-size: 15px; }
@keyframes spin { to { transform: rotate(360deg); } }
.chat-bar-spacer { flex: 1; }
.chat-model-label {
  font-size: 11px; color: var(--text-tertiary); white-space: nowrap; overflow: hidden;
  text-overflow: ellipsis; max-width: 140px; padding: 0 4px;
}
.chat-think-indicator { font-size: 12px; flex-shrink: 0; }
.chat-think-label {
  font-size: 9px; color: var(--text-secondary); font-family: var(--font-mono);
  min-width: 26px; text-align: right; flex-shrink: 0;
}
.chat-think-dots {
  display: flex; align-items: center; gap: 5px; flex-shrink: 0;
  padding: 6px 8px; border-radius: 10px; background: var(--bg-inset);
  cursor: pointer;
}
.chat-think-dot {
  width: 8px; height: 8px; border-radius: 50%; cursor: pointer;
  opacity: 0.35; transition: all 0.15s;
}
.chat-think-dot:hover { opacity: 0.7; }
.chat-think-dot.active { opacity: 1; width: 12px; height: 12px; box-shadow: 0 0 4px currentColor; }
.chat-send-btn {
  width: 30px; height: 30px; border-radius: 8px; font-size: 16px; font-weight: 700;
  background: var(--accent); color: #fff; transition: background 0.25s;
}
.chat-input-section.effort-max .chat-send-btn { background: var(--danger); }
.chat-send-btn:hover { filter: brightness(1.15); }
.chat-send-btn:disabled { background: var(--bg-inset); color: var(--text-tertiary); }
.chat-stop-icon { font-size: 12px; }
.chat-send-icon { font-size: 16px; line-height: 1; }

/* ── Messages ── */
.chat-load-earlier {
  text-align: center; padding: 14px 0 8px; font-size: 11px;
  color: var(--text-tertiary); opacity: 0.6;
  border-bottom: 1px solid var(--border-subtle); margin-bottom: 6px;
}
.chat-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 60px 0; color: var(--text-tertiary); font-size: 14px; }
.chat-empty span { font-size: 36px; opacity: 0.3; }
.chat-empty-hint { font-size: 12px; color: var(--text-tertiary); opacity: 0.6; }
.chat-sug { display: flex; flex-wrap: wrap; gap: 6px; justify-content: center; margin-top: 10px; }

/* User message — box with blue left border */
.chat-user-msg {
  align-self: flex-end; max-width: 80%;
  margin-left: auto;
  display: flex; flex-direction: column; gap: 6px;
}
.chat-user-text {
  padding: 8px 14px; border-radius: var(--radius-sm);
  border-left: 3px solid var(--accent);
  background: rgba(59,130,246,0.08);
  font-size: 13px; line-height: 1.55; color: var(--text-primary);
  user-select: text;
}

/* File cards under user messages */
.chat-file-cards {
  display: flex; flex-direction: column; gap: 3px;
}
.chat-file-card {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 10px; border-radius: var(--radius-sm);
  background: var(--bg-elevated); border: 1px solid var(--border-soft);
  cursor: pointer; transition: background 0.12s, border-color 0.12s;
  max-width: 260px;
}
.chat-file-card:hover { background: var(--bg-hover); border-color: var(--accent); }
.chat-file-card-icon { font-size: 18px; flex-shrink: 0; }
.chat-file-card-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.chat-file-card-name {
  font-size: 11px; font-weight: 550; color: var(--text-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.chat-file-card-meta {
  font-size: 9px; color: var(--text-tertiary); display: flex; align-items: center; gap: 4px;
}
.chat-file-card-badge {
  padding: 0 4px; border-radius: 3px; background: var(--warning-dim); color: var(--warning);
  font-size: 8px; font-weight: 600;
}
.chat-file-card-btn {
  width: 22px; height: 22px; padding: 0; border: none; border-radius: 4px;
  background: transparent; font-size: 13px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; opacity: 0.5; transition: opacity 0.12s;
}
.chat-file-card-btn:hover { opacity: 1; background: var(--accent-dim); }

/* Assistant message — bare, no bubble */
.chat-asst-msg {
  width: 100%; max-width: 100%;
  display: flex; flex-direction: column; gap: 8px;
}

/* Reasoning / chain-of-thought block */
.chat-reasoning-OLD {
  border-left: 3px solid rgba(180,150,60,0.4);
  background: rgba(180,150,60,0.05); max-width: 85%;
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
}
.chat-reasoning-OLD-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 6px 10px; cursor: pointer; user-select: none;
  transition: background 0.12s;
}
.chat-reasoning-OLD-head:hover { background: rgba(180,150,60,0.08); }
.chat-reasoning-OLD-label { font-size: 11px; color: rgba(180,150,60,0.8); font-weight: 500; }
.chat-reasoning-OLD-toggle { font-size: 10px; color: rgba(180,150,60,0.5); }
.chat-reasoning-OLD-body {
  padding: 0 10px 8px; font-size: 11px; line-height: 1.55;
  color: var(--text-tertiary); white-space: pre-wrap; word-break: break-word;
  max-height: 200px; overflow-y: auto;
}
.chat-asst-text {
  font-size: 13px; line-height: 1.6; color: var(--text-primary);
  user-select: text;
}

/* Compact overrides */
.chat-compact .chat-user-msg { max-width: 88%; }
.chat-compact .chat-user-text { font-size: 12px; padding: 6px 10px; }
.chat-compact .chat-asst-msg { max-width: 94%; }
.chat-compact .chat-asst-text { font-size: 12px; }
.chat-compact .chat-panel-box { padding: 10px 10px 14px; gap: 8px; }
.chat-compact .chat-panel-textarea { font-size: 12px; padding: 8px 10px; }

/* Paradigm footnote — subtle bottom-right marker */
.paradigm-footnote {
  text-align: right;
  font-style: italic;
  font-size: 0.75em;
  color: #666;
  margin-top: 6px;
  opacity: 0.6;
  user-select: none;
}

/* Markdown — assistant text */
.chat-asst-text :deep(h1) { font-size: 16px; font-weight: 600; margin: 14px 0 6px; }
.chat-asst-text :deep(h2) { font-size: 14px; font-weight: 600; margin: 12px 0 5px; }
.chat-asst-text :deep(h3) { font-size: 13px; font-weight: 600; margin: 10px 0 4px; }
.chat-asst-text :deep(p) { margin: 0 0 8px; }
.chat-asst-text :deep(p:last-child) { margin-bottom: 0; }
.chat-asst-text :deep(ul), .chat-asst-text :deep(ol) { margin: 6px 0; padding-left: 22px; }
.chat-asst-text :deep(li) { margin-bottom: 2px; }
.chat-asst-text :deep(blockquote) {
  margin: 8px 0; padding: 4px 12px;
  border-left: 3px solid var(--accent);
  background: rgba(0,122,255,0.06); border-radius: 0 6px 6px 0;
  color: var(--text-secondary);
}
.chat-asst-text :deep(hr) { border: none; border-top: 1px solid var(--border-soft); margin: 12px 0; }
.chat-asst-text :deep(a) { color: var(--accent); text-decoration: underline; }
.chat-asst-text :deep(table) {
  border-collapse: collapse; width: 100%; margin: 8px 0;
  font-size: 12px; border: 1px solid var(--border-soft); border-radius: 6px; overflow: hidden;
}
.chat-asst-text :deep(th), .chat-asst-text :deep(td) {
  padding: 6px 10px; text-align: left; border-bottom: 1px solid var(--border-subtle);
}
.chat-asst-text :deep(th) { background: rgba(255,255,255,0.04); font-weight: 600; color: var(--text-primary); }
.chat-asst-text :deep(tr:last-child td) { border-bottom: none; }
.chat-asst-text :deep(code) { font-size: 11px; font-family: var(--font-mono); background: rgba(255,255,255,0.1); padding: 1px 5px; border-radius: 3px; }
.chat-asst-text :deep(pre) { margin: 6px 0; padding: 10px 12px; background: rgba(0,0,0,0.3); border: 1px solid var(--border-soft); border-radius: var(--radius-sm); font-size: 11px; font-family: var(--font-mono); overflow-x: auto; }

/* Tool call tags */
.chat-tc-row-OLD { display: flex; flex-wrap: wrap; gap: 4px; }
.chat-tc-tag-OLD {
  display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px;
  background: var(--bg-inset); border-radius: 4px;
  font-size: 10px; color: var(--text-secondary); cursor: pointer; border: 1px solid var(--border-subtle);
  transition: all 0.15s; font-family: var(--font-mono);
}
.chat-tc-tag-OLD:hover { border-color: var(--accent); color: var(--accent); }
.chat-tc-tag-OLD.expanded { background: var(--accent-dim); border-color: var(--accent); color: var(--accent); }

/* Tool result — compact */
.chat-tool-result-OLDs-OLD { display: flex; flex-direction: column; gap: 4px; }
.chat-tool-result-OLD {
  padding: 6px 10px; background: var(--bg-inset); border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm); font-size: 10px;
}
.chat-tool-result-OLD-head {
  display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px;
}
.chat-tool-result-OLD-name { font-weight: 600; color: var(--text-secondary); font-family: var(--font-mono); font-size: 10px; }
.chat-tool-result-OLD-status { font-size: 10px; }
.chat-tool-result-OLD-status.tool-ok { color: var(--success); }
.chat-tool-result-OLD-status.tool-err { color: var(--danger); }
.chat-tool-result-OLD-json {
  margin: 0; padding: 6px 8px; background: rgba(0,0,0,0.2); border-radius: 3px;
  font-size: 10px; font-family: var(--font-mono); line-height: 1.4;
  color: var(--text-tertiary); white-space: pre-wrap; word-break: break-all;
  max-height: 120px; overflow-y: auto;
}

/* ── Busy / tool-in-progress indicator ── */
.chat-busy-row {
  display: flex; align-items: center; gap: 10px; padding: 6px 0;
  align-self: flex-start;
}
.chat-busy-anim { display: flex; gap: 3px; align-items: center; }
.chat-busy-dot {
  width: 5px; height: 5px; border-radius: 50%; background: var(--accent);
  animation: busy-pulse 1.2s infinite ease-in-out both;
}
.chat-busy-dot:nth-child(1) { animation-delay: -0.4s; }
.chat-busy-dot:nth-child(2) { animation-delay: -0.2s; }
@keyframes busy-pulse {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.8); }
  40% { opacity: 1; transform: scale(1.2); }
}
.chat-busy-msg {
  font-size: 12px; color: var(--text-tertiary); font-style: italic;
  animation: busy-fade 2s infinite;
}
@keyframes busy-fade {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

/* ── Buttons ── */
.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-xs { padding: 2px 7px; font-size: 11px; border: 1px solid var(--border-soft); border-radius: 4px; background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer; }
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
/* Meta blocks (thinking + tool calls) */
.chat-meta-block { width: 100%; border-radius: var(--radius-sm); overflow: hidden; }
.chat-meta-think { border-left: 3px solid rgba(180,150,60,0.5); background: rgba(180,150,60,0.04); }
/* ── Tool call terminal blocks ── */
.chat-meta-tool  { border-left: 3px solid rgba(140,140,150,0.5); background: rgba(140,140,150,0.04); transition: border-color 0.3s, box-shadow 0.3s; }
.chat-meta-tool.terminal-running { border-left-color: rgba(180,150,60,0.7); box-shadow: 0 0 8px rgba(180,150,60,0.08); }
.chat-meta-tool.terminal-done   { border-left-color: rgba(80,180,120,0.6); }
.chat-meta-tool.terminal-err    { border-left-color: var(--danger); background: rgba(255,80,80,0.04); }
.chat-meta-head { display: flex; align-items: center; gap: 6px; padding: 6px 10px; cursor: pointer; user-select: none; transition: background 0.12s; }
.chat-meta-think .chat-meta-head:hover { background: rgba(180,150,60,0.08); }
.chat-meta-tool .chat-meta-head:hover { background: rgba(140,140,150,0.08); }
.chat-meta-label { font-size: 11px; font-weight: 500; }
.chat-meta-think .chat-meta-label { color: rgba(180,150,60,0.85); }
.chat-meta-tool .chat-meta-label { color: rgba(180,180,190,0.85); }

.terminal-label { width: 18px; display: inline-flex; align-items: center; justify-content: center; }
.terminal-spin { animation: spin 1s linear infinite; display: inline-block; color: rgba(180,150,60,0.9); }
@keyframes spin { to { transform: rotate(360deg); } }
.chat-meta-tool.terminal-running .chat-meta-label { color: rgba(210,180,60,0.95); }

.terminal-status { font-size: 10px; font-weight: 500; padding: 1px 6px; border-radius: 3px; flex-shrink: 0; }
.terminal-status-busy { background: rgba(180,150,60,0.12); color: rgba(210,180,60,0.9); }
.terminal-status-ok   { background: rgba(80,180,120,0.1); color: rgba(100,200,140,0.85); }
.terminal-status-err  { background: rgba(255,80,80,0.1); color: rgba(255,120,120,0.85); }

.chat-meta-tool-name {
  font-size: 10px; padding: 1px 6px; border-radius: 3px;
  background: rgba(140,140,150,0.12); color: rgba(180,180,190,0.7);
  font-family: var(--font-mono); flex-shrink: 0;
}
.terminal-args-inline {
  font-size: 10px; color: rgba(160,180,210,0.55); flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-family: var(--font-mono);
}
.chat-meta-toggle { font-size: 10px; opacity: 0.5; flex-shrink: 0; margin-left: auto; }

/* ── Terminal body ── */
.terminal-body {
  padding: 6px 10px 8px; background: rgba(0,0,0,0.25);
  border-top: 1px solid rgba(255,255,255,0.04);
  font-family: var(--font-mono); font-size: 11px; line-height: 1.6;
  color: rgba(200,200,210,0.85); user-select: text; max-height: 320px; overflow-y: auto;
}
.terminal-line { padding: 2px 0; white-space: pre-wrap; word-break: break-word; }
.terminal-prompt { color: rgba(100,200,140,0.8); margin-right: 6px; font-weight: 600; }
.terminal-args { color: rgba(180,180,200,0.7); }
.terminal-args-json { color: rgba(140,160,200,0.65); font-size: 10px; }
.terminal-output { color: rgba(200,200,210,0.8); }
.terminal-output pre { margin: 0; white-space: pre-wrap; word-break: break-word; }
.terminal-error .terminal-output { color: rgba(255,140,140,0.85); }

/* ── Compression divider (collapsible accordion) ── */
.msg-row.chat-compress-divider {
  align-self: stretch; text-align: center; padding: 10px 0; margin: 6px 0;
  border-top: 1px solid rgba(180,150,60,0.35); border-bottom: 1px solid rgba(180,150,60,0.35);
  color: rgba(180,150,60,0.75); font-size: 11px; font-family: var(--font-mono);
  background: rgba(180,150,60,0.05); user-select: none; cursor: pointer;
}
.msg-row.chat-compress-divider:hover { background: rgba(180,150,60,0.1); }
.chat-compress-summary { font-size: 11px; line-height: 1.5; text-align: left;
  padding: 6px 14px; color: var(--text-secondary); white-space: pre-wrap; }

/* ── Compression boundary button ── */
.chat-history-boundary-btn {
  width: 100%; padding: 7px 14px; border: 1px dashed rgba(100,100,120,0.25);
  border-radius: var(--radius-sm); background: rgba(100,100,120,0.04);
  color: var(--text-tertiary); font-size: 11px; cursor: pointer; text-align: left;
  transition: background 0.15s; font-family: var(--font); margin: 2px 0;
}
.chat-history-boundary-btn:hover { background: rgba(100,100,120,0.1); color: var(--text-secondary); }
/* Grayed-out history messages */
.chat-history-msg { opacity: 0.55; filter: grayscale(0.4); transition: opacity 0.15s; }
.chat-history-msg:hover { opacity: 0.75; }
.chat-meta-body { padding: 0 10px 8px; font-size: 11px; line-height: 1.55; color: var(--text-tertiary); white-space: pre-wrap; word-break: break-word; max-height: 240px; overflow-y: auto; user-select: text; }

/* ── Compression card (teal accent — distinct from thinking gold) ── */
.chat-meta-compress { border-left: 3px solid rgba(80,180,180,0.5); background: rgba(80,180,180,0.04); }
.chat-meta-compress .chat-meta-head:hover { background: rgba(80,180,180,0.08); }
.chat-meta-compress .chat-meta-label { color: rgba(80,180,180,0.9); }
.chat-meta-badge {
  font-size: 10px; padding: 1px 6px; border-radius: 3px;
  background: rgba(80,180,180,0.12); color: rgba(80,180,180,0.75);
  font-weight: 500; flex-shrink: 0;
}

/* Compression body — lighter background for readability */
.chat-compress-body {
  background: rgba(0,0,0,0.12); border-top: 1px solid rgba(255,255,255,0.03);
  padding: 8px 12px !important; line-height: 1.65 !important; max-height: 320px !important;
}

/* Structured section */
.compress-section {
  margin-bottom: 6px;
}
.compress-section:last-child { margin-bottom: 0; }
.compress-section-head {
  display: flex; align-items: center; gap: 5px;
  padding: 3px 0; margin-bottom: 2px;
}
.compress-section-icon { font-size: 13px; flex-shrink: 0; }
.compress-section-title {
  font-size: 11px; font-weight: 600;
  color: rgba(80,200,200,0.8);
  letter-spacing: 0.02em;
}
.compress-section-body {
  font-size: 11px; color: rgba(200,210,215,0.7);
  padding-left: 18px; line-height: 1.6;
}

/* Fallback: unstructured compression text */
.compress-plain {
  font-size: 11px; color: rgba(200,210,215,0.7); line-height: 1.6;
}
.chat-tool-item-head { font-size: 10px; margin-bottom: 3px; font-family: var(--font-mono); }
.tool-ok { color: var(--success); }
.tool-err { color: var(--danger); }
.chat-tool-item-json { margin: 0; padding: 6px 8px; background: rgba(0,0,0,0.2); border-radius: 3px; font-size: 10px; font-family: var(--font-mono); line-height: 1.4; color: var(--text-tertiary); white-space: pre-wrap; word-break: break-all; max-height: 120px; overflow-y: auto; user-select: text; }
</style>
