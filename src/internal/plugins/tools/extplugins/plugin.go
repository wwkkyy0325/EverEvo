// Package extplugins manages external plugins (RPC-based tool plugins written
// in Python, Go, or Node.js). It implements core.ToolPlugin so external plugin
// management operations are available as LLM-callable tools.
//
// The App wires itself as an ExtPluginDelegate during startup; all heavy lifting
// (process lifecycle, RPC, health checks) lives here, not in the App struct.
package extplugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"everevo/internal/core"
	"everevo/internal/plugin"
	"everevo/internal/storage"
)

const pluginID = "extplugins"

// ── Delegate interface ──────────────────────────────────────────────────

// ExtPluginDelegate abstracts App-level dependencies the plugin needs.
// The App implements this and wires itself via SetExtPluginDelegate.
type ExtPluginDelegate interface {
	// EmitChanged notifies the frontend that plugin state changed.
	EmitChanged(event, action, id string)
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin manages external plugin lifecycle, RPC, and code-based creation.
// It implements core.ToolPlugin for LLM-facing tools.
type Plugin struct {
	delegate ExtPluginDelegate
	host     *plugin.Host
	client   *plugin.Client
	mu       sync.Mutex
}

var _ core.ToolPlugin = (*Plugin)(nil)

// SetExtPluginDelegate wires the App-level delegate. Called once at startup.
func SetExtPluginDelegate(d ExtPluginDelegate) {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return
	}
	if plug, ok := p.(*Plugin); ok {
		plug.delegate = d
	}
}

// Get returns the registered Plugin instance, or nil if not registered yet.
func Get() *Plugin {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return nil
	}
	plug, _ := p.(*Plugin)
	return plug
}

func init() {
	core.GlobalTools.Register(pluginID, &Plugin{}, core.PluginManifest{
		ID:          pluginID,
		Name:        "外部插件管理",
		Version:     "1.0",
		Description: "plugin_list/status/start/stop/restart/run/install/delete/logs/create — 外部插件生命周期管理与RPC调用",
		Author:      "EverEvo",
		Type:        "toolset",
	})
}

func (p *Plugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID:          pluginID,
		Name:        "外部插件管理",
		Version:     "1.0",
		Description: "External plugin lifecycle management, RPC execution, and code-based creation (Python/Go/Node)",
		Author:      "EverEvo",
		Type:        "toolset",
	}
}

// ToolDefs returns the LLM tool schemas for all external-plugin operations.
func (p *Plugin) ToolDefs() []core.ToolDef {
	return []core.ToolDef{
		{
			Name:        "plugin_list",
			Description: "列出所有已安装的外部插件，包括名称、版本、类型、运行时、暴露的方法列表等",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "plugin_status",
			Description: "查询指定插件的运行状态：是否运行中、PID、启动时间、错误信息。不指定名称则返回所有插件状态",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "插件名称（可选，不传则返回所有)"},
				},
			},
		},
		{
			Name:        "plugin_start",
			Description: "启动指定插件进程，使其进入运行状态并可以接收 RPC 调用",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "插件名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "plugin_stop",
			Description: "停止指定插件的运行进程（优雅关闭，3 秒超时后强制终止）",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "插件名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "plugin_restart",
			Description: "重启指定插件（先停止再启动）",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "插件名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "plugin_run",
			Description: "调用插件的指定方法，传入 JSON 参数并获取返回结果。可用方法见插件列表中的 methods 字段",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "description": "插件名称"},
					"method": map[string]any{"type": "string", "description": "要调用的方法名"},
					"params": map[string]any{"type": "object", "description": "方法参数，JSON 对象格式"},
				},
				"required": []string{"name", "method"},
			},
		},
		{
			Name:        "plugin_install",
			Description: "从 .zip 压缩包或目录路径安装一个新外部插件",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "插件 .zip 文件或目录的绝对路径"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "plugin_delete",
			Description: "卸载指定插件（如果正在运行会先停止，然后删除整个插件目录）",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "要删除的插件名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "plugin_logs",
			Description: "获取指定插件最近的 stderr 日志输出（环形缓冲区，最多 64KB）",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "插件名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "plugin_create",
			Description: "用代码创建新插件并热加载。支持三种运行时：python（默认，自动 venv 隔离）、go（编译为 EXE）、node（Node.js）。Agent 提供名称+代码+运行时，系统自动生成模板、安装、热启动",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "插件名称（小写字母+连字符）"},
					"runtime":      map[string]any{"type": "string", "description": "运行时: python（默认）| go | node"},
					"description":  map[string]any{"type": "string", "description": "插件功能描述"},
					"code":         map[string]any{"type": "string", "description": "插件源码。python: def handle(method,params); go: package main+handler; node: async function handle(method,params)"},
					"methods":      map[string]any{"type": "string", "description": "逗号分隔的方法名，默认 health,info"},
					"dependencies": map[string]any{"type": "string", "description": "python: pip 包名; node: npm 包名"},
					"autoStart":    map[string]any{"type": "boolean", "description": "安装后是否立即热启动（默认 true）"},
				},
				"required": []string{"name", "code"},
			},
		},
	}
}

