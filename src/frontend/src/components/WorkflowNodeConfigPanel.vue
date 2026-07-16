<template>
  <div class="wf-config glass-panel">
    <div class="wf-cfg-head">
      <h4>{{ title }}</h4>
      <button class="wf-cfg-close" @click="$emit('close')">✕</button>
    </div>

    <div class="wf-cfg-group">
      <label>标题</label>
      <input v-model="cfgTitle" class="field wf-field" placeholder="节点标题…" @input="apply" />
    </div>
    <div class="wf-cfg-group">
      <label>描述</label>
      <input v-model="cfgDesc" class="field wf-field" placeholder="可选说明…" @input="apply" />
    </div>

    <!-- Input node -->
    <template v-if="cfgType === 'input'">
      <div class="wf-cfg-group">
        <label>输入字段</label>
        <div v-for="(f, fi) in cfgInputFields" :key="fi" class="wf-cfg-row">
          <input v-model="f.name" class="field wf-field-sm" placeholder="字段名" @change="apply" />
          <input v-model="f.default" class="field wf-field-sm" placeholder="默认值" @change="apply" />
          <button class="btn btn-xs btn-del" @click="cfgInputFields.splice(fi,1); apply()">✕</button>
        </div>
        <button class="btn btn-sm" @click="cfgInputFields.push({name:'',default:''}); apply()">+ 字段</button>
      </div>
    </template>

    <!-- LLM node -->
    <template v-if="cfgType === 'llm'">
      <div class="wf-cfg-group">
        <label>System Prompt</label>
        <textarea v-model="cfgLLM.systemPrompt" class="field wf-textarea" rows="3" @change="apply"></textarea>
      </div>
      <div class="wf-cfg-group">
        <label>User Prompt <span class="hint">&#123;&#123;上游节点标题.字段&#125;&#125;</span></label>
        <textarea v-model="cfgLLM.userPrompt" class="field wf-textarea" rows="4" @change="apply"></textarea>
      </div>
      <div class="wf-cfg-group">
        <label>工具（勾选启用）</label>
        <div class="wf-cfg-checks">
          <label v-for="t in tools" :key="t.name" class="wf-cfg-check">
            <input type="checkbox" :value="t.name" v-model="cfgLLM.tools" @change="apply" />
            <span>{{ t.title || t.name }}</span>
          </label>
        </div>
      </div>
    </template>

    <!-- Tool node -->
    <template v-if="cfgType === 'tool'">
      <div class="wf-cfg-group">
        <label>工具</label>
        <select v-model="cfgTool.toolName" class="field wf-field" @change="apply">
          <option value="">— 选择工具 —</option>
          <option v-for="t in tools" :key="t.name" :value="t.name">{{ t.title || t.name }}</option>
        </select>
      </div>
      <div class="wf-cfg-group">
        <label>参数</label>
        <div v-for="(v, k) in cfgTool.params" :key="k" class="wf-cfg-row">
          <input :value="k" class="field wf-field-sm" placeholder="参数名" @change="e => renameParam(k, (e.target as HTMLInputElement).value)" />
          <input v-model="cfgTool.params[k]" class="field wf-field-sm" placeholder="值" @change="apply" />
          <button class="btn btn-xs btn-del" @click="delete cfgTool.params[k]; apply()">✕</button>
        </div>
        <button class="btn btn-sm" @click="cfgTool.params['p'+(Object.keys(cfgTool.params).length+1)]=''; apply()">+ 参数</button>
      </div>
    </template>

    <!-- Condition node -->
    <template v-if="cfgType === 'condition'">
      <div class="wf-cfg-group">
        <label>条件表达式</label>
        <textarea v-model="cfgCond.expression" class="field wf-textarea" rows="3"
          placeholder="&#123;&#123;node_llm.output.result&#125;&#125; == true" @change="apply"></textarea>
      </div>
      <div class="wf-cfg-row-2">
        <div class="wf-cfg-group"><label>True 标签</label><input v-model="cfgCond.trueLabel" class="field wf-field" @change="apply" /></div>
        <div class="wf-cfg-group"><label>False 标签</label><input v-model="cfgCond.falseLabel" class="field wf-field" @change="apply" /></div>
      </div>
    </template>

    <!-- Code node -->
    <template v-if="cfgType === 'code'">
      <div class="wf-cfg-group">
        <label>模板 <span class="hint">{{ '{' + '{node_id.output.field}' + '}' }}</span></label>
        <textarea v-model="cfgCode.template" class="field wf-textarea" rows="4" @change="apply"></textarea>
      </div>
      <div class="wf-cfg-group"><label>输出字段</label><input v-model="cfgCode.outputField" class="field wf-field" @change="apply" /></div>
    </template>

    <!-- Loop node -->
    <template v-if="cfgType === 'loop'">
      <div class="wf-cfg-group"><label>数据源</label><input v-model="cfgLoop.sourceExpression" class="field wf-field" placeholder="&#123;&#123;node_tool.output.results&#125;&#125;" @change="apply" /></div>
      <div class="wf-cfg-group"><label>变量名</label><input v-model="cfgLoop.itemVariable" class="field wf-field" @change="apply" /></div>
      <div class="wf-cfg-group"><label>最大迭代</label><input v-model.number="cfgLoop.maxIterations" type="number" class="field wf-field-sm" @change="apply" /></div>
    </template>

    <!-- Agent node -->
    <template v-if="cfgType === 'agent'">
      <div class="wf-cfg-group">
        <label>智能体</label>
        <select v-model="cfgAgent.agentId" class="field wf-field" @change="apply">
          <option value="">— 选择智能体 —</option>
          <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select>
      </div>
      <div class="wf-cfg-group">
        <label>任务 / Prompt <span class="hint">&#123;&#123;上游节点标题.字段&#125;&#125;</span></label>
        <textarea v-model="cfgAgent.prompt" class="field wf-textarea" rows="4" @change="apply" placeholder="审计以下内容：&#123;&#123;输入.文本&#125;&#125;"></textarea>
      </div>
    </template>

    <!-- HTTP node -->
    <template v-if="cfgType === 'http'">
      <div class="wf-cfg-group"><label>URL</label><input v-model="cfgHTTP.url" class="field wf-field" @change="apply" placeholder="https://api.example.com/data" /></div>
      <div class="wf-cfg-group"><label>方法</label>
        <select v-model="cfgHTTP.method" class="field wf-field" @change="apply">
          <option>GET</option><option>POST</option><option>PUT</option><option>DELETE</option><option>PATCH</option>
        </select>
      </div>
      <div class="wf-cfg-group"><label>Headers (JSON)</label><textarea v-model="cfgHTTP.headersText" class="field wf-textarea" rows="3" @change="onHeadersChange" placeholder='{"Authorization":"Bearer xxx"}'></textarea></div>
      <div class="wf-cfg-group" v-if="cfgHTTP.method !== 'GET'"><label>Body</label><textarea v-model="cfgHTTP.body" class="field wf-textarea" rows="4" @change="apply" placeholder='{"key":"value"}'></textarea></div>
      <div class="wf-cfg-group"><label>输出字段</label><input v-model="cfgHTTP.outputField" class="field wf-field" @change="apply" /></div>
    </template>

    <!-- Delay node -->
    <template v-if="cfgType === 'delay'">
      <div class="wf-cfg-group"><label>延迟 (毫秒)</label><input v-model.number="cfgDelay.durationMs" type="number" class="field wf-field" @change="apply" placeholder="1000" /></div>
    </template>

    <!-- Notify node -->
    <template v-if="cfgType === 'notify'">
      <div class="wf-cfg-group"><label>标题</label><input v-model="cfgNotify.title" class="field wf-field" @change="apply" placeholder="工作流通知" /></div>
      <div class="wf-cfg-group"><label>消息</label><textarea v-model="cfgNotify.message" class="field wf-textarea" rows="3" @change="apply" placeholder="&#123;&#123;prev_node.output&#125;&#125;"></textarea></div>
      <div class="wf-cfg-group"><label>级别</label>
        <select v-model="cfgNotify.level" class="field wf-field" @change="apply">
          <option>info</option><option>success</option><option>warning</option><option>error</option>
        </select>
      </div>
    </template>

    <!-- Merge node -->
    <template v-if="cfgType === 'merge'">
      <div class="wf-cfg-group"><label>等待策略</label>
        <select v-model="cfgMerge.waitFor" class="field wf-field" @change="apply">
          <option value="all">等待全部输入</option><option value="any">任一输入到达即继续</option>
        </select>
      </div>
      <div class="wf-cfg-group"><label>超时 (毫秒)</label><input v-model.number="cfgMerge.timeout" type="number" class="field wf-field" @change="apply" placeholder="30000" /></div>
    </template>

    <!-- Custom node -->
    <template v-if="cfgType === 'custom'">
      <div class="wf-cfg-group"><label>语言</label>
        <select v-model="cfgCustom.language" class="field wf-field" @change="apply">
          <option value="javascript">JavaScript (内嵌 Goja 引擎)</option>
          <option value="go">Go (内嵌 Yaegi 解释器)</option>
          <option value="shell">Shell (CMD / PowerShell)</option>
        </select>
      </div>
      <div class="wf-cfg-group"><label>脚本</label><textarea v-model="cfgCustom.script" class="field wf-textarea" rows="8" @change="apply" :placeholder="customPlaceholder"></textarea></div>
      <div class="wf-cfg-group"><label>超时 (毫秒)</label><input v-model.number="cfgCustom.timeout" type="number" class="field wf-field" @change="apply" placeholder="30000" /></div>
    </template>

    <!-- Output node -->
    <template v-if="cfgType === 'output'">
      <div class="wf-cfg-group">
        <label>输出字段</label>
        <div v-for="(f, fi) in cfgOutputFields" :key="fi" class="wf-cfg-row">
          <input v-model="f.name" class="field wf-field-sm" placeholder="字段名" @change="apply" />
          <input v-model="f.source" class="field wf-field-sm" placeholder="&#123;&#123;节点标题.output&#125;&#125;" @change="apply" />
          <button class="btn btn-xs btn-del" @click="cfgOutputFields.splice(fi,1); apply()">✕</button>
        </div>
        <button class="btn btn-sm" @click="cfgOutputFields.push({name:'',source:''}); apply()">+ 字段</button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { TYPE_META } from '../utils/workflow-mapper'

