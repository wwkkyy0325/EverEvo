/**
 * Provider / LLM config API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface Provider {
  id: string
  name: string
  icon: string
  notes: string
  website: string
  type: string
  apiFormat: string
  endpoint: string
  apiKey: string
  model: string
  models: string[]
  modelCapabilities: Record<string, ModelCapability>
  enableJSONOutput: boolean
  enabled: boolean
  createdAt: number
}

export interface BalanceInfo {
  isAvailable: boolean
  balanceInfos: BalanceEntry[]
}

export interface BalanceEntry {
  currency: string
  totalBalance: string
  grantedBalance: string
  toppedUpBalance: string
}

export interface ModelCapability {
  supportsVision: boolean
  supportsTools: boolean
  supportsReasoning: boolean
  supportsStreaming: boolean
  supportsJSON: boolean
  supportsFIM: boolean
  maxContextTokens: number
}

/** Unified model profile from the backend registry. */
export interface ModelRegistryEntry {
  label: string
  contextWindow: number
  maxOutputTokens: number
  effectivePct: number
  compactPct: number
  supportsThinkingBudget: boolean
  providerId: string
  providerName: string
  modelName: string
  capabilities: ModelCapability
}

export interface Preset {
  type: string
  name: string
  icon: string
  apiFormat: string
  endpoint: string
  models: string[]
}

export interface LLMConfig {
  activeProvider: string
  mcpPort: number
}

export const providersApi = {
  list() { return call<Provider[]>(() => App().ListProviders()) },
  listPresets() { return call<Preset[]>(() => App().ListPresets()) },
  getConfig() { return call<LLMConfig>(() => App().GetConfig().then((c: any) => c?.llm || {})) },
  create(data: Partial<Provider>) { return call(() => App().CreateProvider(data)) },
  update(id: string, data: Partial<Provider>) { return call(() => App().UpdateProvider(id, data)) },
  remove(id: string) { return call(() => App().DeleteProvider(id)) },
  setActive(id: string) { return call(() => App().SetActiveProvider(id)) },
  setExtractionProvider(id: string) { return call(() => App().SetExtractionProvider(id)) },
  testConnection(id: string) { return call<string>(() => App().TestProviderConnection(id)) },
  probeCapability(endpoint: string, apiKey: string, model: string, format: string) {
    return call<ModelCapability>(() => App().ProbeModelCap(endpoint, apiKey, model, format))
  },
  fetchOllamaModels(base: string) { return call(() => App().FetchOllamaModels(base)) },
  fetchOpenAIModels(base: string, key: string) { return call(() => App().FetchOpenAIModels(base, key)) },
  fetchDeepSeekModels(apiKey: string) { return call<any[]>(() => App().FetchDeepSeekModels(apiKey)) },
  queryBalance(providerID: string) { return call<BalanceInfo>(() => App().QueryBalance(providerID)) },
  getModelRegistry() { return call<ModelRegistryEntry[]>(() => App().GetModelRegistry()) },
  findBestModelForTask(task: string) { return call<Provider | null>(() => App().FindBestModelForTask(task)) },
}