// CallTool dispatches a plugin management tool call.
func (p *Plugin) CallTool(_ context.Context, name string, args map[string]any) (core.ToolResult, error) {
	switch name {
	case "plugin_list":
		specs, err := p.ListPlugins()
		if err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: specs}, nil

	case "plugin_status":
		pluginName := getStr(args, "name")
		if pluginName != "" {
			return core.ToolResult{Success: true, Data: p.GetPluginStatus(pluginName)}, nil
		}
		return core.ToolResult{Success: true, Data: map[string]any{
			"all":  p.ListStatus(),
			"hint": "未指定 name，返回所有插件状态",
		}}, nil

	case "plugin_start":
		pluginName := getStr(args, "name")
		if err := p.StartPlugin(pluginName); err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: map[string]string{"started": pluginName}}, nil

	case "plugin_stop":
		pluginName := getStr(args, "name")
		if err := p.StopPlugin(pluginName); err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: map[string]string{"stopped": pluginName}}, nil

	case "plugin_restart":
		pluginName := getStr(args, "name")
		if err := p.RestartPlugin(pluginName); err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: map[string]string{"restarted": pluginName}}, nil

	case "plugin_run":
		pluginName := getStr(args, "name")
		method := getStr(args, "method")
		if pluginName == "" || method == "" {
			return core.ToolResult{Success: false, Error: "缺少必填参数: name, method"}, nil
		}
		params, _ := args["params"].(map[string]any)
		out, err := p.RunPlugin(pluginName, method, params)
		if err != nil {
			// Attach diagnostics for debuggability.
			status := p.GetPluginStatus(pluginName)
			logs := p.GetPluginLogs(pluginName)
			diag := map[string]any{
				"running": status.Running,
				"pid":     status.PID,
				"error":   err.Error(),
			}
			if status.Error != "" {
				diag["statusError"] = status.Error
			}
			if strings.TrimSpace(logs) != "" {
				tail := logs
				if len(tail) > 1500 {
					tail = "…" + tail[len(tail)-1500:]
				}
				diag["stderr"] = tail
			} else {
				diag["stderr"] = "(空 — 插件未输出任何日志)"
			}
			return core.ToolResult{Success: false, Error: fmt.Sprintf("插件 %s.%s 调用失败: %v\n诊断: %s", pluginName, method, err, asJSON(diag))}, nil
		}
		return core.ToolResult{Success: true, Data: out}, nil

	case "plugin_install":
		path := getStr(args, "path")
		if path == "" {
			return core.ToolResult{Success: false, Error: "缺少必填参数: path"}, nil
		}
		spec, err := p.InstallPlugin(path)
		if err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: spec}, nil

	case "plugin_delete":
		pluginName := getStr(args, "name")
		if err := p.DeletePlugin(pluginName); err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: map[string]string{"deleted": pluginName}}, nil

	case "plugin_logs":
		pluginName := getStr(args, "name")
		return core.ToolResult{Success: true, Data: map[string]string{"logs": p.GetPluginLogs(pluginName)}}, nil

	case "plugin_create":
		pluginName := getStr(args, "name")
		runtime := getStr(args, "runtime")
		code := getStr(args, "code")
		desc := getStr(args, "description")
		methodsStr := getStr(args, "methods")
		deps := getStr(args, "dependencies")
		autoStart := true
		if v, ok := args["autoStart"]; ok {
			if b, ok := v.(bool); ok {
				autoStart = b
			}
		}
		result, err := p.PluginCreate(pluginName, runtime, desc, code, methodsStr, deps, autoStart)
		if err != nil {
			return core.ToolResult{Success: false, Error: err.Error()}, nil
		}
		return core.ToolResult{Success: true, Data: result}, nil

	default:
		return core.ToolResult{Success: false, Error: fmt.Sprintf("extplugins: unknown tool %q", name)}, nil
	}
}