const props = defineProps<{
  nodeId: string | null
  flowNodes: any[]
  tools: any[]
  agents: any[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'applied', data: Record<string, any>): void
}>()

// Config editing state (mirrors selected node)
const cfgType = ref('')
const cfgTitle = ref('')
const cfgDesc = ref('')
const cfgInputFields = ref<{ name: string; default: string }[]>([])
const cfgLLM = reactive<{ systemPrompt: string; userPrompt: string; tools: string[] }>({ systemPrompt: '', userPrompt: '', tools: [] })
const cfgTool = reactive<{ toolName: string; params: Record<string, string> }>({ toolName: '', params: {} })
const cfgCond = reactive<{ expression: string; trueLabel: string; falseLabel: string }>({ expression: '', trueLabel: '成功', falseLabel: '失败' })
const cfgCode = reactive<{ template: string; outputField: string }>({ template: '', outputField: 'output' })
const cfgLoop = reactive<{ sourceExpression: string; itemVariable: string; maxIterations: number }>({ sourceExpression: '', itemVariable: 'item', maxIterations: 100 })
const cfgAgent = reactive<{ agentId: string; prompt: string }>({ agentId: '', prompt: '' })
const cfgHTTP = reactive<{ url: string; method: string; headersText: string; body: string; outputField: string }>({ url: '', method: 'GET', headersText: '', body: '', outputField: 'response' })
const cfgDelay = reactive<{ durationMs: number }>({ durationMs: 1000 })
const cfgNotify = reactive<{ title: string; message: string; level: string }>({ title: '', message: '', level: 'info' })
const cfgMerge = reactive<{ waitFor: string; timeout: number }>({ waitFor: 'all', timeout: 30000 })
const cfgCustom = reactive<{ script: string; language: string; timeout: number }>({ script: '', language: 'javascript', timeout: 30000 })
const cfgOutputFields = ref<{ name: string; source: string }[]>([])

