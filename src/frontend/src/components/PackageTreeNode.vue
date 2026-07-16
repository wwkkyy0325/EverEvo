<template>
  <div class="ptn-root">
    <div class="ptn-row"
      :style="{ paddingLeft: (level * 28 + 14) + 'px' }"
      :class="{ 'ptn-dir': node.isDir, 'ptn-lvl0': level === 0 }"
      @click="node.isDir && toggleSelf()">
      <button type="button" class="ptn-arrow" v-if="node.isDir"
        @click.stop="toggleSelf()" :class="{ expanded }"
        :title="expanded ? '收起' : '展开'">▸</button>
      <span class="ptn-arrow-spacer" v-else></span>
      <span class="ptn-icon">{{ node.isDir ? (expanded ? '📂' : '📁') : iconFor(node.name) }}</span>
      <span class="ptn-name" :title="node.name">{{ node.name }}</span>
      <span class="ptn-size" v-if="node.isDir">{{ node.childFileCount }} 个文件</span>
      <span class="ptn-size" v-else>{{ fmtSize(node.size) }}</span>
      <span class="ptn-actions" v-if="!node.isDir">
        <button type="button" v-if="isRunnable(node.ext)" class="btn btn-sm btn-primary"
          @click.stop="emit('load', node.path, node.baseName)">加载</button>
        <span v-else class="ptn-noload" :title="runHint(node.ext)">{{ runHint(node.ext) ? '' : '—' }}</span>
        <button type="button" class="btn btn-sm ptn-open-dir" @click.stop="openFolder(node.path)" title="打开所在文件夹">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><polyline points="9 14 12 11 15 14"/></svg>
        </button>
        <button type="button" class="btn btn-sm ptn-del" @click.stop="emit('delete', node.relName, false)" title="删除文件">✕</button>
      </span>
      <span v-else class="ptn-actions">
        <button type="button" class="btn btn-sm ptn-open-dir" @click.stop="openFolderDir(node.path)" title="打开所在文件夹">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><polyline points="9 14 12 11 15 14"/></svg>
        </button>
        <button type="button" class="btn btn-sm ptn-del" @click.stop="emit('delete', (props.packageName || '') + '/' + node.path, true)" title="删除目录">✕</button>
      </span>
    </div>
    <template v-if="node.isDir && expanded && node.children">
      <PackageTreeNode
        v-for="child in node.children" :key="child.path"
        :node="child" :level="level + 1"
        :package-name="packageName" :pkg-path="pkgPath"
        @load="(p: string, n: string) => emit('load', p, n)"
        @delete="(r: string, d: boolean) => emit('delete', r, d)"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { systemApi } from '../api/system'
import { fmtSize } from '../utils/formatters'

interface PkgNode {
  name: string; path: string; isDir: boolean; size?: number
  ext?: string; baseName?: string; relName?: string
  childFileCount?: number; children?: PkgNode[]
}

const props = defineProps<{
  node: PkgNode
  level?: number
  packageName?: string
  pkgPath?: string
}>()

const emit = defineEmits<{
  load: [path: string, name: string]
  delete: [relName: string, isDir: boolean]
}>()

const expanded = ref(false)

function toggleSelf() { expanded.value = !expanded.value }

async function openFolder(path: string) {
  try { await systemApi.openFileLocation(path) } catch (_) { /* */ }
}
async function openFolderDir(relPath: string) {
  const full = (props.pkgPath || '') + relPath.replace(/\//g, '\\')
  try { await systemApi.openDir(full) } catch (_) { /* */ }
}

const RUNNABLE = ['.onnx', '.gguf', '.safetensors', '.bin', '.pt', '.pth']
function isRunnable(ext?: string): boolean { return RUNNABLE.includes(ext || '') }
function runHint(ext?: string): string {
  const map: Record<string, string> = { '.safetensors': '仅元数据', '.bin': '仅元数据', '.pt': '需转换', '.pth': '需转换', '.gguf': '需 libllama' }
  return map[ext || ''] || ''
}
function iconFor(name: string): string {
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.')).toLowerCase() : ''
  if (['.bin','.safetensors','.h5','.pt','.pth','.onnx','.gguf','.pb','.mlmodel','.msgpack'].includes(ext)) return '🧩'
  if (['.json','.yaml','.yml'].includes(ext)) return '⚙'
  if (['.txt','.model'].includes(ext)) return '📝'
  if (ext === '.md') return '📋'
  if (['.jpg','.png','.jpeg','.gif'].includes(ext)) return '🖼'
  if (ext === '.meta') return '🔖'
  return '📄'
}
</script>

<style scoped>
.ptn-row {
  position: relative; display: flex; align-items: center; gap: 8px;
  padding: 6px 12px; min-height: 34px;
  border-bottom: 1px solid rgba(255,255,255,0.02);
  transition: background 0.1s;
}
.ptn-row:last-child { border-bottom: none; }
.ptn-row:hover { background: rgba(255,255,255,0.03); }
.ptn-dir { cursor: pointer; background: rgba(255,255,255,0.008); }
.ptn-lvl0 {
  padding-top: 10px; padding-bottom: 10px;
  background: rgba(255,255,255,0.028);
  border-bottom: 1px solid rgba(255,255,255,0.07);
}
.ptn-lvl0 .ptn-name { color: var(--text-primary); font-weight: 600; font-size: 13px; }
.ptn-lvl0 .ptn-icon { font-size: 15px; }
.ptn-arrow {
  width: 20px; height: 20px; flex-shrink: 0; display: inline-flex; align-items: center; justify-content: center;
  border: none; border-radius: 4px; background: transparent; color: var(--text-tertiary);
  font-size: 11px; cursor: pointer; padding: 0;
  transition: background 0.1s, color 0.1s, transform 0.18s cubic-bezier(0.22,0.61,0.36,1);
}
.ptn-arrow:hover { background: rgba(255,255,255,0.08); color: var(--text-primary); }
.ptn-arrow.expanded { transform: rotate(90deg); }
.ptn-arrow-spacer { width: 20px; flex-shrink: 0; }
.ptn-icon { flex-shrink: 0; font-size: 13px; width: 18px; text-align: center; }
.ptn-name { flex: 1; font-size: 12px; font-family: var(--font-mono); color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.ptn-size { font-size: 10px; color: var(--text-tertiary); flex-shrink: 0; min-width: 70px; text-align: right; }
.ptn-actions { flex-shrink: 0; min-width: 140px; display: inline-flex; align-items: center; gap: 6px; justify-content: flex-end; }
.ptn-noload { font-size: 10px; color: var(--text-tertiary); }
.ptn-open-dir { padding: 2px 6px !important; font-size: 11px; color: var(--text-tertiary); border: none !important; background: transparent !important; }
.ptn-open-dir:hover { color: var(--accent); background: var(--bg-hover) !important; }
.ptn-del { padding: 2px 8px !important; font-size: 11px; color: var(--text-tertiary); }
.ptn-del:hover { color: var(--danger); }
.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; transition: all var(--transition); }
.btn:hover { background: var(--bg-hover); }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
</style>