// ─── Host / Client lazy initialization ──────────────────────────────────

func (p *Plugin) initHost() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.host == nil {
		dir := storage.DataDir()
		p.host = plugin.NewHost(dir)
		p.client = plugin.NewClient(p.host)
	}
}

// ─── Public methods (called by App delegation + LLM tools) ─────────────

// ListPlugins scans and returns all installed external plugins.
func (p *Plugin) ListPlugins() ([]plugin.Spec, error) {
	specs, err := plugin.ScanPlugins(storage.PluginsDir())
	if err != nil {
		return nil, err
	}
	if specs == nil {
		specs = []plugin.Spec{}
	}
	return specs, nil
}

// GetPluginStatus returns the runtime status of a single plugin.
func (p *Plugin) GetPluginStatus(name string) plugin.Status {
	p.initHost()
	return p.host.GetStatus(name)
}

// ListStatus returns all plugin statuses.
func (p *Plugin) ListStatus() []plugin.Status {
	p.initHost()
	return p.host.ListStatus()
}

// StartPlugin starts a plugin by name (auto-starts the stdout reader and
// runs a health check).
func (p *Plugin) StartPlugin(name string) error {
	dir := storage.DataDir()
	specs, _ := plugin.ScanPlugins(plugin.PluginsDir(dir))
	spec, err := plugin.Lookup(specs, name)
	if err != nil {
		return err
	}
	p.initHost()
	if err := p.host.Start(*spec); err != nil {
		return err
	}
	p.client.StartReader(name)
	if err := p.client.Health(name); err != nil {
		_ = p.host.Stop(name)
		return fmt.Errorf("插件 %s 健康检查失败（RPC 无响应）: %w", name, err)
	}
	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "update", name)
	}
	return nil
}

// StopPlugin stops a plugin by name.
func (p *Plugin) StopPlugin(name string) error {
	p.initHost()
	if err := p.host.Stop(name); err != nil {
		return err
	}
	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "update", name)
	}
	return nil
}

// RestartPlugin restarts a plugin by name.
func (p *Plugin) RestartPlugin(name string) error {
	p.initHost()
	if err := p.host.Restart(name); err != nil {
		return err
	}
	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "update", name)
	}
	return nil
}

// RunPlugin calls a plugin method, auto-starting the plugin if needed.
func (p *Plugin) RunPlugin(name, method string, params map[string]any) (map[string]any, error) {
	p.initHost()
	if !p.host.IsRunning(name) {
		if err := p.StartPlugin(name); err != nil {
			return nil, fmt.Errorf("插件 %s 未运行且自动启动失败: %w", name, err)
		}
	}
	return p.client.Call(name, method, params, 30*time.Second)
}

