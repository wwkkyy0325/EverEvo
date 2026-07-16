import { ref, computed } from 'vue'
import { defineStore } from 'pinia'

export interface AsyncTask {
  id: string
  sessionId: string
  title: string
  status: string       // pending | running | done | failed | cancelled
  toolName: string
  toolArgs: string
  context: string
  result: string
  error: string
  createdAt: number
  updatedAt: number
  completedAt: number
}

export const useAsyncStore = defineStore('async', () => {
  const tasks = ref<AsyncTask[]>([])
  let _bound = false

  const running = computed(() => tasks.value.filter(t => t.status === 'running' || t.status === 'pending'))
  const completed = computed(() => tasks.value.filter(t => t.status === 'done'))
  const failed = computed(() => tasks.value.filter(t => t.status === 'failed' || t.status === 'cancelled'))
  const activeCount = computed(() => running.value.length)

  function upsert(t: AsyncTask) {
    const idx = tasks.value.findIndex(x => x.id === t.id)
    if (idx >= 0) tasks.value[idx] = t
    else tasks.value.unshift(t)
  }

  function bindEvents() {
    if (_bound) return
    _bound = true
    const rt = (window as any).runtime
    if (!rt?.EventsOn) return
    rt.EventsOn('async-task:created', (t: AsyncTask) => upsert(t))
    rt.EventsOn('async-task:started', (d: any) => {
      const t = tasks.value.find(x => x.id === d.id)
      if (t) t.status = 'running'
    })
    rt.EventsOn('async-task:completed', (t: AsyncTask) => upsert(t))
    rt.EventsOn('async-task:failed', (d: any) => {
      if (typeof d === 'string') {
        // Could be error string
      } else if (d?.id) {
        const t = tasks.value.find(x => x.id === d.id)
        if (t) { t.status = d.status || 'failed'; t.error = d.error || '' }
      }
    })
  }

  async function fetchAll() {
    try {
      const go = (window as any).go
      if (!go?.app?.App?.ListAsyncTasks) return
      tasks.value = await go.app.App.ListAsyncTasks('', '') || []
    } catch (_) { /* ignore */ }
  }

  async function cancelTask(id: string) {
    try {
      await (window as any).go.app.App.CancelAsyncTask(id)
      await fetchAll()
    } catch (_) { /* ignore */ }
  }

  // Called once on app mount
  function init() {
    bindEvents()
    fetchAll()
    // Poll every 10s as fallback
    setInterval(fetchAll, 10_000)
  }

  return { tasks, running, completed, failed, activeCount, init, fetchAll, cancelTask }
})
