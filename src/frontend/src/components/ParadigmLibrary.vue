<template>
  <div class="pl-panel">
    <div class="pl-head">
      <span class="pl-icon">🧠</span>
      <span class="pl-title">思维范式库</span>
      <span class="tag tag-accent">{{ enabledList.length }} / {{ paradigms.length }} 个</span>
      <select v-model="catFilter" class="field pl-filter">
        <option value="">全部分类</option>
        <option v-for="c in categories" :key="c" :value="c">{{ catLabel(c) }}</option>
      </select>
      <input v-model="search" class="field pl-search" placeholder="搜索范式…" />
      <button class="btn btn-sm btn-primary" style="margin-left:auto" @click="showAdd = !showAdd">{{ showAdd ? '取消' : '+ 添加' }}</button>
    </div>

    <!-- Add form -->
    <div v-if="showAdd" class="pl-add-panel">
      <input v-model="newName" class="field" placeholder="名称（如 第一性原理）" />
      <select v-model="newCat" class="field" style="width:100px">
        <option v-for="c in categories" :key="c" :value="c">{{ catLabel(c) }}</option>
      </select>
      <input v-model="newIcon" class="field" style="width:60px" placeholder="🔬" maxlength="2" />
      <input v-model="newDesc" class="field" style="flex:1" placeholder="一句话描述" />
      <button class="btn btn-sm btn-primary" @click="doAdd" :disabled="!newName.trim()">保存</button>
    </div>
    <div v-if="showAdd" class="pl-add-panel">
      <textarea v-model="newMethodology" class="field pl-textarea" placeholder="方法论详情（注入 AI system prompt）" rows="4"></textarea>
    </div>

    <!-- Card list -->
    <div class="pl-list">
      <div v-for="p in filtered" :key="p.id" class="pl-card" :class="{ 'pl-disabled': !p.enabled }">
        <div class="pl-card-top">
          <span class="pl-card-icon">{{ p.icon || '🧩' }}</span>
          <span class="pl-card-name">{{ p.name }}</span>
          <span class="tag" :class="catTagClass(p.category)">{{ catLabel(p.category) }}</span>
          <div class="pl-strength-bar" :title="'强度 ' + (p.strength * 100).toFixed(0) + '% · 使用 ' + (p.useCount || 0) + ' 次 · 成功 ' + (p.successCount || 0) + ' 次'">
            <div class="pl-strength-fill" :style="{ width: (p.strength * 100) + '%' }"></div>
          </div>
          <span class="pl-meta">{{ (p.strength * 100).toFixed(0) }}% / {{ p.useCount || 0 }}次</span>
          <button class="btn btn-xs" @click="toggleExpand(p.id)" :title="expanded === p.id ? '收起' : '展开'">{{ expanded === p.id ? '▲' : '▼' }}</button>
          <button class="btn btn-xs" :class="p.enabled ? '' : 'btn-primary'" @click="doToggle(p.id, !p.enabled)">{{ p.enabled ? '禁用' : '启用' }}</button>
          <button class="btn btn-xs btn-danger" @click="doDelete(p.id)">✕</button>
        </div>
        <div v-if="expanded === p.id" class="pl-card-body">
          <div class="pl-desc">{{ p.description }}</div>
          <div class="pl-section">
            <span class="pl-label">适用场景</span>
            <span class="pl-val">{{ p.applicable || '（未填写）' }}</span>
          </div>
          <div class="pl-section">
            <span class="pl-label">示例</span>
            <span class="pl-val">{{ p.example || '（未填写）' }}</span>
          </div>
          <div class="pl-section">
            <span class="pl-label">方法论</span>
            <pre class="pl-methodology">{{ p.methodology || '（未填写）' }}</pre>
          </div>
          <div class="pl-actions">
            <button class="btn btn-sm" @click="doFeedback(p.id, true)">👍 有效</button>
            <button class="btn btn-sm" @click="doFeedback(p.id, false)">👎 无效</button>
          </div>
        </div>
      </div>
      <div v-if="!filtered.length" class="pl-hint">
        {{ paradigms.length ? '无匹配的范式' : '暂无思维范式 — 点击「+ 添加」创建第一个' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { memoryApi } from '../api/memory'

interface Paradigm {
  id: string; name: string; category: string; icon: string
  description: string; methodology: string; applicable: string; example: string
  strength: number; useCount: number; successCount: number
  enabled: boolean; sourceType: string; libraryId: string; createdAt: number
}

const categories = ['analysis', 'decision', 'creative', 'debug', 'planning']
function catLabel(c: string) {
  const m: Record<string, string> = { analysis: '分析', decision: '决策', creative: '创造', debug: '调试', planning: '规划' }
  return m[c] || c
}
function catTagClass(c: string) {
  const m: Record<string, string> = { analysis: 'tag-blue', decision: 'tag-amber', creative: 'tag-purple', debug: 'tag-red', planning: 'tag-green' }
  return m[c] || ''
}

const paradigms = ref<Paradigm[]>([])
const search = ref('')
const catFilter = ref('')
const showAdd = ref(false)
const expanded = ref<string | null>(null)
const newName = ref(''); const newCat = ref('analysis'); const newIcon = ref('🔬')
const newDesc = ref(''); const newMethodology = ref('')

const enabledList = computed(() => paradigms.value.filter(p => p.enabled))
const filtered = computed(() => paradigms.value.filter(p => {
  if (catFilter.value && p.category !== catFilter.value) return false
  if (search.value && !p.name.includes(search.value) && !(p.description || '').includes(search.value)) return false
  return true
}))

function toggleExpand(id: string) { expanded.value = expanded.value === id ? null : id }

async function load() {
  try { paradigms.value = await memoryApi.paradigmList('') || [] } catch (_) { paradigms.value = [] }
}

async function doAdd() {
  if (!newName.value.trim()) return
  await memoryApi.paradigmAdd({
    name: newName.value.trim(), category: newCat.value, icon: newIcon.value,
    description: newDesc.value.trim(), methodology: newMethodology.value.trim(),
    applicable: '', example: '', enabled: true, sourceType: 'manual', libraryId: '',
  })
  newName.value = ''; newDesc.value = ''; newMethodology.value = ''; showAdd.value = false
  await load()
}

async function doToggle(id: string, enabled: boolean) {
  await memoryApi.paradigmToggle(id, enabled)
  await load()
}

async function doDelete(id: string) {
  await memoryApi.paradigmDelete(id)
  await load()
}

async function doFeedback(id: string, success: boolean) {
  await memoryApi.paradigmFeedback(id, success)
  await load()
}

onMounted(load)
</script>

<style scoped>
.pl-panel { padding: 8px 0; }
.pl-head { display: flex; align-items: center; gap: 6px; margin-bottom: 10px; flex-wrap: wrap; }
.pl-icon { font-size: 1.1em; }
.pl-title { font-size: 0.95em; font-weight: 600; color: #ccc; }
.pl-filter { font-size: 0.8em; padding: 3px 8px; width: 90px; }
.pl-search { font-size: 0.8em; padding: 3px 8px; width: 130px; }
.pl-add-panel { display: flex; gap: 6px; padding: 6px 0; align-items: center; }
.pl-textarea { flex: 1; resize: vertical; font-size: 0.82em; min-height: 60px; }
.pl-list { display: flex; flex-direction: column; gap: 6px; }
.pl-card { padding: 8px 10px; border-radius: 6px; background: rgba(255,255,255,0.02); border: 1px solid rgba(255,255,255,0.06); font-size: 0.85em; }
.pl-disabled { opacity: 0.45; }
.pl-card-top { display: flex; align-items: center; gap: 6px; }
.pl-card-icon { font-size: 1.1em; }
.pl-card-name { font-weight: 600; color: #ddd; flex: 1; }
.pl-strength-bar { width: 80px; height: 5px; background: rgba(255,255,255,0.08); border-radius: 3px; overflow: hidden; }
.pl-strength-fill { height: 100%; background: linear-gradient(90deg, #5abf7f, #7ab8f0); border-radius: 3px; transition: width .3s; }
.pl-meta { font-size: 0.72em; color: #666; min-width: 70px; }
.pl-card-body { margin-top: 8px; padding-top: 8px; border-top: 1px solid rgba(255,255,255,0.05); }
.pl-desc { color: #aaa; margin-bottom: 6px; }
.pl-section { display: flex; gap: 6px; margin-bottom: 4px; }
.pl-label { font-size: 0.75em; color: #555; min-width: 60px; }
.pl-val { font-size: 0.82em; color: #999; }
.pl-methodology { font-size: 0.78em; color: #999; background: rgba(0,0,0,0.2); padding: 8px; border-radius: 4px; white-space: pre-wrap; max-height: 200px; overflow-y: auto; margin: 0; }
.pl-actions { display: flex; gap: 6px; margin-top: 8px; }
.pl-hint { text-align: center; color: #555; padding: 20px 0; font-size: 0.85em; }

.tag { padding: 1px 7px; border-radius: 4px; font-size: 0.72em; }
.tag-accent { background: rgba(120,180,220,0.15); color: rgba(120,200,240,0.8); }
.tag-blue { background: rgba(90,130,210,0.15); color: rgba(130,180,240,0.8); }
.tag-amber { background: rgba(210,160,60,0.15); color: rgba(240,190,80,0.8); }
.tag-purple { background: rgba(150,100,210,0.15); color: rgba(180,140,240,0.8); }
.tag-red { background: rgba(210,80,80,0.15); color: rgba(240,110,110,0.8); }
.tag-green { background: rgba(80,180,120,0.15); color: rgba(110,220,160,0.8); }
.field { background: #121215; border: 1px solid #333; border-radius: 5px; padding: 4px 8px; color: #e0e0e0; }
.btn { border: 1px solid #444; background: #1a1a1e; color: #ccc; padding: 4px 12px; border-radius: 5px; cursor: pointer; font-size: 0.82em; transition: all .15s; }
.btn:hover { background: #2a2a30; }
.btn-primary { background: var(--accent, #7aa2f7); border-color: var(--accent, #7aa2f7); color: #111; }
.btn-primary:hover { opacity: 0.85; }
.btn-sm { padding: 4px 10px; font-size: 0.8em; }
.btn-xs { padding: 2px 7px; font-size: 0.72em; }
.btn-danger { border-color: #5a2a2a; color: #f07070; }
.btn-danger:hover { background: #3a1a1a; }
</style>
