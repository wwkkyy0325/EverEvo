# Portable-Only Storage Refactoring

> **Goal**: Eliminate the dual-mode (Portable / User-mode AppData) split.  
> EverEvo is always distributed with source code for self-evolution; there is no standalone-EXE release scenario.  
> **All data lives under `{projectRoot}/data/` and `{projectRoot}/runtime/`. Zero AppData references.**

---

## 1. Motivation

### Current state

`internal/storage/storage.go` implements a **dual-mode** path resolution:

| Mode | Trigger | Data Dir | Runtime Dir |
|------|---------|----------|-------------|
| Portable | `go.mod` found → `ProjectRoot()` | `{root}/data/` | `{root}/runtime/` |
| User | no `go.mod` → standalone EXE | `%APPDATA%/EverEvo/` | `%APPDATA%/EverEvo/runtime/` |

There is also a **legacy migration** layer (`config.go:201-366`, `storage.go:214-257`) that copies data from `%APPDATA%` → portable layout, plus backward-compatibility `ModelsDir()` fallbacks checking multiple legacy paths.

### Why eliminate the User mode

1. **Self-evolution requires source code** — the app rewrites its own Go/Vue code then rebuilds via `build.ps1`. A standalone EXE cannot self-evolve.
2. **Only one distribution format** — `projectRoot/` with source + EXE. No installer, no "release" build.
3. **Dual-mode adds ~300 lines of dead code** across config migration, legacy path fallbacks, AppData helpers.
4. **Testing complexity** — two code paths means two bug surfaces.
5. **Best practice** (VS Code Portable, Notepad++ Portable, KeePassXC Portable): portability is a design choice, not a runtime toggle.

### What the web research confirms

From VS Code Portable Mode, KeePassXC, and the 2024–2025 portable app best practices:

- **Sentinel-based detection is design smell** — if you're always portable, you don't need detection.
- **One directory, everything inside** — executables, config, user data, logs, state all in one tree.
- **Self-evolving systems need an immutable kernel** (the bootstrapper/build system) and a mutable userland (the app code/data). The `storage` layer is the kernel — it should be simple and deterministic.
- **Clean uninstall by deleting one folder** — no registry, no AppData traces.

---

## 2. Current Architecture (for reference)

### 2.1 All 27 affected files

**Go backend (22 files):**

| File | What it does with storage paths |
|------|-------------------------------|
| `internal/storage/storage.go` | Core: dual-mode resolution, migration, legacy fallbacks |
| `internal/config/config.go` | Config load/save, `UserConfigDir()`, legacy LLM migration, full data migration |
| `internal/app/app.go` | Startup: log path, mode logging, `MigrateLegacyData()`, download history, plans restore, detect source dir |
| `internal/app/app_system.go` | System info paths, file ops |
| `internal/app/app_download.go` | Download history path |
| `internal/app/app_tools_control.go` | Tool control paths |
| `internal/app/app_taskboard.go` | Task board persistence |
| `internal/app/app_collab.go` | Collab session persistence |
| `internal/app/app_agent.go` | Agent persistence |
| `internal/zone/types.go` | Doc comment references `%APPDATA%` |
| `internal/zone/manager.go` | `ZonesDir()` → `RootAppDataDir()` |
| `internal/zone/ports.go` | `portRegistryPath()` → `RootAppDataDir()` |
| `internal/backends/backends.go` | Python detection: `appDataPath()` for portable Python |
| `internal/backends/python.go` | venv/create/run (no direct AppData, clean) |
| `internal/memory/store.go` | DB path via `storage.AppDataDir()` |
| `internal/memory/vector.go` | chromem path via `storage.AppDataDir()` |
| `internal/rag/store.go` | KB chromem path |
| `internal/wiki/wiki.go` | Wiki store path |
| `internal/skills/skill.go` | Skill config path |
| `internal/workflow/manager.go` | Workflow persistence path |
| `internal/evolve/persist.go` | Evolution persistence path |
| `internal/agents/agent.go` | Agent config path |

