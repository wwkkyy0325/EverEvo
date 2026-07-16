<template>
  <PluginUse v-if="activeSpec" :spec="activeSpec" @back="activeSpec = null" />
  <div v-else class="plugins">
    <div class="toolbar"><h2 class="title">插件</h2><div class="toolbar-actions"><button class="btn btn-primary" @click="doImport" :disabled="busy">📦 导入插件</button><button class="btn" @click="refresh" :disabled="busy">刷新</button></div></div>
    <div v-if="!plugins.length && !busy" class="empty"><div class="empty-icon">🔌</div><p class="empty-title">暂无插件</p><p class="empty-hint">点击「导入插件」选择 .zip 包，或手动放入 data/plugins/ 目录</p></div>
    <div class="plugin-list">
      <div v-for="p in plugins" :key="p.name" class="glass-panel plugin-card">
        <div class="plugin-head">
          <div class="plugin-info"><span class="plugin-name">{{ p.name }}</span><span class="tag tag-muted">v{{ p.version }}</span><span class="tag" :class="running(p.name) ? 'tag-ok' : 'tag-muted'">{{ running(p.name) ? '● 运行中' : '○ 已停止' }}</span><span v-if="running(p.name) && pid(p.name)" class="plugin-pid">PID {{ pid(p.name) }}</span></div>
          <div class="plugin-actions">
            <button v-if="!running(p.name)" class="btn btn-sm btn-primary" @click="doStart(p.name)" :disabled="busy">启动</button>
            <button v-else class="btn btn-sm" @click="doStop(p.name)" :disabled="busy">停止</button>
            <button v-if="running(p.name)" class="btn btn-sm" @click="doRestart(p.name)" :disabled="busy">重启</button>
            <button v-if="running(p.name)" class="btn btn-sm btn-primary" @click="activeSpec = p">使用</button>
            <button class="btn btn-sm" @click="toggleLogs(p.name)">日志</button>
            <button class="btn btn-sm btn-danger" @click="doDelete(p.name)" :disabled="busy">删除</button>
          </div>
        </div>
        <div class="plugin-meta"><span v-if="p.description" class="plugin-desc">{{ p.description }}</span><span class="plugin-type">{{ p.type }}</span><span class="plugin-runtime">{{ p.runtime }}</span><span v-if="p.methods.length" class="plugin-methods"><span v-for="m in p.methods" :key="m" class="tag tag-accent">{{ m }}</span></span></div>
        <div v-if="error(p.name)" class="plugin-error">{{ error(p.name) }}</div>
        <div v-if="logVisible[p.name]" class="plugin-logs"><div class="plugin-logs-head"><span>插件日志</span><button class="btn btn-sm" @click="doRefreshLogs(p.name)">刷新</button></div><pre class="plugin-logs-body">{{ logs[p.name] || '(无输出)' }}</pre></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import PluginUse from './PluginUse.vue'
import { pluginsApi } from '../api/plugins'
import { useDataChanged } from '../composables/useDataChanged'

interface Plugin { name: string; version: string; description?: string; type: string; runtime: string; methods: string[] }

const plugins = ref<Plugin[]>([])
const statuses = reactive<Record<string, any>>({})
const logs = reactive<Record<string, string>>({})
const logVisible = reactive<Record<string, boolean>>({})
const busy = ref(false)
const activeSpec = ref<Plugin | null>(null)
let _timer: ReturnType<typeof setInterval> | null = null

function running(name: string) { return !!(statuses[name]?.running) }
function pid(name: string) { return statuses[name]?.pid || 0 }
function error(name: string) { return statuses[name]?.error || '' }

async function refresh() {
  busy.value = true
  try { plugins.value = (await pluginsApi.list()) || []; await refreshStatus() } catch (_) {}
  busy.value = false
}
async function refreshStatus() {
  for (const p of plugins.value) { try { statuses[p.name] = await pluginsApi.getStatus(p.name) } catch (_) {} }
}
async function doImport() {
  if (busy.value) return; busy.value = true
  try { const path = await pluginsApi.pickFile(); if (path) { await pluginsApi.install(path); await refresh() } } catch (_) {}
  busy.value = false
}
async function doDelete(name: string) { busy.value = true; try { await pluginsApi.remove(name); await refresh() } catch (_) {} busy.value = false }
async function doStart(name: string) { busy.value = true; try { await pluginsApi.start(name) } catch (_) {} busy.value = false; await refreshStatus() }
async function doStop(name: string) { busy.value = true; try { await pluginsApi.stop(name) } catch (_) {} busy.value = false; await refreshStatus() }
async function doRestart(name: string) { busy.value = true; try { await pluginsApi.restart(name) } catch (_) {} busy.value = false; await refreshStatus() }
function toggleLogs(name: string) { if (logVisible[name]) { logVisible[name] = false } else { logVisible[name] = true; doRefreshLogs(name) } }
async function doRefreshLogs(name: string) { try { logs[name] = await pluginsApi.getLogs(name) } catch (_) { logs[name] = '(无法获取日志)' } }

useDataChanged('plugins:changed', () => { refresh() })

onMounted(async () => { await refresh(); _timer = setInterval(() => refreshStatus(), 5000) })
onBeforeUnmount(() => { if (_timer) clearInterval(_timer) })
</script>

<style scoped>
.plugins { width: 100%; }
.toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 24px; }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.toolbar-actions { display: flex; gap: 8px; }
.empty { text-align: center; padding: 80px 0; }
.empty-icon { font-size: 40px; opacity: 0.25; margin-bottom: 12px; }
.empty-title { font-size: 15px; font-weight: 500; margin-bottom: 4px; }
.empty-hint { font-size: 13px; color: var(--text-tertiary); }
.plugin-list { display: flex; flex-direction: column; gap: 8px; }
.plugin-card { padding: 14px 16px; }
.plugin-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.plugin-info { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
.plugin-name { font-size: 14px; font-weight: 550; color: var(--text-primary); }
.plugin-pid { font-size: 10px; color: var(--text-tertiary); font-family: var(--font-mono); }
.plugin-actions { display: flex; gap: 6px; flex-shrink: 0; }
.plugin-meta { display: flex; align-items: center; gap: 8px; margin-top: 10px; flex-wrap: wrap; }
.plugin-desc { font-size: 12px; color: var(--text-secondary); }
.plugin-type { font-size: 11px; color: var(--text-tertiary); }
.plugin-runtime { font-size: 10px; color: var(--text-tertiary); padding: 1px 6px; border: 1px solid var(--border-subtle); border-radius: 4px; font-family: var(--font-mono); }
.plugin-methods { display: flex; gap: 4px; flex-wrap: wrap; }
.plugin-error { margin-top: 8px; font-size: 11px; color: var(--danger); padding: 6px 10px; background: var(--danger-dim); border-radius: var(--radius-sm); }
.plugin-logs { margin-top: 12px; border-top: 1px solid var(--border-subtle); padding-top: 10px; }
.plugin-logs-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; font-size: 11px; color: var(--text-tertiary); }
.plugin-logs-body { margin: 0; padding: 10px 12px; background: var(--bg-inset); border-radius: var(--radius-sm); font-size: 11px; font-family: var(--font-mono); line-height: 1.5; color: var(--text-secondary); max-height: 200px; overflow-y: auto; white-space: pre-wrap; word-break: break-all; }
</style>
