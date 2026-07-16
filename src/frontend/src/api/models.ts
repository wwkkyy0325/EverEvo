/**
 * Model / download API — kept methods used by MyModels, Toolbox, and Knowledge.
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

export const modelsApi = {
  /** List loaded models */
  listModels() { return call<any[]>(() => App().ListModels()) },

  /** List downloaded model files */
  listDownloaded() { return call<any[]>(() => App().ListDownloadedModels()) },

  /** List models for toolbox (sentence-embedding, image-classification) */
  listToolModels() { return call<any[]>(() => App().ListToolModels()) },

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

  /** Open folder containing a downloaded file */
  openDownloadedFileDir(filename: string) { return call<void>(() => App().OpenDownloadedFileDir(filename)) },

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
}
