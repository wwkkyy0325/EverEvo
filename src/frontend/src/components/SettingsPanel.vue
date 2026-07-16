<template>
  <div class="settings">
    <header class="page-head">
      <h2 class="title">设置</h2>
      <p class="subtitle">管理应用、配置数据与系统设备信息</p>
    </header>

    <!-- 分组 1：应用与数据 -->
    <section class="group">
      <h3 class="group-title">应用与数据</h3>
      <div class="app-rows glass-panel">
        <div class="arow">
          <span class="arow-label">版本</span>
          <span class="arow-value">v{{ status.version }}</span>
          <span class="arow-spacer"></span>
          <button v-if="!hasShortcut" class="btn btn-sm" @click="doPin">固定到开始菜单</button>
          <button v-else class="btn btn-sm btn-subtle" @click="doUnpin">从开始菜单移除</button>
        </div>
        <div class="arow">
          <span class="arow-label">程序位置</span>
          <code class="arow-path">{{ exeDir || '—' }}</code>
          <button v-if="exeDir" class="btn btn-icon" title="打开所在目录" @click="openDir(exeDir)"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></button>
        </div>
        <div class="arow">
          <span class="arow-label">用户配置</span>
          <code class="arow-path">{{ userConfigDir || '—' }}</code>
          <button v-if="userConfigDir" class="btn btn-icon" title="打开所在目录" @click="openDir(userConfigDir)"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></button>
        </div>
        <div class="arow">
          <span class="arow-label">数据目录</span>
          <code class="arow-path">{{ dataDir || '—' }}</code>
          <button v-if="dataDir" class="btn btn-icon" title="打开所在目录" @click="openDir(dataDir)"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></button>
        </div>
        <div class="arow">
          <span class="arow-label">模型目录</span>
          <code class="arow-path">{{ modelsDir || '—' }}</code>
          <button v-if="modelsDir" class="btn btn-icon" title="打开所在目录" @click="openDir(modelsDir)"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></button>
        </div>
      </div>
    </section>

    <!-- 分组 2：网络代理 -->
    <section class="group">
      <h3 class="group-title">网络代理</h3>
      <p class="group-hint">自动检测系统代理；也可手动配置。检测到不可达时代理自动旁路，亦可手动关闭走直连。</p>
      <div class="proxy-card glass-panel">
        <!-- 行 1：开关 -->
        <div class="proxy-row proxy-switch-row">
          <div class="proxy-switch-left">
            <span class="proxy-switch-label">代理</span>
            <span class="proxy-switch-desc">{{ proxyEnabled ? (proxyStatus.source === 'none' ? '未检测到代理，当前直连' : '通过 ' + proxySourceLabel + ' 连接') : '已关闭 · 所有请求走直连' }}</span>
          </div>
          <button
            type="button"
            class="mac-switch"
            :class="{ active: proxyEnabled }"
            :disabled="proxyToggling"
            @click="doToggleEnabled"
            :aria-label="proxyEnabled ? '关闭代理' : '开启代理'"
          >
            <span class="mac-switch-knob" />
          </button>
        </div>

        <!-- 行 2：当前代理 + 健康警示行（仅启用且有代理时显示） -->
        <div v-if="proxyEnabled && proxyStatus.source !== 'none'" class="proxy-row">
          <span class="proxy-row-label">当前代理</span>
          <div class="proxy-current">
            <span class="proxy-url">{{ proxyStatus.url }}</span>
            <span class="proxy-tag">{{ proxySourceLabel }}</span>
            <span v-if="proxyStatus.source !== 'none' && proxyStatus.healthy === false" class="proxy-health bad">不可达</span>
            <span v-else-if="proxyStatus.source !== 'none' && proxyStatus.healthy !== false" class="proxy-health good" />
          </div>
          <button class="btn btn-sm proxy-test-btn" @click="testProxy(proxyStatus.url)" :disabled="proxyTesting">
            {{ proxyTesting ? '测试中…' : '测试连接' }}
          </button>
        </div>

        <!-- 行 3：手动配置（仅启用时显示） -->
        <div v-if="proxyEnabled" class="proxy-divider" />
        <div v-if="proxyEnabled" class="proxy-row">
          <span class="proxy-row-label">手动配置</span>
          <input
            v-model="proxyInput"
            class="proxy-input"
            type="text"
            placeholder="http://127.0.0.1:7890"
            spellcheck="false"
          />
          <button class="btn btn-sm proxy-test-btn" @click="testProxy(proxyInput)" :disabled="proxyTesting || !proxyInput.trim()">
            {{ proxyTesting ? '…' : '测试' }}
          </button>
          <button class="btn btn-sm btn-primary" @click="doSaveProxy" :disabled="proxySaving || !proxyInput.trim()">
            {{ proxySaving ? '保存中…' : '保存' }}
          </button>
          <button
            v-if="proxyStatus.source === 'config'"
            class="btn btn-sm btn-subtle"
            @click="doClearProxy"
          >清除</button>
        </div>

        <!-- 行 4：测试结果 -->
        <div v-if="proxyTestResult !== null" class="proxy-divider" />
        <div v-if="proxyTestResult !== null" class="proxy-row">
          <span class="proxy-row-label"></span>
          <span :class="proxyTestResult === '' ? 'proxy-ok' : 'proxy-fail'">
            {{ proxyTestResult === '' ? '✓ 连接成功' : '✗ ' + proxyTestResult }}
          </span>
        </div>
      </div>
    </section>

    <!-- 分组 3：底层设施 -->
    <section class="group">
      <h3 id="backends-section" class="group-title">底层设施</h3>
      <p class="group-hint">模型推理引擎与开发环境依赖，缺失时可在此下载</p>
      <div class="be-rows glass-panel">
        <div v-for="be in backends" :key="be.key" class="be-row">
          <span class="be-name">{{ be.name }}</span>
          <span class="be-spacer"></span>
          <template v-if="be.ok">
            <span class="be-version">{{ be.version }}</span>
          </template>
          <template v-else>
            <select v-if="be.key === 'llama'" v-model="llamaVariant" class="be-select">
              <option value="cpu">CPU (AVX2)</option>
              <option value="cuda">CUDA 12.4</option>
            </select>
            <button class="btn btn-sm" @click="openBackendURL(be.key)">下载</button>
            <span v-if="be.reason" class="be-hint" :title="be.reason">?</span>
          </template>
          <span class="be-dot" :class="be.ok ? 'dot-live' : 'dot-dead'"></span>
          <span class="be-status" :class="be.ok ? 'st-ok' : 'st-muted'">{{ be.ok ? '可用' : '未安装' }}</span>
        </div>
        <div class="be-row be-foot-row">
          <span class="be-foot-label">下载镜像</span>
          <span class="be-spacer"></span>
          <select v-model="sharedMirror" class="be-select">
            <option v-for="m in mirrorList" :key="m.url" :value="m.url">{{ m.label }}</option>
          </select>
          <span class="be-foot-platform">{{ platform.os }} / {{ platform.arch }}</span>
        </div>
      </div>
    </section>

    <!-- 分组 3：系统设备 -->
    <section class="group">
      <h3 class="group-title">系统设备</h3>
      <SystemInfo :active="true" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import SystemInfo from './SystemInfo.vue'
