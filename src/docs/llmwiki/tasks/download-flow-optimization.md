# Download Flow Optimization

> **Status: ✅ COMPLETE** — All 5 phases implemented 2025-07-05.

## Current Architecture & Problems

### Architecture Overview

```
Frontend (Vue 3)                          Backend (Go)
─────────────                             ────────────
ModelCatalog ──DownloadModelFile──►  app.go
  │                                    │
  │  dlState (file-level progress)     startDownload()
  │                                    │
  │  EventsOn('download-progress')     dlManager.Start()
  │         ◄──────────────────────── emitter
  │                                    
DownloadCenter ──GetDownloadTasks──►  dlManager.List()
  │  (1s polling)                     
  │  EventsOn('download-progress')    
  │         ◄──────────────────────── 
  │                                    
MyModels ──GetDownloadTasks────────►  dlManager.List()
  │  (1s polling, embedded tab)       
  │  EventsOn('download-progress')    
  │         ◄──────────────────────── 
  │                                    
App.vue ──EventsOn('download-progress')──► toast notifications
```

### Root Problems (Pre-Fix)

| # | Problem | Root Cause | Impact |
|---|---------|-----------|--------|
| 1 | Progress bar flickers 0↔real | `totalWritten()` returned 0 for `tryDownload` (no segments). 1s polling replaced event-driven progress | DownloadCenter shows flickering progress |
| 2 | Pause→Resume restarts download | `tryDownload` wrote to `DestPath` directly, no `.part`, no `saveMeta()`. On resume → start over | Can never resume ModelScope downloads |
| 3 | File tree progress bar always grey | ModelScope HEAD→405→tryDownload→ContentLength=-1→pct=0→fill width=0% | File tree shows no visual progress |

### Additional Issues Identified

| # | Problem | Details |
|---|---------|---------|
| 4 | Data race in `tryDownload` | Local variable `written` read/written by 2 goroutines without synchronization |
| 5 | Download state duplicated 4× | ModelCatalog, DownloadCenter, MyModels, App.vue each maintain independent download state |
| 6 | EventsOn/EventsOff pattern duplicated | 3 components copy-paste the same event binding/unbinding logic |
| 7 | No download queue/priority | Files download in parallel with no user-visible ordering |
| 8 | Error retry is manual only | Failed downloads don't auto-retry, even on transient network errors |
| 9 | No download speed limit | Multiple concurrent downloads can saturate bandwidth |

---

## Fixes Applied (2025-07-05)

### Backend: `internal/downloader/downloader.go`

#### Fix 1: Written tracking for non-segmented downloads
```
Task struct: added `written int64` (atomic) field
totalWritten(): returns atomic written when len(segs)==0
tryDownload(): writes to t.written atomically instead of local var
```
→ `info()` now returns correct pct for both segmented and tryDownload paths.

#### Fix 2: tryDownload resume support
```
tryDownload():
  - Writes to .part file (not DestPath directly)
  - Checks .part existence on start → tries Range resume (HTTP 206)
  - Falls back to full GET if server doesn't support Range
  - saveMeta() on each tick + on pause
  - On completion: delete .meta, rename .part → DestPath

saveMeta(): now persists `written` field alongside segments
loadMeta(): restores `written` but only returns true for segmented data
run(): after loadMeta() fails, checks for orphan .part → routes to tryDownload
```
→ Paused downloads can now resume from where they left off.

#### Fix 3: Data race eliminated
```
Local `written` var in tryDownload → replaced with atomic `t.written`
Local `speedCounter` → already atomic (was correct)
```

### Frontend Changes

#### Fix 4: Event-driven active tasks (no polling interference)

**DownloadCenter.vue + MyModels.vue:**
- `refreshActive()` / `refreshDLActive()` → `refreshHistory()` / `refreshDLHistory()`
- Timer now only refreshes **history** list (completed/failed)
- Active tasks are **entirely event-driven** — no polling replaces event progress

```
Before: timer → refreshActive() → active.value = server data (overwrites events)
After:  timer → refreshHistory() → history.value = server data (non-overlapping)
         events → applyProgress() → active.value[item] updated
```

#### Fix 5: File tree indeterminate progress

**FileTreeNode.vue:**
- When `pct === 0` but `active === true`: shows 12% animated bar + blinking ↓ icon
- When `pct > 0`: normal percentage fill bar
- CSS: `ftn-indeterminate` with sliding gradient animation, `ftn-pct-live` blink

---

## Recommended Full Optimization Roadmap

### Phase A: Download State Unification (Priority: HIGH)

**Goal**: Single source of truth for download state. Eliminate duplicated event listeners, polling, and state.

**Current state**: 4 components independently manage download state.
- App.vue: toast only
- DownloadCenter.vue: active[], history[], EventsOn, timer
- MyModels.vue: dlActive[], dlHistory[], EventsOn, timer  
- ModelCatalog.vue: dlState{}, EventsOn (no timer)

**Target architecture**:
```
src/stores/downloadStore.ts    ← SINGLE EventsOn('download-progress') listener
  │
  ├── reactive activeTasks: Map<id, TaskInfo>
  ├── reactive historyTasks: TaskInfo[]
  ├── reactive fileProgress: Map<filename, FileProgress>  // for file tree
  ├── startPolling() / stopPolling()  // single 1s history refresh
  │
  ▼
All components read-only from store:
  - App.vue → store.completedToast (watch for new completions)
  - DownloadCenter.vue → store.activeTasks + store.historyTasks
  - MyModels.vue → store.activeTasks + store.historyTasks
  - ModelCatalog.vue → store.fileProgress
```

