<template>
  <div class="memory-panel">
    <h2>🧠 记忆管理</h2>
    <div v-if="err" class="mp-err">{{ err }}</div>
    <div v-else-if="!ready">加载中…</div>
    <div v-else>

      <!-- ═══════ 核心记忆 ═══════ -->
      <div class="glass-panel mp-section">
        <div class="mem-head">
          <span class="mem-icon">⭐</span>
          <span class="mem-title">核心记忆</span>
          <span class="tag tag-accent">{{ coreFacts.length }} 条</span>
          <span class="tag tag-dim">全局 · 身份与偏好</span>
          <button class="btn btn-sm btn-primary" style="margin-left:auto"
                  @click="showAddFact = !showAddFact">{{ showAddFact ? '取消' : '+ 添加' }}</button>
        </div>
        <div v-if="showAddFact" class="mp-add-row">
          <input v-model="newFactValue" type="text" placeholder="记忆内容（如：我喜欢用中文回复）"
                 class="field mp-input" @keyup.enter="doAddFact" />
          <button class="btn btn-sm btn-primary" @click="doAddFact" :disabled="!newFactValue.trim() || busy">保存</button>
        </div>
        <div v-if="coreFacts.length" class="mp-list">
          <div v-for="f in coreFacts" :key="f.id" class="mp-item">
            <span class="mp-item-text">{{ f.value }}</span>
            <span class="mp-item-cat">{{ f.category || 'general' }}</span>
            <button class="btn btn-xs btn-danger mp-del" @click="doDeleteFact(f.id)" title="删除">×</button>
          </div>
        </div>
        <div v-else class="mem-hint">暂无 — AI 会在对话中自动存入重要信息</div>
      </div>

      <!-- ═══════ 对话记忆（turns） ═══════ -->
      <div class="glass-panel mp-section">
        <div class="mem-head">
          <span class="mem-icon">💬</span>
          <span class="mem-title">对话记忆</span>
          <span class="tag tag-accent">{{ turns.length }} 条</span>
          <span class="tag tag-dim">{{ turnSessions.length }} 个会话</span>
          <div style="margin-left:auto;display:flex;gap:4px;">
            <select v-model="turnFilter" class="mp-filter" @change="applyTurnFilter">
              <option value="">全部会话</option>
              <option v-for="s in turnSessions" :key="s" :value="s">{{ s }}</option>
            </select>
            <button class="btn btn-xs" @click="showAllTurns = !showAllTurns">
              {{ showAllTurns ? '收起' : '展开全部 (' + turns.length + ')' }}
            </button>
          </div>
        </div>
        <div v-if="filteredTurns.length" class="mp-list">
          <div v-for="t in filteredTurns.slice(0, showAllTurns ? 999 : 10)" :key="t.id" class="mp-item mp-turn-item">
            <div class="mp-turn-q">Q: {{ t.content.slice(0, 200) }}{{ t.content.length > 200 ? '…' : '' }}</div>
            <div class="mp-turn-a">A: {{ (t.reply || '').slice(0, 300) }}{{ (t.reply || '').length > 300 ? '…' : '' }}</div>
            <div class="mp-turn-meta">
              <span class="mp-item-cat">{{ fmtTime(t.createdAt) }}</span>
              <span class="mp-item-cat">{{ t.sessionId?.slice(0, 8) || '-' }}</span>
              <button class="btn btn-xs btn-danger mp-del" @click="doDeleteItem(t.id)" title="删除">×</button>
            </div>
          </div>
        </div>
        <div v-else class="mem-hint">暂无对话记忆</div>
      </div>

      <!-- ═══════ 提取的事实 ═══════ -->
      <div class="glass-panel mp-section">
        <div class="mem-head">
          <span class="mem-icon">📋</span>
          <span class="mem-title">提取的事实</span>
          <span class="tag tag-accent">{{ facts.length }} 条</span>
          <span class="tag tag-dim">从对话中自动提取</span>
        </div>
        <div v-if="facts.length" class="mp-list">
          <div v-for="f in facts.slice(0, showAllFacts ? 999 : 10)" :key="f.id" class="mp-item">
            <span class="mp-item-tag">[{{ f.category || 'general' }}]</span>
            <span class="mp-item-text">{{ f.content }}</span>
            <span class="mp-item-cat">{{ fmtTime(f.createdAt) }}</span>
            <button class="btn btn-xs btn-danger mp-del" @click="doDeleteItem(f.id)" title="删除">×</button>
          </div>
          <button v-if="facts.length > 10" class="btn btn-xs" style="margin-top:4px"
                  @click="showAllFacts = !showAllFacts">
            {{ showAllFacts ? '收起' : '展开全部 (' + facts.length + ')' }}
          </button>
        </div>
        <div v-else class="mem-hint">暂无 — AI 会在对话中自动提取关键事实</div>
      </div>

      <!-- ═══════ 经验教训 ═══════ -->
      <div class="glass-panel mp-section">
        <div class="mem-head">
          <span class="mem-icon">📖</span>
          <span class="mem-title">经验教训</span>
          <span class="tag tag-accent">{{ experiences.length }} 条</span>
          <span class="tag tag-dim">全局 · 跨领域共享</span>
        </div>
        <div v-if="experiences.length" class="mp-list">
          <div v-for="e in experiences" :key="e.id" class="mp-item">
            <span class="mp-item-kind">[{{ e.kind }}]</span>
            <span class="mp-item-text">{{ e.content }}</span>
            <span class="mp-item-conf" :title="'置信度'">{{ (e.confidence * 100).toFixed(0) }}%</span>
            <button class="btn btn-xs btn-danger mp-del" @click="doDeleteExp(e.id)" title="删除">×</button>
          </div>
        </div>
        <div v-else class="mem-hint">暂无 — AI 会在反思时自动生成经验教训</div>
      </div>

      <!-- ═══════ 记忆策略 ═══ -->
      <div class="glass-panel mp-section">
        <div class="mem-head">
          <span class="mem-icon">⏳</span>
          <span class="mem-title">记忆策略</span>
          <span class="tag tag-accent">{{ policy.tier }}</span>
        </div>
        <div class="mp-policy-grid">
          <div class="mp-policy-item">
            <span class="mp-policy-label">半衰期</span>
            <span class="mp-policy-value">{{ policy.halfLifeDays }} 天</span>
            <span class="mp-policy-hint">超过半衰期的记忆权重减半</span>
          </div>
          <div class="mp-policy-item">
            <span class="mp-policy-label">生存周期</span>
            <span class="mp-policy-value">{{ policy.ttlDays }} 天</span>
            <span class="mp-policy-hint">超过 TTL 自动清理</span>
          </div>
          <div class="mp-policy-item">
            <span class="mp-policy-label">召回数量</span>
            <span class="mp-policy-value">Top {{ policy.recallK }}</span>
            <span class="mp-policy-hint">每次对话注入的记忆数上限</span>
          </div>
          <div class="mp-policy-item">
            <span class="mp-policy-label">衰减系数 α</span>
            <span class="mp-policy-value">{{ policy.alpha }}</span>
            <span class="mp-policy-hint">0=纯时间权重, 1=纯重要性权重</span>
          </div>
          <div class="mp-policy-item">
            <span class="mp-policy-label">条目上限</span>
            <span class="mp-policy-value">{{ policy.itemCap }}</span>
            <span class="mp-policy-hint">对话记忆最大条目数</span>
          </div>
          <div class="mp-policy-item">
            <span class="mp-policy-label">核心上限</span>
            <span class="mp-policy-value">{{ policy.coreCap }}</span>
            <span class="mp-policy-hint">核心记忆最大条目数</span>
          </div>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { memoryApi } from '../api/memory'
