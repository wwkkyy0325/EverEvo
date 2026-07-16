/**
 * API layer barrel export.
 *
 * Usage:
 *   import { providersApi, modelsApi, mcpApi } from '@/api'
 *   const list = await providersApi.list()
 */

export { call, callWithCancel, createCancelToken, ApiError } from './client'
export type { CallOptions } from './client'

export { providersApi } from './providers'
export type { Provider, ModelCapability, Preset, LLMConfig } from './providers'

export { modelsApi } from './models'
export type { ModelInfo, DownloadTask } from './models'

export { mcpApi } from './mcp'
export type { MCPServer, MCPStatus, MCPRecommend } from './mcp'

export { skillsApi } from './skills'
export type { Skill, SkillPackage, Guide, GuideSource } from './skills'

export { systemApi } from './system'
export type { SysInfo, DynamicInfo, Backend } from './system'

export { pluginsApi } from './plugins'
export type { Plugin, PluginStatus } from './plugins'

export { workflowApi } from './workflow'
export type { Workflow } from './workflow'

export { knowledgeApi } from './knowledge'
export type { KnowledgeBase, DocEntry, SearchResult } from './knowledge'

export { agentApi } from './agent'
export type { A2AConfig, RemoteAgent, AgentServerStatus } from './agent'

export { agentsApi } from './agents'
export type { LocalAgent, AgentToolDef, AgentChatContext } from './agents'

export { feishuApi } from './feishu'
export type { FeishuConfig, FeishuStatus } from './feishu'

export { memoryApi } from './memory'
export type { MemorySession, MemoryMessage } from './memory'
