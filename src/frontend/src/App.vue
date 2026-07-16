<template>
  <div class="shell">
    <ToastContainer :toasts="toasts" @remove="removeToast" />
    <ConfirmModal ref="confirmModal" />
    <AuditDialog />

    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="sidebar-top">
        <div class="brand" @click="sidebarCollapsed = !sidebarCollapsed"
          :title="sidebarCollapsed ? '展开' : '收起'">
          <span v-if="sidebarCollapsed" class="brand-collapse">▷</span>
          <template v-else>
            <span class="brand-icon">◈</span>
            <span class="brand-name">EverEvo<span class="brand-version">v0.1</span></span>
            <span class="toggle-btn">◀</span>
          </template>
        </div>

        <nav class="nav">
          <button v-for="item in navItems" :key="item.route"
            class="nav-item" :class="{ active: currentRoute === item.route }"
            @click="navigateTo(item.route)"
            :title="sidebarCollapsed ? item.label : ''">
            <span class="nav-icon">{{ item.icon }}</span>
            <span class="nav-label">{{ item.label }}</span>
            <span v-if="item.route === 'my-models' && downloadCount && !sidebarCollapsed" class="nav-badge">{{ downloadCount }}</span>
          </button>
        </nav>

        <div class="nav-sep"></div>
        <div class="nav-sep"></div>
        <button class="nav-item chat-sidebar-toggle"
          :class="{ active: showChatPanel }"
          @click="showChatPanel = !showChatPanel"
          :title="sidebarCollapsed ? 'AI 对话' : ''">
          <span class="nav-icon">◉</span>
          <span class="nav-label">AI 对话</span>
        </button>
      </div>

      <div class="sidebar-foot">
        <div v-if="!sidebarCollapsed" class="foot-row glass-panel">
          <div class="status-bar" ref="statusRef"
            @click="goToBackends" @mouseenter="openStatusPanel" @mouseleave="startStatusClose"
            title="底层设施状态">
            <span class="status-label">设施</span>
            <span class="status-dot" :class="backendsOk ? 'dot-live' : 'dot-dead'"></span>
          </div>
          <Teleport to="body">
            <transition name="acc-fade">
              <div v-if="showStatus" class="status-dropdown glass-panel" :style="statusPanelStyle"
                @mouseenter="cancelStatusClose" @mouseleave="startStatusClose">
              <div class="sd-head">底层设施</div>
              <div v-for="b in backends" :key="b.key" class="sd-row">
                <span class="sd-dot" :class="b.ok ? 'dot-live' : 'dot-dead'"></span>
                <span class="sd-name">{{ b.name }}</span>
                <span class="sd-state">{{ b.ok ? '可用' : '未安装' }}</span>
              </div>
            </div>
          </transition>
          </Teleport>
        </div>
        <div v-else class="status-collapsed" @click="goToBackends" title="底层设施状态">
          <span class="status-dot" :class="backendsOk ? 'dot-live' : 'dot-dead'"></span>
        </div>
      </div>
    </aside>

    <main class="main" :class="{ 'chat-open': showChatPanel }">
      <div class="main-inner">
        <RouterView v-slot="{ Component }">
          <KeepAlive :include="['MyModels']">
            <component :is="Component" />
          </KeepAlive>
        </RouterView>
      </div>

      <div v-if="showChatPanel" class="chat-pane">
        <div class="chat-pane-head">
          <span class="chat-pane-title">AI 对话</span>
          <div class="chat-pane-head-actions">
            <button class="chat-pane-close" @click="showChatPanel = false" title="关闭">✕</button>
          </div>
        </div>
        <div class="chat-pane-body">
          <ChatPanel :compact="true" />
        </div>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import ToastContainer from './components/ToastContainer.vue'
import AuditDialog from "./components/AuditDialog.vue"
import ConfirmModal from './components/ConfirmModal.vue'
import ChatPanel from './components/ChatPanel.vue'
import { useDownloadStore } from './stores/downloadStore'
import { useChatStore } from './stores/chatStore'
import { useAsyncStore } from './stores/asyncStore'
import { useActiveLibrary } from './composables/useActiveLibrary'
import { memoryApi } from './api/memory'

import { systemApi } from './api/system'

const router = useRouter()
const route = useRoute()

const navItems = [
  { route: 'my-models', icon: '□', label: '我的模型' },
  { route: 'memory', icon: '🧠', label: '记忆' },
  { route: 'knowledge', icon: '◈', label: '领域' },
  { route: 'paradigm', icon: '🧠', label: '思维范式' },
  { route: 'guides', icon: '☰', label: '攻略' },
  { route: 'plugins', icon: '⊕', label: '插件' },
  { route: 'capability', icon: '◎', label: '大语言模型' },
  { route: 'workflow', icon: '⇢', label: '工作流' },
  { route: 'workbench', icon: '⬡', label: '协同工作台' },
  { route: 'activity', icon: '⌖', label: '活动历史' },
  { route: 'settings', icon: '⚙', label: '设置' },
]

