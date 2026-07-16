/**
 * Model Profile Configuration — unified, backend-driven.
 *
 * The backend's GetModelRegistry() is the single source of truth.
 * Local MODEL_PRESETS serve as an offline fallback only.
 *
 * Lookup priority:
 *   1. Backend registry cache (populated on app load)
 *   2. Local MODEL_PRESETS (offline / fallback)
 *   3. FALLBACK_PROFILE (conservative default)
 */

import { providersApi } from '@/api'
import type { ModelRegistryEntry } from '@/api'

// ── Types ──

export interface ModelProfile {
  /** Human-readable label (shown in settings). */
  label: string
  /** Model context window in tokens. */
  contextWindow: number
  /** Maximum output tokens the API accepts.
   *  Set to 0 to omit max_tokens entirely (let the model/server decide). */
  maxOutputTokens: number
  /** Percentage of context window usable for input (system + history + recall). */
  effectivePct: number
  /** Percentage of context window at which auto-compaction triggers. */
  compactPct: number
  /** Whether this provider supports a separate thinking/reasoning token budget. */
  supportsThinkingBudget: boolean
}

// ── Backend Registry Cache ──

/** Cached registry from the backend. Keyed by "providerName modelName" (lowercase). */
let _registryCache: Map<string, ModelRegistryEntry> | null = null

/**
 * Fetch and cache the backend model registry.
 * Call this once on app startup (or when providers change).
 */
export async function refreshModelRegistry(): Promise<void> {
  try {
    // Guard: providersApi may not be ready during early module init.
    if (!providersApi?.getModelRegistry) return
    const entries = await providersApi.getModelRegistry()
    if (!entries?.length) return
    _registryCache = new Map()
    for (const entry of entries) {
      const key = `${entry.providerName} ${entry.modelName}`.toLowerCase()
      _registryCache.set(key, entry)
    }
  } catch (e) {
    console.warn('[modelProfiles] failed to refresh backend registry:', e)
  }
}

/** Check if the backend registry has been loaded. */
export function hasBackendRegistry(): boolean {
  return _registryCache !== null && _registryCache.size > 0
}

// ── Local Presets (offline fallback) ──

