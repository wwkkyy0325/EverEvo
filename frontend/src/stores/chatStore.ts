import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { marked } from 'marked'
import { agentsApi } from '../api/agents'
import type { LocalAgent } from '../api/agents'
import { memoryApi } from '../api/memory'
import { wikiApi } from '../api/wiki'
import { knowledgeApi } from '../api/knowledge'
import { useActiveLibrary } from '../composables/useActiveLibrary'
import type { MemorySession } from '../api/memory'

// Cross-view: last graph recall trace (seed/edge ids). The Knowledge viewer reads
// this to highlight which part of the graph the most recent chat answer used.
export const lastGraphTrace = ref<{ seedIds: string[]; edgeIds: string[] }>({ seedIds: [], edgeIds: [] })

// ── Types ──

export interface ChatMessage {
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  tool_calls?: ToolCall[]
  toolCalls?: { name: string }[]   // UI convenience
  toolResults?: ToolResultEntry[]
  tool_call_id?: string
}

interface ToolResultEntry {
  name: string
  args: Record<string, unknown>
  result: ToolCallResult
  startedAt?: number   // ms timestamp; set on placeholder, kept for elapsed display
}

interface ToolCall {
  id: string
  type: string
  function: { name: string; arguments: string }
}

interface LLMProvider {
  id: string
  name: string
  endpoint: string
  apiKey: string
  model: string
  enabled: boolean
  apiFormat: string
  models?: string[]
  modelCapabilities?: Record<string, { maxContextTokens?: number }>
}

/** Lightweight tool definition used in chat loop */
interface ToolDef {
  name: string
  description: string
  parameters: Record<string, unknown>
  /** Raw MCP inputSchema — preferred over parameters when set (external tools). */
  rawParameters?: Record<string, unknown>
  annotations?: { readOnlyHint?: boolean }
}

/** Skill info from backend */
interface SkillInfo {
  name: string
  title: string
  description: string
  category: string
  enabled: boolean
  systemPrompt?: string
}

/** Result from App.CallTool */
interface ToolCallResult {
  success: boolean
  data?: unknown
  error?: string
}

/** A message sent to the LLM API */
interface APIMessage {
  role: string
  content: string
  tool_calls?: ToolCall[]
  tool_call_id?: string
}

/** Stream chunk event data */
interface StreamChunkData { text?: string }

/** Stream done event data */
interface StreamDoneData {
  choices?: { message: { content?: string; tool_calls?: ToolCall[] } }[]
}

/** Stream error event data */
interface StreamErrorData { error?: string }

/**
 * normalizeToolMessages returns a defensive copy of the message list where
 * every assistant turn carrying `tool_calls` is followed by a tool-role
 * message for EACH tool_call_id. Missing responses are back-filled with an
 * error placeholder so the API (DeepSeek/OpenAI) never rejects with
 * "insufficient tool messages following tool_calls".
 *
 * Also drops a trailing assistant.tool_calls that has no results at all
 * (can't legally end a request). This is the root fix for the recurring
 * HTTP 400 tool_calls pairing error.
 */
/**
 * Event-driven wait for async agent runs. Subscribes to collab:event for
 * agent.<id>.done and resolves when every runId in the list has completed.
 * Runs without a backend timeout — the wait naturally ends when all agents
 * finish (or the chat is stopped via stopRequested).
 */
async function waitForCollabRuns(args: Record<string, unknown>): Promise<ToolCallResult> {
  const raw = args.runIds
  if (!Array.isArray(raw) || !raw.length) {
    return { success: false, error: 'collab_wait: runIds must be a non-empty array' }
  }
  const runIds = raw.map(String)
  const remaining = new Set(runIds)
  const results: any[] = []
  const rt = window.runtime as any
  let stopWatcher: ReturnType<typeof setInterval> | null = null

  const cleanup = () => {
    rt.EventsOff('collab:event', handler)
    if (stopWatcher) { clearInterval(stopWatcher); stopWatcher = null }
  }

  const handler = (envelope: any) => {
    if (stopRequested.value) { cleanup(); return }
    const ev = envelope?.data || {}
    const topic: string = ev.topic || ''
    const m = topic.match(/^agent\.(.+)\.done$/)
    if (!m) return
    const payload = ev.payload || {}
    const runId: string = payload.runId || ev.source || ''
    if (!remaining.has(runId)) return
    remaining.delete(runId)
    results.push({ runId, agentId: m[1], status: 'done', result: payload.result })
  }

  return new Promise((resolve) => {
    let resolved = false
    const resolveOnce = (r: ToolCallResult) => {
      if (resolved) return
      resolved = true; cleanup(); resolve(r)
    }
    rt.EventsOn('collab:event', handler)
    stopWatcher = setInterval(() => {
      if (remaining.size === 0) { resolveOnce({ success: true, data: results }) }
      if (stopRequested.value) { resolveOnce({ success: false, error: '用户已终止' }) }
    }, 300)
  })
}

function normalizeToolMessages(msgs: APIMessage[]): APIMessage[] {
  const out: APIMessage[] = []
  for (let i = 0; i < msgs.length; i++) {
    const m = msgs[i]
    // Drop a dangling trailing assistant.tool_calls with zero following tool
    // results — it can't be repaired meaningfully and would 400/422.
    if (m.role === 'assistant' && m.tool_calls?.length) {
      const isLast = i === msgs.length - 1
      const nextIsTool = !isLast && msgs[i + 1].role === 'tool'
      if (isLast || !nextIsTool) {
        // Keep the assistant content but strip tool_calls.
        out.push({ role: 'assistant', content: m.content || '(工具调用未完成)' })
        continue
      }
    }
    out.push(m)
    // If this is an assistant with tool_calls, ensure every id has a tool reply.
    if (m.role === 'assistant' && m.tool_calls?.length) {
      // Collect the tool_call_ids answered by the immediately following tool msgs.
      const answered = new Set<string>()
      for (let j = i + 1; j < msgs.length && msgs[j].role === 'tool'; j++) {
        if (msgs[j].tool_call_id) answered.add(msgs[j].tool_call_id!)
      }
      // Back-fill missing ones right after the assistant message, but ONLY if
      // there is at least one following tool message (otherwise we'd be
      // synthesizing a reply for a dangling turn — handled by the strip above).
      for (const tc of m.tool_calls) {
        if (!answered.has(tc.id)) {
          out.push({
            role: 'tool',
            tool_call_id: tc.id,
            content: JSON.stringify({ success: false, error: '(工具结果缺失，已补占位以维持对话完整性)' }),
          })
        }
      }
    }
  }
  return out
}