const currentRoute = computed(() => (route.name as string) || 'capability')

function navigateTo(name: string) {
  if (currentRoute.value !== name) router.push({ name })
}

// ── State ──
const sidebarCollapsed = ref(false)

// Domain library — shared across all components via useActiveLibrary.
const { activeLibraryId } = useActiveLibrary()
const domainLibs = ref<{ id: string; name: string; icon: string }[]>([])
const currentDomainIcon = computed(() => domainLibs.value.find(l => l.id === activeLibraryId.value)?.icon || '📚')
const currentDomainName = computed(() => domainLibs.value.find(l => l.id === activeLibraryId.value)?.name || '核心领域')

async function loadDomainLibs() {
  try { domainLibs.value = (await memoryApi.libraryList()) || [] }
  catch (_) { domainLibs.value = [] }
  // Validate restored domain ID — if it no longer exists, fall back to first.
  if (activeLibraryId.value && !domainLibs.value.some(l => l.id === activeLibraryId.value)) {
    activeLibraryId.value = ''
  }
  if (domainLibs.value.length && !activeLibraryId.value) {
    activeLibraryId.value = domainLibs.value[0].id
  }
}

// Watch domain switch — bump usage and notify.
watch(activeLibraryId, (id) => {
  if (id) memoryApi.libraryBumpUse(id)
})

const sysInfo = ref<any>(null)
const dynInfo = ref<any>(null)
let dynTimer: ReturnType<typeof setInterval> | null = null
const backends = ref<any[]>([])
const toasts = ref<Array<{ id: number; type: string; title: string; desc: string }>>([])
const downloadStore = useDownloadStore()
	const downloadCount = computed(() => downloadStore.downloadCount)
const showChatPanel = ref(false)
const showStatus = ref(false)
const statusPanelStyle = ref<Record<string, string> | null>(null)
let statusCloseTimer: ReturnType<typeof setTimeout> | null = null

const confirmModal = ref<{ show: (t: string, m: string, o: string) => Promise<boolean> } | null>(null)
const statusRef = ref<HTMLElement | null>(null)

const backendsOk = computed(() => backends.value.length > 0 && backends.value.every(b => b.ok))

// ── Polling ──
function startDynPolling() { if (dynTimer) return; dynTick(); dynTimer = setInterval(() => dynTick(), 2000) }
function stopDynPolling() { if (dynTimer) { clearInterval(dynTimer); dynTimer = null } }
async function dynTick() { try { dynInfo.value = await systemApi.getDynamicInfo() } catch (_) {} }

// ── Toast ──
function showToast(type: string, title: string, desc = '') {
  const id = Date.now() + Math.random()
  toasts.value.push({ id, type, title, desc })
  setTimeout(() => removeToast(id), 4000)
}
function removeToast(id: number) { toasts.value = toasts.value.filter(t => t.id !== id) }
function showConfirm(title: string, msg: string, okLabel = '确定') { return confirmModal.value!.show(title, msg, okLabel) }

// ── Backends ──
async function refreshBackends() { try { backends.value = await systemApi.getBackends() } catch (_) {} }