**Frontend (5 files):**

| File | What it uses |
|------|-------------|
| `frontend/src/components/SystemInfo.vue` | Displays data dir info |
| `frontend/src/components/Knowledge.vue` | Memory/knowledge stats |
| `frontend/src/api/zone.ts` | Zone API calls |
| `frontend/src/api/memory.ts` | Memory API calls |
| `frontend/src/api/knowledge.ts` | Knowledge API calls |

### 2.2 Current path resolution call chain

```
storage.AppDataDir()
  └─ storage.DataDir()
       ├─ ProjectRoot() != ""  → {root}/data/
       └─ ProjectRoot() == ""  → %APPDATA%/EverEvo/

storage.RootAppDataDir()
  └─ storage.DataDir()  (same as above)

storage.ModelsDir()
  ├─ RuntimeDir() + "models"
  ├─ legacy: %APPDATA%/EverEvo/models
  └─ legacy: {exeDir}/data/models

storage.RuntimeDir()
  ├─ ProjectRoot() != ""  → {root}/runtime/
  └─ ProjectRoot() == ""  → %APPDATA%/EverEvo/runtime/

config.UserConfigDir()
  └─ storage.AppDataDir() → .../data/zones/production/

zone.ZonesDir()
  └─ storage.RootAppDataDir() + "zones"

backends.appDataPath()
  └─ os.Getenv("APPDATA") || USERPROFILE/AppData/Roaming
```

---

## 3. Target Architecture

### 3.1 Always-portable path model

```
{projectRoot}/                    ← found by walking up from EXE to go.mod
├── main.go
├── go.mod
├── data/                         ← protected (zone-scoped)
│   ├── zones/
│   │   └── production/           ← default zone (EVEREVO_ZONE overrides)
│   │       ├── config.json
│   │       ├── agents.json
│   │       ├── memory/           ← SQLite memory.db + chromem
│   │       ├── knowledge/        ← RAG chromem
│   │       ├── wiki/             ← wiki chromem
│   │       ├── workflows/
│   │       ├── skills/
│   │       ├── plans.json
│   │       ├── download_history.json
│   │       ├── taskboard.json
│   │       ├── port_registry.json
│   │       └── EverEvo.log
│   ├── zones/alpha/              ← experiment zone (EVEREVO_ZONE=alpha)
│   └── zones/backup-*/           ← backup zones
├── runtime/                      ← rebuildable (shared across zones)
│   ├── models/
│   ├── plugins/
│   ├── downloads/
│   ├── guides/
│   ├── cache/
│   └── python/                   ← portable Python installation
├── build/
│   └── bin/
│       └── everevo.exe
└── plugins/                      ← dev workspace (git tracked, source only)
```

### 3.2 Simplified storage API

```go
// Always finds go.mod from EXE dir or CWD. Panics if not found (not a portable install).
func ProjectRoot() string    // panics if "" — caller must handle
func DataDir() string         // {root}/data
func RuntimeDir() string      // {root}/runtime
func AppDataDir() string      // {root}/data/zones/{zone}
func ModelsDir() string       // {root}/runtime/models   (no legacy fallbacks)
func PluginsDir() string      // {root}/runtime/plugins
func DownloadsDir() string    // {root}/runtime/downloads
func GuidesDir() string       // {root}/runtime/guides
func CacheDir() string        // {root}/runtime/cache
func PythonDir() string       // {root}/runtime/python  (NEW: replaces backends.appDataPath)
func ZonesDir() string        // {root}/data/zones
func PortRegistryPath() string // {root}/data/port_registry.json
func EnsureDirs() error       // simplified, no migration
```

### 3.3 What gets REMOVED

