<template>
  <div class="kb-section">
    <div class="kb-browse-head">
      <span class="kb-browse-count">共 {{ docs.length }} 条</span>
      <button class="btn btn-sm btn-danger" @click="emit('clear')" :disabled="busy || !docs.length"
        title="清空知识库全部文档">清空全部</button>
    </div>
    <div v-if="!docs.length" class="kb-no-results">暂无文档</div>
    <div v-else class="kb-browse-list">
      <div v-for="doc in docs" :key="doc.id" class="kb-browse-item">
        <div class="kb-browse-meta">
          <span v-if="doc.metadata && objLen(doc.metadata)" class="kb-browse-tags">
            <span v-for="(v, k) in doc.metadata" :key="k" class="tag tag-muted">{{ k }}: {{ v }}</span>
          </span>
          <span class="kb-browse-date">{{ fmtDate(doc.addedAt) }}</span>
        </div>
        <div class="kb-browse-text">{{ doc.preview }}</div>
        <button class="btn btn-sm kb-browse-del" @click="emit('deleteDoc', doc.id)" :disabled="busy" title="删除此条">✕</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ docs: any[]; busy: boolean }>()
const emit = defineEmits<{ clear: []; deleteDoc: [docId: string] }>()

function objLen(o: any): number { return o ? Object.keys(o).length : 0 }

function fmtDate(s: string): string {
  if (!s) return ''
  try { return new Date(s).toLocaleDateString() } catch (_) { return s }
}
</script>