const title = computed(() => {
  const m = TYPE_META[cfgType.value]
  return m ? m.label + ' 配置' : '节点配置'
})

function load() {
  if (!props.nodeId) return
  const fn = props.flowNodes.find((n: any) => n.id === props.nodeId)
  if (!fn) return
  const d = fn.data
  const c = d.config || {}
  cfgType.value = d.type
  cfgTitle.value = d.label
  cfgDesc.value = d.desc || ''
  if (d.type === 'input') cfgInputFields.value = [...(c.fields || [])]
  if (d.type === 'llm') { cfgLLM.systemPrompt = c.systemPrompt || ''; cfgLLM.userPrompt = c.userPrompt || ''; cfgLLM.tools = [...(c.tools || [])] }
  if (d.type === 'tool') { cfgTool.toolName = c.toolName || ''; cfgTool.params = { ...(c.params || {}) } }
  if (d.type === 'condition') { cfgCond.expression = c.expression || ''; cfgCond.trueLabel = c.trueLabel || '成功'; cfgCond.falseLabel = c.falseLabel || '失败' }
  if (d.type === 'code') { cfgCode.template = c.template || ''; cfgCode.outputField = c.outputField || 'output' }
  if (d.type === 'loop') { cfgLoop.sourceExpression = c.sourceExpression || ''; cfgLoop.itemVariable = c.itemVariable || 'item'; cfgLoop.maxIterations = c.maxIterations || 100 }
  if (d.type === 'agent') { cfgAgent.agentId = c.agentId || ''; cfgAgent.prompt = c.userPrompt || c.prompt || '' }
  if (d.type === 'http') { cfgHTTP.url = c.url || ''; cfgHTTP.method = c.method || 'GET'; cfgHTTP.headersText = c.headers ? JSON.stringify(c.headers, null, 2) : ''; cfgHTTP.body = c.body || ''; cfgHTTP.outputField = c.outputField || 'response' }
  if (d.type === 'delay') { cfgDelay.durationMs = c.durationMs || 1000 }
  if (d.type === 'notify') { cfgNotify.title = c.title || ''; cfgNotify.message = c.message || ''; cfgNotify.level = c.level || 'info' }
  if (d.type === 'merge') { cfgMerge.waitFor = c.waitFor || 'all'; cfgMerge.timeout = c.timeout || 30000 }
  if (d.type === 'custom') { cfgCustom.script = c.script || ''; cfgCustom.language = c.language || 'javascript'; cfgCustom.timeout = c.timeout || 30000 }
  if (d.type === 'output') cfgOutputFields.value = [...(c.fields || [])]
}