| Function / Code | Reason |
|----------------|--------|
| `IsPortable()` | Always true by construction |
| `walkUpToGoMod()` | Still needed by `ProjectRoot()` |
| `appDataRoot()` | No more AppData |
| `OldAppDataDir()` | No legacy to migrate from |
| `MigrateLegacyData()` | No migration needed in portable-only world |
| `migrateDir()` / `migrateFile()` / `copyDir()` | Migration helpers |
| `ModelsDir()` legacy fallback blocks | No legacy paths |
| `RootAppDataDir()` | Fold into `DataDir()` directly |
| All of `config.go` migration code: `migrateLegacyConfig()`, `migrateAllData()`, `moveOrCopyPath()`, `copyAndDelete()`, `dirSizeBytes()`, `dirHasFiles()`, `isDirEmpty()`, `backupZoneData()`, `migrateOldLLM()` | ~170 lines of dead code |
| `backends.appDataPath()` | Use `storage.PythonDir()` |
| `backends.detectPython()` AppData portable python path | Use `storage.PythonDir()` |
| `app.go` mode log message ("运行模式: 便携版/用户模式") | Always portable |
| `app.go` `storage.MigrateLegacyData()` call | No migration |
| `internal/zone/types.go` doc comment `%APPDATA%` | Update to project-relative |

---

## 4. Implementation Plan

### Step 1: Simplify `internal/storage/storage.go` (core change)

**Remove:**
- `IsPortable()` function
- `appDataRoot()` function  
- `OldAppDataDir()` function
- `MigrateLegacyData()` function
- `migrateDir()` / `migrateFile()` / `copyDir()` helpers
- `RootAppDataDir()` → inline into callers
- `ModelsDir()` legacy fallback loops (lines 127-135)
- `RuntimeDir()` user-mode branch
- `DataDir()` user-mode branch

**Add:**
- `PythonDir()` → `{root}/runtime/python`

**Simplify:**
- `ProjectRoot()` — still walks up to find go.mod; but if not found, log warning and use CWD (graceful degrade for `go run` / `wails dev`)
- `EnsureDirs()` — add `PythonDir()`

**Lines of code change: ~80 removed, ~15 added. Net: ~65 removed.**

### Step 2: Simplify `internal/config/config.go`

**Remove:**
- `GlobalPath()` `RootAppDataDir()` fallback
- `UserConfigDir()` AppData fallback
- `migrateLegacyConfig()` (~40 lines)
- `migrateAllData()` (~30 lines)
- `moveOrCopyPath()` (~10 lines)
- `copyAndDelete()` (~30 lines)
- `dirSizeBytes()` (~10 lines)
- `dirHasFiles()` (~5 lines)
- `isDirEmpty()` (~5 lines)
- `backupZoneData()` (~10 lines)
- `migrateOldLLM()` (~30 lines)
- `Load()` migration call (line 174)

**Lines of code change: ~170 removed, ~0 added. Net: ~170 removed.**

### Step 3: Simplify `internal/zone/`

**`types.go`:**
- Update package doc comment: `%APPDATA%/EverEvo/zones/<name>/` → `{projectRoot}/data/zones/<name>/`

**`manager.go`:**
- `ZonesDir()` → use `storage.DataDir() + "/zones"` directly (no `RootAppDataDir()`)

**`ports.go`:**
- `portRegistryPath()` → `filepath.Join(storage.DataDir(), "port_registry.json")`

**Lines of code change: ~5 lines simplified.**

### Step 4: Simplify `internal/backends/backends.go`

**Replace:**
- `appDataPath()` → `storage.PythonDir()`
- `detectPython()` portable python path: `%APPDATA%/EverEvo/python/` → `{root}/runtime/python/`

**Lines of code change: ~10 changed.**

### Step 5: Simplify `internal/app/app.go`

**Change:**
- Mode log: remove dual-mode message → `log.Printf("数据目录: %s", storage.DataDir())`
- Remove `storage.MigrateLegacyData()` call (line 190)
- `detectEmbeddingModelDir()` — already uses project-relative paths, no change needed

**Lines of code change: ~5 removed.**

### Step 6: Clean up remaining `RootAppDataDir()` callers