import { systemApi } from '../api/system'

const props = defineProps<{
  sysInfo?: any; dynInfo?: any; active?: boolean
}>()

const status = ref({ version: '0.1.0' })
const exeDir = ref('')
const userConfigDir = ref('')
const dataDir = ref('')
const modelsDir = ref('')
const hasShortcut = ref(false)
const backends = ref<any[]>([])
const mirrorList = ref<any[]>([])
const sharedMirror = ref('')
const platform = ref({ os: '', arch: '' })
const llamaVariant = ref('cpu')

// ── Proxy ──
const proxyStatus = ref<{ url: string; source: string; enabled: boolean; healthy: boolean }>({ url: '', source: 'none', enabled: true, healthy: true })
const proxyInput = ref('')
const proxyTesting = ref(false)
const proxySaving = ref(false)
const proxyToggling = ref(false)
const proxyEnabled = ref(true)
const proxyTestResult = ref<string | null>(null)

const proxySourceLabel = computed(() => {
  const m: Record<string, string> = { config: '已配置', env: '环境变量', system: '系统代理' }
  return m[proxyStatus.value.source] || proxyStatus.value.source
})

async function loadProxy() {
  try { proxyStatus.value = await systemApi.getProxyStatus() } catch (_) {}
  proxyEnabled.value = proxyStatus.value.enabled
  if (proxyStatus.value.source === 'config') proxyInput.value = proxyStatus.value.url
}

async function doToggleEnabled() {
  const next = !proxyEnabled.value
  proxyToggling.value = true
  try {
    systemApi.setProxyEnabled(next)
    proxyEnabled.value = next
  } catch (_) {}
  proxyToggling.value = false
}

async function testProxy(url: string) {
  if (!url?.trim()) return
  proxyTesting.value = true
  proxyTestResult.value = null
  try {
    const err = await systemApi.testProxy(url.trim())
    proxyTestResult.value = err || ''
  } catch (e: any) {
    proxyTestResult.value = e?.message || String(e)
  }
  proxyTesting.value = false
}

async function doSaveProxy() {
  const url = proxyInput.value.trim()
  proxySaving.value = true
  try {
    await systemApi.setProxy(url)
    await loadProxy()
    proxyTestResult.value = null
  } catch (e: any) {
    proxyTestResult.value = '保存失败: ' + (e?.message || String(e))
  }
  proxySaving.value = false
}