// InstallPlugin installs a plugin from a .zip or directory path.
func (p *Plugin) InstallPlugin(path string) (plugin.Spec, error) {
	dataDir := storage.DataDir()
	pluginsDir := plugin.PluginsDir(dataDir)
	tmpDir := plugin.TmpDir(dataDir)
	os.MkdirAll(tmpDir, 0755)

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		spec, err := plugin.InstallFromZip(path, pluginsDir, tmpDir)
		if err != nil {
			return plugin.Spec{}, err
		}
		log.Printf("[plugin] 已安装: %s v%s (from %s)", spec.Name, spec.Version, filepath.Base(path))
		if p.delegate != nil {
			p.delegate.EmitChanged("plugins:changed", "update", spec.Name)
		}
		return *spec, nil
	}
	spec, err := plugin.InstallFromDir(path, pluginsDir)
	if err != nil {
		return plugin.Spec{}, err
	}
	log.Printf("[plugin] 已安装: %s v%s (from %s)", spec.Name, spec.Version, filepath.Base(path))
	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "update", spec.Name)
	}
	return *spec, nil
}

// DeletePlugin uninstalls a plugin (stops first if running, then removes dir).
func (p *Plugin) DeletePlugin(name string) error {
	p.initHost()
	if p.host.IsRunning(name) {
		if err := p.host.Stop(name); err != nil {
			log.Printf("[plugin] 停止 %s 失败: %v", name, err)
		}
	}
	pluginsDir := plugin.PluginsDir(storage.DataDir())
	if err := plugin.DeletePlugin(pluginsDir, name); err != nil {
		return err
	}
	log.Printf("[plugin] 已卸载: %s", name)
	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "update", name)
	}
	return nil
}

// GetPluginLogs returns the plugin's recent stderr log (up to 64KB).
func (p *Plugin) GetPluginLogs(name string) string {
	p.initHost()
	return p.host.GetLogs(name)
}

