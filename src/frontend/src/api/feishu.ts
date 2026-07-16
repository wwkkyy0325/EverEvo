/**
 * Feishu Bot API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface FeishuConfig {
  enabled: boolean
  appId: string
  appSecret: string
  verificationToken: string
}

export interface FeishuStatus {
  running: boolean
  appId: string
}

export const feishuApi = {
  getConfig() { return call<FeishuConfig>(() => App().GetFeishuConfig()) },
  updateConfig(cfg: FeishuConfig) { return call(() => App().UpdateFeishuConfig(cfg)) },
  start() { return call(() => App().StartFeishu()) },
  stop() { return call(() => App().StopFeishu()) },
  getStatus() { return call<FeishuStatus>(() => App().GetFeishuStatus()) },
}