async function doClearProxy() {
  proxyInput.value = ''
  proxySaving.value = true
  try {
    await systemApi.setProxy('')
    await loadProxy()
    proxyTestResult.value = null
  } catch (_) {}
  proxySaving.value = false
}

// dataDir / modelsDir are now refs loaded from backend — see loadDataDir/loadModelsDir

onMounted(async () => {
  await Promise.all([
    loadExeDir(), loadUserConfigDir(), loadDataDir(), loadModelsDir(),
    loadShortcut(), loadBackends(), loadPlatform(), loadMirrors(), loadProxy()
  ])
})

async function loadExeDir() { try { exeDir.value = await systemApi.getExeDir() } catch (_) {} }
async function loadUserConfigDir() { try { userConfigDir.value = await systemApi.getUserConfigDir() } catch (_) {} }
async function loadDataDir() { try { dataDir.value = await systemApi.getDataDir() } catch (_) {} }
async function loadModelsDir() { try { modelsDir.value = await systemApi.getModelsDir() } catch (_) {} }
async function loadShortcut() { try { hasShortcut.value = await systemApi.hasStartMenuShortcut() } catch (_) {} }
function openDir(p: string) { if (p) systemApi.openDir(p).catch(() => {}) }
async function doPin() {
  try { await systemApi.pinToStartMenu(); hasShortcut.value = true; /* toast via parent */ } catch (e: any) {}
}
async function doUnpin() {
  try { await systemApi.unpinFromStartMenu(); hasShortcut.value = false } catch (e: any) {}
}
async function loadBackends() { try { backends.value = await systemApi.getBackends() } catch (_) {} }
async function loadPlatform() { try { platform.value = await systemApi.getPlatformInfo() } catch (_) {} }
async function loadMirrors() {
  try {
    mirrorList.value = await systemApi.getMirrors()
    if (mirrorList.value?.length) sharedMirror.value = mirrorList.value[0].url
  } catch (_) {}
}
function openBackendURL(key: string) {
  const variant = key === 'llama' ? llamaVariant.value : ''
  systemApi.getBackendDownloadURL(key, sharedMirror.value || '', variant).then((url: string) => {
    if (url) window.open(url, '_blank')
  }).catch(() => {})
}
</script>

<style scoped>
.settings { display: flex; flex-direction: column; gap: 22px; }

/* ─── 页面头部 ─── */
.page-head { display: flex; flex-direction: column; gap: 4px; }
.title { font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
.subtitle { font-size: 12px; color: var(--text-tertiary); font-weight: 450; }

/* ─── 分组标题 ─── */
.group { display: flex; flex-direction: column; gap: 14px; }
.group-title {
  font-size: 15px; font-weight: 600; letter-spacing: 0.01em;
  color: var(--text-primary); padding: 0 2px; margin: 0;
  display: flex; align-items: center; gap: 10px;
}
.group-title::before {
  content: ''; width: 3px; height: 14px; border-radius: 2px;
  background: var(--accent);
}

/* ─── 应用与数据 ─── */
.app-rows {
  padding: 18px 20px;
  border-radius: var(--radius);
}
.arow {
  display: flex; align-items: center; gap: 12px;
  padding: 7px 0;
  font-size: 12px;
  border-bottom: 1px solid rgba(255,255,255,0.03);
}
.arow:last-child { border-bottom: none; }
.arow-label {
  width: 72px; flex-shrink: 0;
  color: var(--text-tertiary); text-align: right;
}
.arow-value { color: var(--text-primary); }
.arow-path {
  flex: 1; min-width: 0;
  font-size: 11px; font-family: var(--font-mono);
  background: rgba(255,255,255,0.05); padding: 2px 8px; border-radius: 4px;
  color: var(--accent); word-break: break-all;
}
.arow-spacer { flex: 1; }

/* ─── 底层设施行列表 ─── */
.be-rows {
  padding: 14px 20px;
  border-radius: var(--radius);
}
.be-row {
  display: flex; align-items: center; gap: 10px;
  padding: 9px 0;
  font-size: 12px;
  border-bottom: 1px solid rgba(255,255,255,0.03);
}
.be-row:last-child { border-bottom: none; }
.be-name {
  font-weight: 540; color: var(--text-primary);
  min-width: 120px; flex-shrink: 0;
}
.be-dot {
  width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0;
}
.dot-live { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5); }
.dot-dead { background: var(--danger); box-shadow: 0 0 3px rgba(255,69,58,0.4); }
.be-status { font-size: 11px; font-weight: 500; }
.st-ok { color: var(--success); }
.st-muted { color: var(--text-tertiary); }
.be-version {
  font-size: 11px; color: var(--text-tertiary);
  font-family: var(--font-mono);
}
.be-spacer { flex: 1; }
.be-select {
  padding: 4px 8px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm);
  background: var(--bg-elevated); color: var(--text-primary); font-size: 11px;
}
.be-hint {
  display: inline-flex; align-items: center; justify-content: center;
  width: 18px; height: 18px; border-radius: 50%;
  background: var(--bg-hover); color: var(--text-tertiary);
  font-size: 10px; font-weight: 600; cursor: help;
  transition: all var(--transition);
}
.be-hint:hover { background: var(--bg-active); color: var(--text-secondary); }
.be-foot-row { padding-top: 12px; }
.be-foot-label {
  font-size: 11px; color: var(--text-tertiary);
  min-width: 120px; flex-shrink: 0;
}
.be-foot-platform {
  font-size: 10px; color: var(--text-tertiary);
  font-family: var(--font-mono);
}