import type { MemoryItem } from '../api/memory'

interface UserFact { id: string; key: string; value: string; category: string; importance: string; locked: boolean }
interface ExperienceItem { id: string; kind: string; content: string; context: string; confidence: number }
interface MemoryPolicy { tier: string; halfLifeDays: number; ttlDays: number; recallK: number; itemCap: number; coreCap: number; alpha: number }

const coreFacts = ref<UserFact[]>([])
const experiences = ref<ExperienceItem[]>([])
const turns = ref<MemoryItem[]>([])
const facts = ref<MemoryItem[]>([])
const policy = ref<MemoryPolicy>({ tier: 'standard', halfLifeDays: 14, ttlDays: 90, recallK: 3, itemCap: 2000, coreCap: 200, alpha: 0.7 })
const showAddFact = ref(false)
const showAllTurns = ref(false)
const showAllFacts = ref(false)
const newFactValue = ref('')
const turnFilter = ref('')
const err = ref('')
const ready = ref(false)
const busy = ref(false)

const turnSessions = computed(() => {
  const ids = new Set(turns.value.map(t => t.sessionId).filter(Boolean))
  return [...ids].sort().reverse()
})
const filteredTurns = computed(() => {
  if (!turnFilter.value) return turns.value
  return turns.value.filter(t => t.sessionId === turnFilter.value)
})

