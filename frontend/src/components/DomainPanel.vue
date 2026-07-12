<template>
  <div class="domain-panel">
    <h2>领域管理</h2>
    <div v-if="err" class="dp-err">{{ err }}</div>
    <div v-else-if="!ready">加载中…</div>
    <div v-else>
      <select v-model="activeLibId" @change="onSwitch" class="dp-select">
        <option v-for="lib in libs" :key="lib.id" :value="lib.id">{{ lib.icon || '📚' }} {{ lib.name }}</option>
      </select>

      <div class="dp-cards">
        <div class="dp-card">📄 知识库: {{ kbs.length }}</div>
        <div class="dp-card">⬡ 智能体: {{ agents.length }}</div>
        <div class="dp-card">⇌ MCP: {{ mcps.length }}</div>
        <div class="dp-card">⚡ 技能: {{ skills.length }}</div>
      </div>

      <h3 v-if="activeLib">{{ activeLib.icon }} {{ activeLib.name }}</h3>

      <div v-if="agents.length">
        <h4>智能体列表</h4>
        <div v-for="a in agents" :key="a.id" class="dp-item">{{ a.icon || '⬡' }} {{ a.name }}</div>
      </div>
      <div v-if="mcps.length">
        <h4>MCP 列表</h4>
        <div v-for="m in mcps" :key="m.id" class="dp-item">⇌ {{ m.name }} ({{ m.status }})</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { memoryApi } from '../api/memory'
import { knowledgeApi } from '../api/knowledge'
import { agentsApi } from '../api/agents'
import { mcpApi } from '../api/mcp'
import { skillsApi } from '../api/skills'

const libs = ref<any[]>([])
const activeLibId = ref('')
const kbs = ref<any[]>([])
const agents = ref<any[]>([])
const mcps = ref<any[]>([])
const skills = ref<any[]>([])
const err = ref('')
const ready = ref(false)

const activeLib = computed(() => libs.value.find((l: any) => l.id === activeLibId.value))

onMounted(async () => {
  try {
    // Step 1: load libraries
    libs.value = await memoryApi.libraryList()
    if (libs.value.length) activeLibId.value = libs.value[0].id

    // Step 2: load resources one at a time
    const libId = activeLibId.value
    if (libId) {
      try { kbs.value = await knowledgeApi.list(libId) } catch (e: any) { console.warn('kb list:', e?.message) }
      try { agents.value = await agentsApi.listByLibrary(libId) } catch (e: any) { console.warn('agents:', e?.message) }
      try { mcps.value = await mcpApi.listServers(libId) } catch (e: any) { console.warn('mcp:', e?.message) }
      try { skills.value = await skillsApi.list(libId) } catch (e: any) { console.warn('skills:', e?.message) }
    }
    ready.value = true
  } catch (e: any) {
    err.value = e?.message || String(e)
    console.error('[DomainPanel]', e)
    ready.value = true
  }
})

async function onSwitch() {
  if (!activeLibId.value) return
  const libId = activeLibId.value
  try { kbs.value = await knowledgeApi.list(libId) } catch (_: any) { kbs.value = [] }
  try { agents.value = await agentsApi.listByLibrary(libId) } catch (_: any) { agents.value = [] }
  try { mcps.value = await mcpApi.listServers(libId) } catch (_: any) { mcps.value = [] }
  try { skills.value = await skillsApi.list(libId) } catch (_: any) { skills.value = [] }
}
</script>

<style scoped>
.domain-panel { padding: 24px; max-width: 960px; margin: 0 auto; color: #e0e0e0; }
.dp-err { background:#3a1a1a; border:1px solid #5a2a2a; color:#f07070; padding:16px; border-radius:8px; margin-bottom:16px; }
.dp-select { width:100%; padding:8px 12px; background:#1a1a1e; border:1px solid #333; border-radius:6px; color:#e0e0e0; font-size:0.95em; margin-bottom:16px; }
.dp-cards { display:flex; gap:12px; margin-bottom:16px; flex-wrap:wrap; }
.dp-card { background:#1a1a1e; border:1px solid #2a2a2e; border-radius:8px; padding:12px 16px; font-size:0.9em; }
.dp-item { padding:6px 12px; background:#1a1a1e; border:1px solid #2a2a2e; border-radius:5px; margin-bottom:4px; font-size:0.88em; display:flex; justify-content:space-between; }
</style>
