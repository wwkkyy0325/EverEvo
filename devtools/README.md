# devtools

Development tools, test scripts, and utilities for EverEvo. **Nothing in this directory is part of the shipped application.**

## Conventions

- Test scripts: `test-<name>.py` / `test-<name>.bat` / `test-<name>.ps1`
- One-off utilities: `util-<purpose>.py`
- MCP server quick-tests: `mcp-test-<server>.json`

## Examples

- `test-web-search.bat` — quick DuckDuckGo search test
- `test-mcp-handler.py` — send handcrafted JSON-RPC requests to an MCP server
- `util-model-info.py` — dump ONNX model metadata

## What does NOT belong here

- Unit / integration tests: those live under `internal/*_test.go` or `frontend/src/__tests__/`.
- Plugin source code: that belongs in `plugins/<name>/` (development) or `data/plugins/<name>/` (installed).
- Build scripts: `build.ps1` / `Makefile` at the project root.