Search shows `RootAppDataDir()` used in:
- `internal/zone/manager.go:ZonesDir()` → already addressed in Step 3
- `internal/zone/ports.go:portRegistryPath()` → already addressed in Step 3
- `internal/config/config.go:GlobalPath()` → addressed in Step 2

Also check `storage.AppDataDir()` callers for any AppData fallback patterns:

All callers (`app.go`, `app_system.go`, `app_download.go`, `app_tools_control.go`, `app_taskboard.go`, `app_collab.go`, `app_agent.go`, `memory/store.go`, `memory/vector.go`, `rag/store.go`, `wiki/wiki.go`, `skills/skill.go`, `workflow/manager.go`, `evolve/persist.go`, `agents/agent.go`) use `storage.AppDataDir()` as the zone-scoped data directory. Since `AppDataDir()` will now always return `{root}/data/zones/{zone}`, these callers **need zero changes** — the simplification is transparent.

### Step 7: Frontend cleanup

The 5 frontend files reference data paths only via API calls. The backend API response already uses resolved paths. No frontend changes needed unless UI displays "AppData" in labels/text. Check:
- `SystemInfo.vue` — if it labels paths as "AppData", update to "项目数据目录"
- Other files — verify no hardcoded "AppData" mention

**Lines of code change: ~5 (cosmetic label updates).**

### Step 8: Update docs

- `docs/llmwiki/design.md` — update architecture diagram, remove "User mode" mention
- `plugins/README.md` — update runtime path table if it mentions AppData
- `README.md` — if it mentions AppData

---

## 5. Risk & Rollback

### Risk assessment

| Risk | Level | Mitigation |
|------|-------|-----------|
| Breaking existing user data at `%APPDATA%/EverEvo/` | Low | The production zone already lives at `data/zones/production/`. The migration code already moved data on previous launches. Users running portable mode (which is all users) already have data in `data/`. |
| `ProjectRoot()` returns "" during `wails dev` (EXE in temp dir) | Low | Already handled: `ProjectRoot()` falls back to `os.Getwd()` which points to the project root in dev mode. |
| `ProjectRoot()` returns "" for `go run` | Low | Same CWD fallback. |
| Breaking the `cmd/test_onnx` tool | None | That tool is standalone and doesn't use `storage` package. |

### Rollback plan

1. This is a pure simplification — no data migration, no new directories.
2. If `ProjectRoot()` fails, the app fails fast with a clear error — easy to debug.
3. Git revert the commit to restore dual-mode.

---

## 6. Step-by-step execution checklist

- [ ] **Step 1**: Rewrite `internal/storage/storage.go` — remove dual-mode, migration, legacy fallbacks
- [ ] **Step 2**: Simplify `internal/config/config.go` — remove all migration code
- [ ] **Step 3**: Simplify `internal/zone/types.go`, `manager.go`, `ports.go` — remove `RootAppDataDir()` indirection
- [ ] **Step 4**: Update `internal/backends/backends.go` — use `storage.PythonDir()`
- [ ] **Step 5**: Clean `internal/app/app.go` — remove mode logging, migration call
- [ ] **Step 6**: Verify all `AppDataDir()` callers work without changes
- [ ] **Step 7**: Frontend label check
- [ ] **Step 8**: Update docs
- [ ] **Verify**: `go build ./...` compiles clean
- [ ] **Verify**: `go vet ./...` passes
- [ ] **Verify**: `wails build` produces working EXE
- [ ] **Verify**: `.\build.ps1 dev` starts correctly
- [ ] **Verify**: Data created under `data/` and `runtime/`, NOT under `%APPDATA%/`

---

## 7. References

- VS Code Portable Mode: https://code.visualstudio.com/docs/editor/portable
- Portable App Design Best Practices (2024–2025): 1-directory self-contained, sentinel-free, SQLite + flat files hybrid
- Self-evolving AI patterns: immutable kernel (build system + storage layer) + mutable userland (app code/data); git snapshots for rollback safety
- KeePassXC Portable: single-directory, encrypted SQLite, zero system traces
