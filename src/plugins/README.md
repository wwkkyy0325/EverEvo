# plugins

Plugin development workspace — write and test plugin source code here before installing.

## Directory layout

```
plugins/
├── hello-plugin/         # example: a simple echo plugin
│   ├── manifest.json     # plugin metadata + runtime config
│   └── main.py           # entry point (JSON-RPC I/O loop)
└── my-custom-tool/       # your plugin
    ├── manifest.json
    └── entry.py
```

## Plugin manifest

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "type": "tool",
  "description": "What this plugin does",
  "runtime": "python",
  "entry": "main.py",
  "env": "venv",
  "methods": ["health", "info", "my_method"]
}
```

- `runtime`: `python` | `go` | `node` | `""` (compiled binary)
- `env`: `venv` (uses plugin-local venv), or `""` (system interpreter)
- `entry`: relative path to the script / binary that implements the JSON-RPC I/O loop

## How to install a plugin

1. Develop your plugin in this directory.
2. In EverEvo, go to **插件** → "安装插件" → select your plugin folder (or a `.zip` of it).
3. The plugin manager copies it to `data/plugins/<name>/` and starts it.

Or via command line / AI agent:
```
plugin_install(path="plugins/my-custom-tool")
plugin_start("my-custom-tool")
plugin_run("my-custom-tool", method="health")
```

## Runtime paths

| Purpose | Path |
|---------|------|
| Development workspace | `plugins/<name>/` (this directory) |
| Installed plugins | `data/plugins/<name>/` (beside the EXE at runtime) |
| Temp (during install) | `data/plugin-tmp/` |

`data/plugins/` is what `plugin.ScanPlugins` reads at startup to populate the plugin list.