// ── Toast injection (same pattern as downloadStore) ──

let _toastFn: ((type: string, title: string, desc?: string) => void) | null = null

// ── Store ──

/** A file pending to be sent with the next message. */
export interface PendingFile {
  name: string
  type: string   // file extension
  size: number   // bytes
  path: string   // backend disk path
  preview: string // first ~300 chars of extracted text
  isScanned: boolean
}

export const useChatStore = defineStore('chat', () => {
  // ── State ──
  const messages = ref<ChatMessage[]>([])
  const inputText = ref('')
  const busy = ref(false)
  const expandedTool = ref<Record<string, boolean>>({})
  const providers = ref<LLMProvider[]>([])
  const activeId = ref('')
  const skills = ref<SkillInfo[]>([])
  const agents = ref<LocalAgent[]>([])
  const selectedAgentId = ref('')
  const sessions = ref<MemorySession[]>([])
  const currentSessionId = ref('')
  const pendingFiles = ref<PendingFile[]>([])
  const hasMoreMessages = ref(false)       // true when earlier messages exist but aren't loaded
  const totalMessageCount = ref(0)         // total messages in current session
  const thinkMode = ref(true)              // extended thinking toggle, default ON
  const thinkEffort = ref<'high' | 'max'>('high')  // effort level (high=default, max=deep)
  const stopRequested = ref(false)          // set to true to stop current generation
  const currentStreamId = ref('')            // track active stream for cancellation
  const reasoningContent = ref<Record<number, string>>({})  // reasoning per message index
  const compressedAt = ref<number>(-1)  // message index where auto-compression happened

  // ── Getters ──

  const activeProvider = computed(() =>
    providers.value.find(p => p.id === activeId.value && p.enabled) || null
  )

  const { activeLibraryId } = useActiveLibrary()

  // Agents visible in the current domain: agents whose library matches the
  // active library, plus core agents from the default library (always shown).
  const visibleAgents = computed(() => {
    const list = agents.value || []
    const libId = activeLibraryId.value
    if (!libId) return list
    return list.filter(a =>
      !a.libraryId || a.libraryId === libId || a.isDefault
    )
  })

  const selectedAgentName = computed(() => {
    const a = agents.value.find(x => x.id === selectedAgentId.value)
    return a ? a.name : ''
  })

  const chatReady = computed(() => {
    const p = activeProvider.value
    return !!(p && p.endpoint && p.apiKey && p.model)
  })

  // ── Context window management ──

  /** Estimated token count: ~3 chars/token for English, ~1.5 for CJK. Heuristic. */
  function estimateTokens(text: string): number {
    let cjk = 0, ascii = 0
    for (const ch of text) {
      const code = ch.charCodeAt(0)
      if (code >= 0x4E00 && code <= 0x9FFF || code >= 0x3000 && code <= 0x303F || code >= 0xFF00) {
        cjk++
      } else if (code < 128) {
        ascii++
      } else {
        cjk++ // treat other non-ASCII as CJK-like
      }
    }
    return Math.ceil(cjk / 1.5 + ascii / 3.5)
  }

  const contextTokens = computed(() => {
    let total = 0
    for (const m of messages.value) {
      total += estimateTokens(m.content || '')
    }
    return total
  })

  /** Target window: 64K. MemGPT research shows hierarchical summarization keeps
   *  quality high at 50-75% of model's effective context. DeepSeek v4 handles 64K
   *  reliably. Overflow triggers pause→archive→summarize→resume cycle. */
  const contextTarget = 64000
  /** Absolute maximum the model supports (fallback if no capability probe data). */
  const contextLimit = computed(() => {
    const p = activeProvider.value
    if (!p) return 128000
    const caps = p.modelCapabilities?.[p.model]
    if (caps?.maxContextTokens && caps.maxContextTokens > 0) return caps.maxContextTokens
    if (p.model?.includes('v4') || p.model?.includes('deepseek')) return 1000000
    return 128000
  })
  const contextPct = computed(() => Math.round((contextTokens.value / contextTarget) * 100))
  const contextLevel = computed(() => contextPct.value < 60 ? 'ok' : contextPct.value < 85 ? 'warn' : 'critical')

  /** MemGPT-style overflow handler: warn → self-save → archive → summarize → resume. */
  async function maybeCompressContext() {
    if (contextPct.value < 85 || busy.value) return
    const cutoff = Math.floor(messages.value.length * 0.6)
    if (cutoff < 6) return

    // Phase 1: Warn the LLM and let it self-save key facts before compression.
    // The LLM can call memory_save tool to persist critical information.
    const warnMsg: ChatMessage = {
      role: 'system' as const,
      content: '⚠ 上下文使用率达 ' + contextPct.value + '%，即将自动压缩早期对话。如有需要长期记住的关键信息，请调用 memory_save 保存。'
    }
    messages.value.splice(cutoff, 0, warnMsg)

    // Phase 2: Archive — save oldest messages to session DB.
    const oldMsgs = messages.value.slice(0, cutoff)
    let dialogue = ''
    for (const m of oldMsgs) {
      if (m.role === 'user') dialogue += '用户: ' + m.content + '\n'
      else if (m.role === 'assistant') dialogue += '助手: ' + (m.content || '').slice(0, 300) + '\n'
    }
    if (!dialogue.trim()) return

    // Phase 3: Summarize via extraction provider.
    try {
      const go = window.go.app.App
      const resp = await go.ChatProxy(
        [{ role: 'system', content: '用中文总结以下对话的关键信息、决定和未解决问题。保留具体数字、文件名、技术决策。最多 300 字。' },
         { role: 'user', content: dialogue }],
        [] as any
      )
      const summary = resp?.choices?.[0]?.message?.content || ''
      if (summary) {
        // Phase 4: Compress + Archive.
        messages.value = [
          { role: 'system' as const, content: '📝 早期对话摘要：' + summary },
          ...messages.value.slice(cutoff),
        ]
        compressedAt.value = 0
        // Save to long-term memory so future memory_recall can retrieve it.
        memoryApi.saveSummary(summary).catch(() => {})
      }
    } catch (e) { console.error('[context] compression failed:', errMsg(e)) }
  }

  const suggestedPrompts = [
    '列出所有已下载的模型',
    '搜索模型市场上的 bert 模型',
    '查看系统信息',
    '列出所有插件',
  ]

  // ── Helpers ──

  function errMsg(e: unknown): string {
    if (typeof e === 'string') return e
    if (e instanceof Error) return e.message
    return String(e)
  }

  function chatRole(r: string): string {
    const map: Record<string, string> = { user: '你', assistant: 'AI', tool: '工具' }
    return map[r] || r
  }

  function chatRender(text: string): string {
    if (!text) return ''
    return marked.parse(text, { breaks: true, gfm: true }) as string
  }

  function toast(type: string, title: string, desc?: string) {
    try { _toastFn?.(type, title, desc) } catch (_) { /* noop */ }
  }

  // ── Actions ──

  async function loadConfig() {
    try {
      providers.value = await window.go.app.App.ListProviders() || []
      const cfg = await window.go.app.App.GetConfig()
      if (cfg?.llm) {
        activeId.value = cfg.llm.activeProvider || ''
      }
    } catch (_) { /* use defaults */ }
  }

  async function loadSkills() {
    try {
      skills.value = await window.go.app.App.ListSkills('') || []
    } catch (_) { /* use defaults */ }
  }

  async function loadAgents() {
    try {
      agents.value = await agentsApi.list() || []
    } catch (_) { /* use defaults */ }
  }

  function selectAgent(id: string) {
    selectedAgentId.value = id
  }

  // ── Session persistence (P0) ──

  /** Load sessions from disk; ensure at least one exists and is selected. */
  async function loadSessions() {
    try {
      sessions.value = await memoryApi.sessionList() || []
    } catch (_) { sessions.value = [] }
    // Clean up stale empty sessions — keep at most one.
    const empties = sessions.value.filter(s => s.title === '新对话')
    if (empties.length > 1) {
      for (let i = 1; i < empties.length; i++) {
        try {
          const count = await memoryApi.messageCount(empties[i].id)
          if (count === 0) {
            await memoryApi.sessionDelete(empties[i].id)
          }
        } catch (_) { /* best-effort */ }
      }
      sessions.value = await memoryApi.sessionList() || []
    }
    if (!sessions.value.length) {
      await createSession()
      return
    }
    const cur = currentSessionId.value
    if (!cur || !sessions.value.find(s => s.id === cur)) {
      await selectSession(sessions.value[0].id)
    }
  }

  /** Create a new empty session and switch to it. Auto-removes the previous session if it was empty. */
  async function createSession() {
    try {
      // If current session is empty (0 messages, default title), delete it first.
      const cur = sessions.value.find(s => s.id === currentSessionId.value)
      if (cur && cur.title === '新对话') {
        const count = await memoryApi.messageCount(currentSessionId.value)
        if (count === 0 && sessions.value.length > 1) {
          await memoryApi.sessionDelete(currentSessionId.value)
          sessions.value = sessions.value.filter(s => s.id !== currentSessionId.value)
        }
      }
      const s = await memoryApi.sessionCreate('', selectedAgentId.value)
      sessions.value.unshift(s)
      await selectSession(s.id)
    } catch (e) {
      toast('error', '新建会话失败', errMsg(e))
    }
  }

  /** Switch to a session, loading only the last 50 messages for fast switching. */
  async function selectSession(id: string) {
    if (currentSessionId.value === id && messages.value.length) return
    currentSessionId.value = id
    expandedTool.value = {}
    hasMoreMessages.value = false
    totalMessageCount.value = 0
    loadMessageFiles()
    try {
      // Load only the most recent 50 messages. Full history loads on demand.
      const PAGE_SIZE = 50
      const [msgs, total] = await Promise.all([
        memoryApi.messageListRecent(id, PAGE_SIZE),
        memoryApi.messageCount(id),
      ])
      totalMessageCount.value = total
      // Restore tool results as separate tool messages so the API is happy.
      const toolMsgs: ChatMessage[] = []
      for (const m of (msgs || [])) {
        if (m.role === 'tool' && m.toolJson) {
          try {
            const tr = JSON.parse(m.toolJson)
            toolMsgs.push({ role: 'tool', content: m.content, tool_call_id: tr.tool_call_id, name: tr.name || '' } as any)
          } catch (_) {}
        }
      }
      const filtered = (msgs || [])
        .filter(m => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => {
          const msg: ChatMessage = { role: m.role as ChatMessage['role'], content: m.content }
          if (m.toolJson) {
            try {
              const extra = JSON.parse(m.toolJson)
              if (extra.tool_calls) {
                msg.tool_calls = extra.tool_calls
                msg.toolCalls = extra.tool_calls.map((tc: any) => ({ name: tc.function?.name || '' }))
              }
              if (extra.tool_results) {
                msg.toolResults = extra.tool_results
              }
              if (extra.reasoning) {
                reasoningContent.value[i] = extra.reasoning
              }
            } catch (_) {}
          }
          return msg
        })
      // Interleave tool messages after their corresponding assistant messages.
      // If any tool_call lacks a matching tool message, strip the tool_calls
      // entirely so the API doesn't see a dangling tool_calls without results.
      const merged: ChatMessage[] = []
      for (const msg of filtered) {
        if (msg.role === 'assistant' && msg.tool_calls?.length) {
          const matchedToolCalls: any[] = []
          const matchedToolMsgs: ChatMessage[] = []
          for (const tc of msg.tool_calls) {
            const tm = toolMsgs.find(t => t.tool_call_id === tc.id)
            if (tm) {
              matchedToolCalls.push(tc)
              matchedToolMsgs.push(tm)
            }
          }
          // Always include the message with its tool calls visible.
          // If tool_results were not persisted (pre-existing data before the
          // toolResults feature), reconstruct them from the matching tool
          // messages so the terminal widget doesn't show "executing…" forever.
          const displayToolResults: any[] = (msg.toolResults || []).slice()
          if (!displayToolResults.length && matchedToolMsgs.length) {
            for (const tm of matchedToolMsgs) {
              try {
                const r = JSON.parse(tm.content || '{}')
                displayToolResults.push({ name: (tm as any).name || '', args: {}, result: r })
              } catch (_) {
                displayToolResults.push({ name: (tm as any).name || '', args: {}, result: { success: false, error: '(解析失败)' } })
              }
            }
          }
          // Back-fill missing slots so every tool_call has a toolResults entry.
          if (displayToolResults.length < msg.tool_calls.length) {
            for (let j = displayToolResults.length; j < msg.tool_calls.length; j++) {
              displayToolResults.push({
                name: msg.tool_calls[j].function?.name || '',
                args: {},
                result: { success: false, error: '(结果丢失)' },
              })
            }
          }
          msg.toolResults = displayToolResults
          merged.push(msg)
          merged.push(...matchedToolMsgs)
        } else {
          merged.push(msg)
        }
      }
      messages.value = merged
      // If the DB has more messages than we loaded, mark for lazy-load.
      hasMoreMessages.value = total > PAGE_SIZE
    } catch (_) {
      messages.value = []
    }
  }

  /** Load earlier (older) messages for the current session. */
  async function loadEarlierMessages() {
    const sid = currentSessionId.value
    if (!sid || !hasMoreMessages.value) return
    const PAGE_SIZE = 50
    try {
      const all = await memoryApi.messageList(sid) || []
      const filtered = all
        .filter(m => m.role === 'user' || m.role === 'assistant')
        .map(m => ({ role: m.role as ChatMessage['role'], content: m.content }))
      messages.value = filtered
      hasMoreMessages.value = false
    } catch (_) { /* keep current messages */ }
  }

  /** Rename a session (title). */
  async function renameSession(id: string, title: string) {
    try {
      await memoryApi.sessionRename(id, title)
      const s = sessions.value.find(x => x.id === id)
      if (s) s.title = title
    } catch (e) {
      toast('error', '重命名失败', errMsg(e))
    }
  }

  /** Delete a session; switch to another (or create one) if it was current. */
  async function deleteSession(id: string) {
    try {
      await memoryApi.sessionDelete(id)
    } catch (e) {
      toast('error', '删除失败', errMsg(e))
      return
    }
    // Clean up file attachments stored in localStorage.
    try { localStorage.removeItem('everevo_files_' + id) } catch (_) {}
    sessions.value = sessions.value.filter(s => s.id !== id)
    if (currentSessionId.value === id) {
      if (sessions.value.length) await selectSession(sessions.value[0].id)
      else await createSession()
    }
  }

  /** "Clear" now means: start a fresh session (history is preserved on disk). */
  function clearMessages() {
    createSession()
  }

  /** Persist a finalized message to the current session (best-effort). Returns the message ID. */
  async function persist(role: 'user' | 'assistant', content: string, toolJSON?: string): Promise<string> {
    const sid = currentSessionId.value
    const text = (content || '').trim()
    if (!sid || (!text && !toolJSON)) return ''
    try {
      const msg = await memoryApi.messageAppend(sid, role, content, toolJSON || '')
      return msg?.id || ''
    } catch (e) {
      console.error('[chat] persist failed:', errMsg(e))
      return ''
    }
  }

  /** Most recent user message text (the recall/remember query anchor). */
  function lastUserContent(): string {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].role === 'user') return messages.value[i].content
    }
    return ''
  }

  /** Sanitize text for embedding/recall: trim to the user's actual question,
   *  dropping bulk file content that was auto-injected. */
  function sanitizeForRecall(text: string): string {
    if (!text) return ''
    // Take the last line(s) after the last double-newline — that's the user's question.
    // File content is prepended; user question is at the end.
    const parts = text.split('\n\n')
    // Take the last 3 segments (user question + any short context)
    let clean = parts.slice(-3).join(' ').replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F-\x9F]/g, '').trim()
    if (clean.length > 1000) clean = clean.slice(0, 1000)
    return clean
  }

  function toggleToolResult(msgIdx: number, toolIdx: number) {
    const key = `${msgIdx}-${toolIdx}`
    expandedTool.value[key] = !expandedTool.value[key]
  }

  /** Add a parsed file to the pending list (caller handles text extraction). */
  function addFile(file: PendingFile) {
    // Avoid duplicates by name
    if (pendingFiles.value.some(f => f.name === file.name)) return
    pendingFiles.value.push(file)
  }
  function removeFile(name: string) {
    pendingFiles.value = pendingFiles.value.filter(f => f.name !== name)
  }
  function clearFiles() {
    pendingFiles.value = []
  }
  /** Map from message index to files attached to that message. */
  const messageFiles = ref<Record<number, PendingFile[]>>({})

  /** Snapshot the current pending files to a message index before sending. */
  function attachFilesToMessage(msgIdx: number) {
    if (pendingFiles.value.length) {
      messageFiles.value[msgIdx] = [...pendingFiles.value]
      persistMessageFiles()
    }
  }

  /** Persist messageFiles to localStorage keyed by session. */
  function persistMessageFiles() {
    const sid = currentSessionId.value
    if (!sid) return
    try {
      localStorage.setItem('everevo_files_' + sid, JSON.stringify(messageFiles.value))
    } catch (_) { /* quota exceeded — best effort */ }
  }

  /** Restore messageFiles from localStorage for the current session. */
  function loadMessageFiles() {
    const sid = currentSessionId.value
    if (!sid) { messageFiles.value = {}; return }
    try {
      const raw = localStorage.getItem('everevo_files_' + sid)
      messageFiles.value = raw ? JSON.parse(raw) : {}
    } catch (_) { messageFiles.value = {} }
  }

  async function sendMessage(text: string) {
    const t = (text || '').trim()
    const hasFiles = pendingFiles.value.length > 0
    if (!t && !hasFiles) return

    // Build clean user message with file content auto-injected (ChatGPT-style).
    // Readable text files → content prepended cleanly. Scanned/unsupported → just the user
    // question. File cards below the bubble handle visual display.
    let displayText = t
    if (hasFiles) {
      const isImageExt = (e: string) => ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg'].includes(e)
      const readable = pendingFiles.value.filter(f => !f.isScanned && f.preview && !isImageExt(f.type))
      const imageFiles = pendingFiles.value.filter(f => isImageExt(f.type))
      const scannedFiles = pendingFiles.value.filter(f => f.isScanned)
      if (readable.length && !t) {
        // No user question → just the file content with a default prompt
        const parts = readable.map(f => f.preview.length > 4000 ? f.preview.slice(0, 4000) + '\n…(内容过长已截断)' : f.preview)
        displayText = parts.join('\n\n') + '\n\n请分析以上文件内容。'
      } else if (readable.length && t) {
        // User question + file content
        const parts = readable.map(f => f.preview.length > 4000 ? f.preview.slice(0, 4000) + '\n…(内容过长已截断)' : f.preview)
        displayText = parts.join('\n\n') + '\n\n' + t
      } else if (!t) {
        displayText = '请分析上传的文件。'
      }
      // Append image file hints so the LLM knows to use read_media_file
      if (imageFiles.length) {
        const imgPaths = imageFiles.map(f => f.path).join('\n')
        displayText = displayText + '\n\n[用户上传了 ' + imageFiles.length + ' 张图片，路径如下，请使用 read_media_file 工具查看：\n' + imgPaths + ']'
      }
      if (scannedFiles.length) {
        const scanPaths = scannedFiles.map(f => f.path).join('\n')
        displayText = displayText + '\n\n[用户上传了 ' + scannedFiles.length + ' 个扫描件 PDF，路径如下：\n' + scanPaths + ']'
      }
    }

    inputText.value = ''
    const msgIdx = messages.value.length
    attachFilesToMessage(msgIdx)
    pendingFiles.value = []
    messages.value.push({ role: 'user', content: displayText })
    await persist('user', displayText)
    busy.value = true

    try {
      await chatLoop()
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e)
      console.error('[chat] chatLoop error:', msg)
      messages.value.push({ role: 'assistant', content: '❗ 错误: ' + msg })
    }
    busy.value = false
  }

  // ── Chat loop (internal) ──

  async function chatLoop() {
    const go = window.go.app.App
    const rt = window.runtime

    // Resolve chat inputs. A selected local Agent persona drives its own
    // system prompt + tool set + provider/model override; otherwise fall back
    // to the global enabled-skills behavior (the default main agent is equivalent).
    let systemContent: string
    let tools: ToolDef[]
    let agentStream: { providerId: string; model: string; temperature: number; maxTokens: number } | null = null

    const selId = selectedAgentId.value
    if (selId) {
      try {
        const ctx = await agentsApi.getChatContext(selId)
        systemContent = ctx.systemPrompt
        tools = (ctx.tools || []) as ToolDef[]
        const ag = agents.value.find(a => a.id === selId)
        // Inject thinking instruction when thinkMode is on.
        if (thinkMode.value) {
          const effortHint = thinkEffort.value === 'max' ? 'Think deeply and exhaustively before answering.' : 'Think briefly before answering.'
          systemContent += '\n\nYou MUST think before answering. Use internal reasoning in English to plan your approach, then respond to the user in Chinese. ' + effortHint
        }
        agentStream = {
          providerId: ctx.providerId || '',
          model: ctx.model || '',
          temperature: ag?.temperature != null ? ag.temperature : -1,
          maxTokens: ag?.maxTokens || 0,
        }
      } catch (e: unknown) {
        messages.value.push({ role: 'assistant', content: '❗ 加载 Agent 失败: ' + errMsg(e) })
        return
      }
      // P10: Append domain context even when using a specific agent.
      const libId = activeLibraryId.value
      if (libId) {
        try {
          const domainCtx = await go.BuildDomainSystemPrompt(libId)
          if (domainCtx) systemContent += '\n' + domainCtx
        } catch (_) { /* best-effort */ }
      }
    } else {
      let enabledNames: string[]
      try { enabledNames = (await go.GetEnabledToolNames()) || [] } catch (_) { enabledNames = [] }

      let allTools: ToolDef[]
      try { allTools = (await go.ListTools()) || [] } catch (_) { allTools = [] }

      const isExternal = (n: string) => n.startsWith('mcp__')
      tools = allTools.filter(t => enabledNames.includes(t.name) || isExternal(t.name))

      if (!tools.length) {
        messages.value.push({
          role: 'assistant',
          content: '⚠ 没有启用的工具。请先在「能力清单」中启用至少一个 Skill，或连接外部 MCP Server。',
        })
        return
      }

      // Build system prompt from enabled skills — domain-scoped.
      let enabledSkills: SkillInfo[]
      try { enabledSkills = (await go.ListEnabledSkills(activeLibraryId.value)) || [] } catch (_) { enabledSkills = [] }

      const basePrompt = '你是 EverEvo 桌面软件的 AI 助手。用户说中文，用中文回复。当需要执行操作时使用工具调用。每次回复尽量简洁。\n\n用户可能通过拖拽或粘贴上传文件到对话中。对于文本文件（TXT、MD、CSV、JSON 等），内容会自动注入。对于 PDF 和图片文件，请使用 read_file 或 read_media_file 工具读取。对于扫描件 PDF（isScanned=true），请使用 read_media_file 工具以图片形式查看页面。'
      const skillPrompts = enabledSkills
        .filter(s => s.systemPrompt)
        .map(s => `【${s.title}】${s.systemPrompt}`)
        .join('\n')
      systemContent = skillPrompts
        ? `${basePrompt}\n\n当前启用的能力角色：\n${skillPrompts}`
        : basePrompt

      // P10: Inject domain-scoped context (agents, skills, MCP for this domain only).
      const libId = activeLibraryId.value
      if (libId) {
        try {
          const domainCtx = await go.BuildDomainSystemPrompt(libId)
          if (domainCtx) systemContent += '\n' + domainCtx
        } catch (_) { /* best-effort: domain context is additive, not required */ }
      }
    }

    // Think mode: inject English reasoning instruction with effort level.
    if (thinkMode.value) {
      const effortHint = thinkEffort.value === 'max' ? 'Think deeply and exhaustively before answering.' : 'Think briefly before answering.'
      systemContent += '\n\nYou MUST think before answering. Use internal reasoning in English to plan your approach, then respond to the user in Chinese. ' + effortHint
    }

    // P1.5: inject long-term semantic memory (cross-session recall). Two-pass:
    // relevant past Q&A turns + extracted facts. Empty when no embedding model
    // is bound — zero impact on the chat.
    const userQuery = sanitizeForRecall(lastUserContent())
    if (userQuery) {
      try {
        const result = await memoryApi.recall(userQuery, 3) || { turns: [], facts: [], graph: '', graphTrace: { seedIds: [], edgeIds: [] }, core: [] }
        // P7.3: rule-based library routing — which domain libraries match this query?
        let libMatch = ''
        const q = userQuery.toLowerCase()
        if (q) {
          try {
            const libs = await memoryApi.libraryList()
            const matched: string[] = []
            for (const lib of (libs || [])) {
              let tags: string[] = []
              try { tags = JSON.parse(lib.tags || '[]') } catch (_) {}
              if (typeof tags === 'string') try { tags = JSON.parse(tags) } catch (_) {}
              const terms = [lib.name.toLowerCase(), lib.description.toLowerCase(), ...tags.map((t: string) => t.toLowerCase())]
              if (terms.some((t: string) => t && q.includes(t))) matched.push(lib.name)
            }
            if (matched.length && matched[0] !== '默认') libMatch = '领域库匹配：' + matched.join(', ')
          } catch (_) {}
        }
        const parts: string[] = []
        if (libMatch) parts.push(libMatch)
        // P5: forced core memory (identity/preferences) — always injected, never decayed.
        if (result.core?.length) {
          parts.push('核心记忆（身份与偏好，永久）：\n' + result.core.map((f: any) => `- ${f.value}`).join('\n'))
        }
        // P3.6: session summary (already loaded via sessionList).
        const curSession = sessions.value.find(s => s.id === currentSessionId.value)
        if (curSession?.summary) {
          parts.push('会话摘要：\n' + curSession.summary)
        }
        if (result.turns?.length) {
          parts.push('相关历史问答：\n' + result.turns.map((t, i) => `${i + 1}. 问：${t.content}\n   答：${t.reply}`).join('\n'))
        }
        if (result.facts?.length) {
          parts.push('已知事实：\n' + result.facts.map((f, i) => `${i + 1}. [${f.category}] ${f.content}`).join('\n'))
        }
        if (result.graph) {
          parts.push('知识图谱（相关实体与关系）：\n' + result.graph)
        }
        // P8: distilled experience recall
        try {
          const expItems = await memoryApi.recallExperience(activeLibraryId.value, 3) || []
          if (expItems.length) {
            parts.push('经验教训（过去的反思沉淀）：\n' + expItems.map((e: any) => `- [${e.kind}] ${e.content}`).join('\n'))
          }
        } catch (_) { /* best-effort */ }
        lastGraphTrace.value = result.graphTrace || { seedIds: [], edgeIds: [] }
        if (parts.length) {
          let memBlock = parts.join('\n')
          if (memBlock.length > 1200) memBlock = memBlock.slice(0, 1200) + '…'
          systemContent += '\n\n长期记忆（与当前问题相关的过往信息，仅供参考，可能过时）：\n' + memBlock
        }
      } catch (e) { console.error('[chat] recall failed:', errMsg(e)) }
      // P6.1: project-docs recall (llmwiki) — surface relevant design/task notes.
      try {
        const wiki = await wikiApi.recall(activeLibraryId.value, userQuery)
        if (wiki) systemContent += '\n\n项目文档（与问题相关的设计/任务记录）：\n' + wiki
      } catch (_) {}
	      // P9: RAG knowledge base recall — auto-inject relevant KB chunks into context.
	      try {
	        const ragHits = await knowledgeApi.searchAllKBs(userQuery, activeLibraryId.value, 6, 3)
	        if (ragHits?.length) {
	          let ragBlock = ragHits.map((h: any) =>
	            `[${h.kbName}, ${(h.similarity * 100).toFixed(0)}%] ${h.content}`
	          ).join('\n')
	          if (ragBlock.length > 1500) ragBlock = ragBlock.slice(0, 1500) + '…'
	          systemContent += '\n\n知识库检索结果（来自知识库的相关文档片段，请参考这些内容回答用户问题）：\n' + ragBlock
	        }
	      } catch (_) {}
	    }

    const apiMsgs: APIMessage[] = normalizeToolMessages([
      { role: 'system', content: systemContent },
      ...messages.value.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'tool'),
    ])

    // No round cap (requested): the loop runs until the model returns a final
    // answer (no more tool calls) or the user stops it. No iteration /
    // productive budgets.
    stopRequested.value = false
    currentStreamId.value = ''
    let round = 0
    for (;;) {
      if (stopRequested.value) { busy.value = false; currentStreamId.value = ''; return }
      round++
      // MemGPT-style: emergency compression mid-loop (multi-turn spillover).
      if (contextPct.value > 50 && round > 5 && round % 3 === 0) {
        await maybeCompressContext()
      }
      const msgIdx = messages.value.length
      messages.value.push({ role: 'assistant', content: '' })

      try {
        const streamId = 's' + Date.now() + '_' + Math.random().toString(36).slice(2)
        currentStreamId.value = streamId
        const chunkEvent = `chat-chunk-${streamId}`
        const doneEvent = `chat-done-${streamId}`
        const errEvent = `chat-err-${streamId}`

        const chunkHandler = (data: StreamChunkData & { reasoning?: string }) => {
          if (data?.text) {
            messages.value[msgIdx].content += data.text
          }
          if (data?.reasoning) {
            if (!reasoningContent.value[msgIdx]) reasoningContent.value[msgIdx] = ''
            reasoningContent.value[msgIdx] += data.reasoning
          }
        }
        rt.EventsOn(chunkEvent, chunkHandler)

        const resp = await new Promise<StreamDoneData>((resolve, reject) => {
          const doneHandler = (data: StreamDoneData) => {
            rt.EventsOff(doneEvent, doneHandler)
            rt.EventsOff(errEvent, errHandler)
            rt.EventsOff(chunkEvent, chunkHandler)
            resolve(data)
          }
          const errHandler = (data: StreamErrorData) => {
            rt.EventsOff(doneEvent, doneHandler)
            rt.EventsOff(errEvent, errHandler)
            rt.EventsOff(chunkEvent, chunkHandler)
            reject(new Error(data?.error || 'Stream error'))
          }
          rt.EventsOn(doneEvent, doneHandler)
          rt.EventsOn(errEvent, errHandler)
          const toolPayload = tools.map(t => ({
            type: 'function',
            function: {
              name: t.name,
              description: t.description,
              // Prefer raw MCP inputSchema to avoid schema fidelity loss;
              // fall back to typed parameters for internal tools.
              parameters: t.rawParameters || t.parameters,
            },
          }))
          const effort = thinkMode.value ? thinkEffort.value : ''
          // Normalize before every send: guarantee every assistant.tool_calls
          // is followed by a matching tool result (DeepSeek/OpenAI reject
          // dangling tool_calls with HTTP 400). Cheap defensive copy per turn.
          const sendMsgs = normalizeToolMessages(apiMsgs)
          if (agentStream) {
            agentsApi.streamAs(streamId, sendMsgs, toolPayload, agentStream.providerId, agentStream.model, agentStream.temperature, agentStream.maxTokens, effort).catch(reject)
          } else {
            go.ChatStream(streamId, sendMsgs, toolPayload, effort).catch(reject)
          }
        })

        if (!resp?.choices?.length) {
          if ((resp as any)?.cancelled) {
            // User stopped — keep partial content, mark as stopped.
            if (!messages.value[msgIdx].content) messages.value[msgIdx].content = '⏸ 已停止'
          } else {
            messages.value[msgIdx].content = '❗ API 返回为空'
          }
          currentStreamId.value = ''
          return
        }

        const msg = resp.choices[0].message
        // Replace placeholder with full message (preserve tool_calls)
        messages.value[msgIdx] = {
          role: 'assistant',
          content: msg.content || '',
          tool_calls: msg.tool_calls || undefined,
          toolCalls: msg.tool_calls ? msg.tool_calls.map((tc: ToolCall) => ({ name: tc.function.name })) : undefined,
        }
        // Persist the finalized assistant turn with tool_calls + reasoning.
        // Must await so the message is saved before autoTitleSession counts it.
        const extra: any = {}
        if (msg.tool_calls) extra.tool_calls = msg.tool_calls
        if (reasoningContent.value[msgIdx]) extra.reasoning = reasoningContent.value[msgIdx]
        const toolJSON = Object.keys(extra).length ? JSON.stringify(extra) : ''
        const persistedId = await persist('assistant', msg.content || '', toolJSON)
        // Auto-title: rename "新对话" sessions after enough rounds.
        // Best-effort; failures are silent inside the backend goroutine.
        memoryApi.sessionAutoTitle(currentSessionId.value)
        // Auto-compress context when approaching the model's limit.
        maybeCompressContext()
        // P1.5: remember the final answer (not tool-only rounds) for future recall.
        if (!msg.tool_calls?.length && msg.content) {
          const u = lastUserContent()
          if (u) {
            memoryApi.remember(u, msg.content, currentSessionId.value, activeLibraryId.value)
              .catch(e => console.error('[chat] remember failed:', errMsg(e)))
          }
        }

        if (msg.tool_calls?.length) {
          messages.value[msgIdx].toolResults = []
          apiMsgs.push({ role: 'assistant', content: msg.content || '', tool_calls: msg.tool_calls })
          for (const tc of msg.tool_calls) {
            let args: Record<string, unknown> = {}
            try { args = JSON.parse(tc.function.arguments || '{}') } catch (_) { /* keep empty */ }
            if (stopRequested.value) {
              messages.value[msgIdx].toolResults!.push({ name: tc.function.name, args, result: { success: false, error: '用户已终止' } })
              break
            }
            // Push placeholder so the UI immediately shows "executing…" before
            // the (potentially slow) CallTool resolves. The startedAt field
            // lets the UI show elapsed time so the user can judge stuck vs slow.
            const idx = messages.value[msgIdx].toolResults!.length
            messages.value[msgIdx].toolResults!.push({ name: tc.function.name, args, result: null as any, startedAt: Date.now() })
            messages.value[msgIdx] = { ...messages.value[msgIdx] } // trigger reactivity for placeholder
            let result: ToolCallResult
            // collab_wait is event-driven: subscribe to agent.<id>.done events
            // so the UI can show each sub-agent finishing in real time instead
            // of blocking silently on the backend. No per-tool timeout — the
            // wait naturally ends when all runs complete.
            if (tc.function.name === 'collab_wait') {
              result = await waitForCollabRuns(args)
            } else {
              const TOOL_TIMEOUT = 60_000
              const callPromise = go.CallTool(tc.function.name, args) as Promise<ToolCallResult>
              try {
                result = await Promise.race([
                  callPromise,
                  new Promise<never>((_, reject) => setTimeout(() => reject(new Error('工具调用超时 (60s)')), TOOL_TIMEOUT)),
                ])
              } catch (e: unknown) {
                const msg = e instanceof Error ? e.message : String(e)
                result = { success: false, error: msg }
                // The backend is still running — when it finishes, replace the
                // timeout error with the real result so the conversation (and
                // the terminal widget) can see it in later rounds.
                const toolCallId = tc.id
                callPromise.then((late) => {
                  // Update terminal display (tool result block)
                  if (idx < messages.value[msgIdx].toolResults!.length) {
                    messages.value[msgIdx].toolResults![idx].result = late
                    messages.value[msgIdx] = { ...messages.value[msgIdx] }
                  }
                  // Swap the "超时" tool message for the real result so the LLM
                  // can consume it in the next round.
                  for (let k = messages.value.length - 1; k >= 0; k--) {
                    const mk = messages.value[k]
                    if (mk.role === 'tool' && mk.tool_call_id === toolCallId && (mk.content || '').includes('超时')) {
                      mk.content = JSON.stringify(late)
                      break
                    }
                  }
                }).catch(() => {})
              }
            }
            // Swap placeholder with real result and force reactivity so the
            // terminal widget transitions from "executing…" to "done/error".
            if (idx < messages.value[msgIdx].toolResults!.length) {
              messages.value[msgIdx].toolResults![idx].result = result
              messages.value[msgIdx] = { ...messages.value[msgIdx] }
            }
            const toolMsg = { role: 'tool' as const, tool_call_id: tc.id, content: JSON.stringify(result) }
            apiMsgs.push(toolMsg)
            messages.value.push(toolMsg)
            // Persist tool result as a tool message so it can be restored.
            const toolJson = JSON.stringify({ tool_call_id: tc.id, name: tc.function.name, args })
            memoryApi.messageAppend(currentSessionId.value, 'tool', JSON.stringify(result), toolJson).catch(() => {})
          }
          // Update assistant message with tool execution results.
          // Backend uses JSON merge (not replace), so reasoning from the
          // earlier persist is preserved automatically.
          if (persistedId && messages.value[msgIdx].toolResults?.length) {
            const updated: any = {
              tool_results: messages.value[msgIdx].toolResults!.map(tr => ({
                name: tr.name, args: tr.args, result: tr.result,
              })),
            }
            memoryApi.messageUpdateToolJSON(persistedId, JSON.stringify(updated)).catch(() => {})
          }
          continue
        }

        if (msg.content) {
          apiMsgs.push({ role: 'assistant', content: msg.content })
        }
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e)
        console.error('[chat] round error:', msg)
        messages.value[msgIdx].content = '❗ 错误: ' + msg
      }
      return
    }
    // The for(;;) above has no break — the function returns from inside the loop.
    // This line is unreachable and satisfies TypeScript's control-flow analysis.
  }

  return {
    // state
    messages, inputText, busy, expandedTool, providers, activeId, skills, agents, selectedAgentId,
    sessions, currentSessionId, pendingFiles, messageFiles, hasMoreMessages, totalMessageCount, thinkMode, thinkEffort, stopRequested, currentStreamId, reasoningContent, compressedAt,
    contextTokens, contextTarget, contextLimit, contextPct, contextLevel,
    // getters
    activeProvider, chatReady, suggestedPrompts, selectedAgentName, visibleAgents, activeLibraryId,
    // helpers
    errMsg, chatRole, chatRender,
    // actions
    loadConfig, loadSkills, loadAgents, selectAgent, clearMessages, sendMessage, toggleToolResult,
    loadSessions, createSession, selectSession, renameSession, deleteSession,
    addFile, removeFile, clearFiles,
    loadEarlierMessages,
    maybeCompressContext,
  }
})

// ── Toast injection (called by App.vue during setup) ──

export function setChatToast(fn: (type: string, title: string, desc?: string) => void) {
  _toastFn = fn
}
