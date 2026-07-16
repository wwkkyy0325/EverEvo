import { ref } from 'vue'
import { defineStore } from 'pinia'
import { memoryApi } from '../api/memory'

export const useWorkspaceStore = defineStore('workspace', () => {
  const activeId = ref('default')
  const workspaces = ref<{ id: string; name: string; createdAt: number }[]>([])

  async function load() {
    try {
      workspaces.value = ((await memoryApi.workspaceList()) || []) as any[]
      if (workspaces.value.length && !activeId.value) activeId.value = workspaces.value[0].id
    } catch (_) { workspaces.value = [] }
  }

  function setActive(id: string) { activeId.value = id }

  async function create(name: string) {
    const id = await memoryApi.workspaceCreate(name)
    await load()
    activeId.value = id
    return id
  }
  async function remove(id: string) {
    if (workspaces.value.length <= 1) return
    await memoryApi.workspaceDelete(id)
    await load()
    if (activeId.value === id) activeId.value = workspaces.value[0]?.id || 'default'
  }

  return { activeId, workspaces, load, setActive, create, remove }
})
