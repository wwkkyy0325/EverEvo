/**
 * Memory API — chat session persistence (P0) + long-term semantic memory (P1.5).
 *
 * Sessions/messages live in SQLite (data/memory/memory.db); semantic memory
 * vectors live in chromem (data/memory/chromem). All calls go through the
 * shared `call()` wrapper (timeout / cancel / error formatting).
 */
import { call } from './client'

const App = () => window.go.app.App

export interface MemorySession {
  id: string
  title: string
  agentId: string
  summary: string
  createdAt: number
  updatedAt: number
}

export interface MemoryMessage {
  id: string
  sessionId: string
  seq: number
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  toolJson: string
  createdAt: number
}

// ── Long-term semantic memory (P1.5) ──

/** A recalled user question with its associated assistant reply. */
export interface TurnHit { content: string; reply: string }

/** A recalled extracted fact. */
export interface FactHit { content: string; category: string }

/** Result of recall: past Q&A turns + extracted facts + a knowledge-graph context block. */
export interface GraphTrace { seedIds: string[]; edgeIds: string[] }

export interface RecallResult { turns: TurnHit[]; facts: FactHit[]; graph: string; graphTrace: GraphTrace; core: UserFact[]; coreSearch: FactHit[]; experience: FactHit[] }

/** A permanent core-memory fact (identity/preference/constraint). */
export interface UserFact { id: string; key: string; value: string; category: string; importance: string; locked: boolean; source: string; createdAt: number }

/** Hardware-adaptive memory retention policy. */
export interface MemoryPolicy { tier: string; halfLifeDays: number; ttlDays: number; recallK: number; itemCap: number; coreCap: number; alpha: number }

/** A knowledge-graph entity node (for the viewer). */
export interface KgNode { id: string; type: string; name: string; createdAt: number }

/** A knowledge-graph relation, currently valid (for the viewer). */
export interface KgEdge { id: string; srcId: string; dstId: string; type: string; srcName: string; dstName: string; validFrom: number; validTo: number; recordedAt: number; weight: number }

/** Aggregate graph stats (for the header). */
export interface GraphHub { id: string; name: string; degree: number }
export interface GraphStats { edgesPerType: Record<string, number>; topHubs: GraphHub[] }

export interface MemoryStatus {
  bound: boolean
  modelDir: string
  turnCount: number
  factCount: number
  nodeCount: number
  edgeCount: number
}

export interface MemoryItem {
  id: string
  kind: 'turn' | 'fact'
  content: string
  reply: string
  category: string
  sessionId: string
  createdAt: number
}

