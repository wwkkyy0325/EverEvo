import { getCurrentInstance } from 'vue'

/**
 * Type-safe toast helper. Captures $root during setup so it works in async callbacks
 * where getCurrentInstance() returns null.
 */
export function useToast() {
  const root = (getCurrentInstance()?.proxy as any)?.$root
  return {
    show(type: 'success' | 'error' | 'info' | 'warning', title: string, desc?: string) {
      try { root?.showToast?.(type, title, desc) } catch (_) {}
    },
    async confirm(title: string, message: string): Promise<boolean> {
      try { return await root?.showConfirm?.(title, message) ?? false } catch (_) { return false }
    },
  }
}
