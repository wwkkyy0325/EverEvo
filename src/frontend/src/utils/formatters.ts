// Shared formatting utilities used across multiple components.

/** Format a number with locale-aware thousands separator. */
export function fmtNum(n: number | undefined): string {
  if (n == null || isNaN(n)) return '0'
  return n.toLocaleString('zh-CN')
}

/**
 * Format a byte count into human-readable size string.
 * Handles negative values gracefully ("—").
 */
export function fmtSize(bytes: number): string {
  if (bytes <= 0) return '—'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
}

/** Format a large count into compact K/M notation for display. */
export function fmtCount(n: number): string {
  return n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? (n / 1e3).toFixed(1) + 'K' : String(n || 0)
}

/** Format context window tokens into human-readable string. */
export function fmtCtx(tokens: number): string {
  if (!tokens || tokens <= 0) return '未知'
  if (tokens >= 1000) return Math.round(tokens / 1000) + 'K'
  return tokens.toString()
}
