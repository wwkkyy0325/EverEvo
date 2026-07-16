<template>
  <div class="kb-add-wrap">
    <!-- File drop zone -->
    <div
      class="kb-drop-zone"
      :class="{ 'kb-drop-active': dragOver }"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="dragOver = false"
      @drop.prevent="handleDrop"
    >
      <span class="kb-drop-icon">📂</span>
      <span class="kb-drop-text">{{ dragOver ? '释放文件以导入' : '拖拽文件到此处，或点击选择' }}</span>
      <span class="kb-drop-hint">支持 .txt, .md, .pdf</span>
      <button class="btn btn-sm" @click.stop="pickFile" :disabled="props.busy">选择文件</button>
    </div>

    <!-- Import status -->
    <p v-if="importing" class="kb-add-result" style="color:var(--text-secondary);">正在解析文件…</p>

    <!-- Text input area -->
    <textarea v-model="text" class="kb-textarea" placeholder="粘贴文本到这里…（自动按段落分块，每块最大 480 字符）" rows="5" />
    <div class="kb-add-actions">
      <input v-model="metaKey" type="text" class="kb-meta-input" placeholder="元数据 key（可选）" />
      <input v-model="metaVal" type="text" class="kb-meta-input" placeholder="元数据 value（可选）" />
      <div class="kb-add-spacer"></div>
      <button class="btn" @click="text = ''" :disabled="!text.trim()">清空</button>
      <button class="btn btn-primary" @click="doAdd" :disabled="props.busy || !text.trim()">
        {{ props.busy ? '添加中…' : '添加' }}
      </button>
    </div>
    <p v-if="result !== null" class="kb-add-result">已添加 <strong>{{ result }}</strong> 个文本块</p>
    <p v-if="errorMsg" class="kb-add-result" style="color:var(--danger);">{{ errorMsg }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useToast } from '../../composables/useToast'
import { knowledgeApi } from '../../api/knowledge'

const props = defineProps<{ kbId: string; busy: boolean }>()
const emit = defineEmits<{ done: [] }>()

const toast = useToast()
const text = ref('')
const metaKey = ref('')
const metaVal = ref('')
const result = ref<number | null>(null)
const errorMsg = ref('')
const dragOver = ref(false)
const importing = ref(false)

async function doAdd() {
  if (!text.value.trim()) return
  emit('done')
  errorMsg.value = ''
  const meta: Record<string, string> = {}
  if (metaKey.value.trim()) meta[metaKey.value.trim()] = metaVal.value.trim()
  try {
    const count = await knowledgeApi.addTexts(props.kbId, [text.value], meta)
    result.value = count
    text.value = ''
    metaKey.value = ''
    metaVal.value = ''
    toast.show('success', '已添加', count + ' 个文本块')
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    errorMsg.value = msg
    toast.show('error', '添加失败', msg)
  }
}

async function handleDrop(e: DragEvent) {
  dragOver.value = false
  const file = e.dataTransfer?.files?.[0]
  if (file) await importFile(file)
}

async function pickFile() {
  try {
    const rt = window.runtime
    const filePath = await rt.OpenFileDialog({
      Filters: [
        { DisplayName: '文档文件 (*.txt, *.md, *.pdf)', Pattern: '*.txt;*.md;*.pdf;*.markdown' },
        { DisplayName: '所有文件', Pattern: '*.*' },
      ],
    })
    if (!filePath) return
    importing.value = true
    errorMsg.value = ''
    const content = await knowledgeApi.parseFile(filePath)
    text.value = content
    result.value = null
    toast.show('success', '文件已导入', filePath.split('\\').pop() || filePath)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    errorMsg.value = '解析文件失败: ' + msg
    toast.show('error', '导入失败', msg)
  } finally {
    importing.value = false
  }
}

async function importFile(file: File) {
  importing.value = true
  errorMsg.value = ''
  try {
    const ext = file.name.split('.').pop()?.toLowerCase()
    if (ext === 'txt' || ext === 'md' || ext === 'markdown') {
      // Read text files directly in browser — no backend round-trip needed.
      text.value = await file.text()
    } else {
      // PDF and other formats go through the backend parser.
      // We must use the path; in Wails desktop, use file.name as a hint
      // and fall back to reading via backend.
      // For a Wails desktop app, we can use the file's path via webkitRelativePath
      // or handle via the file dialog. For drag-and-drop PDF, read as binary
      // and send base64 to backend.
      const buf = await file.arrayBuffer()
      const bytes = new Uint8Array(buf)
      // For drag-and-drop PDF in Wails, we write temp and parse.
      // Simpler path: show a hint that drag-and-drop PDF needs the file picker.
      errorMsg.value = 'PDF 文件请通过「选择文件」按钮导入（桌面安全限制）'
      importing.value = false
      return
    }
    result.value = null
    toast.show('success', '文件已导入', file.name)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    errorMsg.value = '读取文件失败: ' + msg
    toast.show('error', '导入失败', msg)
  } finally {
    importing.value = false
  }
}
</script>

<style scoped>
.kb-add-wrap { padding: 4px 0; }

/* Drop zone */
.kb-drop-zone {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px; margin-bottom: 10px;
  border: 2px dashed var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); transition: border-color 0.15s, background 0.15s;
  flex-wrap: wrap; justify-content: center;
}
.kb-drop-active { border-color: var(--accent); background: var(--accent-dim); }
.kb-drop-icon { font-size: 18px; opacity: 0.5; }
.kb-drop-text { font-size: 12px; color: var(--text-secondary); }
.kb-drop-hint { font-size: 10px; color: var(--text-tertiary); margin-left: 4px; }

.kb-textarea {
  width: 100%; box-sizing: border-box; padding: 8px 10px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-primary);
  font-size: 12px; font-family: var(--font); line-height: 1.6;
  outline: none; resize: vertical;
  transition: border-color 0.15s;
}
.kb-textarea::placeholder { color: var(--text-tertiary); }
.kb-textarea:focus { border-color: var(--accent); }
.kb-add-actions { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
.kb-add-spacer { flex: 1; }
.kb-meta-input {
  width: 120px; padding: 5px 8px;
  border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-inset); color: var(--text-secondary);
  font-size: 11px; font-family: var(--font); outline: none;
  transition: border-color 0.15s;
}
.kb-meta-input::placeholder { color: var(--text-tertiary); }
.kb-meta-input:focus { border-color: var(--accent); }
.kb-add-result { font-size: 11px; color: var(--success); margin-top: 6px; }
</style>
