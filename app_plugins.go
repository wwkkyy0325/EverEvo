//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"everevo/internal/plugin"
	"everevo/internal/storage"
)

// ─── 插件 API ────────────────────────────────────────────────

var pluginHost *plugin.Host
var pluginClient *plugin.Client
var pluginMu sync.Mutex

func (a *App) getPluginHost() *plugin.Host {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if pluginHost == nil {
		dir, _ := storage.DataDir()
		pluginHost = plugin.NewHost(dir)
		pluginClient = plugin.NewClient(pluginHost)
	}
	return pluginHost
}

// ListPlugins 扫描并返回所有已安装插件。
func (a *App) ListPlugins() ([]plugin.Spec, error) {
	dir, err := storage.DataDir()
	if err != nil {
		return nil, err
	}
	specs, err := plugin.ScanPlugins(plugin.PluginsDir(dir))
	if err != nil {
		return nil, err
	}
	if specs == nil {
		specs = []plugin.Spec{}
	}
	return specs, nil
}

// GetPluginStatus 返回插件的运行状态。
func (a *App) GetPluginStatus(name string) plugin.Status {
	host := a.getPluginHost()
	return host.GetStatus(name)
}

// StartPlugin 启动指定插件（会自动启动 stdout 读取协程）。
func (a *App) StartPlugin(name string) error {
	dir, _ := storage.DataDir()
	specs, _ := plugin.ScanPlugins(plugin.PluginsDir(dir))
	spec, err := plugin.Lookup(specs, name)
	if err != nil {
		return err
	}
	host := a.getPluginHost()
	if err := host.Start(*spec); err != nil {
		return err
	}
	// 启动后台读取协程以接收 RPC 响应
	pluginClient.StartReader(name)
	// 启动健康检查（致命）：RPC 不通时立即失败并清理半启动进程，避免后续
	// RunPlugin 干等 30s 超时才暴露。
	if err := pluginClient.Health(name); err != nil {
		_ = host.Stop(name)
		return fmt.Errorf("插件 %s 健康检查失败（RPC 无响应）: %w", name, err)
	}
	a.emitChanged("plugins:changed", "update", name)
	return nil
}

// StopPlugin 停止指定插件。
func (a *App) StopPlugin(name string) error {
	if err := a.getPluginHost().Stop(name); err != nil {
		return err
	}
	a.emitChanged("plugins:changed", "update", name)
	return nil
}

// RestartPlugin 重启指定插件。
func (a *App) RestartPlugin(name string) error {
	if err := a.getPluginHost().Restart(name); err != nil {
		return err
	}
	a.emitChanged("plugins:changed", "update", name)
	return nil
}

// RunPlugin 调用插件方法。若插件未运行，先自动启动（避免已安装但未 start
// 导致的 RPC 超时——进程不在时 Call 会干等 30 秒）。
func (a *App) RunPlugin(name, method string, params map[string]any) (map[string]any, error) {
	host := a.getPluginHost()
	if !host.IsRunning(name) {
		if err := a.StartPlugin(name); err != nil {
			return nil, fmt.Errorf("插件 %s 未运行且自动启动失败: %w", name, err)
		}
	}
	return pluginClient.Call(name, method, params, 30*time.Second)
}

// PickPluginFile 打开文件选择对话框，选 .zip 插件包。
func (a *App) PickPluginFile() string {
	path, _ := pickPluginDialog()
	return path
}

// InstallPlugin 从给定路径安装插件（支持 .zip 和目录）。
func (a *App) InstallPlugin(path string) (plugin.Spec, error) {
	dataDir, err := storage.DataDir()
	if err != nil {
		return plugin.Spec{}, err
	}
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
		a.emitChanged("plugins:changed", "update", spec.Name)
		return *spec, nil
	}
	spec, err := plugin.InstallFromDir(path, pluginsDir)
	if err != nil {
		return plugin.Spec{}, err
	}
	log.Printf("[plugin] 已安装: %s v%s (from %s)", spec.Name, spec.Version, filepath.Base(path))
	a.emitChanged("plugins:changed", "update", spec.Name)
	return *spec, nil
}

