# Task: ONNX load crash — replace syscall binding with yalue/onnxruntime_go

> Fixed 2026-06-29. See `changelog.md` entry of the same date.

## Problem

Clicking 加载 on an ONNX model in "我的模型" crashed the app instantly (闪退),
with nothing in `EverEvo.log`.

## Root cause (confirmed by repro)

The hand-written `syscall` ORT C-API binding (`internal/backends/onnx/capi.go`)
had fatal ABI defects causing a C-level access violation that kills the process
(Go `recover` can't catch it; the SIGSEGV stack goes to stderr, which the EXE
doesn't capture, so it was silent):

- [x] **uintptr-GC trap** in `strPtr`/`strToBytes` — primary crash cause and the
  reason the crash location was non-deterministic.
- [x] Wrong `OrtApi` vtable indices (`Run=21` vs real `10`, `CreateSessionOptions=45`
  vs real `~11`, …).
- [x] Missing `OrtAllocator*` arg in `SessionGetInputName`/`SessionGetOutputName`.
- [x] Wrong arity in `CreateMemoryInfo` (5-arg function called with 3 args).

A minimal repro test calling `onnx.LoadModel` directly captured the stack:
`Exception 0xc0000005 … onnx.createEnv capi.go:136`.

## Fix

- [x] `go get github.com/yalue/onnxruntime_go` (v1.31.0, pins `ORT_API_VERSION 26`).
- [x] Delete `internal/backends/onnx/capi.go`.
- [x] New `internal/backends/onnx/onnx.go` — idempotent `Init(dllPath)`/`Close()`
  over `ort.SetSharedLibraryPath` + `ort.InitializeEnvironment`/`DestroyEnvironment`.
- [x] Rewrite `internal/backends/onnx/session.go` on yalue (`DynamicAdvancedSession`,
  `GetInputOutputInfo`, dtype-aware `CustomDataTensor`, real-sized output via typed
  `Tensor[T]` / `CustomDataTensor.GetData`). Kept exported `Init/LoadModel/Session.Run/Close/TensorInfo`.
- [x] `internal/model/onnx_runner.go` — drop hardcoded `float32 [1,len/4]` shape;
  pass `nil` so the session derives shape/dtype from metadata.
- [x] `app.go` — `shutdown` calls `onnx.Close()` after `manager.Shutdown()`.
- [x] Bundle ONNX Runtime 1.26 DLL: `third_party/onnxruntime/win-x64/onnxruntime.dll`
  + `Bundle-Runtime` step in `scripts/build.ps1` (all/build/package).
- [x] Repurposed `repro_test.go` → `TestLoadAndRun` smoke test.

## Verification

- [x] `go build ./...` ✓ (first CGo compile; gcc present).
- [x] `go test -run TestLoadAndRun ./internal/backends/onnx/` ✓ — loads MiniLM
  (3 inputs: input_ids/attention_mask/token_type_ids, 1 output), runs inference,
  output 3072 B = `[1,2,384]` float32. No crash.
- [ ] End-to-end via `scripts/build.ps1 build` + click 加载 in the app (deferred — same code
  path as the passing test).

## Follow-ups (out of scope)

- Multi-input Run correctness: all inputs currently fed the same bytes (fine for
  "doesn't crash"; not semantically correct for real inference).
- `design.md`'s Rust-engine/`internal/bridge` CGo architecture is still unimplemented;
  the only CGo today is yalue/onnxruntime_go.