function applyTurnFilter() { showAllTurns.value = true }
function fmtTime(ts: number): string {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

onMounted(async () => {
  try { await loadAll(); ready.value = true }
  catch (e: any) { err.value = e?.message || String(e); ready.value = true }
})

async function loadAll() {
  try { coreFacts.value = await memoryApi.coreList() } catch (_) { coreFacts.value = [] }
  try { experiences.value = await memoryApi.recallExperience('', 50) || [] } catch (_) { experiences.value = [] }
  try { policy.value = await memoryApi.policy() } catch (_) { /* keep default */ }
  // Memory items: load both turns and facts, up to 500 recent entries
  try {
    const items = await memoryApi.list(500, '') || []
    turns.value = items.filter(i => i.kind === 'turn')
    facts.value = items.filter(i => i.kind === 'fact')
  } catch (_) { turns.value = []; facts.value = [] }
}

async function doAddFact() {
  const v = newFactValue.value.trim()
  if (!v || busy.value) return
  busy.value = true
  try { await memoryApi.coreAdd('', v, 'global'); newFactValue.value = ''; showAddFact.value = false; await loadAll() }
  catch (e: any) { err.value = e?.message || String(e) }
  finally { busy.value = false }
}
async function doDeleteFact(id: string) {
  busy.value = true
  try { await memoryApi.coreDelete(id); await loadAll() } catch (_) {}
  finally { busy.value = false }
}
async function doDeleteItem(id: string) {
  busy.value = true
  try { await memoryApi.itemDelete(id); await loadAll() } catch (_) {}
  finally { busy.value = false }
}
async function doDeleteExp(id: string) {
  busy.value = true
  try { await memoryApi.experienceDelete(id); await loadAll() } catch (_) {}
  finally { busy.value = false }
}
</script>

<style scoped>
.memory-panel { padding: 12px 16px; overflow-y: auto; height: 100%; }
.mp-err { color: #f07070; padding: 10px; }
.mp-section { margin-bottom: 14px; }
.mp-add-row { display: flex; gap: 6px; padding: 8px 0; }
.mp-input { flex: 1; font-size: 0.85em; }
.mp-filter { background: #121215; border: 1px solid #333; border-radius: 4px; padding: 2px 6px; color: #aaa; font-size: 0.75em; }
.mp-list { display: flex; flex-direction: column; gap: 3px; padding: 4px 0 0 0; }
.mp-item { display: flex; align-items: flex-start; gap: 8px; padding: 4px 8px; border-radius: 4px; background: rgba(255,255,255,0.02); font-size: 0.85em; }
.mp-item:hover { background: rgba(255,255,255,0.05); }
.mp-item-text { flex: 1; color: #ccc; word-break: break-word; }
.mp-item-tag { font-size: 0.72em; color: rgba(160,180,200,0.7); flex-shrink: 0; font-weight: 500; min-width: 50px; }
.mp-item-cat { font-size: 0.72em; color: #666; flex-shrink: 0; }
.mp-item-kind { font-size: 0.75em; color: rgba(120,180,220,0.7); flex-shrink: 0; font-weight: 500; }
.mp-item-conf { font-size: 0.72em; color: #888; flex-shrink: 0; }
.mp-del { opacity: 0; transition: opacity 0.15s; align-self: center; }
.mp-item:hover .mp-del { opacity: 1; }

/* Turn items */
.mp-turn-item { flex-direction: column; gap: 2px; padding: 6px 8px; }
.mp-turn-q { color: #aaa; font-size: 0.82em; word-break: break-word; }
.mp-turn-a { color: #777; font-size: 0.8em; word-break: break-word; padding-left: 12px; border-left: 2px solid rgba(255,255,255,0.06); }
.mp-turn-meta { display: flex; align-items: center; gap: 8px; margin-top: 2px; }

/* Policy grid */
.mp-policy-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 8px; padding: 8px 0; }
.mp-policy-item { padding: 8px 12px; border-radius: 6px; background: rgba(255,255,255,0.03); border: 1px solid rgba(255,255,255,0.06); }
.mp-policy-label { font-size: 0.75em; color: #888; display: block; }
.mp-policy-value { font-size: 1.1em; font-weight: 600; color: #e0e0e0; margin: 2px 0; }
.mp-policy-hint { font-size: 0.7em; color: #555; }

/* Reused utilities */
.glass-panel { background: rgba(255,255,255,0.02); border: 1px solid rgba(255,255,255,0.06); border-radius: 8px; padding: 10px 14px; }
.mem-head { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; flex-wrap: wrap; }
.mem-icon { font-size: 1em; }
.mem-title { font-size: 0.95em; font-weight: 600; color: #ccc; }
.mem-hint { font-size: 0.8em; color: #555; padding: 8px 0; }
.tag { padding: 1px 7px; border-radius: 4px; font-size: 0.72em; }
.tag-accent { background: rgba(120,180,220,0.15); color: rgba(120,200,240,0.8); }
.tag-dim { background: rgba(120,120,120,0.1); color: #666; }
.btn { border: 1px solid #444; background: #1a1a1e; color: #ccc; padding: 4px 12px; border-radius: 5px; cursor: pointer; font-size: 0.82em; transition: all .15s; }
.btn:hover { background: #2a2a30; }
.btn-primary { background: var(--accent, #7aa2f7); border-color: var(--accent, #7aa2f7); color: #111; }
.btn-primary:hover { opacity: 0.85; }
.btn-sm { padding: 4px 10px; font-size: 0.8em; }
.btn-xs { padding: 2px 7px; font-size: 0.72em; }
.btn-danger { border-color: #5a2a2a; color: #f07070; }
.btn-danger:hover { background: #3a1a1a; }
.field { background: #121215; border: 1px solid #333; border-radius: 5px; padding: 6px 10px; color: #e0e0e0; }
</style>
