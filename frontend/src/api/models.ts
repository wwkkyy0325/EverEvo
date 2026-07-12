/**
 * Model catalog / download API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface ModelInfo {
  id: string
  name: string
  repoId: string
  source: string
  author: string
  task: string
  downloads: number
  url: string
  description?: string
  fileTree?: FileNode[]
  fileEntries?: FileEntry[]
  filesError?: string
}

export interface FileNode {
  name: string
  path: string
  type: 'file' | 'directory'
  size?: number
  children?: FileNode[]
}

export interface FileEntry {
  name: string
  type: string
  size: number
}

export interface DownloadTask {
  id: string
  filename: string
  status: 'downloading' | 'paused' | 'completed' | 'error'
  written: number
  total: number
  speed: number
  pct: number
  reason: string
  createdAt: number
  completedAt: number
}

export interface SearchFilter {
  task?: string
  library?: string
  language?: string
  sort?: string
}

export const modelsApi = {
  /** Search catalog with optional filter */
  search(source: string, query: string, filter?: SearchFilter) {
    return call<any>(() => App().SearchCatalog(source, query, filter ?? {}))
  },

  /** Get model detail */
  getDetail(source: string, repoId: string) { return call<ModelInfo>(() => App().GetModelDetail(source, repoId)) },

  /** Check if a file is already on disk (source of truth for dedup) */
  isFileDownloaded(source: string, repoId: string, file: string) {
    return call<boolean>(() => App().IsFileDownloaded(source, repoId, file))
  },

  /** Download a single file */
  downloadFile(source: string, repoId: string, file: string) {
    return call<string>(() => App().DownloadModelFile(source, repoId, file))
  },

  /** Batch download selected files */
  downloadSelected(source: string, repoId: string, files: string[]) {
    return call(() => App().DownloadSelectedFiles(source, repoId, files))
  },

  /** List loaded models */
  listModels() { return call<any[]>(() => App().ListModels()) },

  /** List downloaded model files */
  listDownloaded() { return call<any[]>(() => App().ListDownloadedModels()) },

  /** Open folder containing a downloaded file */
  openDownloadedFileDir(filename: string) { return call<void>(() => App().OpenDownloadedFileDir(filename)) },

  /** Load a model file into memory */
  loadModelFile(id: string, name: string, path: string) {
    return call(() => App().LoadModelFile(id, name, path))
  },

  /** Unload a model */
  unloadModel(id: string) { return call(() => App().UnloadModel(id)) },

  /** Run model inference */
  runModel(id: string, input: any) { return call<any>(() => App().RunModel(id, input)) },

  /** Pick a model file from filesystem */
  pickModelFile() { return call<string>(() => App().PickModelFile()) },

  /** List tool models (sentence-embedding, image-classification) */
  listToolModels() { return call<any[]>(() => App().ListToolModels()) },

  /** Delete a downloaded file */
  deleteFile(name: string) { return call(() => App().DeleteDownloadedFile(name)) },

  /** Delete a downloaded directory */
  deleteDir(name: string) { return call(() => App().DeleteDownloadedDir(name)) },

  // ── Download tasks ──
  getDownloadTasks() { return call<DownloadTask[]>(() => App().GetDownloadTasks()) },
  getDownloadHistory() { return call<DownloadTask[]>(() => App().GetDownloadHistory()) },
  pauseDownload(id: string) { return call(() => App().PauseDownload(id)) },
  resumeDownload(id: string) { return call(() => App().ResumeDownload(id)) },
  cancelDownload(id: string) { return call(() => App().CancelDownload(id)) },
  clearDownloadHistory() { return call(() => App().ClearDownloadHistory()) },
  openDownloadDir() { return call(() => App().OpenDownloadDir()) },

  // ── Cache ──
  invalidateCache(key: string) { return call(() => App().InvalidateCache(key)) },
  openDir(path: string) { return call(() => App().OpenDir(path)) },
}