function buildConfig(): Record<string, any> {
  const cfg: Record<string, any> = { label: cfgTitle.value, desc: cfgDesc.value }
  if (cfgType.value === 'input') cfg.config = { fields: [...cfgInputFields.value] }
  else if (cfgType.value === 'llm') cfg.config = { systemPrompt: cfgLLM.systemPrompt, userPrompt: cfgLLM.userPrompt, tools: [...cfgLLM.tools], temperature: 0.7 }
  else if (cfgType.value === 'tool') cfg.config = { toolName: cfgTool.toolName, params: { ...cfgTool.params } }
  else if (cfgType.value === 'condition') cfg.config = { expression: cfgCond.expression, trueLabel: cfgCond.trueLabel, falseLabel: cfgCond.falseLabel }
  else if (cfgType.value === 'code') cfg.config = { template: cfgCode.template, outputField: cfgCode.outputField }
  else if (cfgType.value === 'loop') cfg.config = { sourceExpression: cfgLoop.sourceExpression, itemVariable: cfgLoop.itemVariable, maxIterations: cfgLoop.maxIterations }
  else if (cfgType.value === 'agent') cfg.config = { agentId: cfgAgent.agentId, userPrompt: cfgAgent.prompt }
  else if (cfgType.value === 'http') { let headers: any = {}; try { headers = JSON.parse(cfgHTTP.headersText || '{}') } catch (_) {} cfg.config = { url: cfgHTTP.url, method: cfgHTTP.method, headers, body: cfgHTTP.body, outputField: cfgHTTP.outputField } }
  else if (cfgType.value === 'delay') cfg.config = { durationMs: cfgDelay.durationMs }
  else if (cfgType.value === 'notify') cfg.config = { title: cfgNotify.title, message: cfgNotify.message, level: cfgNotify.level }
  else if (cfgType.value === 'merge') cfg.config = { waitFor: cfgMerge.waitFor, timeout: cfgMerge.timeout }
  else if (cfgType.value === 'custom') cfg.config = { script: cfgCustom.script, language: cfgCustom.language, timeout: cfgCustom.timeout }
  else if (cfgType.value === 'output') cfg.config = { fields: [...cfgOutputFields.value] }
  if (cfgType.value === 'condition') {
    cfg.trueLabel = cfgCond.trueLabel
    cfg.falseLabel = cfgCond.falseLabel
  }
  return cfg
}