// PluginCreate writes a new plugin from Agent-provided code, installs it,
// and optionally hot-starts it. Supports three runtimes:
//
//   - "python" (default): creates venv, pip-installs deps, wraps user
//     handler with JSON-RPC I/O loop.
//   - "go": compiles entry.go with go build → entry.exe.
//   - "node": runs entry.js with Node.js, installs npm deps in plugin dir.
func (p *Plugin) PluginCreate(name, runtime, description, code, methodsStr, deps string, autoStart bool) (map[string]any, error) {
	if name == "" || code == "" {
		return nil, fmt.Errorf("name 和 code 为必填参数")
	}
	if runtime == "" {
		runtime = "python"
	}

	name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	methods := []string{"health", "info"}
	if methodsStr != "" {
		methods = strings.Split(methodsStr, ",")
		for i := range methods {
			methods[i] = strings.TrimSpace(methods[i])
		}
	}

	dataDir := storage.DataDir()
	pluginsDir := plugin.PluginsDir(dataDir)
	tmpBase := plugin.TmpDir(dataDir)
	if err := os.MkdirAll(tmpBase, 0755); err != nil {
		return nil, fmt.Errorf("创建 plugin-tmp 目录失败: %w", err)
	}
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("创建 plugins 目录失败: %w", err)
	}
	tmpDir := filepath.Join(tmpBase, name)
	if err := os.RemoveAll(tmpDir); err != nil {
		return nil, fmt.Errorf("清理临时目录失败: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}

	var manifestEntry string
	var manifestRuntime string
	var manifestEnv string

	switch runtime {
	case "python":
		manifestEntry = "entry.py"
		manifestRuntime = "python"
		manifestEnv = "venv"

		log.Printf("[plugin] 为 %s 创建 venv...", name)
		venvDir := filepath.Join(tmpDir, "venv")
		cmdVenv := exec.Command("python", "-m", "venv", venvDir)
		if out, err := cmdVenv.CombinedOutput(); err != nil {
			log.Printf("[plugin] venv 创建失败 (非致命): %v\n%s", err, string(out))
			manifestEnv = ""
		}

		if deps != "" && manifestEnv != "" {
			pipExe := filepath.Join(venvDir, "Scripts", "pip.exe")
			pipArgs := append([]string{"install"}, strings.Fields(deps)...)
			cmdPip := exec.Command(pipExe, pipArgs...)
			cmdPip.Dir = tmpDir
			if out, err := cmdPip.CombinedOutput(); err != nil {
				log.Printf("[plugin] pip install 失败 (非致命): %v\n%s", err, string(out))
			} else {
				log.Printf("[plugin] %s pip 依赖已安装: %s", name, deps)
			}
		}

		entryCode := code
		if !strings.Contains(code, "sys.stdin") && !strings.Contains(code, "for line in") {
			entryCode = pyTemplate + code + pySuffix
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "entry.py"), []byte(entryCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.py 失败: %w", err)
		}

	case "go":
		manifestEntry = "entry.exe"
		manifestRuntime = ""
		manifestEnv = ""

		goCode := code
		if !strings.Contains(code, "json.Unmarshal") && !strings.Contains(code, "json.NewDecoder") {
			goCode = goTemplatePrefix + code + goTemplateSuffix
		}
		goFile := filepath.Join(tmpDir, "entry.go")
		if err := os.WriteFile(goFile, []byte(goCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.go 失败: %w", err)
		}

		log.Printf("[plugin] 编译 Go 插件 %s...", name)
		exeFile := filepath.Join(tmpDir, "entry.exe")
		cmdGo := exec.Command("go", "build", "-o", exeFile, goFile)
		cmdGo.Dir = tmpDir
		if out, err := cmdGo.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("go build 失败: %w\n%s", err, string(out))
		}
		log.Printf("[plugin] Go 插件编译完成: %s", exeFile)

	case "node":
		manifestEntry = "entry.js"
		manifestRuntime = "node"
		manifestEnv = ""

		jsCode := code
		if !strings.Contains(code, "process.stdin") && !strings.Contains(code, "readline") {
			jsCode = nodeTemplate + code + nodeSuffix
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "entry.js"), []byte(jsCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.js 失败: %w", err)
		}

		if deps != "" {
			npmArgs := append([]string{"install"}, strings.Fields(deps)...)
			cmdNpm := exec.Command("npm", npmArgs...)
			cmdNpm.Dir = tmpDir
			if out, err := cmdNpm.CombinedOutput(); err != nil {
				log.Printf("[plugin] npm install 失败 (非致命): %v\n%s", err, string(out))
			} else {
				log.Printf("[plugin] %s npm 依赖已安装: %s", name, deps)
			}
		}

	default:
		return nil, fmt.Errorf("不支持的运行时: %s (支持 python, go, node)", runtime)
	}

	// Write manifest.json
	manifest := map[string]any{
		"name":        name,
		"version":     "1.0.0",
		"type":        "tool",
		"author":      "EverEvo Agent",
		"description": description,
		"entry":       manifestEntry,
		"runtime":     manifestRuntime,
		"env":         manifestEnv,
		"methods":     methods,
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), manifestData, 0644); err != nil {
		return nil, fmt.Errorf("写入 manifest.json 失败: %w", err)
	}

	// Install
	spec, err := plugin.InstallFromDir(tmpDir, pluginsDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}
	os.RemoveAll(tmpDir)

	log.Printf("[plugin] Agent 创建插件: %s (runtime=%s, methods=%s)", name, runtime, strings.Join(methods, ","))

	// Auto-start
	started := false
	if autoStart {
		if err := p.StartPlugin(name); err != nil {
			log.Printf("[plugin] %s 热启动失败: %v (安装成功，可稍后手动启动)", name, err)
		} else {
			started = true
			log.Printf("[plugin] %s 已热启动", name)
		}
	}

	if p.delegate != nil {
		p.delegate.EmitChanged("plugins:changed", "create", name)
	}

	return map[string]any{
		"name":      spec.Name,
		"version":   spec.Version,
		"runtime":   runtime,
		"methods":   spec.Methods,
		"dir":       spec.Dir,
		"started":   started,
		"message":   fmt.Sprintf("[%s] 插件 %s 已创建并安装", runtime, name),
		"nextSteps": fmt.Sprintf("plugin_run(name=%q, method=\"health\")", name),
	}, nil
}

