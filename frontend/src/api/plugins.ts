/**
 * Plugin API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface Plugin {
  name: string
  version: string
  description: string
  type: string
  runtime: string
  methods: string[]
}

export interface PluginStatus {
  running: boolean
  pid: number
  error: string
}

export const pluginsApi = {
  list() { return call<Plugin[]>(() => App().ListPlugins()) },
  getStatus(name: string) { return call<PluginStatus>(() => App().GetPluginStatus(name)) },
  install(path: string) { return call<Plugin>(() => App().InstallPlugin(path)) },
  start(name: string) { return call(() => App().StartPlugin(name)) },
  stop(name: string) { return call(() => App().StopPlugin(name)) },
  restart(name: string) { return call(() => App().RestartPlugin(name)) },
  remove(name: string) { return call(() => App().DeletePlugin(name)) },
  pickFile() { return call<string>(() => App().PickPluginFile()) },
  getLogs(name: string) { return call<string>(() => App().GetPluginLogs(name)) },
  execute(name: string, method: string, args: any) {
    return call<any>(() => App().ExecutePlugin(name, method, args))
  },
}
