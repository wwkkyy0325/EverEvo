import { defineStore } from 'pinia'
import { ref, computed, nextTick } from 'vue'
import { marked } from 'marked'
import { agentsApi } from '../api/agents'
import type { LocalAgent } from '../api/agents'
import { memoryApi } from '../api/memory'
import { wikiApi } from '../api/wiki'
import { knowledgeApi } from '../api/knowledge'
import { useActiveLibrary } from '../composables/useActiveLibrary'
import { useAsyncStore } from './asyncStore'
import { getModelProfile, type ModelProfile } from '../config/modelProfiles'
import type { MemorySession, MemoryMessage } from '../api/memory'

// Cross-view: last graph recall trace (seed/edge ids). The Knowledge viewer reads
// this to highlight which part of the graph the most recent chat answer used.
export const lastGraphTrace = ref<{ seedIds: string[]; edgeIds: string[] }>({ seedIds: [], edgeIds: [] })

// ── Types ──

export interface ChatMessage {
  id?: string                    // stable key for v-for; preserved from backend or generated
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
  let _chatBoxRef: HTMLElement | null = null
  let _loadingMore = false
  const thinkMode = ref(true)              // extended thinking toggle, default ON
  const thinkEffort = ref<'high' | 'max'>('high')  // effort level (high=default, max=deep)
  const stopRequested = ref(false)          // set to true to stop current generation

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

  const currentStreamId = ref('')            // track active stream for cancellation
  const reasoningContent = ref<Record<number, string>>({})  // reasoning per message index
  const compressedAt = ref<number>(-1)  // message index where auto-compression happened
  const compressionMarker = ref<number>(-1) // index of the compression divider (apiMsgs starts after this)
  const showCompressedHistory = ref(false) // user toggled to view messages before marker
  const historyVisibleCount = ref(0)        // progressive reveal: how many history msgs to show
  const compressedRoundCount = ref(0)       // round count at compression time (frozen, not reactive to load-more)
  const HISTORY_BATCH = 20                  // messages per scroll-up batch
  function revealMoreHistory(): number {
    if (compressionMarker.value < 0) return 0
    const maxVisible = compressionMarker.value
    if (historyVisibleCount.value >= maxVisible) return 0
    const prev = historyVisibleCount.value
    historyVisibleCount.value = Math.min(maxVisible, prev + HISTORY_BATCH)
    showCompressedHistory.value = true
    return historyVisibleCount.value - prev
  }
  /** Find a clean conversation-round boundary near the given position. */
  function _findRoundBoundary(nearPct: number): number {
    const target = Math.floor(messages.value.length * nearPct)
    // Scan forward from target for a user message that follows a completed round.
    for (let i = Math.max(4, target); i < messages.value.length - 2; i++) {
      const m = messages.value[i]
      if (m.role !== 'user') continue
      // This user message starts a new round. Check that the previous round is
      // complete: the message before this user should be assistant or tool (not
      // a dangling assistant with pending tool_calls).
      const prev = messages.value[i - 1]
      if (!prev) continue
      if (prev.role === 'assistant') {
        // If assistant has tool_calls, it must have results (final answer after tools).
        if (prev.tool_calls?.length && !prev.toolResults?.length) continue
        return i
      }
      if (prev.role === 'tool') return i // tool result = previous round complete
    }
    // Fallback: just find any user message.
    for (let i = Math.max(4, target); i < messages.value.length; i++) {
      if (messages.value[i].role === 'user') return i
    }
    return target
  }

  /** Count conversation rounds (user messages) in the given message range. */
  function _countRounds(msgs: ChatMessage[]): number {
    let count = 0
    for (const m of msgs) if (m.role === 'user') count++
    return count
  }

  function hideCompressedHistory() {
    showCompressedHistory.value = false
    historyVisibleCount.value = 0
  }

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

  // ═══════════════════════════════════════════════════════════════════════
  // ── Context Window Management ──
  //
  // Based on:
  //   MemGPT (Packer et al., arXiv 2310.08560) — virtual context / paging
  //   Lost in the Middle (Liu et al., TACL 2023, arXiv 2307.03172) — U-shaped attention
  //   CoMem (Zhang et al., ICML 2026) — async decoupled compression pipeline
  //   Factory AI (2025) — structured incremental summaries
  //   Anthropic Context Engineering (2025) — compaction + note-taking
  //
  // Architecture:
  //   ┌─────────────────────────────────────────────────────┐
  //   │  CONTEXT BUDGET MODEL (modeled on MemGPT tiers)     │
  //   ├─────────────────────────────────────────────────────┤
  //   │  FIXED OVERHEAD (counted, rarely change):           │
  //   │    System Prompt     10-15%  (identity + domain)    │
  //   │    Tool Definitions   8-12%  (enabled tools)        │
  //   │    Core Memory        3-5%   (locked facts)         │
  //   │                                                     │
  //   │  DYNAMIC CONTENT (per-session, grows):              │
  //   │    Recall / RAG       5-20%  (memory + KB chunks)   │
  //   │    Conversation       40-60% (recent turns raw)     │
  //   │    Tool I/O           5-15%  (current turn results) │
  //   │                                                     │
  //   │  RESERVED (not sent, but budgeted):                 │
  //   │    Model Response     10-15% (generation headroom)  │
  //   │    Thinking Tokens     5-10% (if think mode on)     │
  //   │    Safety Margin       5-8%  (estimation variance)  │
  //   └─────────────────────────────────────────────────────┘
  //
  //  Staged triggers (evidence-backed thresholds):
  //    < 50%  → Normal operation
  //    50%    → Background pre-compute summary (async, non-blocking)
  //    65%    → Inject soft system-note (LLM self-manages)
  //    80%    → Apply pre-computed summary + truncate (instant)
  //    90%    → Hard truncation (safety net, drop from middle first)
  // ═══════════════════════════════════════════════════════════════════════

  /**
   * Estimate token count using a conservative heuristic calibrated for BPE
   * tokenizers (DeepSeek / OpenAI / Claude all use similar sub-word schemes).
   *
   * Observed ratios (conservative → slight over-estimate for safety):
   *   Chinese (CJK):  ~0.8–1.5 chars/token  → use 1.0
   *   English ASCII:  ~2.5–4.0 chars/token  → use 2.5
   *   Code / mixed:   ~1.5–3.0 chars/token  → treated as CJK-like
   *
   * 1.15× safety margin covers estimation variance.
   */
  const EST_SAFETY = 1.15
  const MSG_OVERHEAD = 4 // role label + JSON framing per message in the API array

  function estimateTokens(text: string): number {
    if (!text) return 0
    let cjk = 0, ascii = 0
    for (const ch of text) {
      const code = ch.charCodeAt(0)
      if ((code >= 0x4E00 && code <= 0x9FFF) ||
          (code >= 0x3400 && code <= 0x4DBF) ||
          (code >= 0x3000 && code <= 0x303F) ||
          (code >= 0xFF00 && code <= 0xFFEF) ||
          (code >= 0x2000 && code <= 0x206F) ||
          code > 127) {
        cjk++
      } else if (code < 128) {
        ascii++
      }
    }
    return Math.ceil(EST_SAFETY * (cjk / 1.0 + ascii / 2.5))
  }