// ── Panel ──
function goToBackends() { router.push({ name: 'settings' }); nextTick(() => { const el = document.getElementById('backends-section'); if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' }) }) }
function openStatusPanel() { if (statusCloseTimer) { clearTimeout(statusCloseTimer); statusCloseTimer = null }; const r = statusRef.value?.getBoundingClientRect(); if (r) { statusPanelStyle.value = { left: (r.right + 8) + 'px', bottom: (window.innerHeight - r.top + 4) + 'px' } }; showStatus.value = true }
function startStatusClose() { statusCloseTimer = setTimeout(() => { showStatus.value = false }, 250) }
function cancelStatusClose() { if (statusCloseTimer) { clearTimeout(statusCloseTimer); statusCloseTimer = null } }
// ── Lifecycle ──
onMounted(() => {
  loadDomainLibs()
  // Eager-load chat sessions so conversation history survives restart
  // regardless of whether the chat panel is visible.
  try { useChatStore().loadSessions() } catch (_) {}
  refreshBackends()
  systemApi.getSysInfo().then((info: any) => { sysInfo.value = info }).catch(() => {})
  startDynPolling()
  // Download store: single event listener for all components.
  downloadStore.setToast(showToast)
  downloadStore.bindEvents()
  downloadStore.startPolling()
  // Async task store: background task event listener.
  useAsyncStore().init()
})
onBeforeUnmount(() => {
  stopDynPolling()
  downloadStore.stopPolling()
})

defineExpose({ showToast, showConfirm })
</script>

<style scoped>
.shell { display: flex; width: 100%; height: 100vh; background: transparent; }
.sidebar {
  width: 180px; flex-shrink: 0;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-subtle);
  display: flex; flex-direction: column; justify-content: space-between;
  padding: 16px 10px 12px;
  transition: width 0.22s cubic-bezier(0.22, 0.61, 0.36, 1);
}
.sidebar.collapsed { width: 52px; padding: 16px 6px 12px; }
.sidebar-top { display: flex; flex-direction: column; gap: 14px; }
.brand { display: flex; align-items: center; gap: 9px; padding: 2px 6px 0; cursor: pointer; user-select: none; position: relative; }
.sidebar.collapsed .brand { justify-content: center; padding: 2px 0 0; }
.brand-icon { font-size: 17px; line-height: 1; opacity: 0.9; flex-shrink: 0; color: var(--accent); }
.brand-name { font-size: 14px; font-weight: 600; letter-spacing: 0.012em; white-space: nowrap; flex: 1; display: inline-flex; align-items: baseline; gap: 4px; }
.brand-version { font-size: 9px; font-weight: 500; color: var(--text-tertiary); letter-spacing: 0.03em; line-height: 1; position: relative; top: -1px; }
.brand-collapse { font-size: 15px; color: var(--text-secondary); line-height: 1; transition: color var(--transition); }
.brand-collapse:hover { color: var(--text-primary); }
.toggle-btn { width: 20px; height: 20px; display: inline-flex; align-items: center; justify-content: center; padding: 0; border: none; border-radius: 5px; background: transparent; color: var(--text-tertiary); font-size: 10px; cursor: pointer; transition: all var(--transition); line-height: 1; flex-shrink: 0; }
.toggle-btn:hover { color: var(--text-secondary); background: var(--bg-hover); }
.nav { display: flex; flex-direction: column; gap: 3px; }
.nav-item {
  position: relative; display: flex; align-items: center; gap: 10px;
  width: 100%; padding: 7px 10px; border: none; border-radius: var(--radius-sm);
  background: transparent; color: var(--text-secondary);
  font-family: var(--font); font-size: 13px; font-weight: 450;
  cursor: pointer; transition: background var(--transition), color var(--transition);
  text-align: left; white-space: nowrap; overflow: hidden;
}
.nav-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.nav-item.active { background: var(--bg-active); color: var(--accent); font-weight: 540; }
.nav-item.active::before { content: ''; position: absolute; left: -10px; top: 50%; transform: translateY(-50%); width: 3px; height: 16px; border-radius: 0 2px 2px 0; background: var(--accent); opacity: 0.9; }
.nav-icon { font-size: 15px; width: 18px; text-align: center; opacity: 0.7; flex-shrink: 0; line-height: 1; }
.nav-item:hover .nav-icon { opacity: 0.9; }
.nav-item.active .nav-icon { opacity: 1; }
.nav-label { flex: 1; letter-spacing: 0.01em; }
.nav-badge { margin-left: auto; flex-shrink: 0; }
.sidebar.collapsed .nav-label { display: none; }
.sidebar.collapsed .nav-badge { display: none; }
.sidebar.collapsed .nav-item { justify-content: center; padding: 8px 0; gap: 0; }
.sidebar.collapsed .nav-item.active::before { left: -6px; height: 18px; }
/* Domain Switcher */
.domain-switcher { padding: 0 4px; margin-bottom: 2px; }
.domain-select { width: 100%; padding: 5px 24px 5px 8px !important; font-size: 11px !important; border-radius: 5px !important; }
.domain-indicator { display: none; }
.domain-collapsed { display: flex; justify-content: center; padding: 6px 0; cursor: pointer; }
.domain-collapsed-icon { font-size: 15px; opacity: 0.8; transition: opacity var(--transition); }
.domain-collapsed-icon:hover { opacity: 1; }

.nav-badge { font-size: 11px; font-weight: 600; color: var(--accent); background: var(--bg-active); padding: 1px 7px; border-radius: 10px; line-height: 1.4; flex-shrink: 0; font-variant-numeric: tabular-nums; }
.nav-sep { height: 1px; background: var(--border-subtle); margin: 6px 8px 4px; }
.sidebar.collapsed .nav-sep { margin: 4px 6px 2px; }
.sidebar-foot { padding-top: 10px; border-top: 1px solid var(--border-subtle); overflow: visible; }
.foot-row { display: flex; align-items: center; justify-content: space-between; padding: 6px 10px; margin: 0; gap: 6px; }
.status-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; transition: box-shadow 0.3s ease, transform 0.2s ease; }
.dot-live { background: var(--success); box-shadow: 0 0 5px rgba(48,209,88,0.55); }
.dot-dead { background: var(--danger); box-shadow: 0 0 4px rgba(255,69,58,0.45); }
.status-bar { display: flex; align-items: center; gap: 7px; padding: 4px 8px; border-radius: var(--radius-sm); cursor: pointer; position: relative; transition: background var(--transition); }
.status-bar:hover { background: var(--bg-hover); }
.status-bar:hover .status-dot { transform: scale(1.15); }
.status-label { font-size: 11px; font-weight: 520; color: var(--text-tertiary); letter-spacing: 0.02em; transition: color var(--transition); }
.status-bar:hover .status-label { color: var(--text-secondary); }
.status-dropdown { position: fixed; min-width: 200px; padding: 14px; z-index: 3000; box-shadow: 0 8px 32px rgba(0,0,0,0.6); display: flex; flex-direction: column; gap: 5px; }
.sd-head { font-size: 10px; font-weight: 600; color: var(--text-tertiary); text-transform: uppercase; letter-spacing: 0.05em; padding-bottom: 8px; margin-bottom: 4px; border-bottom: 1px solid var(--border-subtle); }
.sd-row { display: flex; align-items: center; gap: 10px; font-size: 12px; padding: 3px 0; }
.sd-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.sd-name { color: var(--text-secondary); flex: 1; font-weight: 450; }
.sd-state { font-size: 11px; color: var(--text-tertiary); font-weight: 460; }
.status-collapsed { display: flex; justify-content: center; padding: 8px 0; cursor: pointer; }
.status-collapsed .status-dot { width: 8px; height: 8px; }
.main {
  flex: 1; min-width: 0; overflow-y: auto; scrollbar-gutter: stable;
  background: radial-gradient(ellipse 80% 60% at 80% 20%, rgba(0,122,255,0.03) 0%, transparent 60%),
    radial-gradient(ellipse 40% 50% at 20% 80%, rgba(120,120,255,0.02) 0%, transparent 50%), rgba(18,18,20,0.55);
}
.main.chat-open { display: flex; flex-direction: row; overflow: hidden; }
.main.chat-open .main-inner { flex: 1; min-width: 0; overflow-y: auto; }
.main-inner { padding: clamp(16px, 3vw, 32px) clamp(12px, 2.5vw, 40px); min-height: 100%; display: flex; flex-direction: column; flex: 1; min-width: 0; }
.chat-pane { width: clamp(320px, 32vw, 440px); flex-shrink: 0; display: flex; flex-direction: column; border-left: 1px solid var(--border-subtle); background: var(--bg-sidebar); }
.chat-pane-head { display: flex; align-items: center; justify-content: space-between; padding: 8px 14px; flex-shrink: 0; border-bottom: 1px solid var(--border-subtle); }
.chat-pane-head-actions { display: flex; align-items: center; gap: 4px; }
.chat-pane-title { font-size: 12px; font-weight: 600; color: var(--text-secondary); letter-spacing: 0.02em; }
.chat-pane-clear-btn { padding: 2px 8px; font-size: 11px; font-weight: 450; border: 1px solid var(--border-subtle); border-radius: 4px; background: transparent; color: var(--text-tertiary); cursor: pointer; transition: all var(--transition); }
.chat-pane-clear-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
.chat-pane-close { width: 24px; height: 24px; border: none; border-radius: 5px; background: transparent; color: var(--text-tertiary); font-size: 13px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all var(--transition); }
.chat-pane-close:hover { background: var(--danger-dim); color: var(--danger); }
.chat-pane-body { flex: 1; min-height: 0; display: flex; flex-direction: column; }
</style>

<style>
select {
  padding: 6px 28px 6px 10px !important;
  border: 1px solid rgba(255,255,255,0.18) !important;
  border-radius: 6px !important; background: #1e1e1e !important;
  color: rgba(255,255,255,0.9) !important;
  font-size: 12px; font-family: inherit; outline: none; cursor: pointer;
  appearance: none; -webkit-appearance: none; -moz-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M0 0l5 6 5-6z' fill='%23888'/%3E%3C/svg%3E") !important;
  background-repeat: no-repeat !important;
  background-position: right 10px center !important;
  background-size: 10px 6px !important;
}
select:focus { border-color: var(--accent, #007aff) !important; }
select option { background: #1e1e1e !important; color: rgba(255,255,255,0.9) !important; }
select:hover { border-color: rgba(255,255,255,0.3) !important; }
</style>
