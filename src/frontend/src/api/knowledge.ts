/**
 * Knowledge Base (RAG) API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface KnowledgeBase {
  id: string
  name: string
  modelDir: string
  createdAt: string
  docCount?: number
}

export interface DocEntry {
  id: string
  content: string
  metadata: Record<string, string>
}

export interface SearchResult {
  id: string
  content: string
  source: string
  position: number
  metadata: Record<string, string>
  similarity: number
}

export const knowledgeApi = {
  list(libraryId = '') { return call<KnowledgeBase[]>(() => App().ListKnowledgeBases(libraryId)) },
  create(name: string, modelDir: string, libraryId = '') { return call<KnowledgeBase>(() => App().CreateKnowledgeBase(name, modelDir, libraryId)) },
  delete(kbId: string) { return call(() => App().DeleteKnowledgeBase(kbId)) },
  clear(kbId: string) { return call(() => App().ClearKnowledgeBase(kbId)) },
  addTexts(kbId: string, texts: string[], metadata: Record<string, string>) {
    return call<number>(() => App().AddTexts(kbId, texts, metadata))
  },
  search(kbId: string, query: string, k?: number, filter?: Record<string, string>) {
    return call<SearchResult[]>(() => App().SearchKnowledge(kbId, query, k || 5, filter))
  },
  listDocuments(kbId: string) { return call<DocEntry[]>(() => App().ListKBDocuments(kbId)) },
  deleteChunks(kbId: string, ids: string[]) { return call<number>(() => App().DeleteKBChunks(kbId, ids)) },
  embedTexts(modelDir: string, texts: string[]) { return call<number[][]>(() => App().EmbedTexts(modelDir, texts)) },
  /** Rebind a KB's model (only when the KB is empty). */
  updateModelDir(kbId: string, newDir: string) { return call(() => App().UpdateKBModelDir(kbId, newDir)) },
  /** Re-embed all KB docs with a new model and rebind. */
  migrateModel(kbId: string, newDir: string) { return call(() => App().MigrateKBModel(kbId, newDir)) },
  /** Search across all KBs in a domain library (for auto-inject into chat context). */
  searchAllKBs(query: string, libraryId: string, k?: number, perKB?: number) {
    return call<RagContextResult[]>(() => App().SearchAllKnowledgeBases(query, libraryId, k || 6, perKB || 3))
  },
  /** Parse a file from disk and return its text content. Supports .txt, .md, .pdf. */
  parseFile(filePath: string) { return call<string>(() => App().ParseFileForKB(filePath)) },
  /** Parse a file from base64-encoded bytes (for drag-and-drop in chat). */
  parseFileBytes(b64: string, filename: string) { return call<string>(() => App().ParseFileBytes(b64, filename)) },
  /** Save a chat drag-and-drop file to disk. Returns path + preview + isScanned flag. */
  saveChatFile(b64: string, filename: string) { return call<ChatFileInfo>(() => App().SaveChatFile(b64, filename)) },
}

/** Info returned by SaveChatFile for a chat upload. */
export interface ChatFileInfo {
  path: string
  preview: string
  isScanned: boolean
  sizeBytes: number
}

/** A RAG search result enriched with its parent KB name. */
export interface RagContextResult {
  kbName: string
  content: string
  similarity: number
}