  // ── Token counters (reactive — set in chatLoop, tracked by computed) ──
  const _sysPromptTokens = ref(0)   // system prompt
  const _toolDefTokens = ref(0)     // tool definitions (JSON schemas)
  const _memRagTokens = ref(0)      // memory recall + RAG chunks injected into system prompt

  // ── Model profile (declarative, per-model config) ──
  // Replaces the old heuristic contextLimit / contextTarget / contextReserved
  // with explicit per-model profiles. Edit ../config/modelProfiles.ts to tune.
  const modelProfile = computed<ModelProfile>(() => {
    const p = activeProvider.value
    return getModelProfile(p?.name, p?.model)
  })

  /** Model's absolute context window (from the active model profile). */
  const contextLimit = computed(() => modelProfile.value.contextWindow)

  /**
   * Usable input budget: context_window × effectivePct.
   * Codex uses 95% (reserves 5% for system overhead + model output).
   */
  const contextTarget = computed(() =>
    Math.floor(contextLimit.value * modelProfile.value.effectivePct / 100))

  /**
   * Reserved budget: tokens set aside for model output.
   * Simple: the gap between context window and effective target.
   */
  const contextReserved = computed(() =>
    contextLimit.value - contextTarget.value)

  /** Usable budget: what we can actually send (target minus reserved). */
  const contextUsable = computed(() => Math.max(0, contextTarget.value - contextReserved.value))

  // ── Computed context state ──
  /** Messages BEFORE the compression marker (hidden history, not sent to API). */
  const historyMessages = computed(() => {
    const marker = compressionMarker.value
    if (marker < 0) return []
    return messages.value.slice(0, marker)
  })
  /** Number of conversation rounds in the hidden history. */
  const historyRoundCount = computed(() => _countRounds(historyMessages.value))
  /** Messages that will actually be sent to the API (after compression marker). */
  const apiMessages = computed(() => {
    const marker = compressionMarker.value
    if (marker < 0) return messages.value
    // Include the compression summary (marker message) + everything after it.
    return messages.value.slice(marker)
  })

  const contextTokens = computed(() => {
    let total = _sysPromptTokens.value + _toolDefTokens.value + _memRagTokens.value
    for (const m of apiMessages.value) {
      total += estimateTokens(m.content || '') + MSG_OVERHEAD
      if (m.tool_calls?.length) total += m.tool_calls.length * 20
    }
    // Reasoning content is stored separately in reasoningContent (not in m.content),
    // so it was never added to `total` — no need to subtract it.
    // Math.max(0, …) guards against estimation variance producing negative values.
    return Math.max(0, total)
  })

  /** Per-round context growth logging (helps diagnose what's consuming tokens). */
  let _lastLoggedTokens = 0
  function _logContextGrowth(round: number) {
    const now = contextTokens.value
    const delta = now - _lastLoggedTokens
    const bd = contextBreakdown.value
    console.log('[context] round ' + round
      + ' | total=' + fmtTokens(now) + ' (Δ+' + fmtTokens(delta) + ')'
      + ' | sys=' + fmtTokens(bd.system) + ' tools=' + fmtTokens(bd.tools)
      + ' | mem=' + fmtTokens(bd.memory) + ' msgs=' + fmtTokens(bd.messages)
      + ' | pct=' + contextPct.value + '%'
      + ' | toolMsgs=' + messages.value.filter(m => m.role === 'tool').length)
    _lastLoggedTokens = now
  }

  /** Context budget breakdown (shown in progress-bar tooltip). */
  const contextBreakdown = computed(() => ({
    system: _sysPromptTokens.value,
    tools: _toolDefTokens.value,
    memory: _memRagTokens.value,
    messages: Math.max(0, contextTokens.value - _sysPromptTokens.value - _toolDefTokens.value - _memRagTokens.value),
    reserved: contextReserved.value,
    total: contextTokens.value,
    usable: contextUsable.value,
    limit: contextLimit.value,
    target: contextTarget.value,
  }))

  /** % of usable budget (for display label). */
  const contextPct = computed(() =>
    Math.min(999, Math.round((contextTokens.value / Math.max(1, contextUsable.value)) * 100)))
  /** % for progress bar width (capped at 100%). */
  const contextBarPct = computed(() => Math.min(100, contextPct.value))
  /** % of model's absolute limit. */
  const contextLimitPct = computed(() =>
    Math.round((contextTokens.value / Math.max(1, contextLimit.value)) * 100))

  const contextLevel = computed(() => {
    const p = contextPct.value
    if (p >= 90) return 'danger'
    if (p >= 80) return 'critical'
    if (p >= 65) return 'warn'
    return 'ok'
  })

  /**
   * Structured summary prompt — produces sectioned output so compressed context
   * preserves critical information in a searchable format (Claude compaction style).
   * Sections: decisions, unsolved, files, constraints.
   */
  const STRUCTURED_SUMMARY_PROMPT = `用中文总结以下对话，按固定格式输出（不要用markdown标题，用【】标号）：

【关键决策】- 用户做了什么重要决定？选择了什么方案？每条一行。
【未解决问题】- 哪些问题还没解决？错误还在排查？每条一行。
【涉及文件】- 提到了哪些文件路径？改了哪些代码？每条一行。
【约束条件】- 用户提出了什么硬性要求？预算/技术栈/时间限制？
【下一步】- 接下来应该做什么？

每条保持简洁（10-20字），总字数不超过300字。只记事实不推测。保留具体的数字、文件名、API名称、错误信息原文。`

  // ── Async pre-compute state ──
  /** Cached summary from background compression (CoMem-style async pipeline). */
  let _pendingSummary: string | null = null
  let _precomputing = false

  /**
   * Fire-and-forget background summarization. When the result arrives, it's
   * cached in _pendingSummary. The next compression trigger applies it instantly
   * without blocking the chat loop (CoMem-style k-step-off pipeline).
   */
  async function _precomputeSummary(dialogue: string) {
    if (_precomputing || _pendingSummary) return
    _precomputing = true
    try {
      const go = window.go.app.App
      const PRE_COMPUTE_TIMEOUT = 30_000
      const resp = await Promise.race([
        go.ChatProxy(
          [{ role: 'system', content: STRUCTURED_SUMMARY_PROMPT },
           { role: 'user', content: dialogue }],
          [] as any
        ),
        new Promise<never>((_, reject) =>
          setTimeout(() => reject(new Error('precompute timeout')), PRE_COMPUTE_TIMEOUT)),
      ])
      const summary = resp?.choices?.[0]?.message?.content || ''
      if (summary) {
        _pendingSummary = summary
        memoryApi.saveSummary(summary).catch(() => {})
      }
    } catch (e) { console.error('[context] precompute failed:', errMsg(e)) }
    finally { _precomputing = false }
  }