export const memoryApi = {
  sessionList() { return call<MemorySession[]>(() => App().MemorySessionList()) },
  sessionCreate(title: string, agentId: string) {
    return call<MemorySession>(() => App().MemorySessionCreate(title, agentId))
  },
  sessionRename(id: string, title: string) { return call(() => App().MemorySessionRename(id, title)) },
  /** Trigger async LLM-based auto-title for a session (best-effort). */
  sessionAutoTitle(id: string) {
    App().MemorySessionAutoTitle(id).catch(() => {}) // fire-and-forget
  },
  sessionDelete(id: string) { return call(() => App().MemorySessionDelete(id)) },
  messageList(sessionId: string) { return call<MemoryMessage[]>(() => App().MemoryMessageList(sessionId)) },
  /** Load only the last N messages for fast session switching. */
  messageListRecent(sessionId: string, limit: number) {
    return call<MemoryMessage[]>(() => App().MemoryMessageListRecent(sessionId, limit))
  },
  /** Total message count (to know if there are earlier messages to load). */
  messageCount(sessionId: string) { return call<number>(() => App().MemoryMessageCount(sessionId)) },
  messageAppend(sessionId: string, role: string, content: string, toolJson: string) {
    return call<MemoryMessage>(() => App().MemoryMessageAppend(sessionId, role, content, toolJson))
  },
  messageUpdateToolJSON(msgId: string, toolJson: string) {
    return call(() => App().MemoryMessageUpdateToolJSON(msgId, toolJson))
  },
  messageClear(sessionId: string) { return call(() => App().MemoryMessageClear(sessionId)) },

  /** Semantic recall: past Q&A turns + extracted facts relevant to the query. */
  recall(query: string, k: number, libraryId = '') { return call<RecallResult>(() => App().MemoryRecallScoped(query, k, libraryId)) },
  /** Store a finalized user→assistant turn; triggers async fact extraction. */
  remember(userText: string, reply: string, sessionId: string, libraryId = '') {
    return call(() => App().MemoryRemember(userText, reply, sessionId, libraryId))
  },
  /** Save a compression summary to long-term memory for future recall. */
  saveSummary(text: string) { return call(() => App().MemorySaveSummary(text)) },
  status(libraryId = '') { return call<MemoryStatus>(() => App().MemoryStatus(libraryId)) },
  list(k: number, libraryId = '') { return call<MemoryItem[]>(() => App().MemoryList(k, libraryId)) },
  /** Delete a single memory item by ID. */
  itemDelete(id: string) { return call(() => App().MemoryItemDelete(id)) },
  clear() { return call(() => App().MemoryClear()) },
  /** Bind an embedding model (only safe when no memories exist yet). */
  setEmbeddingModel(dir: string) { return call(() => App().MemorySetEmbeddingModel(dir)) },
  /** Re-embed all memories with a new model and rebind (use when data exists). */
  migrateModel(newDir: string) { return call(() => App().MemoryMigrateModel(newDir)) },
  /** List the knowledge graph (current nodes + edges) for the viewer. */
  listGraph(history = false, libraryId = '') { return call<{ nodes: KgNode[]; edges: KgEdge[] }>(() => App().MemoryGraphList(history, libraryId)) },
  /** Delete a graph node (cascades its edges). */
  deleteNode(id: string) { return call(() => App().MemoryNodeDelete(id)) },
  /** Delete a single graph edge. */
  deleteEdge(id: string) { return call(() => App().MemoryEdgeDelete(id)) },
  /** Rename a graph entity. */
  renameNode(id: string, name: string) { return call(() => App().MemoryNodeRename(id, name)) },
  /** Merge dropId into keepId (re-points edges, deletes dropId). */
  mergeNodes(keepId: string, dropId: string) { return call(() => App().MemoryNodesMerge(keepId, dropId)) },
  /** Manually add a relation. */
  addEdge(srcId: string, dstId: string, type: string, replaces: boolean) {
    return call(() => App().MemoryEdgeAdd(srcId, dstId, type, replaces))
  },
  /** Rename an edge's relation type. */
  renameEdge(id: string, newType: string) { return call(() => App().MemoryEdgeRename(id, newType)) },
  /** Aggregate graph stats (edge counts per type + top hubs). */
  graphStats() { return call<GraphStats>(() => App().MemoryGraphStats()) },
  /** Lightweight keyword-based graph entity search (no embedding needed). */
  recallGraphContext(query: string, libraryId: string) { return call<string>(() => App().MemoryRecallGraphContext(query, libraryId)) },
  /** List permanent core-memory facts. */
  coreList() { return call<UserFact[]>(() => App().MemoryCoreList()) },
  /** Manually add a core-memory fact. */
  coreAdd(key: string, value: string, category: string) { return call(() => App().MemoryCoreAdd(key, value, category)) },
  /** Lock/unlock a core-memory fact (locked = never touched). */
  coreLock(id: string, locked: boolean) { return call(() => App().MemoryCoreLock(id, locked)) },
  /** Delete a core-memory fact. */
  coreDelete(id: string) { return call(() => App().MemoryCoreDelete(id)) },
  /** Current hardware-adaptive memory policy. */
  policy() { return call<MemoryPolicy>(() => App().MemoryPolicy()) },

  // ── Workspaces (P7) ──
  workspaceList() { return call<any[]>(() => App().WorkspaceList()) },
  workspaceCreate(name: string) { return call<string>(() => App().WorkspaceCreate(name)) },
  workspaceDelete(id: string) { return call(() => App().WorkspaceDelete(id)) },

  // ── Experience Recall (P8) ──
  recallExperience(workspaceId: string, k?: number) {
    return call<any[]>(() => App().MemoryRecallExperience(workspaceId, k || 5))
  },
  entityLinks() { return call<any[]>(() => App().MemoryEntityLinks()) },
  /** Delete an experience item by ID. */
  experienceDelete(id: string) { return call(() => App().MemoryExperienceDelete(id)) },

  // ── Domain Libraries (P7) ──
  libraryList() { return call<any[]>(() => App().LibraryList()) },
  libraryCreate(name: string, description: string, icon: string, autoCreated: boolean) { return call<string>(() => App().LibraryCreate(name, description, icon, autoCreated)) },
  libraryDelete(id: string) { return call(() => App().LibraryDelete(id)) },
  libraryUpdate(id: string, name: string, description: string, icon: string) { return call(() => App().LibraryUpdate(id, name, description, icon)) },
  libraryBumpUse(id: string) { return call(() => App().LibraryBumpUse(id)) },
  libraryMerge(keepId: string, dropId: string) { return call(() => App().LibraryMerge(keepId, dropId)) },

  // ── Thinking Paradigm Library (P10) ──
  paradigmList(libraryId = '') { return call<any[]>(() => App().ParadigmList(libraryId)) },
  paradigmGet(id: string) { return call<any>(() => App().ParadigmGet(id)) },
  paradigmAdd(p: any) { return call<any>(() => App().ParadigmAdd(p)) },
  paradigmUpdate(id: string, p: any) { return call(() => App().ParadigmUpdate(id, p)) },
  paradigmDelete(id: string) { return call(() => App().ParadigmDelete(id)) },
  paradigmToggle(id: string, enabled: boolean) { return call(() => App().ParadigmToggle(id, enabled)) },
  paradigmFeedback(id: string, match: number, exec: number, outcome: number, reason: string) { return call(() => App().ParadigmFeedback(id, match, exec, outcome, reason)) },
  paradigmSelect(id: string) { return call<string>(() => App().ParadigmSelect(id)) },
  paradigmRefine(id: string) { return call<any>(() => App().ParadigmRefine(id)) },
  paradigmDistill(text: string, workspaceId: string) { return call<any>(() => App().ParadigmDistill(text, workspaceId)) },
  paradigmMatch(task: string) { return call<any[]>(() => App().ParadigmMatch(task)) },
}
