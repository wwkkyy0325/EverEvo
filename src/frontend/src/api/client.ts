/**
 * Wails API call wrapper — timeout, abort, unified error handling.
 *
 * Usage:
 *   const models = await call(() => window.go.app.App.ListModels())
 *   const { data, cancel } = callWithCancel(() => window.go.app.App.ProbeModelCap(...))
 */

export interface CallOptions {
  /** Timeout in ms (default 30_000) */
  timeout?: number
  /** AbortSignal for cancellation */
  signal?: AbortSignal
}

export class ApiError extends Error {
  code: string
  detail: string
  constructor(message: string, code = 'UNKNOWN', detail = '') {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.detail = detail
  }
}

function errMsg(e: unknown): string {
  if (typeof e === 'string') return e
  if (e && typeof e === 'object' && 'message' in e) return (e as Error).message
  return String(e)
}

/**
 * Call a Wails backend function with timeout and abort support.
 * Returns null-guarded: null/undefined array results are coerced to [].
 */
export async function call<T>(fn: () => Promise<T>, opts: CallOptions = {}): Promise<T> {
  const { timeout = 30_000, signal } = opts

  return new Promise<T>((resolve, reject) => {
    let settled = false

    const finish = (fn: () => void) => {
      if (settled) return
      settled = true
      fn()
    }

    // Timeout
    const tid = setTimeout(() => {
      finish(() => reject(new ApiError('请求超时', 'TIMEOUT')))
    }, timeout)

    // External abort
    if (signal) {
      if (signal.aborted) {
        clearTimeout(tid)
        return reject(new ApiError('请求已取消', 'CANCELLED'))
      }
      const onAbort = () => {
        finish(() => reject(new ApiError('请求已取消', 'CANCELLED')))
      }
      signal.addEventListener('abort', onAbort, { once: true })
    }

    fn()
      .then((val) => {
        // Root fix: Go nil slices marshal to null over Wails bridge.
        // Coerce null arrays to empty arrays so downstream .filter()/.map()
        // never see null. Non-array types pass through unchanged.
        if (val === null || val === undefined) {
          resolve([] as unknown as T)
        } else {
          resolve(val)
        }
      })
      .catch((e) => finish(() => reject(new ApiError(errMsg(e), 'BACKEND_ERROR', errMsg(e)))))
      .finally(() => clearTimeout(tid))
  })
}

/**
 * Create an AbortController + signal pair for cancellation.
 */
export function createCancelToken(): { controller: AbortController; signal: AbortSignal } {
  const controller = new AbortController()
  return { controller, signal: controller.signal }
}

/**
 * Convenience — call with auto-cancel:
 *   const { data, cancel } = callWithCancel(fn)
 *   cancel() // later
 */
export async function callWithCancel<T>(
  fn: (signal: AbortSignal) => Promise<T>,
  opts: Omit<CallOptions, 'signal'> = {},
): Promise<{ data: T; cancel: () => void }> {
  const { controller, signal } = createCancelToken()
  const data = await call(() => fn(signal), { ...opts, signal })
  return { data, cancel: () => controller.abort() }
}
