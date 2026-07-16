<template>
  <div class="async-panel" v-if="store.tasks.length">
    <!-- header -->
    <div class="async-head">
      <span class="async-title">后台任务</span>
      <span class="async-badge" v-if="store.activeCount">{{ store.activeCount }} 进行中</span>
      <button class="btn btn-xs" @click="store.fetchAll()" title="刷新">↻</button>
    </div>

    <!-- running -->
    <div class="async-section" v-if="store.running.length">
      <div class="async-section-title">进行中</div>
      <div v-for="t in store.running" :key="t.id" class="async-card async-card-running">
        <span class="async-card-spin">◐</span>
        <div class="async-card-body">
          <div class="async-card-name">{{ t.title }}</div>
          <div class="async-card-meta">{{ fmtTool(t.toolName) }} · {{ ago(t.createdAt) }}</div>
        </div>
        <button class="btn btn-xs btn-danger async-card-btn" @click="store.cancelTask(t.id)">取消</button>
      </div>
    </div>

    <!-- completed -->
    <div class="async-section" v-if="store.completed.length">
      <div class="async-section-title">已完成</div>
      <div v-for="t in store.completed" :key="t.id"
           class="async-card async-card-done"
           @click="emit('resume', t)">
        <span class="async-card-icon">✓</span>
        <div class="async-card-body">
          <div class="async-card-name">{{ t.title }}</div>
          <div class="async-card-meta">{{ fmtTool(t.toolName) }} · {{ ago(t.completedAt || t.updatedAt) }}</div>
        </div>
        <span class="async-card-hint">点击恢复 →</span>
      </div>
    </div>

    <!-- failed -->
    <div class="async-section" v-if="store.failed.length">
      <div class="async-section-title">失败/取消</div>
      <div v-for="t in store.failed" :key="t.id" class="async-card async-card-fail">
        <span class="async-card-icon">✗</span>
        <div class="async-card-body">
          <div class="async-card-name">{{ t.title }}</div>
          <div class="async-card-meta">{{ t.error || fmtTool(t.toolName) }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAsyncStore, type AsyncTask } from '../stores/asyncStore'

const store = useAsyncStore()
const emit = defineEmits<{ (e: 'resume', task: AsyncTask): void }>()

function fmtTool(name: string) {
  const m: Record<string, string> = {
    web_fetch: '抓取网页', web_search: '搜索', shell_exec: '执行命令',
    ingest_deep: '深度导入', ingest_deep_analyze: '分析文件',
    ingest_deep_commit: '建知识库', kb_add_texts: '添加文档',
    workflow_execute: '运行工作流', agent_run: '委派Agent',
  }
  return m[name] || name
}

function ago(ts: number) {
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 60) return s + 's前'
  if (s < 3600) return Math.floor(s / 60) + 'min前'
  return Math.floor(s / 3600) + 'h前'
}
</script>

<style scoped>
.async-panel {
  background: rgba(5,5,16,0.92);
  border: 1px solid rgba(120,160,200,0.15);
  border-radius: 8px;
  padding: 12px 14px;
  max-height: 360px;
  overflow-y: auto;
  color: #c8d8e8;
  font-size: 12px;
}
.async-head {
  display: flex; align-items: center; gap: 8px; margin-bottom: 10px;
}
.async-title { font-weight: 600; color: #8ab4d8; }
.async-badge {
  background: rgba(200,160,40,0.2); color: #e0c060; padding: 1px 8px;
  border-radius: 10px; font-size: 11px;
}
.async-section { margin-top: 8px; }
.async-section-title { font-size: 10px; color: #607080; text-transform: uppercase; margin-bottom: 4px; }
.async-card {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 8px; border-radius: 6px; margin-bottom: 4px;
  cursor: default;
}
.async-card-running { background: rgba(200,160,40,0.12); border-left: 3px solid #c0a030; }
.async-card-done { background: rgba(80,200,80,0.08); border-left: 3px solid #50a050; cursor: pointer; }
.async-card-done:hover { background: rgba(80,200,80,0.16); }
.async-card-fail { background: rgba(200,80,80,0.08); border-left: 3px solid #a05050; }
.async-card-spin { font-size: 14px; animation: spin 1s linear infinite; color: #d0b040; }
.async-card-icon { font-size: 14px; color: #50a050; }
.async-card-fail .async-card-icon { color: #c06060; }
.async-card-body { flex: 1; min-width: 0; }
.async-card-name { font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.async-card-meta { font-size: 10px; color: #708090; }
.async-card-hint { font-size: 10px; color: #5090c0; white-space: nowrap; }
.async-card-btn { flex-shrink: 0; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