// DeletePlugin 卸载指定插件（先停止再删除目录）。
func (a *App) DeletePlugin(name string) error {
	// Stop first if running
	host := a.getPluginHost()
	if host.IsRunning(name) {
		if err := host.Stop(name); err != nil {
			log.Printf("[plugin] 停止 %s 失败: %v", name, err)
		}
	}
	dataDir, err := storage.DataDir()
	if err != nil {
		return err
	}
	pluginsDir := plugin.PluginsDir(dataDir)
	if err := plugin.DeletePlugin(pluginsDir, name); err != nil {
		return err
	}
	log.Printf("[plugin] 已卸载: %s", name)
	a.emitChanged("plugins:changed", "update", name)
	return nil
}

// GetPluginLogs 返回插件最近的 stderr 日志（最多 64KB）。
func (a *App) GetPluginLogs(name string) string {
	return a.getPluginHost().GetLogs(name)
}

// PluginCreate writes a new plugin from Agent-provided code, installs
// it, and optionally hot-starts it. Supports three runtimes:
//
//   - "python" (default): creates venv, pip-installs deps, wraps user
//     handler with JSON-RPC I/O loop. Completely isolated from system Python.
//   - "go": compiles entry.go with go build → entry.exe, no runtime needed.
//   - "node": runs entry.js with Node.js, installs npm deps in plugin dir.
func (a *App) PluginCreate(name, runtime, description, code, methodsStr, deps string, autoStart bool) (map[string]any, error) {
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

	dataDir, err := storage.DataDir()
	if err != nil {
		return nil, err
	}
	// Ensure plugin-tmp and plugins directories exist.
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

		// Create venv for isolation
		log.Printf("[plugin] 为 %s 创建 venv...", name)
		venvDir := filepath.Join(tmpDir, "venv")
		cmdVenv := exec.Command("python", "-m", "venv", venvDir)
		if out, err := cmdVenv.CombinedOutput(); err != nil {
			log.Printf("[plugin] venv 创建失败 (非致命): %v\n%s", err, string(out))
			manifestEnv = "" // fallback: use system python
		}

		// pip install deps into venv
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

		// Wrap user handler code with I/O loop if needed
		entryCode := code
		if !strings.Contains(code, "sys.stdin") && !strings.Contains(code, "for line in") {
			entryCode = pyTemplate + code + pySuffix
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "entry.py"), []byte(entryCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.py 失败: %w", err)
		}

	case "go":
		manifestEntry = "entry.exe"
		manifestRuntime = "" // compiled binary — no runtime launcher
		manifestEnv = ""

		// Wrap user handler with Go JSON-RPC I/O template if needed
		goCode := code
		if !strings.Contains(code, "json.Unmarshal") && !strings.Contains(code, "json.NewDecoder") {
			goCode = goTemplatePrefix + code + goTemplateSuffix
		}
		goFile := filepath.Join(tmpDir, "entry.go")
		if err := os.WriteFile(goFile, []byte(goCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.go 失败: %w", err)
		}

		// go build
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

		// Wrap user handler with Node JSON-RPC I/O template if needed
		jsCode := code
		if !strings.Contains(code, "process.stdin") && !strings.Contains(code, "readline") {
			jsCode = nodeTemplate + code + nodeSuffix
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "entry.js"), []byte(jsCode), 0644); err != nil {
			return nil, fmt.Errorf("写入 entry.js 失败: %w", err)
		}

		// npm install deps in plugin dir
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
		if err := a.StartPlugin(name); err != nil {
			log.Printf("[plugin] %s 热启动失败: %v (安装成功，可稍后手动启动)", name, err)
		} else {
			started = true
			log.Printf("[plugin] %s 已热启动", name)
		}
	}

	a.emitChanged("plugins:changed", "create", name)

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

// ─── Runtime templates ────────────────────────────────────────────
// Each template provides the JSON-RPC I/O loop. The Agent only writes
// the handle(method, params) function body. The system wraps it.

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
    # Use sys.stdin.buffer for raw binary read, decode manually for robustness.
    # The for-line iterator can buffer and block in unexpected ways on Windows.
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