// ─── Runtime templates ──────────────────────────────────────────────────

const pyTemplate = `import sys, json, traceback, os

# ── Startup log (visible in plugin_logs) ──
sys.stderr.write(f"[plugin] Python {sys.version} starting, pid={os.getpid()}\n")
sys.stderr.flush()

# ── Your handler ──
# Define handle(method, params) → dict

`

const pySuffix = `

# ── JSON-RPC I/O loop ──
if __name__ == "__main__":
    while True:
        line = sys.stdin.readline()
        if not line:
            sys.stderr.write("[plugin] stdin closed, exiting\n")
            sys.stderr.flush()
            break
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            result = handle(req.get("method", ""), req.get("params", {}))
            resp = {"id": req.get("id", ""), "ok": True, "result": result}
        except Exception as e:
            resp = {"id": req.get("id", ""), "ok": False, "error": str(e)}
            sys.stderr.write(f"[plugin] ERROR in handle: {e}\n")
            traceback.print_exc(file=sys.stderr)
            sys.stderr.flush()
        out = json.dumps(resp, ensure_ascii=False) + "\n"
        sys.stdout.write(out)
        sys.stdout.flush()
`

const goTemplatePrefix = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	ID     string         ` + "`" + `json:"id"` + "`" + `
	Method string         ` + "`" + `json:"method"` + "`" + `
	Params map[string]any ` + "`" + `json:"params"` + "`" + `
}

type response struct {
	ID     string         ` + "`" + `json:"id"` + "`" + `
	OK     bool           ` + "`" + `json:"ok"` + "`" + `
	Result map[string]any ` + "`" + `json:"result,omitempty"` + "`" + `
	Error  string         ` + "`" + `json:"error,omitempty"` + "`" + `
}

// ── Your handler ──
// Implement handle(method string, params map[string]any) map[string]any
`

const goTemplateSuffix = `

// ── JSON-RPC I/O loop ──
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" { continue }
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil { continue }
		func() {
			var resp response
			resp.ID = req.ID
			defer func() {
				if r := recover(); r != nil {
					resp.OK = false
					resp.Error = fmt.Sprintf("panic: %v", r)
					out, _ := json.Marshal(resp)
					fmt.Println(string(out))
				}
			}()
			result := handle(req.Method, req.Params)
			resp.OK = true
			resp.Result = result
			out, _ := json.Marshal(resp)
			fmt.Println(string(out))
		}()
	}
}
`

const nodeTemplate = `// ── Your handler ──
// Implement async function handle(method, params) returning an object

`

const nodeSuffix = `

// ── JSON-RPC I/O loop ──
const readline = require("readline");
const rl = readline.createInterface({ input: process.stdin });

rl.on("line", (line) => {
  line = line.trim();
  if (!line) return;
  let req;
  try { req = JSON.parse(line); } catch (e) { return; }
  const rid = req.id || "";
  Promise.resolve()
    .then(() => handle(req.method || "", req.params || {}))
    .then((result) => {
      process.stdout.write(JSON.stringify({ id: rid, ok: true, result }) + "\n");
    })
    .catch((e) => {
      process.stderr.write(e.stack + "\n");
      process.stdout.write(JSON.stringify({ id: rid, ok: false, error: String(e) }) + "\n");
    });
});
`

// ─── Helpers ────────────────────────────────────────────────────────────

func getStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