/** Source: official docs, API responses, and Codex models.json reference. */
export const MODEL_PRESETS: Record<string, ModelProfile> = {
  // ── DeepSeek ──
  'deepseek-v4-pro': {
    label: 'DeepSeek V4 Pro',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'deepseek-v4-flash': {
    label: 'DeepSeek V4 Flash',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'deepseek-chat': {
    label: 'DeepSeek V4 (legacy: deepseek-chat)',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'deepseek': {
    label: 'DeepSeek (latest / unknown model)',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },

  // ── Anthropic Claude ──
  'claude opus': {
    label: 'Claude Opus 4+',
    contextWindow: 200_000,
    maxOutputTokens: 128_000,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },
  'claude sonnet': {
    label: 'Claude Sonnet 4+',
    contextWindow: 200_000,
    maxOutputTokens: 64_000,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },
  'claude haiku': {
    label: 'Claude Haiku 4+',
    contextWindow: 200_000,
    maxOutputTokens: 64_000,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },
  'claude': {
    label: 'Claude (generic)',
    contextWindow: 200_000,
    maxOutputTokens: 64_000,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },

  // ── OpenAI ──
  'gpt-5': {
    label: 'GPT-5 / GPT-5.x',
    contextWindow: 272_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'gpt-4': {
    label: 'GPT-4o / GPT-4 Turbo',
    contextWindow: 128_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'o4': {
    label: 'OpenAI o4 / o3',
    contextWindow: 200_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },

  // ── Google Gemini ──
  'gemini 2.5': {
    label: 'Gemini 2.5+',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },
  'gemini': {
    label: 'Gemini (generic)',
    contextWindow: 1_000_000,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: true,
  },

  // ── Qwen / Tongyi (local) ──
  'qwen2.5-coder-3b': {
    label: 'Qwen2.5 Coder 3B',
    contextWindow: 32_768,
    maxOutputTokens: 0,
    effectivePct: 90,
    compactPct: 85,
    supportsThinkingBudget: false,
  },
  'qwen2.5-coder': {
    label: 'Qwen2.5 Coder',
    contextWindow: 32_768,
    maxOutputTokens: 0,
    effectivePct: 90,
    compactPct: 85,
    supportsThinkingBudget: false,
  },
  // Qwen3.5 — before 'qwen' so "qwen3.5" doesn't fall through to generic
  'qwen3.5-9b': {
    label: 'Qwen3.5 9B',
    contextWindow: 65_536,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'qwen3.5': {
    label: 'Qwen3.5',
    contextWindow: 65_536,
    maxOutputTokens: 0,
    effectivePct: 95,
    compactPct: 90,
    supportsThinkingBudget: false,
  },
  'qwen': {
    label: 'Qwen (generic)',
    contextWindow: 131_072,
    maxOutputTokens: 0,
    effectivePct: 90,
    compactPct: 85,
    supportsThinkingBudget: false,
  },

  // ── Llama (local) ──
  'llama-3': {
    label: 'Llama 3',
    contextWindow: 8_192,
    maxOutputTokens: 0,
    effectivePct: 90,
    compactPct: 85,
    supportsThinkingBudget: false,
  },
  'llama': {
    label: 'Llama (generic)',
    contextWindow: 8_192,
    maxOutputTokens: 0,
    effectivePct: 85,
    compactPct: 80,
    supportsThinkingBudget: false,
  },

  // ── Mistral (local) ──
  'mistral': {
    label: 'Mistral (generic)',
    contextWindow: 32_000,
    maxOutputTokens: 0,
    effectivePct: 90,
    compactPct: 85,
    supportsThinkingBudget: false,
  },
}

// ── Fallback for unknown models ──

export const FALLBACK_PROFILE: ModelProfile = {
  label: 'Unknown Model (conservative fallback)',
  contextWindow: 32_768,
  maxOutputTokens: 0,
  effectivePct: 80,
  compactPct: 80,
  supportsThinkingBudget: false,
}

// ── Lookup ──

/**
 * Find the best-matching ModelProfile for a provider+model string.
 * Priority: backend registry → local presets → fallback.
 */
export function getModelProfile(providerName?: string, modelName?: string): ModelProfile {
  const haystack = `${providerName || ''} ${modelName || ''}`.toLowerCase().trim()

  // Guard: empty input → conservative fallback (don't match the first entry)
  if (!haystack) return FALLBACK_PROFILE

  // 1. Backend registry (authoritative)
  if (_registryCache) {
    // Try exact match first
    const exact = _registryCache.get(haystack)
    if (exact) return entryToProfile(exact)
    // Try substring match — key must be a substring of haystack (not reverse)
    for (const [key, entry] of _registryCache) {
      if (haystack.includes(key)) {
        return entryToProfile(entry)
      }
    }
  }

  // 2. Local presets (offline fallback)
  for (const [key, profile] of Object.entries(MODEL_PRESETS)) {
    if (haystack.includes(key)) return profile
  }

  // 3. Conservative default
  return FALLBACK_PROFILE
}

/** Convert a backend ModelRegistryEntry to the frontend ModelProfile shape. */
function entryToProfile(entry: ModelRegistryEntry): ModelProfile {
  return {
    label: entry.label,
    contextWindow: entry.contextWindow,
    maxOutputTokens: entry.maxOutputTokens,
    effectivePct: entry.effectivePct,
    compactPct: entry.compactPct,
    supportsThinkingBudget: entry.supportsThinkingBudget,
  }
}

/**
 * Return all registered presets (for settings UI).
 */
export function listProfiles(): Array<{ key: string } & ModelProfile> {
  return Object.entries(MODEL_PRESETS).map(([key, profile]) => ({ key, ...profile }))
}
