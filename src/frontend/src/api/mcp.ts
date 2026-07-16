/**
 * MCP (Model Context Protocol) server API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface MCPServer {
  id: string
  name: string
  transport: 'stdio' | 'http'
  command: string
  args: string[]
  url: string
  libraryId?: string
  status: 'connected' | 'connecting' | 'disconnected' | 'error'
  toolCount?: number
  error?: string
}

export interface MCPStatus {
  running: boolean
  url: string
}

export interface MCPRecommend {
  key: string
  name: string
  description: string
  category: string
  transport: string
  command: string
  args: string[]
  url: string
}

export const mcpApi = {
  // Local MCP server
  getStatus() { return call<MCPStatus>(() => App().GetMCPStatus()) },
  startServer() { return call(() => App().StartMCPServer()) },
  stopServer() { return call(() => App().StopMCPServer()) },
  setPort(port: number) { return call(() => App().SetMCPPort(port)) },

  // External MCP servers
  listServers(libraryId = '') { return call<MCPServer[]>(() => App().ListMCPServers(libraryId)) },
  addServer(cfg: Partial<MCPServer>) { return call(() => App().AddMCPServer(cfg)) },
  removeServer(id: string) { return call(() => App().RemoveMCPServer(id)) },
  connectServer(id: string) { return call(() => App().ConnectMCPServer(id)) },
  disconnectServer(id: string) { return call(() => App().DisconnectMCPServer(id)) },
  getServerTools(id: string) { return call<any[]>(() => App().GetMCPServerTools(id)) },

  // Recommendations
  listRecommends() { return call<MCPRecommend[]>(() => App().ListMCPRecommends()) },
}
