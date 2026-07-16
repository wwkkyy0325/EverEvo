/**
 * Local Agent (persona) API — manage local 智能体 personas.
 *
 * Distinct from agent.ts (A2A remote agents): those are external peers reached
 * over the A2A protocol; these are local named LLM profiles.
 */
import { call } from './client'

const App = () => window.go.app.App

export interface LocalAgent {
  id: string
  name: string
  description: string
  icon?: string
  systemPrompt: string
  providerId?: string
  model?: string
  inheritSkills: boolean
  skills?: string[]
  tools?: string[]
  mcpTools?: string[]
  temperature?: number | null
  maxTokens?: number
  isDefault?: boolean
  libraryId?: string
  createdAt?: number
  updatedAt?: number
}

/** A callable tool definition (subset used by chat). */
export interface AgentToolDef {
  name: string
  description: string
  parameters: Record<string, unknown>
  annotations?: { readOnlyHint?: boolean }
}

/** Resolved chat inputs for an agent (from GetAgentChatContext). */
export interface AgentChatContext {
  agentId: string
  name: string
  systemPrompt: string
  tools: AgentToolDef[]
  providerId: string
  model: string
}

export const agentsApi = {
  list() { return call<LocalAgent[]>(() => App().ListAgents()) },
  listByLibrary(libraryId: string) { return call<LocalAgent[]>(() => App().ListAgentsByLibrary(libraryId)) },
  get(id: string) { return call<LocalAgent>(() => App().GetAgent(id)) },
  create(agent: Partial<LocalAgent>) { return call<LocalAgent>(() => App().CreateAgent(agent)) },
  update(id: string, agent: LocalAgent) { return call<LocalAgent>(() => App().UpdateAgent(id, agent)) },
  remove(id: string) { return call(() => App().DeleteAgent(id)) },
  getCoreAgent() { return call<LocalAgent>(() => App().GetCoreAgent()) },
  getChatContext(id: string) { return call<AgentChatContext>(() => App().GetAgentChatContext(id)) },

  /**
   * Stream a chat turn under a specific provider/model (agent override).
   * temperature < 0 and maxTokens <= 0 mean "omit" (use provider default).
   * Listeners for chat-chunk-<streamId> / chat-done-<streamId> / chat-err-<streamId>
   * must be registered before calling.
   */
  streamAs(
    streamId: string,
    messages: unknown[],
    tools: unknown[],
    providerId: string,
    model: string,
    temperature: number,
    maxTokens: number,
    thinkEffort: string,
  ) {
    return call(() => App().ChatStreamAs(streamId, messages, tools, providerId, model, temperature, maxTokens, thinkEffort))
  },
}
