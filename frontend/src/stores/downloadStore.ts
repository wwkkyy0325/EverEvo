import { ref, computed } from 'vue'
import { defineStore } from 'pinia'

export interface TaskInfo {
  id: string
  filename: string
  status: string
  written: number
  total: number
  speed: number
  pct: number
  reason: string
  createdAt: number
  completedAt: number
}

export interface FileProgress {
  active: boolean
  pct: number
  status: string
  written: number
  total: number
  speed: number
  filename: string
  reason: string
}

type ToastFn = (type: string, title: string, desc?: string) => void

export const useDownloadStore = defineStore('download', () => {
  const activeTasks = ref<TaskInfo[]>([])
  const historyTasks = ref<TaskInfo[]>([])
  const fileProgress = ref<Record<string, FileProgress>>({})

  let _timer: ReturnType<typeof setInterval> | null = null
  let _bound = false
  let _toastFn: ToastFn | null = null
  let _lastToastKey = ''
  let _lastToastTime = 0
  // Track ids being cancelled so polling doesn't race and restore them.
  const _cancelling = new Set<string>()

  const completed = computed(() => historyTasks.value.filter(t => t.status === 'completed'))
  const failed = computed(() => historyTasks.value.filter(t => t.status === 'error'))
  const downloadCount = computed(() => activeTasks.value.length)

  async function fetchAll() {
    try {
      activeTasks.value = await window.go.app.App.GetDownloadTasks() || []
      historyTasks.value = await window.go.app.App.GetDownloadHistory() || []
    } catch (_) {}
  }

  async function refreshHistory() {
    try {
      const history = await window.go.app.App.GetDownloadHistory() || []
      // Filter out items that are currently being cancelled to avoid
      // the polling timer racing with cancelDownload's optimistic removal.
      historyTasks.value = _cancelling.size > 0
        ? history.filter(t => !_cancelling.has(t.id))
        : history
    } catch (_) {}
  }

  function _onProgress(data: any) {
    if (!data?.id) return

    const idx = activeTasks.value.findIndex(t => t.id === data.id)
    const item: TaskInfo = {
      id: data.id,
      filename: data.file || data.filename || '',
      status: data.status || 'downloading',
      written: data.written || 0,
      total: data.total || 0,
      speed: data.speed || 0,
      pct: data.pct || 0,
      reason: data.reason || '',
      createdAt: data.createdAt || Date.now(),
      completedAt: (data.status === 'completed' || data.status === 'error') ? Date.now() : 0,
    }

    // ── active / history ──
    if (idx >= 0) {
      activeTasks.value.splice(idx, 1, item)
      if (data.status === 'completed' || data.status === 'error') {
        activeTasks.value.splice(idx, 1)
        historyTasks.value.unshift(item)
      }
    } else if (data.status === 'downloading' || data.status === 'paused' || data.status === 'queued') {
      activeTasks.value.push(item)
    }

    // ── File-level progress (for ModelCatalog file tree) ──
    const f = data.file
    if (f) {
      const prev = fileProgress.value[f]
      // Skip if already marked completed, unless a new download is starting
      if (prev?.status === 'completed' && data.status !== 'downloading') return

      const entry: FileProgress = {
        active: data.status === 'downloading',
        pct: data.pct || 0,
        status: data.status,
        written: data.written || 0,
        total: data.total || 0,
        speed: data.speed || 0,
        filename: f,
        reason: data.reason || '',
      }
      if (data.status === 'completed') { entry.active = false; entry.pct = 100 }
      // Keep all file entries — completed ones serve as the "already downloaded"
      // indicator in the file tree. Re-downloading a file resets its entry via
      // setFileDownloading().
      fileProgress.value = { ...fileProgress.value, [f]: entry }
    }

    // ── Toast (debounced) ──
    if (!data.status) return
    const toastKey = data.status + '|' + (data.file || '')
    const now = Date.now()
    if (_lastToastKey === toastKey && now - _lastToastTime < 2000) return
    _lastToastKey = toastKey
    _lastToastTime = now

    if (data.status === 'completed') {
      _toastFn?.('success', '下载完成', data.file || '')
    } else if (data.status === 'error') {
      _toastFn?.('error', '下载失败', (data.reason || '') + ' — ' + (data.file || ''))
    }
  }

  function setToast(fn: ToastFn) { _toastFn = fn }

  function bindEvents() {
    if (_bound) return
    _bound = true
    window.runtime.EventsOn('download-progress', _onProgress)
  }

  function startPolling() {
    stopPolling()
    _timer = setInterval(() => refreshHistory(), 1000)
  }

  function stopPolling() {
    if (_timer) { clearInterval(_timer); _timer = null }
  }

  async function pauseDownload(id: string) {
    try { await window.go.app.App.PauseDownload(id) } catch (_) {}
  }
  async function resumeDownload(id: string) {
    try { await window.go.app.App.ResumeDownload(id) } catch (_) {}
  }
  async function retryDownload(id: string) {
    try {
      await window.go.app.App.RetryDownload(id)
      historyTasks.value = historyTasks.value.filter(t => t.id !== id)
      await fetchAll()
    } catch (_) {}
  }
  async function cancelDownload(id: string) {
    // Optimistic removal — immediately reflect the deletion in UI.
    activeTasks.value = activeTasks.value.filter(t => t.id !== id)
    historyTasks.value = historyTasks.value.filter(t => t.id !== id)
    _cancelling.add(id)

    try {
      await window.go.app.App.CancelDownload(id)
    } catch (e) {
      console.error('[download] cancel failed:', id, e)
      // Backend call failed — re-fetch to restore correct state.
      await refreshHistory()
      try {
        activeTasks.value = await window.go.app.App.GetDownloadTasks() || []
      } catch (_) {}
    } finally {
      _cancelling.delete(id)
    }
  }
  async function clearHistory() {
    try { await window.go.app.App.ClearDownloadHistory() } catch (_) {}
    historyTasks.value = []
  }
  function openDownloadDir() { window.go.app.App.OpenDownloadDir().catch(() => {}) }
  function openDownloadedFileDir(filename: string) { window.go.app.App.OpenDownloadedFileDir(filename).catch(() => {}) }

  function setFileDownloading(file: string) {
    fileProgress.value = {
      ...fileProgress.value,
      [file]: { active: true, pct: 0, status: 'downloading', written: 0, total: 0, speed: 0, filename: file, reason: '' },
    }
  }

  function isFileDownloaded(file: string, downloadedSet: Record<string, boolean>): boolean {
    return !!downloadedSet[file]
  }

  return {
    activeTasks, historyTasks, fileProgress,
    completed, failed, downloadCount,
    fetchAll, refreshHistory, bindEvents, setToast, startPolling, stopPolling,
    pauseDownload, resumeDownload, retryDownload, cancelDownload, clearHistory,
    openDownloadDir, openDownloadedFileDir,
    setFileDownloading, isFileDownloaded,
  }
})
