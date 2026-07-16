<template>
  <div>
    <!-- ═══ Section: Feishu Bot service ═══ -->
    <div class="feishu-section">
      <div class="feishu-section-head">
        <div class="feishu-section-icon">⊹</div>
        <div class="feishu-section-info">
          <div class="feishu-section-title">飞书机器人</div>
          <div class="feishu-section-desc">通过飞书企业自建应用的 WebSocket 长连接接入：飞书群 @机器人 → EverEvo 用 LLM 处理 → 回复到群。EverEvo 主动出站连飞书，无需公网 IP / 内网穿透。</div>
        </div>
      </div>

      <div class="feishu-cards">
        <!-- Status card -->
        <div class="glass-panel feishu-status-card">
          <div class="feishu-status-row">
            <span class="be-dot" :class="status.running ? 'dot-live' : 'dot-dead'"></span>
            <span class="feishu-status-label">{{ status.running ? '已连接' : '未连接' }}</span>
            <span v-if="status.running && status.appId" class="feishu-status-url">AppID …{{ status.appId }}</span>
          </div>
          <div class="feishu-actions">
            <button v-if="!status.running" class="btn btn-primary" @click="start">连接</button>
            <button v-else class="btn" @click="stop">断开</button>
          </div>
        </div>

        <!-- Config card -->
        <div class="glass-panel feishu-config-card">
          <div class="feishu-field">
            <label class="feishu-label">App ID</label>
            <input v-model="cfg.appId" type="text" class="feishu-input" placeholder="cli_xxxx" />
          </div>
          <div class="feishu-field">
            <label class="feishu-label">App Secret</label>
            <input v-model="cfg.appSecret" type="password" class="feishu-input" placeholder="应用 secret" />
          </div>
          <div class="feishu-field">
            <label class="feishu-label">Verification Token</label>
            <input v-model="cfg.verificationToken" type="text" class="feishu-input" placeholder="事件订阅 token（可留空）" />
          </div>
          <div class="feishu-actions">
            <label class="feishu-toggle">
              <input type="checkbox" v-model="cfg.enabled" />
              <span>启动时自动连接</span>
            </label>
            <button class="btn btn-sm btn-primary" @click="save" :disabled="saving">保存</button>
          </div>
        </div>
      </div>

      <!-- Access guide -->
      <div class="glass-panel feishu-guide-card">
        <div class="feishu-guide-toggle" @click="showGuide = !showGuide">
          <span>{{ showGuide ? '▾' : '▸' }} 接入指南 — 飞书开放平台配置步骤</span>
        </div>
        <div v-if="showGuide" class="feishu-guide-body">
          <div class="feishu-guide-item">
            <strong>1. 创建企业自建应用</strong>
            <p>打开 <code>open.feishu.cn/app</code> → 创建企业自建应用，获取 <strong>App ID</strong> 与 <strong>App Secret</strong>，填入左侧。</p>
          </div>
          <div class="feishu-guide-item">
            <strong>2. 开启机器人能力</strong>
            <p>应用能力 → 添加「机器人」。</p>
          </div>
          <div class="feishu-guide-item">
            <strong>3. 权限</strong>
            <p>权限管理 → 开通 <code>im:message</code>（收发消息）、<code>im:message:send_as_bot</code>。</p>
          </div>
          <div class="feishu-guide-item">
            <strong>4. 事件订阅（长连接）</strong>
            <p>事件订阅 → 选择「使用长连接接收事件」→ 添加事件 <code>im.message.receive_v1</code>（接收消息）。</p>
          </div>
          <div class="feishu-guide-item">
            <strong>5. 发布 + 加群</strong>
            <p>版本管理 → 创建版本并发布 → 把机器人加到测试群 → 在群里 @机器人 发消息。</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useToast } from '../../composables/useToast'
import { useDataChanged } from '../../composables/useDataChanged'
import { feishuApi } from '../../api/feishu'
import type { FeishuConfig, FeishuStatus } from '../../api/feishu'

const toast = useToast()
function t(type: string, title: string, desc?: string) {
  try { toast.show(type as any, title, desc || '') } catch (_) {}
}

const status = ref<FeishuStatus>({ running: false, appId: '' })
const showGuide = ref(false)
const saving = ref(false)

const cfg = reactive<FeishuConfig>({
  enabled: false,
  appId: '',
  appSecret: '',
  verificationToken: '',
})

