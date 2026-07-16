<template>
  <div v-if="visible" class="overlay" @click.self="visible = false">
    <div class="glass-panel dialog market-dialog">
      <div class="market-dialog-head">
        <h3>Skill 市场</h3>
        <div class="market-dialog-head-actions">
          <button class="btn btn-xs" @click="refreshMarket" :disabled="refreshingMarket">
            {{ refreshingMarket ? '刷新中…' : '⟳ 刷新' }}
          </button>
          <button class="market-dialog-close" @click="visible = false">✕</button>
        </div>
      </div>
      <div class="market-list">
        <div v-if="!marketSkills.length" class="market-empty">
          <span>暂无可用的 Skill</span>
          <span class="hint">点击「⟳ 刷新」从在线市场获取最新 Skill 列表</span>
        </div>
        <div v-for="pkg in marketSkills" :key="pkg.name" class="market-item">
          <div class="market-item-left">
            <span class="market-icon">{{ pkg.icon || '⊞' }}</span>
            <div class="market-info">
              <div class="market-name">{{ pkg.title }}</div>
              <div class="market-desc">{{ pkg.description }}</div>
              <div class="market-meta">
                <span class="market-cat">{{ pkg.category }}</span>
                <span v-if="pkg.mcpServers && pkg.mcpServers.length" class="market-deps">
                  依赖: {{ pkg.mcpServers.map((d: any) => d.name).join(', ') }}
                </span>
              </div>
            </div>
          </div>
          <div class="market-item-right">
            <button
              v-if="!pkg.installed"
              class="btn btn-sm btn-primary"
              @click="installMarketSkill(pkg)"
              :disabled="installing === pkg.name"
            >
              {{ installing === pkg.name ? '安装中…' : '安装' }}
            </button>
            <button v-else class="btn btn-sm btn-del" @click="uninstallMarketSkill(pkg)">卸载</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useToast } from '../../composables/useToast'
import { skillsApi } from '../../api/skills'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'installed'): void
}>()

const toast = useToast()

const marketSkills = ref<any[]>([])
const installing = ref<string | null>(null)
const refreshingMarket = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

async function loadMarket() {
  try { marketSkills.value = await skillsApi.listMarket() || [] } catch (_) {}
}

watch(() => props.modelValue, async (v: boolean) => {
  if (v) await loadMarket()
})

defineExpose({ refresh: loadMarket })

async function installMarketSkill(pkg: any) {
  installing.value = pkg.name
  try {
    const result = await skillsApi.installMarket(pkg)
    if (result.mcpServersAdded && result.mcpServersAdded.length) {
      t('success', '已安装 ' + pkg.title, '自动添加 MCP Server: ' + result.mcpServersAdded.join(', '))
    } else {
      t('success', '已安装 ' + pkg.title)
    }
    visible.value = false
    emit('installed')
  } catch (e: any) { t('error', '安装失败', e.message || e) }
  installing.value = null
}

async function refreshMarket() {
  refreshingMarket.value = true
  try {
    marketSkills.value = await skillsApi.refreshMarket() || []
    t('success', '市场已刷新', marketSkills.value.length + ' 个 Skill')
  } catch (e: any) {
    t('error', '刷新失败', e.message || e)
    try { marketSkills.value = await skillsApi.listMarket() || [] } catch (_) {}
  }
  refreshingMarket.value = false
}

async function uninstallMarketSkill(pkg: any) {
  if (!await toast.confirm('卸载 Skill', '确定卸载「' + pkg.title + '」？')) return
  try {
    await skillsApi.uninstallMarket(pkg.name)
    emit('installed')
    t('success', '已卸载 ' + pkg.title)
  } catch (e: any) { t('error', '卸载失败', e.message || e) }
}
</script>

<style scoped>
.overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center;
  z-index: 100;
}

/* ── Market dialog ── */
.market-dialog {
  width: 600px; max-width: 90vw; max-height: 75vh; overflow-y: auto;
  padding: 24px 28px;
}
.market-dialog-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 20px;
}
.market-dialog-head h3 {
  font-size: 16px; font-weight: 600; margin: 0;
}
.market-dialog-head-actions {
  display: flex; align-items: center; gap: 8px;
}
.market-dialog-close {
  width: 28px; height: 28px; border: none; border-radius: 6px;
  background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}
.market-dialog-close:hover {
  background: var(--bg-hover); color: var(--text-primary);
}

.market-list {
  display: flex; flex-direction: column; gap: 8px;
}
.market-empty {
  text-align: center; padding: 32px 0; color: var(--text-tertiary);
  font-size: 13px; display: flex; flex-direction: column; gap: 6px;
}
.market-empty .hint {
  font-size: 11px; opacity: 0.7;
}
.market-item {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; padding: 14px 16px;
  border: 1px solid var(--border-soft); border-radius: var(--radius);
  background: var(--bg-elevated);
}
.market-item-left {
  display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0;
}
.market-icon {
  font-size: 24px; flex-shrink: 0;
}
.market-info {
  display: flex; flex-direction: column; gap: 3px; min-width: 0;
}
.market-name {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
}
.market-desc {
  font-size: 11px; color: var(--text-secondary); line-height: 1.4;
}
.market-meta {
  display: flex; gap: 10px; align-items: center;
}
.market-cat {
  font-size: 10px; color: var(--text-tertiary); font-family: var(--font-mono);
}
.market-deps {
  font-size: 10px; color: var(--accent);
}
.market-item-right {
  flex-shrink: 0;
}

/* ── Shared button styles (self-contained so the component works standalone) ── */
.btn {
  padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer;
}
.btn:hover { background: var(--bg-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
.btn-sm { padding: 3px 10px; font-size: 11px; }
.btn-xs {
  padding: 2px 7px; font-size: 11px; line-height: 1.3;
  border: 1px solid var(--border-soft); border-radius: 4px;
  background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer;
}
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-primary {
  background: var(--accent); border-color: var(--accent); color: #fff;
}
.btn-primary:hover { background: var(--accent-hover); }
.btn-del {
  color: var(--danger); border-color: rgba(255,69,58,0.3);
}
.btn-del:hover { background: rgba(255,69,58,0.1); }
</style>
