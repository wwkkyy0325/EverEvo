/**
 * System info API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface SysInfo {
  cpu: { vendor: string; name: string; threads: number; features: any[] }
  memory: { totalGB: number }
  gpus: any[]
}

export interface DynamicInfo {
  cpuPercent: number
  memoryUsedGB: number
  memoryTotalGB: number
  memoryPercent: number
  gpus: any[]
  disks: any[]
  physicalDisks: any[]
}

export interface Backend {
  key: string
  name: string
  ok: boolean
}

export const systemApi = {
  getSysInfo() { return call<SysInfo>(() => App().GetSysInfo()) },
  getDynamicInfo() { return call<DynamicInfo>(() => App().GetDynamicInfo()) },
  getBackends() { return call<Backend[]>(() => App().GetBackends()) },
  getPlatformInfo() { return call<any>(() => App().GetPlatformInfo()) },
  getMirrors() { return call<any[]>(() => App().GetMirrors()) },
  getBackendDownloadURL(key: string, mirror: string, variant: string) {
    return call<string>(() => App().GetBackendDownloadURL(key, mirror, variant))
  },
  getExeDir() { return call<string>(() => App().GetExeDir()) },
  getUserConfigDir() { return call<string>(() => App().GetUserConfigDir()) },
  getDataDir() { return call<string>(() => App().GetDataDir()) },
  getModelsDir() { return call<string>(() => App().GetModelsDir()) },
  openDir(path: string) { return call(() => App().OpenDir(path)) },
  openFileLocation(path: string) { return call<void>(() => App().OpenFileLocation(path)) },
  logToTerminal(msg: string) { return call<void>(() => App().LogToTerminal(msg)) },
  hasStartMenuShortcut() { return call<boolean>(() => App().HasStartMenuShortcut()) },
  pinToStartMenu() { return call(() => App().PinToStartMenu()) },
  unpinFromStartMenu() { return call(() => App().UnpinFromStartMenu()) },
  getConfig() { return call<any>(() => App().GetConfig()) },

  // ── Proxy ──
  getProxyStatus() { return call<{ url: string; source: string; enabled: boolean; healthy: boolean }>(() => App().GetProxyStatus()) },
  setProxy(url: string) { return call(() => App().SetProxy(url)) },
  setProxyEnabled(enabled: boolean) { return call<void>(() => App().SetProxyEnabled(enabled)) },
  testProxy(url: string) { return call<string>(() => App().TestProxy(url)) },
}