**Implementation**:
1. Create `src/stores/downloadStore.ts` with Pinia
2. Move all EventsOn/download logic into the store
3. Components become pure consumers
4. Remove all per-component timers and event listeners

**Benefit**: No flickering, no duplicates, no missed events, ~200 lines removed from components.

---

### Phase B: Download Queue & Concurrency (Priority: MEDIUM)

**Goal**: User-visible ordering, configurable concurrency, bandwidth awareness.

**Current state**: `DownloadSelectedFiles` launches all downloads in parallel goroutines. No limit.

**Target**:
```
downloadStore (frontend)          downloader.Manager (backend)
  │                                    │
  ├── queue: Task[]                    ├── maxConcurrent: 3
  ├── startNext(): dequeue → start     ├── queue: pending tasks
  │                                    ├── startNext(): dequeue → Start()
  │                                    └── onComplete(): startNext()
  └── UI: shows "3 downloading, 5 queued"
```

**Implementation**:
1. Backend: Add `maxConcurrent` config to Manager; queue excess tasks
2. Frontend: Show queue position in DownloadCenter ("排队中 (第 3 位)")
3. Add "Move to top" button for priority reordering

---

### Phase C: Auto-Retry & Error Recovery (Priority: MEDIUM)

**Goal**: Transient failures recover automatically.

**Current state**: Failed downloads stay failed until user clicks Retry. Network blips = permanent failure.

**Target**:
```
taskConfig:
  maxRetries: 3
  retryDelay: [5s, 30s, 2m]     // exponential backoff
  retryableErrors: ["timeout", "connection reset", "503", "429"]
```

**Implementation**:
1. Backend: Add retry config to Task; auto-retry on retryable errors
2. Frontend: Show "重试中 (2/3)..." status with countdown
3. `RetryDownload` API for manual retry (already exists)

---

### Phase D: Smart File Tree Progress (Priority: LOW)

**Goal**: Per-file real-time progress in ModelCatalog file tree without flickering.

**Current state**: `dlState` in ModelCatalog is updated by `download-progress` event. Progress bar works after fixes but has no size/ETA display.

**Target**:
```
FileTreeNode (download state):
  ┌──────────────────────────────────────────────┐
  │ ☑ 📄 model.safetensors  12.3 MB / 4.2 GB    │
  │   ▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░  28%  · 2.1 MB/s    │
  │   剩余 3 分 15 秒                              │
  └──────────────────────────────────────────────┘
```

**Implementation**:
1. Add `written`, `total`, `speed` to `dlState` entries
2. FileTreeNode shows size/speed/ETA when downloading
3. Mini progress bar stays compact (current design), details on hover

---

### Phase E: Use `useEvents` Composable (Priority: LOW)

**Goal**: Eliminate copy-pasted EventsOn/EventsOff boilerplate.

**Current state**: 3 components repeat:
```ts
let _onDLProgress = null
onMounted(() => {
  _onDLProgress = (data) => { ... }
  window.runtime.EventsOn('download-progress', _onDLProgress)
})
onBeforeUnmount(() => {
  if (_onDLProgress) { EventsOff(...); _onDLProgress = null }
})
```

**Target**:
```ts
// src/composables/useEvents.ts
export function useEvents(event: string, handler: (data: any) => void) {
  onMounted(() => window.runtime.EventsOn(event, handler))
  onBeforeUnmount(() => { try { window.runtime.EventsOff(event, handler) } catch {} })
}
```

```ts
// In components:
useEvents('download-progress', applyProgress)
```

---

## Summary: State After Fixes

| Metric | Before | After |
|--------|--------|-------|
| Progress bar accuracy | Flickers 0↔real for tryDownload | Stable, correct for all paths |
| Pause/Resume | Restarts from 0 for tryDownload | Resumes via Range (or restarts gracefully) |
| File tree progress | Always grey (0% fill) | Shows % when total known, animated ↓ when unknown |
| Data race | `written` in tryDownload unprotected | Atomic int64 |
| Polling interference | Timer overwrites event progress | Timer only refreshes history |
| Meta persistence | Segments-only | Segments + tryDownload written bytes |

---

## Verification Checklist

- [x] `go build` passes
- [x] `npm run build` passes
- [ ] Download a file from ModelScope — progress bar shows real % (not flickering)
- [ ] Download a file from HuggingFace — multi-segment progress works
- [ ] Pause then resume a ModelScope download — continues from where paused
- [ ] Pause then resume a HuggingFace download — continues from where paused
- [ ] File tree in ModelCatalog shows colored fill + speed (not just grey track)
- [ ] DownloadCenter progress doesn't flicker between 0 and real value
- [ ] MyModels embedded download tab works correctly
- [ ] Download completion toast appears exactly once
- [ ] Download history persists across app restart
- [ ] Queue: 4+ simultaneous downloads → first 3 start, rest show "排队中"
- [ ] Auto-retry: network error → "重试 1/3 (5s 后)" → auto-restarts
- [ ] Nav bar download count badge reads from store directly
