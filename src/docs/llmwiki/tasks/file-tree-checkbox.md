# Task: Model-catalog file tree — tri-state checkbox & blue-bg fix

> Fixed 2026-06-29. See `changelog.md` entry of the same date.

## Problem

In the model-market (ModelCatalog) "文件列表" tab, the recursive file-tree checkboxes had chaotic state:

- wrong glyph for the state (empty / check / dash didn't match actual selection);
- the root row's blue background spread across the whole tree and looked bad.

## Root causes

- [x] **Tri-state logic.** The checkbox mixed a declarative `:checked` binding with an imperative `ref.indeterminate` (kept in sync via `watch` + `mounted` + `updated`). In a recursive tree under `nextTick` batching the two update paths desync → wrong states. Parent `ModelCatalog` blacklist semantics were correct.
- [x] **Blue background.** Wrapper `<div class="ftn-root">` (every node) and root row `:class="{'ftn-root': isRoot}"` shared the class `.ftn-root`; its `background: rgba(0,122,255,.06)` tinted every level and stacked.

## Changes

- [x] Rewrite `FileTreeNode.vue` checkbox as a pure declarative tri-state (`all`/`some`/`none`) computed `checkState` + SVG glyphs; drop all imperative DOM. — verify: `npm run build` passes; states are pure functions of `checkedFiles`.
- [x] Rename wrapper → `.ftn-wrap` (no bg); root row → `.ftn-root-row` (restrained highlight). — verify: no `.ftn-root` clash remains (grep).
- [x] Beautify (frontend-design skill, refined minimalism within iOS-dark palette).
- [x] Verify build: `npm run build` → ✓ 32 modules, 1.28s.
- [x] Update `changelog.md`.

## Follow-up (done 2026-06-29): unify both trees + fix expand/collapse jitter

User asked to apply the same restrained highlight to `PackageTreeNode` too (keep both trees consistent) and fix a "twitch" on expand/collapse.

- [x] `PackageTreeNode` `.ptn-lvl0` blue wash → restrained highlight (matches `.ftn-root-row`); row min-height 30 → 34.
- [x] Jitter root cause: scrollbar-gutter jitter (expand/collapse flips the scrollbar on/off → content width changes → flex rows jump). Fix: `scrollbar-gutter: stable` on `.main` (App.vue), `.detail-inner`, `.dp-tab-content` (ModelCatalog.vue).
- [x] Both trees' expand arrows: `▾`/`▸` char-swap → single `▸` + `rotate(90deg)` (constant glyph ⇒ no size change on toggle; matches `MyModels` pkg-head).
- [x] `npm run build` ✓.
