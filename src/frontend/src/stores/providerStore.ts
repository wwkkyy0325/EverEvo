/**
 * LLM Provider store — CRUD + active selection + model capabilities.
 */
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { providersApi } from '@/api'
import type { Provider, Preset, ModelCapability } from '@/api'

export const useProviderStore = defineStore('providers', () => {
  const providers = ref<Provider[]>([])
  const presets = ref<Preset[]>([])
  const activeId = ref<string>('')
  const loading = ref(false)

  const activeProvider = computed(() =>
    providers.value.find(p => p.id === activeId.value && p.enabled) || null
  )

  async function load() {
    try {
      providers.value = await providersApi.list()
      presets.value = await providersApi.listPresets()
      const cfg = await providersApi.getConfig()
      activeId.value = cfg.activeProvider || ''
    } catch (_) { /* */ }
  }

  async function create(data: Partial<Provider>) {
    await providersApi.create(data)
    await load()
  }

  async function update(id: string, data: Partial<Provider>) {
    await providersApi.update(id, data)
    await load()
  }

  async function remove(id: string) {
    await providersApi.remove(id)
    await load()
  }

  async function setActive(id: string) {
    const p = providers.value.find(p => p.id === id)
    if (p) { p.enabled = true; await providersApi.update(id, p) }
    await providersApi.setActive(id)
    await load()
  }

  async function testConnection(id: string) {
    return await providersApi.testConnection(id)
  }

  async function probeCapability(endpoint: string, apiKey: string, model: string, format: string) {
    return await providersApi.probeCapability(endpoint, apiKey, model, format)
  }

  return {
    providers, presets, activeId, activeProvider, loading,
    load, create, update, remove, setActive, testConnection, probeCapability,
  }
})
