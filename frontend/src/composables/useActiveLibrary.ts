/**
 * Shared active domain library state — persisted to localStorage so the
 * selected domain survives app restarts.
 *
 * Both Knowledge.vue (domain picker) and ChatPanel/chatStore (agent filter)
 * read/write this composable so changing the active library in one place
 * automatically updates the other.
 */
import { ref, watch } from 'vue'

const STORAGE_KEY = 'everevo_active_library'
const activeLibraryId = ref(loadFromStorage())

function loadFromStorage(): string {
  try { return localStorage.getItem(STORAGE_KEY) || '' } catch { return '' }
}

// Auto-persist on change.
watch(activeLibraryId, (val) => {
  try { localStorage.setItem(STORAGE_KEY, val || '') } catch { /* ignore */ }
})

export function useActiveLibrary() {
  return { activeLibraryId }
}