function apply() {
  emit('applied', buildConfig())
}

function onHeadersChange() { apply() }

const customPlaceholder = computed(() => {
  if (cfgCustom.language === 'javascript') return '// 内嵌 Goja JS 引擎，打包后零依赖\n// ctx.input  → 上游节点输出\n// return     → 传给下游\nconst data = ctx.input;\nreturn { result: data };'
  if (cfgCustom.language === 'go') return '// 内嵌 Yaegi Go 解释器，打包后零依赖\n// 可用所有 Go 标准库\nfunc(ctx map[string]interface{}) interface{} {\n    data := ctx["input"]\n    return map[string]interface{}{\"result\": data}\n}'
  return ':: Windows CMD / PowerShell 原生支持\n:: stdin:  上游 JSON\n:: stdout: 下游 JSON\n'
})

function renameParam(oldK: string, newK: string) {
  if (oldK === newK) return
  const v = cfgTool.params[oldK]
  delete cfgTool.params[oldK]
  cfgTool.params[newK] = v
  apply()
}

// Reload when node selection changes (and on mount)
watch(() => props.nodeId, () => { load() }, { immediate: true })
watch(() => props.flowNodes, () => { if (props.nodeId) load() }, { deep: true })

// Expose load for initial manual call
defineExpose({ load })
</script>

<style scoped>
.wf-config { width: 260px; flex-shrink: 0; padding: 12px 14px; overflow-y: auto; }
.wf-cfg-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.wf-cfg-head h4 { font-size: 13px; font-weight: 600; margin: 0; }
.wf-cfg-close { width: 22px; height: 22px; border: none; border-radius: 5px; background: transparent; color: var(--text-tertiary); font-size: 12px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.wf-cfg-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.wf-cfg-group { display: flex; flex-direction: column; gap: 5px; margin-bottom: 10px; }
.wf-cfg-group label { font-size: 11px; font-weight: 500; color: var(--text-secondary); }
.wf-cfg-group .hint { font-weight: 400; color: var(--text-tertiary); }
.wf-cfg-row { display: flex; gap: 4px; align-items: center; }
.wf-cfg-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.wf-cfg-checks { max-height: 140px; overflow-y: auto; display: flex; flex-direction: column; gap: 2px; }
.wf-cfg-check { display: flex; align-items: center; gap: 6px; font-size: 11px; cursor: pointer; }
.field { padding: 6px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; outline: none; font-family: var(--font-mono); box-sizing: border-box; }
.field:focus { border-color: var(--accent); }
.wf-field { width: 100%; }
.wf-field-sm { flex: 1; }
.wf-textarea { resize: vertical; font-family: var(--font-mono); font-size: 11px; line-height: 1.5; width: 100%; }
.btn { padding: 5px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; white-space: nowrap; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 8px !important; font-size: 11px; }
.btn-xs { padding: 2px 5px; font-size: 10px; line-height: 1.3; border: 1px solid var(--border-soft); border-radius: 4px; background: var(--bg-elevated); color: var(--text-secondary); cursor: pointer; }
.btn-xs:hover { background: var(--bg-hover); color: var(--text-primary); }
.btn-del { color: var(--danger); border-color: rgba(255,69,58,0.3); }
.btn-del:hover { background: var(--danger-dim); }
</style>
