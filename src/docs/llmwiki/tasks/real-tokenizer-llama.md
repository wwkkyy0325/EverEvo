# Task: Real tokenizer (ONNX) + real llama.cpp generation

## Phase 1 — Tokenizer + ONNX real embedding (DONE, verified)

**Problem:** running MiniLM errored with token-index-out-of-bounds — raw text bytes were fed as int64 token ids.

**Fix:**
- [x] New `internal/tokenizer/tokenizer.go` wrapping `github.com/sugarme/tokenizer` (loads `tokenizer.json`, BERT WordPiece, outputs the input triple, truncate 128).
- [x] `internal/backends/onnx/session.go`: `Run` → `RunInt64(map[string][]int64)` (distinct int64 tensor per input name).
- [x] `internal/model/onnx_runner.go`: per-model tokenizer; `Run` = tokenize → `RunInt64` → attention-mask-weighted mean-pool → `embedding[384]: [...]`.
- [x] `internal/backends/onnx/repro_test.go`: rewritten to tokenize real text.
- [x] Verified: `TestONNXRunnerEndToEnd` → `embedding[384]: [0.2252, -0.3291, …]`, no crash.

## Phase 2 — Real llama.cpp generation (BLOCKED → stubbed)

**Problem:** `llama.go`/`LlamaModel.Run` were a placeholder; real generation wanted.

**Blocker — no usable Go binding on Windows:**
- [x] `develerltd/go-llama-pure`: Windows build fails (`purego.Dlopen` is Unix-only).
- [x] `dianlight/gollama.cpp` v0.1.0: `Decode` is "not implemented for non-Darwin" (macOS-only at runtime).
- [x] `tcpipuk/llama-go`: go-get module lacks `libbinding.a` + `llama.cpp` source; needs `clone + make libbinding.a` (C++ build).
- [x] Reverted `internal/backends/llama/llama.go` to a stub (clear "not implemented" error, no crash). `LlamaModel.Run` reports unavailable.

**Path forward (future):**
- `tcpipuk` + pre-built `libbinding.a` (clone repo, `make libbinding.a` or Dockerfile.build, place `.a` in module cache). Heavy build; wails integration non-trivial.
- Or wait for a Windows-complete pure-Go llama binding.

## Out of scope
- Multi-input correctness beyond MiniLM's standard BERT triple.
- llama.cpp on Windows (blocked on Go ecosystem).