  /**
   * Force-compress: user clicked the button → do synchronous summarisation
   * regardless of percentage stage. Uses cached summary if available.
   */
  /**
   * Logical compression: keep full history in messages.value for scrolling,
   * but mark a split point so apiMsgs only includes summary + recent context.
   * Old messages remain viewable in the UI — only the API context is reduced.
   */
  async function _forceCompress(): Promise<string> {
    // Find a clean round boundary near 60%.
    const cutoff = _findRoundBoundary(0.6)
    if (cutoff < 4) return ''

    // Collect any existing compression summaries (from previous compressions).
    let priorSummary = ''
    for (const m of messages.value.slice(0, cutoff)) {
      if (m.role === 'system' && (m.content.includes('📦 上下文压缩') || m.content.includes('上下文已截断'))) {
        priorSummary += m.content.replace(/^━━━.*━━━\n*/, '') + '\n'
      }
    }

    // ── Summarise the older portion ──
    const summary = _pendingSummary
    _pendingSummary = null
    let summaryText = summary

    if (!summaryText) {
      const oldMsgs = messages.value.slice(0, cutoff)
      let dialogue = ''
      for (const m of oldMsgs) {
        if (m.role === 'user') dialogue += '用户: ' + m.content + '\n'
        else if (m.role === 'assistant') dialogue += '助手: ' + (m.content || '').slice(0, 300) + '\n'
      }
      if (!dialogue.trim() && !priorSummary) return ''
      try {
        // Prepend prior summary so the new summary accumulates (cumulative compression).
        const summaryInput = priorSummary
          ? '【之前的压缩摘要】\n' + priorSummary + '\n\n【最近对话】\n' + dialogue
          : dialogue
        const resp = await window.go.app.App.ChatProxy(
          [{ role: 'system', content: STRUCTURED_SUMMARY_PROMPT },
           { role: 'user', content: summaryInput }],
          [] as any
        )
        summaryText = resp?.choices?.[0]?.message?.content || ''
      } catch (e) { console.error('[context] force compress failed:', errMsg(e)); return '' }
    }

    if (!summaryText) return ''

    // ── Insert divider + summary as a new message (don't delete old ones!) ──
    const dividerId = _tempId()
    const header = '━━━ 📦 上下文压缩 ' + new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) + ' ━━━'
    messages.value.splice(cutoff, 0, {
      id: dividerId, role: 'system' as const,
      content: header + '\n\n' + summaryText,
    })
    compressionMarker.value = cutoff // split point: apiMsgs starts after this
    compressedAt.value = cutoff
    compressedRoundCount.value = _countRounds(messages.value.slice(0, cutoff))
    // Reset progressive reveal state — the old count may exceed the new marker.
    historyVisibleCount.value = 0
    showCompressedHistory.value = false

    // ── Persist only the summary message to DB (don't delete anything) ──
    const sid = currentSessionId.value
    if (sid) {
      memoryApi.messageAppend(sid, 'system', header + '\n\n' + summaryText, '').catch(() => {})
      memoryApi.saveSummary(summaryText).catch(() => {})
    }

    // ── Re-inject ReAct reminder ──
    _reinjectReAct()

