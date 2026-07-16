/**
 * File control API — mode switching and audit resolution.
 */
import { call } from './client'

const App = () => window.go.app.App

export type FileControlMode = 'readonly' | 'audit' | 'full'

export interface AuditRequest {
  id: string
  tool: string
  path: string
  content: string
  size: number
  createdAt: number
}

export const filectlApi = {
  getMode() { return call<{ mode: string }>(() => App().GetFileControlMode()) },
  setMode(mode: FileControlMode) { return call(() => App().SetFileControlMode(mode)) },
  resolveAudit(requestId: string, approved: boolean, permanent: boolean) {
    return call(() => App().ResolveAudit(requestId, approved, permanent))
  },
}
