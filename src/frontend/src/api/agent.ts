/**
 * A2A Agent API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface A2AConfig {
  enabled: boolean
  name: string
  description: string
  version: string
  port: number
  secret: string
}

export interface AgentServerStatus {
  running: boolean
  port: number
  url: string
}

export interface RemoteAgent {
  id: string
  name: string
  url: string
  secret?: string
  status: 'connected' | 'disconnected' | 'error'
  error?: string
  card?: {
    name: string
    description: string
    version: string
    capabilities: Record<string, boolean>
    skills: any[]
  }
  connectedAt?: number
}

export const agentApi = {
  // Config
  getConfig() { return call<A2AConfig>(() => App().GetA2AConfig()) },
  updateConfig(cfg: A2AConfig) { return call(() => App().UpdateA2AConfig(cfg)) },

  // Server
  getServerStatus() { return call<AgentServerStatus>(() => App().GetAgentServerStatus()) },
  startServer() { return call(() => App().StartAgentServer()) },
  stopServer() { return call(() => App().StopAgentServer()) },

  // Remote agents
  listRemoteAgents() { return call<RemoteAgent[]>(() => App().ListRemoteAgents()) },
  addRemoteAgent(name: string, url: string, secret: string) { return call<RemoteAgent>(() => App().AddRemoteAgent(name, url, secret)) },
  removeRemoteAgent(id: string) { return call(() => App().RemoveRemoteAgent(id)) },
  connectRemoteAgent(id: string) { return call(() => App().ConnectRemoteAgent(id)) },
  disconnectRemoteAgent(id: string) { return call(() => App().DisconnectRemoteAgent(id)) },
  updateRemoteAgent(id: string, name: string, url: string, secret: string) { return call(() => App().UpdateRemoteAgent(id, name, url, secret)) },

  // Tasks
  sendTask(agentID: string, text: string) { return call<any>(() => App().SendAgentTask(agentID, text)) },
  getTask(agentID: string, taskID: string) { return call<any>(() => App().GetAgentTask(agentID, taskID)) },

  // Agent as Skill
  createAgentSkill(agentID: string, packageName: string) { return call<any>(() => App().CreateAgentSkill(agentID, packageName)) },
  removeAgentSkill(agentID: string) { return call(() => App().RemoveAgentSkill(agentID)) },
}