    return 'compressed'
  }

  /**
   * Staged context management (MemGPT-inspired, CoMem-async-augmented):
   *
   *   Stage 1 (50%):  Fire background pre-compute summary (async, zero blocking).
   *   Stage 2 (65%):  Inject soft system note — LLM self-manages.
   *   Stage 3 (80%):  Apply cached summary + truncate (instant if cached, else sync).
   *   Stage 4 (90%):  Hard truncation — drop oldest from the MIDDLE first
   *                    (respecting Lost-in-the-Middle: keep primacy + recency).
   *
   * When `force` is true the busy guard is skipped (used between chat-loop rounds).
   */
  /**
   * Returns a description of what happened (for user feedback).
   *   ''           = nothing done (below threshold or busy)
   *   'precompute' = background pre-compute fired
   *   'warned'     = soft warning injected
   *   'compressed' = summary applied / messages compacted
   *   'truncated'  = hard truncation applied
   */
  async function maybeCompressContext(force = false): Promise<string> {
    const pct = contextPct.value
    if (pct < 50 || (!force && busy.value)) return ''

    // ── Force mode: user explicitly clicked the button → do real work now ──
    if (force) {
      return await _forceCompress()
    }

    // ── Stage 1: Background pre-compute (50-65%) ──
    if (pct < 65 && pct >= 50) {
      const oldMsgs = messages.value.slice(0, Math.floor(messages.value.length * 0.5))
      let dialogue = ''
      for (const m of oldMsgs) {
        if (m.role === 'user') dialogue += '用户: ' + m.content + '\n'
        else if (m.role === 'assistant') dialogue += '助手: ' + (m.content || '').slice(0, 300) + '\n'
      }
      if (dialogue.trim()) _precomputeSummary(dialogue)
      return 'precompute'
    }

    // ── Stage 2: Soft warning (65-80%) ──
    if (pct < 80 && pct >= 65) {
      if (compressedAt.value >= 0) return 'warned' // already warned
      const warnMsg: ChatMessage = {
        id: _tempId(), role: 'system' as const,
        content: '⚠ 上下文使用率 ' + pct + '%（可用 ' + fmtTokens(contextUsable.value) + ' / 模型上限 ' + fmtTokens(contextLimit.value) + '）。请尽量简洁回复，避免不必要的工具调用。'
      }
      messages.value.push(warnMsg)
      compressedAt.value = messages.value.length - 1
      return 'warned'
    }

    // ── Stage 3: Apply compression (80-90%) ──
    if (pct < 90 && pct >= 80) {
      if (compressedAt.value > 0 && messages.value.length - compressedAt.value < 4) return '' // anti-thrash

      const summary = _pendingSummary
      _pendingSummary = null

      const cutoff = Math.floor(messages.value.length * 0.6)
      if (cutoff < 6) return ''

      if (summary) {
        messages.value = [
          { id: _tempId(), role: 'system' as const, content: '📝 早期对话摘要：' + summary },
          ...messages.value.slice(cutoff),
        ]
        compressedAt.value = messages.value.length
        return 'compressed'
      }

      // Fallback: synchronous summarization.
      const oldMsgs = messages.value.slice(0, cutoff)
      let dialogue = ''
      for (const m of oldMsgs) {
        if (m.role === 'user') dialogue += '用户: ' + m.content + '\n'
        else if (m.role === 'assistant') dialogue += '助手: ' + (m.content || '').slice(0, 300) + '\n'
      }
      if (!dialogue.trim()) return ''
      try {
        const go = window.go.app.App
        const resp = await go.ChatProxy(
          [{ role: 'system', content: STRUCTURED_SUMMARY_PROMPT },
           { role: 'user', content: dialogue }],
          [] as any
        )
        const s = resp?.choices?.[0]?.message?.content || ''
        if (s) {
          messages.value = [
            { id: _tempId(), role: 'system' as const, content: '📝 早期对话摘要：' + s },
            ...messages.value.slice(cutoff),
          ]
          compressedAt.value = messages.value.length
          _cleanOrphanToolMessages()
          _reinjectReAct()
          memoryApi.saveSummary(s).catch(() => {})
          return 'compressed'
        }
      } catch (e) { console.error('[context] sync compression failed:', errMsg(e)) }
      return ''
    }

    // ── Stage 4: Hard truncation (≥90%) ──
    if (pct >= 90) {
      const didTruncate = _truncateForLimit()
      return didTruncate ? 'truncated' : ''
    }
    return ''
  }

  /**
   * Hard truncation guard — called before every API request.
   *
   * Uses a "drop-from-middle" strategy informed by Lost-in-the-Middle research:
   *   - System messages stay at position 0 (primacy — highest attention)
   *   - Last N conversation turns stay at the end (recency — second-highest)
   *   - Tool results at the tail are kept (needed for API correctness)
   *   - Messages are dropped from the MIDDLE of the conversation array
   *     (the attention dead zone — Liu et al. shows models ignore middle content)
   *
   * Reserves ~10% for the model response + thinking.
   */
  /**
   * Hard truncation guard — now uses LOGICAL truncation (adds a compression
   * marker instead of deleting messages). History stays viewable in the UI.
   */
  function _truncateForLimit(): boolean {
    const limit = contextLimit.value
    const safeLimit = Math.floor(limit * modelProfile.value.compactPct / 100)
    if (contextTokens.value <= safeLimit) return false

    // If we already have a recent marker, don't add another.
    if (compressionMarker.value > 0 &&
        messages.value.length - compressionMarker.value < 10) return false

    // Use the same clean-boundary logic as compression.
    const cutoff = _findRoundBoundary(0.5)
    if (cutoff < 4) return false

    // Insert a minimal truncation marker (no LLM summary — just a note).
    const header = '⚠ 上下文已截断：早期消息已移至历史（适配模型限制 ' + fmtLimit(limit) + '）'
    messages.value.splice(cutoff, 0, {
      id: _tempId(), role: 'system' as const,
      content: header,
    })
    compressionMarker.value = cutoff
    compressedAt.value = cutoff
    compressedRoundCount.value = _countRounds(messages.value.slice(0, cutoff))
    historyVisibleCount.value = 0
    showCompressedHistory.value = false

    return true
  }

  /**
   * Strip tool messages that no longer have a matching assistant message with
   * tool_calls. These orphans cause HTTP 400 ("Messages with role 'tool' must
   * be a response to a preceding message with 'tool_calls'").
   * Called after any message manipulation that may drop assistant messages.
   */
  function _cleanOrphanToolMessages() {
    const validIds = new Set<string>()
    for (const m of messages.value) {
      if (m.role === 'assistant' && m.tool_calls) {
        for (const tc of m.tool_calls) validIds.add(tc.id)
      }
    }
    const before = messages.value.length
    messages.value = messages.value.filter(m => {
      if (m.role !== 'tool') return true
      return m.tool_call_id ? validIds.has(m.tool_call_id) : true
    })
    if (messages.value.length < before) {
      console.warn('[context] cleaned ' + (before - messages.value.length) + ' orphan tool messages')
    }
  }

  /**
   * After compression, re-inject a concise ReAct reminder so the model doesn't
   * drift from the framework (Claude Code PostCompact hook pattern).
   */
  function _reinjectReAct() {
    if (compressedAt.value <= 0) return
    const since = messages.value.length - compressedAt.value
    if (since > 3) return
    const recent = messages.value.slice(-4)
    if (recent.some(m => m.role === 'system' && m.content.includes('ReAct 框架提醒'))) return
    messages.value.push({
      id: _tempId(), role: 'system' as const,
      content: '【ReAct 框架提醒】上下文已压缩。继续遵循：1.分析需求 → 2.调用工具 → 3.观察结果 → 4.重复 → 5.最终回答。先思考再行动，工具失败换方案。'
    })
  }

  /**
   * Replace verbose tool results from older rounds with compact stubs.
   * Preserves the tool-role message (needed for API tool_call pairing) but
   * truncates the JSON content to save context tokens.
   */
  function _pruneOldToolResults(currentRound: number) {
    const MAX_AGE = 3
    if (currentRound <= MAX_AGE) return
    let asstTurn = 0
    for (let i = messages.value.length - 1; i >= 0; i--) {
      const m = messages.value[i]
      if (m.role === 'assistant' && m.tool_calls?.length) asstTurn++
      if (m.role === 'tool' && asstTurn > MAX_AGE) {
        const brief = (m.content || '').slice(0, 120)
        if (m.content && m.content.length > 150) {
          m.content = '[stale r' + asstTurn + '] ' + brief + '…'
        }
      }
    }
  }

  function fmtLimit(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
    if (n >= 1000) return (n / 1000).toFixed(0) + 'K'
    return String(n)
  }

  /** Format tokens for display. */
  function fmtTokens(n: number): string {
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
    return String(n)
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

  /** Icon mapping for compression summary sections. */
  const COMPRESS_SECTION_ICONS: Record<string, string> = {
    '关键决策': '🎯',
    '未解决问题': '❓',
    '涉及文件': '📁',
    '约束条件': '🔒',
    '下一步': '👉',
  }

  /**
   * Render compression summary with section icons and structured layout.
   * Input is plain text with 【Section】 headers; output is styled HTML.
   */
  function renderCompression(text: string): string {
    if (!text) return ''
    // Escape HTML first, then apply section formatting
    const escaped = text
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    // Split on 【...】 section headers, preserving the headers
    const parts = escaped.split(/【([^】]+)】/)
    if (parts.length <= 1) {
      // No structured sections found — render as plain text with line breaks
      return '<div class="compress-plain">' + escaped.replace(/\n/g, '<br>') + '</div>'
    }
    let html = ''
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i].trim()
      if (!part) continue
      // Even indices (0, 2, 4...) are content between headers
      // Odd indices (1, 3, 5...) are the header text inside 【】
      if (i % 2 === 1) {
        // Section header
        const icon = COMPRESS_SECTION_ICONS[part] || '📌'
        html += '<div class="compress-section"><div class="compress-section-head">'
          + '<span class="compress-section-icon">' + icon + '</span>'
          + '<span class="compress-section-title">' + part + '</span>'
          + '</div><div class="compress-section-body">'
      } else if (i > 0 && i % 2 === 0) {
        // Content after a header — close the section
        html += part.replace(/\n/g, '<br>') + '</div></div>'
      } else {
        // Leading content before first header
        html += '<div class="compress-plain">' + part.replace(/\n/g, '<br>') + '</div>'
      }
    }
    return html
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

  /**
   * Parse raw backend messages into display-ready ChatMessage[], preserving
   * tool_calls, tool_results, reasoning, and interleaving tool-role messages.
   * Returns merged messages + a reasoning map keyed by final merged index.
   */
  function _parseMessages(raw: MemoryMessage[]): { messages: ChatMessage[]; reasoning: Record<number, string> } {
    // 1. Extract tool-role messages.
    const toolMsgs: ChatMessage[] = []
    for (const m of raw) {
      if (m.role === 'tool' && m.toolJson) {
        try {
          const tr = JSON.parse(m.toolJson)
          toolMsgs.push({ role: 'tool', content: m.content, tool_call_id: tr.tool_call_id, name: tr.name || '' } as any)
        } catch (_) {}
      }
    }
    // 2. Map user/assistant messages + parse toolJson.
    const rawReasoning: Record<number, string> = {} // keyed by filtered index
    const filtered = raw
      .filter(m => m.role === 'user' || m.role === 'assistant'
        || (m.role === 'system' && (m.content.includes('📦') || m.content.includes('上下文已截断'))))
      .map((m, i) => {
        const msg: ChatMessage = { id: m.id, role: m.role as ChatMessage['role'], content: m.content }
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
              rawReasoning[i] = extra.reasoning
            }
          } catch (_) {}
        }
        return msg
      })
    // 3. Interleave tool messages after their assistant messages + back-fill tool results.
    const merged: ChatMessage[] = []
    for (const msg of filtered) {
      if (msg.role === 'assistant' && msg.tool_calls?.length) {
        const matchedToolMsgs: ChatMessage[] = []
        for (const tc of msg.tool_calls) {
          const tm = toolMsgs.find(t => t.tool_call_id === tc.id)
          if (tm) matchedToolMsgs.push(tm)
        }
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
    // 4. Remap reasoning from filtered indices to merged indices.
    const reasoning: Record<number, string> = {}
    if (Object.keys(rawReasoning).length) {
      let fi = 0
      for (let mi = 0; mi < merged.length; mi++) {
        const m = merged[mi]
        if (m.role === 'user' || m.role === 'assistant') {
          if (rawReasoning[fi]) reasoning[mi] = rawReasoning[fi]
          fi++
        }
      }
    }
    return { messages: merged, reasoning }
  }

  /** Switch to a session, loading only the last PAGE_SIZE messages for fast switching. */
  async function selectSession(id: string) {
    if (currentSessionId.value === id && messages.value.length) return
    currentSessionId.value = id
    expandedTool.value = {}
    hasMoreMessages.value = false
    totalMessageCount.value = 0
    compressionMarker.value = -1
    _olderBuffer = []
    _olderReasoning = {}
    loadMessageFiles()
    try {
      const PAGE_SIZE = 100
      const [msgs, total] = await Promise.all([
        memoryApi.messageListRecent(id, PAGE_SIZE),
        memoryApi.messageCount(id),
      ])
      totalMessageCount.value = total
      const parsed = _parseMessages(msgs || [])
      messages.value = parsed.messages
      reasoningContent.value = parsed.reasoning
      // Restore compression marker: find the last compression/truncation divider.
      for (let i = messages.value.length - 1; i >= 0; i--) {
        const c = messages.value[i].content || ''
        if (c.includes('📦 上下文压缩') || c.includes('上下文已截断')) {
          compressionMarker.value = i
          compressedAt.value = i
          compressedRoundCount.value = _countRounds(messages.value.slice(0, i))
          break
        }
      }
      // If the DB has more messages than we loaded, mark for lazy-load.
      hasMoreMessages.value = total > PAGE_SIZE
      // Estimate static token costs for the breakdown display.
      await _estimateStaticCounters()
      // Auto-compress if context is dangerously high after loading.
      if (contextPct.value >= 80) {
        console.warn('[context] session loaded at ' + contextPct.value + '% — auto-compressing')
        await _forceCompress()
      }
    } catch (_) {
      messages.value = []
    }
  }

  /** Rough estimate of system/tool/memory tokens for display when not in active chat. */
  async function _estimateStaticCounters() {
    try {
      const go = window.go.app.App
      // Estimate system prompt from enabled skills.
      let sysPrompt = '你是 EverEvo 桌面软件的 AI 助手，遵循 ReAct（推理-行动）框架工作。\n\n## 工具发现\n你只有核心工具（read_file、shell_exec、web_search、web_fetch、agent_run 等），其他工具需通过 tool_search 按需获取完整 Schema。\n- 使用 tool_search(query="关键词", category="类别") 搜索工具并获取参数定义\n- 可以先用空 query 或 "*" 查看所有工具类别，再按需搜索具体工具\n- 获取到的工具 Schema 在当前对话轮次内有效，可多次调用\n\n## 工作流程\n1. 分析需求 → 2. tool_search 发现工具 → 3. 调用工具 → 4. 观察结果 → 5. 重复直至完成\n\n## 输出规则\n- 工具结果为 JSON 时，只提取关键字段展示，不要照搬原始 JSON\n- 大文件读取会被自动截断（标注了截断位置），需要更多内容时重新读取指定 offset\n- 不需要工具就直接回答\n\n## 语言\n- 用户说中文用中文回复'
      const enabledSkills = await go.ListEnabledSkills(activeLibraryId.value).catch(() => [])
      for (const s of (enabledSkills || [])) {
        if (s.systemPrompt) sysPrompt += '\n【' + (s.title || '') + '】' + s.systemPrompt
      }
      _sysPromptTokens.value = estimateTokens(sysPrompt)

      // Estimate tool definitions — use lazy (core) tools to match what's actually sent.
      const allTools = await go.ListToolsLazy().catch(() => [])
      const enabledTools = allTools || []
      _toolDefTokens.value = (enabledTools as any[]).reduce((sum: number, t: any) => {
        const fullDef = {
          type: 'function',
          function: { name: t.name, description: t.description || '', parameters: t.rawParameters || t.parameters || {} }
        }
        return sum + estimateTokens(JSON.stringify(fullDef))
      }, 0)

      _memRagTokens.value = 0 // no active recall during session load
    } catch (_) {
      // Conservative fallback
      _sysPromptTokens.value = 3000
      _toolDefTokens.value = 1500
      _memRagTokens.value = 0
    }
  }

  // Buffer of older messages yet to be prepended (client-side pagination).
  let _olderBuffer: ChatMessage[] = []
  // Reasoning content for buffered messages (merged-index → text), pending prepend.
  let _olderReasoning: Record<number, string> = {}

  /** Generate a temporary ID for newly-created messages (before backend persist). */
  function _tempId(): string { return 'm_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 8) }

  /** Load earlier (older) messages for the current session — batched, 50 per pull. */
  async function loadEarlierMessages() {
    const sid = currentSessionId.value
    if (!sid || !hasMoreMessages.value || _loadingMore) return
    _loadingMore = true
    try {
      // If buffer is empty, fetch all messages from backend and parse them fully
      // (tool calls, results, reasoning, interleaved tool messages).
      if (!_olderBuffer.length) {
        const all = await memoryApi.messageList(sid) || []
        const existingIds = new Set(messages.value.map(m => m.id).filter(Boolean))
        const olderRaw = all.filter(m => !existingIds.has(m.id))
        if (!olderRaw.length) { hasMoreMessages.value = false; _loadingMore = false; return }
        const parsed = _parseMessages(olderRaw)
        _olderBuffer = parsed.messages
        _olderReasoning = parsed.reasoning
      }
      if (!_olderBuffer.length) { hasMoreMessages.value = false; _loadingMore = false; return }

      // Take a batch from the end of the buffer (closest to currently-visible messages).
      const BATCH = 50
      const batch = _olderBuffer.slice(-BATCH)
      _olderBuffer = _olderBuffer.slice(0, -BATCH)
      // Split reasoning: entries in the batch (indices 0..batchLen-1) vs. remaining.
      const remainingStart = _olderBuffer.length
      const batchReasoning: Record<number, string> = {}
      const nextReasoning: Record<number, string> = {}
      for (const [k, v] of Object.entries(_olderReasoning)) {
        const ki = Number(k)
        if (ki >= remainingStart) {
          batchReasoning[ki - remainingStart] = v
        } else {
          nextReasoning[ki] = v
        }
      }
      _olderReasoning = nextReasoning

      hasMoreMessages.value = _olderBuffer.length > 0 || Object.keys(_olderReasoning).length > 0

      // Scroll anchor: capture scrollHeight before DOM mutation.
      const el = _chatBoxRef
      const prevScrollHeight = el?.scrollHeight || 0
      const prevScrollTop = el?.scrollTop || 0

      // Prepend batch + shift existing index-keyed state.
      const shift = batch.length
      const newReasoning: Record<number, string> = {}
      for (const [k, v] of Object.entries(batchReasoning)) newReasoning[Number(k)] = v
      for (const [k, v] of Object.entries(reasoningContent.value)) newReasoning[Number(k) + shift] = v
      reasoningContent.value = newReasoning

      // Shift expandedTool keys (format: "N-suffix" where N is the message index).
      const newExpanded: Record<string, boolean> = {}
      for (const [k, v] of Object.entries(expandedTool.value)) {
        const dash = k.indexOf('-')
        if (dash > 0) {
          const idx = Number(k.slice(0, dash))
          if (!isNaN(idx)) {
            newExpanded[(idx + shift) + k.slice(dash)] = v
            continue
          }
        }
        newExpanded[k] = v
      }
      expandedTool.value = newExpanded

      messages.value = [...batch, ...messages.value]

      // Shift compression state to match the new indices.
      if (compressionMarker.value >= 0) {
        // Marker was in the old messages — shift by the prepend amount.
        compressionMarker.value += shift
        compressedAt.value += shift
      } else {
        // Compression marker may be in the newly loaded batch (it was outside
        // the initial PAGE_SIZE window). Scan the prepended messages for it.
        for (let i = batch.length - 1; i >= 0; i--) {
          const c = batch[i].content || ''
          if (c.includes('📦 上下文压缩') || c.includes('上下文已截断')) {
            compressionMarker.value = i
            compressedAt.value = i
            compressedRoundCount.value = _countRounds(messages.value.slice(0, i))
            break
          }
        }
      }

      // Restore scroll before the browser paints so the transition is seamless.
      await nextTick()
      if (el) {
        await new Promise<void>(resolve => {
          requestAnimationFrame(() => {
            el.scrollTop = prevScrollTop + (el.scrollHeight - prevScrollHeight)
            resolve()
          })
        })
      }
      _loadingMore = false
    } catch (_) { _loadingMore = false; _olderBuffer = []; _olderReasoning = {} }
  }

  /** Set the chat box element ref for scroll position restore on load-more. */
  function setChatBoxRef(el: any) { _chatBoxRef = el }

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
    messages.value.push({ id: _tempId(), role: 'user', content: displayText })
    await persist('user', displayText)
    busy.value = true

    try {
      await chatLoop()
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e)
      console.error('[chat] chatLoop error:', msg)
      messages.value.push({ id: _tempId(), role: 'assistant', content: '❗ 错误: ' + msg })
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
        // System prompt is fully assembled by backend buildOrchestratorPrompt
        // (ReAct framework + skills + thinkLang hint + paradigm hint — all inline)
        agentStream = {
          providerId: ctx.providerId || '',
          model: ctx.model || '',
          temperature: ag?.temperature != null ? ag.temperature : -1,
          maxTokens: ag?.maxTokens || 0,
        }
      } catch (e: unknown) {
        messages.value.push({ id: _tempId(), role: 'assistant', content: '❗ 加载 Agent 失败: ' + errMsg(e) })
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
      try { allTools = (await go.ListToolsLazy()) || [] } catch (_) { allTools = [] }

      const isExternal = (n: string) => n.startsWith('mcp__')
      tools = allTools.filter(t => enabledNames.includes(t.name) || isExternal(t.name))

      if (!tools.length) {
        messages.value.push({
          id: _tempId(),
          role: 'assistant',
          content: '⚠ 没有启用的工具。请先在「能力清单」中启用至少一个 Skill，或连接外部 MCP Server。',
        })
        return
      }

      // Build system prompt from enabled skills — domain-scoped.
      let enabledSkills: SkillInfo[]
      try { enabledSkills = (await go.ListEnabledSkills(activeLibraryId.value)) || [] } catch (_) { enabledSkills = [] }

      const basePrompt = `你是 EverEvo 桌面软件的 AI 助手，遵循 ReAct（推理-行动）框架工作。

## 工作流程 (ReAct Framework)
1. **分析 (Thought)**: 理解用户意图，判断需要什么信息、调用哪些工具。
2. **行动 (Action)**: 选择合适的工具，用精确的参数调用。不确定时先想清楚再调。
3. **观察 (Observation)**: 仔细阅读工具返回结果。成功？失败？有什么关键信息？
4. **重复 1-3** 直到掌握足够信息，然后给出最终答案。
5. **最终回答 (Final Answer)**: 用简洁中文直接回复用户，不要照搬工具输出的原始 JSON。

## 工具使用规则
- 思考再行动：永远先想清楚需要什么工具、传什么参数，不要盲调。
- 工具失败时分析错误原因，尝试替代方案（换个工具、换个参数）。
- 工具结果如果是 JSON，提取关键字段后再回复，不要整套 JSON 贴出来。
- 如果不需要工具就能回答，直接回答即可（零工具调用）。

## 其他规则
- 用户说中文，用中文回复。每次回复尽量简洁直接。
- 用户可通过拖拽或粘贴上传文件。文本文件（TXT/MD/CSV/JSON 等）内容会自动注入。PDF 和图片请用 read_file 或 read_media_file 工具读取。扫描件 PDF (isScanned=true) 请用 read_media_file 以图片形式查看。`

      const skillPrompts = enabledSkills
        .filter(s => s.systemPrompt)
        .map(s => `【${s.title}】${s.systemPrompt}`)
        .join('\n')
      systemContent = skillPrompts
        ? `${basePrompt}\n\n当前启用的能力角色：\n${skillPrompts}`
        : basePrompt

      // P10: Inject domain-scoped context (agents, skills, MCP, tools for this domain).
      const libId = activeLibraryId.value
      if (libId) {
        try {
          const domainCtx = await go.BuildDomainSystemPrompt(libId)
          if (domainCtx) {
            systemContent += '\n' + domainCtx
          } else {
            // Fallback: at minimum show which domain we're in.
            systemContent += `\n【当前领域】${libId}`
          }
        } catch (_) { /* best-effort */ }
      }
    }

    // Per-turn thinking language control: backend classifies the query for
    // Language-Mixed CoT (Li et al. EMNLP 2025, KO-REAson 2025).
    // English = logic/reasoning/math/code. Chinese = entities/user content.
    // MUST run before userQuery is consumed by memory/recall below.
    const userQuery = sanitizeForRecall(lastUserContent())
    try {
      const tlResult = await go.ClassifyThinkLang(userQuery)
      if (tlResult?.rule) systemContent += '\n\n' + tlResult.rule
    } catch (e) { console.warn('[chat] thinkLang classification failed:', errMsg(e)) }

    // ── Paradigm list: full catalog (19 items, ~350 tokens) — LLM picks directly ──
    try {
      const allParadigms = await memoryApi.paradigmList() || []
      const enabled = allParadigms.filter((p: any) => p.enabled !== false)
      if (enabled.length) {
        const lines = enabled.map((p: any) =>
          `- \`${p.id}\` ${p.icon || '🧠'} **${p.name}** [${p.category}] ${p.description || ''}`
        )
        systemContent += '\n\n---\n## 🧠 思维范式（选择后调用 `paradigm_select` 加载方法论）\n\n' + lines.join('\n')
        systemContent += '\n\n完成后调用 `paradigm_feedback(id, match, exec, outcome, "原因")` 提交反馈。'
      }
    } catch (_) { /* best-effort */ }

    // P1.5: inject long-term semantic memory (cross-session recall).
    if (userQuery) {
      // ── Layered memory budget (per-source, % of effective context window) ──
      // Codex: 70% of effective window for memory rollouts. We allocate ~40%
      // total across all sources (core + summary + turns + facts + graph + exp).
      // Each source gets a portion; the final MEM_BLOCK_MAX is a hard safety net.
      const effWin = contextTarget.value
      const pct = (p: number) => Math.max(200, Math.floor(effWin * p / 100))
      const BUDGET_CORE   = pct(1.5)  // identity/preferences
      const BUDGET_SUMMARY = pct(1.0) // current session summary
      const BUDGET_TURNS   = pct(4.0) // related historical Q&A
      const BUDGET_FACTS   = pct(1.5) // extracted facts
      const BUDGET_GRAPH   = pct(2.5) // knowledge graph entities + relations
      const BUDGET_EXP     = pct(1.5) // distilled experience / lessons
      const BUDGET_KB      = pct(6.0) // RAG knowledge base chunks
      const BUDGET_WIKI    = pct(4.0) // project docs (llmwiki)
      const MEM_BLOCK_MAX  = pct(20)  // overall safety net (~22% total)
      // Middle-truncation: keep head (60%) + tail (40%), drop middle.
      // Preserves both opening context and closing details (Codex-style).
      function _trunc(s: string, max: number): string {
        if (s.length <= max) return s
        const headLen = Math.floor(max * 0.6)
        const tailLen = max - headLen - 30  // 30 chars for the marker
        if (tailLen <= 0) return s.slice(0, max) + '…'
        return s.slice(0, headLen)
          + `\n…[省略 ${s.length - max} 字符]…\n`
          + s.slice(-tailLen)
      }

      try {
        const result = await memoryApi.recall(userQuery, 3, activeLibraryId.value) || { turns: [], facts: [], graph: '', graphTrace: { seedIds: [], edgeIds: [] }, core: [] }
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
          parts.push('核心记忆（身份与偏好，永久）：\n' + _trunc(
            result.core.map((f: any) => `- ${f.value}`).join('\n'), BUDGET_CORE))
        }
        // P3.6: session summary (already loaded via sessionList).
        const curSession = sessions.value.find(s => s.id === currentSessionId.value)
        if (curSession?.summary) {
          parts.push('会话摘要：\n' + _trunc(curSession.summary, BUDGET_SUMMARY))
        }
        if (result.turns?.length) {
          parts.push('相关历史问答：\n' + _trunc(
            result.turns.map((t, i) => `${i + 1}. 问：${t.content}\n   答：${t.reply}`).join('\n'), BUDGET_TURNS))
        }
        if (result.facts?.length) {
          parts.push('已知事实：\n' + _trunc(
            result.facts.map((f, i) => `${i + 1}. [${f.category}] ${f.content}`).join('\n'), BUDGET_FACTS))
        }
        // P2: knowledge graph — vector-seeded 2-hop expansion.
        // Fall back to keyword-based node search when the vector path is empty.
        let graphText = result.graph || ''
        if (!graphText) {
          try {
            graphText = await memoryApi.recallGraphContext(userQuery, activeLibraryId.value) || ''
          } catch (_) { /* best-effort */ }
        }
        if (graphText) {
          parts.push('知识图谱（相关实体与关系）：\n' + _trunc(graphText, BUDGET_GRAPH))
        }
        // P8: distilled experience recall
        try {
          const expItems = await memoryApi.recallExperience('', 3) || []  // experience is global, not domain-scoped
          if (expItems.length) {
            parts.push('经验教训（过去的反思沉淀）：\n' + _trunc(
              expItems.map((e: any) => `- [${e.kind}] ${e.content}`).join('\n'), BUDGET_EXP))
          }
        } catch (_) { /* best-effort */ }
        lastGraphTrace.value = result.graphTrace || { seedIds: [], edgeIds: [] }
        if (parts.length) {
          let memBlock = parts.join('\n')
          if (memBlock.length > MEM_BLOCK_MAX) memBlock = memBlock.slice(0, MEM_BLOCK_MAX) + '…'
          systemContent += '\n\n长期记忆（与当前问题相关的过往信息，仅供参考，可能过时）：\n' + memBlock
        }
      } catch (e) { console.error('[chat] recall failed:', errMsg(e)) }
      // P6.1: project-docs recall (llmwiki) — surface relevant design/task notes.
      try {
        const wiki = await wikiApi.recall(activeLibraryId.value, userQuery)
        if (wiki) systemContent += '\n\n项目文档（与问题相关的设计/任务记录）：\n' + _trunc(wiki, BUDGET_WIKI)
      } catch (_) {}
	      // P9: RAG knowledge base recall — auto-inject relevant KB chunks into context.
	      try {
	        const ragHits = await knowledgeApi.searchAllKBs(userQuery, activeLibraryId.value, 6, 3)
	        if (ragHits?.length) {
	          let ragBlock = ragHits.map((h: any) =>
	            `[${h.kbName}, ${(h.similarity * 100).toFixed(0)}%] ${h.content}`
	          ).join('\n')
	          ragBlock = _trunc(ragBlock, BUDGET_KB)
	          systemContent += '\n\n知识库检索结果（来自知识库的相关文档片段，请参考这些内容回答用户问题）：\n' + ragBlock
	        }
	      } catch (_) {}
	    }

    // ── Tool discovery hint (replaces Token Budget noise) ──
    systemContent += '\n\n💡 调用 `tool_search` 发现更多工具 | 思维范式: `paradigm_match` 匹配 → `paradigm_select` 加载方法论 → `paradigm_feedback` 反馈'

    const apiMsgs: APIMessage[] = normalizeToolMessages([
      { role: 'system', content: systemContent },
      // Only send messages after the compression marker (logical truncation).
      // Full history is preserved in messages.value for UI scrolling.
      ...apiMessages.value.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'tool'),
    ])

    // Cache token estimates for the fixed-cost components (used by contextTokens).
    _sysPromptTokens.value = estimateTokens(systemContent)
    // Count the FULL JSON payload (type + function wrapper) that goes in the API request.
    _toolDefTokens.value = tools.reduce((sum, t) => {
      const fullDef = {
        type: 'function',
        function: {
          name: t.name,
          description: t.description || '',
          parameters: t.rawParameters || t.parameters || {},
        },
      }
      return sum + estimateTokens(JSON.stringify(fullDef))
    }, 0)
    if (tools.length > 30) {
      console.warn('[context] ' + tools.length + ' tools enabled — research shows >30 degrades agent performance. Consider disabling unused skills.')
    }
    // Memory/RAG is embedded in systemContent; we estimate it separately for the breakdown.
    // The recall/RAG injection happens above; its token count is the difference between
    // the full systemContent and the base prompt (before memory injection).
    // A simpler approach: re-estimate just the memory-related portions.
    _memRagTokens.value = 0
    // Roughly: if memory/recall/RAG blocks were injected, count them.
    const memMarker = '长期记忆（与当前问题相关的过往信息'
    const ragMarker = '知识库检索结果'
    const wikiMarker = '项目文档'
    for (const line of systemContent.split('\n')) {
      if (line.includes(memMarker) || line.includes(ragMarker) || line.includes(wikiMarker)) {
        _memRagTokens.value += estimateTokens(line)
      }
    }

    // No round cap (requested): the loop runs until the model returns a final
    // answer (no more tool calls) or the user stops it. No iteration /
    // productive budgets.
    stopRequested.value = false
    currentStreamId.value = ''
    let round = 0
    for (;;) {
      if (stopRequested.value) { busy.value = false; currentStreamId.value = ''; return }
      round++
      // ── Stale tool result pruning (save context space) ──
      _pruneOldToolResults(round)
      // ── Staged context management between rounds (MemGPT + CoMem hybrid) ──
      // force=true bypasses the busy guard (we ARE busy, but between API calls).
      const pct = contextPct.value
      if (pct >= 50 && round > 5) {
        // Frequency adapts to urgency:
        //   50-65%  → every 5 rounds (gentle pre-compute)
        //   65-80%  → every 3 rounds (soft warning + pre-compute)
        //   80-90%  → every 2 rounds (apply compression)
        //   90%+    → every round (truncation guard)
        const freq = pct >= 90 ? 1 : pct >= 80 ? 2 : pct >= 65 ? 3 : 5
        if (round % freq === 0) {
          await maybeCompressContext(true)
        }
      }
      // Hard guard: if still over the absolute model limit, truncate.
      _truncateForLimit()
      const msgIdx = messages.value.length
      const asstId = _tempId()
      messages.value.push({ id: asstId, role: 'assistant', content: '' })

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
          // ── Pre-request context safety check ──
          // If context exceeds 90% of the model's absolute limit, hard-truncate
          // the oldest messages so the request doesn't fail.
          _truncateForLimit()
          // Normalize before every send: guarantee every assistant.tool_calls
          // is followed by a matching tool result (DeepSeek/OpenAI reject
          // dangling tool_calls with HTTP 400). Cheap defensive copy per turn.
          const sendMsgs = normalizeToolMessages(apiMsgs)
          if (agentStream) {
            agentsApi.streamAs(streamId, sendMsgs, toolPayload, agentStream.providerId, agentStream.model, agentStream.temperature, agentStream.maxTokens, effort).catch(reject)
          } else {
            // Use the model profile's declared maxOutputTokens directly.
            // Over-allocating max_tokens is harmless — the model stops at
            // finish_reason="stop" when done (Codex doesn't even set it).
            // Only clamp when context is truly full.
            const defP = activeProvider.value
            const maxOut = modelProfile.value.maxOutputTokens
            go.ChatStreamAs(streamId, sendMsgs, toolPayload,
              defP?.id || '', defP?.model || '', -1, maxOut, effort).catch(reject)
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
        // Replace placeholder with full message (preserve id + tool_calls)
        messages.value[msgIdx] = {
          id: asstId,
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
        // Log context growth per round for diagnostics.
        _logContextGrowth(round)
        // Auto-title: rename "新对话" sessions after enough rounds.
        // Best-effort; failures are silent inside the backend goroutine.
        memoryApi.sessionAutoTitle(currentSessionId.value)
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
                // ── Convert timeout to async background task ──
                const asyncStore = useAsyncStore()
                const go2 = (window as any).go?.app?.App
                const isTimeout = msg.includes('超时') || msg.includes('timeout')
                if (isTimeout && go2?.CreateAsyncTask && currentSessionId.value) {
                  const title = tc.function.name + ' - ' + JSON.stringify(args).slice(0, 80)
                  try {
                    const ctx = JSON.stringify({ messages: messages.value.slice(-40) })
                    const at = await go2.CreateAsyncTask(currentSessionId.value, title, tc.function.name, JSON.stringify(args), ctx)
                    go2.RunAsyncTool(at.id, tc.function.name, args)  // fire-and-forget
                    result = {
                      success: true,
                      data: { asyncTaskId: at.id, message: '已移至后台执行 (async:' + at.id.slice(0, 8) + ')，完成后可恢复对话' },
                    }
                  } catch (_) {
                    result = { success: false, error: msg }
                  }
                } else {
                  result = { success: false, error: msg }
                }
                // Late-result patching: backend promise continues, update when done.
                const toolCallId = tc.id
                callPromise.then((late) => {
                  if (idx < messages.value[msgIdx].toolResults!.length) {
                    messages.value[msgIdx].toolResults![idx].result = late
                    messages.value[msgIdx] = { ...messages.value[msgIdx] }
                  }
                  // Complete async task if one was created
                  if ((result as any)?.data?.asyncTaskId && late?.success) {
                    const rid = JSON.stringify(late.data || late)
                    go2?.CompleteAsyncTask?.((result as any).data.asyncTaskId, rid).catch(() => {})
                  }
                  if ((result as any)?.data?.asyncTaskId && !late?.success) {
                    go2?.FailAsyncTask?.((result as any).data.asyncTaskId, late?.error || 'unknown').catch(() => {})
                  }
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
            // Truncate oversized tool results: keep the result object FULL for the
            // terminal widget (UI), but send a truncated version to the API.
            // Strategy: drop verbose `data` field, keep head+tail if still too large.
            const MAX_RESULT = 3000
            const HEAD = 2000
            const TAIL = 800
            let apiResultStr = JSON.stringify(result)
            if (apiResultStr.length > MAX_RESULT) {
              // Try dropping the data field (usually the bulk of web_fetch/read_file).
              if (result.data) {
                const slim = { ...result, data: '[已截断，原始长度 ' + JSON.stringify(result.data).length + ' 字符]' }
                apiResultStr = JSON.stringify(slim)
              }
              if (apiResultStr.length > MAX_RESULT) {
                apiResultStr = apiResultStr.slice(0, HEAD) +
                  '…[截断 ' + (apiResultStr.length - HEAD - TAIL) + ' 字符]…' +
                  apiResultStr.slice(-TAIL)
              }
            }
            const toolMsg = { role: 'tool' as const, tool_call_id: tc.id, content: apiResultStr }
            apiMsgs.push(toolMsg)
            messages.value.push(toolMsg)
            // Persist truncated tool result.
            const toolJson = JSON.stringify({ tool_call_id: tc.id, name: tc.function.name, args })
            memoryApi.messageAppend(currentSessionId.value, 'tool', apiResultStr, toolJson).catch(() => {})
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
            // Preserve reasoning from the earlier persist (messageUpdateToolJSON replaces the
            // whole tool_json column — we must re-include reasoning or it will be lost).
            if (reasoningContent.value[msgIdx]) {
              updated.reasoning = reasoningContent.value[msgIdx]
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
    sessions, currentSessionId, pendingFiles, messageFiles, hasMoreMessages, totalMessageCount, thinkMode, thinkEffort, stopRequested, currentStreamId, reasoningContent, compressedAt, compressionMarker, compressedRoundCount, showCompressedHistory, historyVisibleCount, historyMessages, historyRoundCount, apiMessages, revealMoreHistory, hideCompressedHistory,
    contextTokens, contextTarget, contextLimit, contextUsable, contextReserved,
    contextPct, contextBarPct, contextLimitPct, contextLevel, contextBreakdown,
    // getters
    activeProvider, chatReady, suggestedPrompts, selectedAgentName, visibleAgents, activeLibraryId,
    // helpers
    errMsg, chatRole, chatRender, renderCompression,
    // actions
    loadConfig, loadSkills, loadAgents, selectAgent, clearMessages, sendMessage, toggleToolResult,
    loadSessions, createSession, selectSession, renameSession, deleteSession,
    addFile, removeFile, clearFiles,
    loadEarlierMessages, setChatBoxRef,
    maybeCompressContext,
  }
})

// ── Toast injection (called by App.vue during setup) ──

export function setChatToast(fn: (type: string, title: string, desc?: string) => void) {
  _toastFn = fn
}