.btn-icon {
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; padding: 0; border-radius: 6px;
  background: transparent; border: 1px solid var(--border-soft);
  color: var(--text-tertiary); cursor: pointer; flex-shrink: 0;
}
.btn-icon:hover { background: rgba(255,255,255,0.06); color: var(--text-primary); }

.btn-sm { padding: 5px 14px; font-size: 12px; }
.btn-subtle { background: transparent; border-color: var(--border-soft); color: var(--text-secondary); }
.btn-subtle:hover { background: rgba(255,69,58,0.08); border-color: rgba(255,69,58,0.3); color: var(--danger); }

.group-hint { font-size: 12px; color: var(--text-tertiary); margin: -10px 0 0 2px; }

/* ─── 网络代理 ─── */

.proxy-card {
  padding: 4px 0;
  border-radius: var(--radius);
  overflow: hidden;
}

.proxy-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 20px;
  min-height: 44px;
}

.proxy-row-label {
  width: 72px;
  flex-shrink: 0;
  font-size: 12px;
  color: var(--text-tertiary);
  text-align: right;
  line-height: 1.4;
}

.proxy-divider {
  height: 1px;
  margin: 0 20px;
  background: var(--border-subtle);
}

/* Mac 滑动开关 */

.mac-switch {
  position: relative;
  width: 44px;
  height: 24px;
  border: none;
  border-radius: 12px;
  background: rgba(120, 120, 128, 0.28);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.25s cubic-bezier(0.22, 0.61, 0.36, 1);
  padding: 0;
  outline: none;
}
.mac-switch:disabled { opacity: 0.5; cursor: not-allowed; }
.mac-switch.active { background: var(--accent); }

.mac-switch-knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25), 0 0 0 0.5px rgba(0, 0, 0, 0.06);
  transition: transform 0.25s cubic-bezier(0.22, 0.61, 0.36, 1);
}
.mac-switch.active .mac-switch-knob { transform: translateX(20px); }

/* 开关行 */

.proxy-switch-row {
  padding: 16px 20px;
  justify-content: space-between;
}

.proxy-switch-left {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.proxy-switch-label {
  font-size: 13px;
  font-weight: 540;
  color: var(--text-primary);
  letter-spacing: 0.01em;
}

.proxy-switch-desc {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.4;
}

/* 当前代理 */

.proxy-current {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.proxy-url {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--text-primary);
  background: rgba(255, 255, 255, 0.04);
  padding: 2px 8px;
  border-radius: 4px;
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.proxy-tag {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--accent-dim);
  color: var(--accent);
  font-weight: 500;
  white-space: nowrap;
}

/* 健康指示 */

.proxy-health {
  font-size: 10px;
  font-weight: 500;
  white-space: nowrap;
}
.proxy-health.bad {
  color: var(--danger);
  background: rgba(255, 69, 58, 0.08);
  padding: 2px 6px;
  border-radius: 4px;
}
.proxy-health.good::after {
  content: '';
  display: inline-block;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--success);
  box-shadow: 0 0 4px rgba(48, 209, 88, 0.4);
}

/* 输入框 */

.proxy-input {
  width: 240px;
  padding: 5px 10px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-primary);
  font-size: 12px;
  font-family: var(--font-mono);
  outline: none;
  transition: border-color var(--transition);
}
.proxy-input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-dim);
}
.proxy-input::placeholder { color: var(--text-tertiary); opacity: 0.6; }

/* 测试/保存按钮 */

.proxy-test-btn { padding: 4px 12px; font-size: 11px; white-space: nowrap; }

/* 测试结果 */

.proxy-ok {
  color: var(--success);
  font-size: 12px;
  font-weight: 500;
}
.proxy-fail {
  color: var(--danger);
  font-size: 12px;
  word-break: break-word;
}
</style>
