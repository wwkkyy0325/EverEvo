<template>
  <div class="sysinfo">
    <!-- 处理器 / 内存 -->
    <div class="glass-panel card">
      <div class="card-head">
        <h3>处理器 · {{ info && info.cpu.vendor === 'intel' ? 'Intel' : (info && info.cpu.vendor === 'amd' ? 'AMD' : 'CPU') }}</h3>
      </div>
      <div v-if="info" class="card-sub">{{ info.cpu.name }} · {{ info.cpu.threads }} 线程 · {{ info.memory.totalGB }} GB</div>
      <div v-if="info && info.cpu.features && info.cpu.features.length" class="tags">
        <span v-for="f in info.cpu.features" :key="f.key" class="tag-sm" :class="f.available ? 't-on' : 't-off'">
          {{ f.label }}{{ f.available ? '' : ' ✕' }}
        </span>
      </div>
      <div class="duo">
        <div class="ring-box">
          <div class="ring" :class="dyn.cpuPercent > 80 ? 'hot' : ''">
            <svg viewBox="0 0 72 72"><circle cx="36" cy="36" r="30" class="rbg"/><circle cx="36" cy="36" r="30" class="rfill" :style="{strokeDashoffset: cpuOff}"/></svg>
            <span class="rval">{{ dyn.cpuPercent }}<em>%</em></span>
          </div>
          <span class="rlabel">CPU</span>
        </div>
        <div class="bar-box">
          <div class="bar-head"><span>内存</span><span>{{ dyn.memoryUsedGB }} / {{ dyn.memoryTotalGB }} GB</span></div>
          <div class="bar-track"><div class="bar-fill" :class="dyn.memoryPercent > 80 ? 'hot' : ''" :style="{width: dyn.memoryPercent + '%'}"></div></div>
          <span class="bar-pct">{{ dyn.memoryPercent }}%</span>
        </div>
      </div>
    </div>

    <!-- 显卡 (每个 GPU 一张卡，带编号) -->
    <div v-for="(g, i) in dyn.gpus || []" :key="'gpu'+i" class="glass-panel card" :class="{ disabled: !gpuUsable(i) }">
      <div class="card-head">
        <h3>
          <span class="gpu-badge">GPU {{ g.index }}</span>
          {{ gpuVendorLabel(i) }}
        </h3>
        <span class="tag" :class="gpuUsable(i) ? 'tag-ok' : 'tag-off'">
          {{ gpuUsable(i) ? '可用' : '不可用' }}
        </span>
      </div>
      <div class="card-sub">{{ gpuName(i) }}</div>
      <div v-if="gpuFeatures(i).length" class="tags">
        <span v-for="f in gpuFeatures(i)" :key="f.key" class="tag-sm" :class="f.available ? 't-on' : 't-off'">
          <template v-if="f.key === 'cuda' && gpuCudaVer(i)">CUDA {{ gpuCudaVer(i) }}</template>
          <template v-else>{{ f.label }}{{ f.available ? '' : ' ✕' }}</template>
        </span>
        <span v-if="gpuComputeCap(i)" class="tag-sm t-on">sm{{ gpuComputeCap(i).replace('.','') }}</span>
      </div>
      <div class="duo">
        <div class="ring-box">
          <div class="ring" :class="g.utilPercent > 80 ? 'hot' : ''">
            <svg viewBox="0 0 72 72"><circle cx="36" cy="36" r="30" class="rbg"/><circle cx="36" cy="36" r="30" class="rfill" :style="{strokeDashoffset: gpuOff(g.utilPercent)}"/></svg>
            <span class="rval">{{ g.utilPercent }}<em>%</em></span>
          </div>
          <span class="rlabel">GPU</span>
        </div>
        <div class="bar-box">
          <div class="bar-head"><span>显存</span><span>{{ vramDisplay(g) }}</span></div>
          <div class="bar-track"><div class="bar-fill" :class="vramPct(g) > 80 ? 'hot' : ''" :style="{width: vramPct(g) + '%'}"></div></div>
          <span class="bar-pct">{{ vramPct(g) }}%</span>
        </div>
      </div>
    </div>

    <!-- 磁盘（按物理磁盘分组） -->
    <div v-if="dyn.physicalDisks && dyn.physicalDisks.length" class="glass-panel card disk-card">
      <div class="card-head"><h3>磁盘</h3></div>
      <div v-for="(pd, pi) in dyn.physicalDisks" :key="'pd'+pi" class="disk-group">
        <div class="disk-group-head">
          <span class="disk-model">{{ pd.model || ('磁盘 ' + (pi + 1)) }}</span>
          <span class="disk-total">{{ fmtGB(pd.sizeGB) }}</span>
        </div>
        <div v-for="(v, vi) in pd.volumes" :key="'v'+vi" class="disk-vol">
          <div class="disk-vol-head">
            <span class="disk-vol-drive">{{ v.drive }}</span>
            <span class="disk-vol-usage">{{ fmtGB(v.totalGB - v.freeGB) }} / {{ fmtGB(v.totalGB) }}</span>
          </div>
          <div class="disk-bar-track">
            <div
              class="disk-bar-fill"
              :class="v.percent > 80 ? 'disk-bar-hot' : 'disk-bar-ok'"
              :style="{ width: v.percent + '%' }"
            ></div>
          </div>
        </div>
      </div>
    </div>

    <!-- 回退：无物理磁盘信息时按盘符显示 -->
    <div v-else class="glass-panel card">
      <div class="card-head"><h3>磁盘</h3></div>
      <div v-for="d in dyn.disks || []" :key="d.drive" class="disk-row">
        <div class="bar-head"><span class="disk-drive">{{ d.drive }}</span><span>{{ d.freeGB }} / {{ d.totalGB }} GB</span></div>
        <div class="bar-track"><div class="bar-fill disk" :class="d.percent > 80 ? 'hot' : ''" :style="{width: d.percent + '%'}"></div></div>
      </div>
      <div v-if="!dyn.disks || dyn.disks.length === 0" class="empty-hint">未检测到磁盘</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { systemApi, type DynamicInfo } from '../api/system'

