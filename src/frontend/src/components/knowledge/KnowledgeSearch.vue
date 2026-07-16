<template>
  <div class="kb-section">
    <div class="kb-search-row">
      <input v-model="query" type="text" class="kb-search-input"
        placeholder="输入查询…" @keyup.enter="doSearch" />
      <button class="btn btn-primary" @click="doSearch" :disabled="busy || !query.trim()">
        {{ busy ? '搜索中…' : '搜索' }}
      </button>
    </div>
    <div class="kb-filter-section">
      <span class="kb-filter-label">元数据过滤</span>
      <div class="kb-filter-list">
        <div v-for="(f, i) in filters" :key="i" class="kb-filter-row">
          <input v-model="f.key" type="text" class="kb-filter-input" placeholder="key" />
          <span class="kb-filter-sep">:</span>
          <input v-model="f.val" type="text" class="kb-filter-input" placeholder="value" />
          <button class="btn btn-sm kb-filter-remove" @click="filters.splice(i, 1)" title="移除">✕</button>
        </div>
      </div>
      <button class="btn btn-sm" @click="filters.push({key:'',val:''})" :disabled="filters.length >= 4">+ 添加条件</button>
    </div>
    <div v-if="results.length" class="kb-results">
      <div v-for="(r, i) in results" :key="r.id" class="kb-result-item">
        <div class="kb-result-head">
          <span class="kb-result-rank">#{{ i + 1 }}</span>
          <span class="kb-result-score">{{ (r.similarity * 100).toFixed(1) }}%</span>
          <span v-if="r.metadata && objLen(r.metadata)" class="kb-result-meta">
            <span v-for="(v, k) in r.metadata" :key="k" class="tag tag-muted">{{ k }}: {{ v }}</span>
          </span>
        </div>
        <div class="kb-result-text">{{ r.content }}</div>
      </div>
    </div>
    <div v-if="searched && !results.length" class="kb-no-results">未找到相关内容</div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useToast } from '../../composables/useToast'
import { knowledgeApi } from '../../api/knowledge'

const props = defineProps<{ kbId: string; busy: boolean }>()
const emit = defineEmits<{ done: [] }>()

const toast = useToast()
const query = ref('')
const filters = ref<Array<{ key: string; val: string }>>([])
const results = ref<any[]>([])
const searched = ref(false)

function objLen(o: any): number { return o ? Object.keys(o).length : 0 }

async function doSearch() {
  if (!query.value.trim()) return
  emit('done')
  results.value = []
  searched.value = false
  const filter: Record<string, string> = {}
  for (const f of filters.value) {
    if (f.key.trim()) filter[f.key.trim()] = f.val.trim()
  }
  try {
    results.value = (await knowledgeApi.search(props.kbId, query.value, 5, Object.keys(filter).length ? filter : null)) || []
    searched.value = true
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    toast.show('error', '搜索失败', msg)
  }
}
</script>
