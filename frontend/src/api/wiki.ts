/**
 * Wiki API — per-library dev-doc index + recall (P6.1).
 */
import { call } from './client'

const App = () => window.go.app.App

export interface WikiChunk { page: string; heading: string; content: string }

export interface WikiPageInfo { id: string; title: string; path: string; chunkCount: number; source: string }
export interface WikiPageContent { id: string; path: string; content: string }

export const wikiApi = {
  status(libraryId: string) { return call<{ pages: number; chunks: number }>(() => App().WikiStatus(libraryId)) },
  reindex(libraryId: string) { return call<{ pages: number; chunks: number }>(() => App().WikiReindex(libraryId)) },
  search(libraryId: string, q: string) { return call<WikiChunk[]>(() => App().WikiSearch(libraryId, q)) },
  recall(libraryId: string, q: string) { return call<string>(() => App().WikiRecall(libraryId, q)) },
  listPages(libraryId: string) { return call<WikiPageInfo[]>(() => App().WikiListPages(libraryId)) },
  readPage(libraryId: string, pageId: string) { return call<WikiPageContent>(() => App().WikiReadPage(libraryId, pageId)) },
  savePage(libraryId: string, pageId: string, title: string, content: string) {
    return call(() => App().WikiSavePage(libraryId, pageId, title, content))
  },
  deletePage(libraryId: string, pageId: string) {
    return call(() => App().WikiDeletePage(libraryId, pageId))
  },
}