const R = 2 * Math.PI * 30

const props = defineProps<{ info?: any; dynInfo?: any; active?: boolean }>()

const info = ref<any>(props.info || null)
const dyn = ref<DynamicInfo>({ cpuPercent: 0, memoryUsedGB: 0, memoryTotalGB: 0, memoryPercent: 0, gpus: [], disks: [], physicalDisks: [] })
let _timer: ReturnType<typeof setInterval> | null = null

async function loadStatic() { try { info.value = await systemApi.getSysInfo() } catch (_) {} }
async function tick() { try { dyn.value = await systemApi.getDynamicInfo() } catch (_) {} }
function startPolling() { if (_timer) return; loadStatic(); tick(); _timer = setInterval(() => tick(), 2000) }
function stopPolling() { if (_timer) { clearInterval(_timer); _timer = null } }

watch(() => props.active, (on) => on ? startPolling() : stopPolling(), { immediate: true })
onBeforeUnmount(() => stopPolling())
const cpuOff = computed(() => R * (1 - dyn.value.cpuPercent / 100))
function gpuOff(p: number) { return R * (1 - (p || 0) / 100) }
function fmtGB(n: number): string {
  if (!n || n <= 0) return '—'; if (n >= 1000) return (n / 1000).toFixed(1) + ' TB'; return n.toFixed(0) + ' GB'
}
function vramDisplay(g: any): string {
  const u = g.vramUsedMB || 0; const t = g.vramTotalMB || 0
  if (t >= 1024) return (u / 1024).toFixed(1) + ' / ' + (t / 1024).toFixed(1) + ' GB'
  return u + ' / ' + t + ' MB'
}
function vramPct(g: any): number { return g.vramTotalMB ? Math.min(100, Math.round(g.vramUsedMB * 100 / g.vramTotalMB)) : 0 }
function staticGPU(i: number) { return (info.value?.gpus?.[i]) || {} }
function gpuName(i: number) { return staticGPU(i).name || '—' }
function gpuCudaVer(i: number) { return staticGPU(i).cudaVer || '' }
function gpuComputeCap(i: number) { return staticGPU(i).computeCap || '' }
function gpuFeatures(i: number) { return staticGPU(i).features || [] }
function gpuUsable(i: number) { return staticGPU(i).usable !== false }
function gpuVendorLabel(i: number): string {
  const v = staticGPU(i).vendor
  return v === 'nvidia' ? 'NVIDIA' : v === 'amd' ? 'AMD' : v === 'intel' ? 'Intel' : 'GPU'
}
</script>

<style scoped>
.sysinfo { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 14px; }
.card { padding: 16px 18px; }
.card-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; gap: 8px; }
.card-head h3 { font-size: 13px; font-weight: 600; display: flex; align-items: center; gap: 8px; }
.live { font-size: 10px; color: var(--success); }
.card-sub { font-size: 11px; color: var(--text-tertiary); margin-bottom: 6px; }
.card-sub-mini { font-size: 10px; color: var(--accent); margin-bottom: 8px; font-family: var(--font-mono); }

