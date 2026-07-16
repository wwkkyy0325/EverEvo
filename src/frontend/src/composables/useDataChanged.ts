import { onMounted, onBeforeUnmount } from 'vue'

/**
 * Subscribe to a backend `*:changed` event and invoke `cb` when the entity
 * mutates, so a page refreshes in real time while the LLM (or any backend op)
 * changes the data it displays. Auto-unsubscribes on unmount.
 *
 * Each event name (e.g. "models:changed", "kb:changed") is assumed to have a
 * single active subscriber — the one page currently mounted for that entity —
 * so EventsOff(name) on unmount only removes this page's listener.
 */
export function useDataChanged(event: string, cb: (d: { action: string; id?: string }) => void) {
  const handler = (d: any) => cb({ action: d?.action, id: d?.id })
  onMounted(() => { try { window.runtime.EventsOn(event, handler) } catch (_) {} })
  onBeforeUnmount(() => { try { window.runtime.EventsOff(event) } catch (_) {} })
}