useDataChanged('feishu:changed', () => { loadAll() })

onMounted(() => { loadAll() })

async function loadAll() {
  try {
    const c = await feishuApi.getConfig()
    Object.assign(cfg, c)
  } catch (_) {}
  try {
    status.value = await feishuApi.getStatus()
  } catch (_) {}
}

async function start() {
  try {
    await feishuApi.start()
    status.value = await feishuApi.getStatus()
    t('success', '已连接', '飞书机器人已启动')
  } catch (e: any) { t('error', '连接失败', e.message || e) }
}

async function stop() {
  try {
    await feishuApi.stop()
    status.value = await feishuApi.getStatus()
    t('info', '已断开')
  } catch (e: any) { t('error', '断开失败', e.message || e) }
}

async function save() {
  if (!cfg.appId || !cfg.appSecret) { t('warning', '缺少凭据', '请填写 App ID 和 App Secret'); return }
  saving.value = true
  try {
    await feishuApi.updateConfig(cfg)
    status.value = await feishuApi.getStatus()
    t('success', '已保存')
  } catch (e: any) { t('error', '保存失败', e.message || e) }
  finally { saving.value = false }
}
</script>

<style scoped>
/* Mirrors the LLMAgent / LLMMCP section + guide-card pattern. */
.feishu-section { display: flex; flex-direction: column; gap: 14px; }
.feishu-section-head { display: flex; align-items: flex-start; gap: 14px; padding-bottom: 12px; border-bottom: 1px solid var(--border-subtle); }
.feishu-section-icon { width: 40px; height: 40px; border-radius: 10px; background: var(--bg-elevated); border: 1px solid var(--border-soft); display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--accent); flex-shrink: 0; }
.feishu-section-info { display: flex; flex-direction: column; gap: 3px; }
.feishu-section-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.feishu-section-desc { font-size: 12px; color: var(--text-tertiary); line-height: 1.4; }

.feishu-cards { display: grid; grid-template-columns: 1fr 320px; gap: 12px; }
.feishu-status-card { padding: 16px 18px; display: flex; flex-direction: column; gap: 12px; }
.feishu-status-row { display: flex; align-items: center; gap: 10px; }
.feishu-status-label { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.feishu-status-url { font-size: 12px; font-family: var(--font-mono); color: var(--accent); background: var(--bg-inset); padding: 2px 8px; border-radius: 4px; }
.feishu-actions { display: flex; align-items: center; gap: 10px; }

.feishu-config-card { padding: 16px 18px; display: flex; flex-direction: column; gap: 10px; }
.feishu-field { display: flex; flex-direction: column; gap: 4px; }
.feishu-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.feishu-input { padding: 6px 10px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; font-family: var(--font-mono); outline: none; }
.feishu-input:focus { border-color: var(--accent); }
.feishu-toggle { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-secondary); cursor: pointer; }
.feishu-toggle input { accent-color: var(--accent); }

.feishu-guide-card { padding: 14px 18px; }
.feishu-guide-toggle { display: flex; align-items: center; gap: 10px; cursor: pointer; user-select: none; font-size: 13px; font-weight: 500; color: var(--text-secondary); transition: color 0.15s; }
.feishu-guide-toggle:hover { color: var(--text-primary); }
.feishu-guide-body { margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border-subtle); }
.feishu-guide-item { margin-bottom: 12px; font-size: 12px; }
.feishu-guide-item:last-child { margin-bottom: 0; }
.feishu-guide-item strong { color: var(--text-primary); }
.feishu-guide-item p { color: var(--text-secondary); margin: 4px 0; }
.feishu-guide-item code { font-size: 11px; font-family: var(--font-mono); padding: 1px 5px; background: var(--bg-inset); border-radius: 3px; color: var(--accent); }

/* glass-panel comes from global CSS (same as LLMMCP / LLMAgent). */
.be-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-live { background: var(--success); box-shadow: 0 0 4px rgba(48,209,88,0.5); }
.dot-dead { background: var(--danger); box-shadow: 0 0 3px rgba(255,69,58,0.4); }

.btn { padding: 6px 12px; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--bg-elevated); color: var(--text-primary); font-size: 12px; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 3px 10px !important; font-size: 11px; }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn:disabled { opacity: 0.4; cursor: default; }
</style>