.gpu-badge {
  font-size: 10px; font-weight: 700; padding: 1px 7px; border-radius: 4px;
  background: var(--accent-dim); color: var(--accent);
  font-family: var(--font-mono);
}

.tags { display: flex; flex-wrap: wrap; gap: 5px; margin-bottom: 14px; }
.tag-sm { font-size: 10px; font-weight: 600; padding: 2px 8px; border-radius: 4px; }
.t-on { background: var(--success-dim); color: var(--success); }
.t-off { background: rgba(255,255,255,0.04); color: var(--text-tertiary); }

.tag { font-size: 10px; font-weight: 600; padding: 2px 9px; border-radius: 10px; }
.tag-ok { background: var(--success-dim); color: var(--success); }
.tag-off { background: rgba(255,69,58,0.12); color: var(--danger); }

.card.disabled { opacity: 0.55; }

.duo { display: flex; align-items: center; gap: 18px; }
.ring-box { display: flex; flex-direction: column; align-items: center; gap: 4px; flex-shrink: 0; }
.ring { position: relative; width: 68px; height: 68px; }
.ring svg { width: 68px; height: 68px; transform: rotate(-90deg); }
.rbg { fill: none; stroke: rgba(255,255,255,0.08); stroke-width: 5; }
.rfill { fill: none; stroke: var(--accent); stroke-width: 5; stroke-linecap: round; stroke-dasharray: 188.5; transition: stroke-dashoffset 0.7s ease; }
.ring.hot .rfill { stroke: var(--danger); }
.rval { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 16px; font-weight: 650; }
.rval em { font-size: 10px; font-weight: 400; color: var(--text-tertiary); font-style: normal; }
.rlabel { font-size: 11px; color: var(--text-secondary); }

.bar-box { flex: 1; min-width: 0; }
.bar-head { display: flex; justify-content: space-between; align-items: baseline; font-size: 11px; color: var(--text-secondary); margin-bottom: 5px; }
.bar-track { height: 8px; background: rgba(255,255,255,0.06); border-radius: 4px; overflow: hidden; }
.bar-fill { height: 100%; border-radius: 4px; min-width: 2px; transition: width 0.7s ease; background: linear-gradient(90deg, var(--accent), #5ac8fa, #30d158); }
.bar-fill.hot { background: linear-gradient(90deg, #ff6b35, #ff453a, #ff375f); }
.bar-fill.disk { background: linear-gradient(90deg, var(--accent), #5ac8fa, #30d158); }
.bar-pct { font-size: 10px; color: var(--text-tertiary); margin-top: 4px; display: block; }

/* ── Disk (physical grouping) ── */
.disk-card { padding-bottom: 12px; }
.disk-group {
  padding: 0 2px; margin-bottom: 18px;
}
.disk-group:last-child { margin-bottom: 0; }

/* Physical disk header */
.disk-group-head {
  display: flex; align-items: center; gap: 10px;
  padding: 6px 0 8px 4px;
  border-bottom: 1px solid var(--border-subtle);
  margin-bottom: 10px;
}
.disk-model {
  flex: 1; min-width: 0;
  font-size: 13px; font-weight: 550; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.disk-total {
  font-size: 11px; color: var(--text-tertiary); flex-shrink: 0;
  font-family: var(--font-mono);
}

/* Volume row */
.disk-vol {
  padding: 8px 0 8px 12px;
  border-bottom: 1px solid rgba(255,255,255,0.02);
}
.disk-vol:last-child { border-bottom: none; }
.disk-vol-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 6px;
}
.disk-vol-drive {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  font-family: var(--font-mono);
}
.disk-vol-usage {
  font-size: 11px; color: var(--text-secondary);
  font-family: var(--font-mono);
}

/* Gradient progress bar */
.disk-bar-track {
  height: 8px; background: rgba(255,255,255,0.06);
  border-radius: 4px; overflow: hidden;
}
.disk-bar-fill {
  height: 100%; border-radius: 4px; min-width: 2px;
  transition: width 0.7s ease;
}
.disk-bar-ok {
  background: linear-gradient(90deg, var(--accent), #5ac8fa, #30d158);
}
.disk-bar-hot {
  background: linear-gradient(90deg, #ff6b35, #ff453a, #ff375f);
}

/* ── Legacy disk fallback ── */
.disk-row { margin-bottom: 12px; }
.disk-row:last-child { margin-bottom: 0; }
.disk-drive { font-weight: 600; color: var(--text-primary); }
.bar-fill.disk { background: var(--success); }
.empty-hint { font-size: 12px; color: var(--text-tertiary); padding: 8px 0; }
</style>
