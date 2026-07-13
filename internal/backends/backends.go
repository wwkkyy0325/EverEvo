package backends

import (
	"encoding/json"
	"fmt"
	"everevo/internal/httpclient"
	"everevo/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Status is the runtime status of a single backend.
type Status struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
	DLLPath string `json:"-"` // full DLL path (not exposed to frontend)
}

// BackendSpec defines detection rules for a known inference backend.
type BackendSpec struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	DLL    string   `json:"dll"`    // filename pattern (supports * wildcard)
	Symbol string   `json:"symbol"` // exported entry symbol name
	URL    string   `json:"url"`    // official download/releases page
}

// Known backends. Add new engines here.
var Known = []BackendSpec{
	{
		Key: "onnx", Name: "ONNX Runtime",
		DLL: "onnxruntime*.dll", Symbol: "OrtGetApiBase",
		URL: "https://github.com/microsoft/onnxruntime/releases",
	},
	{
		Key: "llama", Name: "llama.cpp",
		DLL: "llama-server*", Symbol: "",
		URL: "https://github.com/ggml-org/llama.cpp/releases",
	},
	{
		Key: "ollama", Name: "Ollama",
		DLL: "ollama*", Symbol: "",
		URL: "https://ollama.com/download",
	},
	{
		Key: "cuda", Name: "CUDA Runtime",
		DLL: "cudart64_*.dll", Symbol: "",
		URL: "https://developer.nvidia.com/cuda-downloads",
	},
	{
		Key: "nodejs", Name: "Node.js",
		DLL: "", Symbol: "",
		URL: "https://nodejs.org",
	},
	{
		Key: "python", Name: "Python Portable",
		DLL: "python.exe", Symbol: "",
		URL: "https://www.python.org/ftp/python/3.11.9/python-3.11.9-embed-amd64.zip",
	},
}

// ─── Platform & Download Helpers ─────────────────────────────────

// PlatformInfo describes the user's OS/arch for download selection.
type PlatformInfo struct {
	OS   string `json:"os"`   // windows / linux / darwin
	Arch string `json:"arch"` // x64 / arm64
}

// GetPlatform returns the current platform.
func GetPlatform() PlatformInfo {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return PlatformInfo{OS: runtime.GOOS, Arch: arch}
}

// Mirror represents a download mirror with a label.
type Mirror struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// GetMirrors returns available download mirrors.
func GetMirrors() []Mirror {
	return []Mirror{
		{Label: "GitHub Releases", URL: "https://github.com/ggml-org/llama.cpp/releases"},
		{Label: "ghproxy 镜像", URL: "https://ghproxy.com/https://github.com/ggml-org/llama.cpp/releases"},
	}
}

// ResolveMirrorURL converts a GitHub release URL through the given mirror.
func ResolveMirrorURL(githubURL, mirrorURL string) string {
	// If the URL is from a mirror, return as-is
	if !strings.Contains(githubURL, "github.com") {
		return githubURL
	}
	// For ghproxy-style mirrors, prepend the mirror prefix to the GitHub URL
	if strings.Contains(mirrorURL, "ghproxy.com") {
		return strings.Replace(githubURL, "https://github.com", mirrorURL, 1)
	}
	// For other mirrors, just return the mirror page
	return mirrorURL
}

// GetBackendDownloadURL returns the latest release download URL for a backend.
// variant is optional: "" = default (CPU), "cuda" = CUDA build.
func GetBackendDownloadURL(key string, mirror string, variant string) string {
	pi := GetPlatform()
	switch key {
	case "llama":
		return getLlamaDownloadURL(pi, mirror, variant)
	case "ollama":
		return "https://ollama.com/download"
	case "cuda":
		return "https://developer.nvidia.com/cuda-downloads"
	case "onnx":
		return "https://github.com/microsoft/onnxruntime/releases"
	case "nodejs":
		return "https://nodejs.org"
	case "python":
		return "https://www.python.org/ftp/python/3.11.9/python-3.11.9-embed-amd64.zip"
	}
	return ""
}

// getLlamaDownloadURL builds the platform-specific llama.cpp download URL.
// variant "cuda" selects the CUDA-enabled build (cu12.4 on Windows/Linux).
func getLlamaDownloadURL(pi PlatformInfo, mirror string, variant string) string {
	// Latest stable release tag (update periodically)
	const releaseTag = "b4984"

	var filename string
	if variant == "cuda" && (pi.OS == "windows" || pi.OS == "linux") && pi.Arch == "x64" {
		// CUDA 12.4 builds — only available for Windows/Linux x64
		cuPlatform := map[string]string{"windows": "win", "linux": "linux"}
		filename = fmt.Sprintf("llama-%s-bin-%s-cuda-cu12.4-x64.zip", releaseTag, cuPlatform[pi.OS])
	} else {
		var platform string
		switch pi.OS {
		case "windows":
			platform = "win-x64"
			if pi.Arch == "arm64" {
				platform = "win-arm64"
			}
		case "linux":
			platform = "linux-x64"
			if pi.Arch == "arm64" {
				platform = "linux-arm64"
			}
		case "darwin":
			platform = "macos-x64"
			if pi.Arch == "arm64" {
				platform = "macos-arm64"
			}
		default:
			platform = "win-x64"
		}
		filename = fmt.Sprintf("llama-%s-%s.zip", releaseTag, platform)
	}

	baseURL := fmt.Sprintf("https://github.com/ggml-org/llama.cpp/releases/download/%s/%s", releaseTag, filename)

	if mirror != "" && mirror != "https://github.com/ggml-org/llama.cpp/releases" {
		return ResolveMirrorURL(baseURL, mirror)
	}
	return baseURL
}

// ─── Detection ───────────────────────────────────────────────────

// GPUInfo describes the detected GPU for CUDA compatibility checks.
type GPUInfo struct {
	Found       bool   // NVIDIA GPU + driver present
	Name        string // e.g., "NVIDIA GeForce RTX 4060"
	DriverVer   string // human-readable driver version, e.g., "560.94"
	CUDAVersion string // max supported CUDA version, e.g., "12.4"
}

// Detect scans known backends and checks if their DLL is available.
func Detect() []Status {
	var list []Status
	for _, spec := range Known {
		switch spec.Key {
		case "cuda":
			list = append(list, detectCUDA())
		case "nodejs":
			list = append(list, detectNodeJS(spec))
		case "python":
			list = append(list, detectPython(spec))
		case "ollama":
			list = append(list, detectOllama(spec))
		default:
			list = append(list, detectOne(spec))
		}
	}
	return list
}

func detectOne(spec BackendSpec) Status {
	st := Status{Key: spec.Key, Name: spec.Name}

	dllPath := FindDLL(spec.DLL)
	if dllPath == "" {
		// Give a helpful hint about where to place files
		exeDir := ""
		if exe, err := os.Executable(); err == nil {
			exeDir = filepath.Dir(exe)
		}
		if exeDir != "" {
			st.Reason = fmt.Sprintf("未找到 %s\n将引擎文件放到 %s 或其子文件夹即可\n下载: %s", spec.DLL, exeDir, spec.URL)
		} else {
			st.Reason = fmt.Sprintf("未找到 %s\n下载: %s", spec.DLL, spec.URL)
		}
		return st
	}

	// If no symbol specified, just check file existence (e.g., for .exe backends)
	if spec.Symbol == "" {
		st.OK = true
		st.DLLPath = dllPath
		st.Version = filepath.Base(dllPath)
		return st
	}

	// Try to load and find entry symbol
	ok, err := loadSymbol(dllPath, spec.Symbol)
	if !ok {
		st.Reason = fmt.Sprintf("%s 已找到，但缺少入口 %s\n%v", filepath.Base(dllPath), spec.Symbol, err)
		return st
	}

	st.OK = true
	st.DLLPath = dllPath
	st.Version = filepath.Base(dllPath)
	return st
}

// ─── Ollama Detection ───────────────────────────────────────────

// detectOllama checks for Ollama executable and running service.
// Ollama is an HTTP service, not a DLL library — detection is different
// from other backends.
func detectOllama(spec BackendSpec) Status {
	st := Status{Key: spec.Key, Name: spec.Name}

	// Step 1: find ollama executable
	exePath := findOllamaExe()
	if exePath == "" {
		st.Reason = fmt.Sprintf("未找到 ollama 可执行文件\n下载: %s", spec.URL)
		return st
	}

	// Step 2: get version
	ver := ollamaVersion(exePath)
	if ver != "" {
		st.Version = ver
	}

	// Step 3: check if service is running
	if ollamaRunning() {
		st.OK = true
		if models := ollamaModelCount(); models > 0 {
			st.Version = fmt.Sprintf("%s · %d 个模型已就绪", ver, models)
		} else if ver != "" {
			st.Version = ver + " · 运行中"
		} else {
			st.Version = "运行中"
		}
	} else {
		if ver != "" {
			st.Reason = fmt.Sprintf("已安装 (%s)\n服务未运行 — 在终端执行 ollama serve 启动", ver)
		} else {
			st.Reason = "已安装\n服务未运行 — 在终端执行 ollama serve 启动"
		}
	}

	return st
}

// PythonInfo describes a detected Python installation.
type PythonInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Source  string `json:"source"` // "portable" | "system" | "conda"
}

// detectPython scans for all available Python installations: portable first,
// then system PATH, then conda environments.
func detectPython(spec BackendSpec) Status {
	st := Status{Key: spec.Key, Name: spec.Name}

	var found []PythonInfo

	// 1. Portable Python (our managed installation under runtime/)
	portableExe := filepath.Join(storage.PythonDir(), "python.exe")
	if ver := pythonVersion(portableExe); ver != "" {
		found = append(found, PythonInfo{Path: portableExe, Version: ver, Source: "portable"})
	}

	// 2. System Python (from PATH)
	if sysExe, err := exec.LookPath("python"); err == nil && sysExe != portableExe {
		if ver := pythonVersion(sysExe); ver != "" {
			found = append(found, PythonInfo{Path: sysExe, Version: ver, Source: "system"})
		}
	}
	if sysExe, err := exec.LookPath("python3"); err == nil && sysExe != portableExe {
		already := false
		for _, f := range found {
			if f.Path == sysExe {
				already = true
				break
			}
		}
		if !already {
			if ver := pythonVersion(sysExe); ver != "" {
				found = append(found, PythonInfo{Path: sysExe, Version: ver, Source: "system"})
			}
		}
	}

	// 3. Conda (common install paths + env var)
	condaPrefix := os.Getenv("CONDA_PREFIX")
	if condaPrefix != "" {
		condaExe := filepath.Join(condaPrefix, "python.exe")
		if runtime.GOOS != "windows" {
			condaExe = filepath.Join(condaPrefix, "bin", "python")
		}
		if ver := pythonVersion(condaExe); ver != "" {
			found = append(found, PythonInfo{Path: condaExe, Version: ver, Source: "conda"})
		}
	}
	// Also check common conda install locations on Windows
	if runtime.GOOS == "windows" {
		for _, base := range []string{
			filepath.Join(os.Getenv("USERPROFILE"), "miniconda3", "python.exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "anaconda3", "python.exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "miniconda3", "python.exe"),
		} {
			if ver := pythonVersion(base); ver != "" {
				already := false
				for _, f := range found {
					if f.Path == base {
						already = true
						break
					}
				}
				if !already {
					found = append(found, PythonInfo{Path: base, Version: ver, Source: "conda"})
				}
			}
		}
	}

	if len(found) == 0 {
		st.Reason = fmt.Sprintf("未检测到 Python\n可安装便携版本（无需管理员权限）\n或安装系统 Python: https://python.org")
		return st
	}

	// Pick best: portable > conda > system
	best := found[0]
	for _, f := range found[1:] {
		if f.Source == "portable" {
			best = f
			break
		}
		if f.Source == "conda" && best.Source == "system" {
			best = f
		}
	}

	st.OK = true
	st.Version = fmt.Sprintf("%s (%s)", best.Version, best.Source)
	// List all found in reason for the detail view
	if len(found) > 1 {
		var lines []string
		for _, f := range found {
			lines = append(lines, fmt.Sprintf("%s: %s (%s)", f.Source, f.Version, f.Path))
		}
		st.Reason = "检测到多个 Python:\n" + strings.Join(lines, "\n")
	}
	st.DLLPath = best.Path // stash best path for use by tools
	return st
}

// pythonVersion returns the version string of a python executable, or "" on failure.
func pythonVersion(exe string) string {
	if _, err := os.Stat(exe); os.IsNotExist(err) {
		return ""
	}
	cmd := exec.Command(exe, "--version")
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "Python ")
}

func detectNodeJS(spec BackendSpec) Status {
	st := Status{Key: spec.Key, Name: spec.Name}

	nodePath, nodeErr := exec.LookPath("node")
	npxPath, npxErr := exec.LookPath("npx")

	if nodeErr != nil && npxErr != nil {
		st.Reason = "未检测到 Node.js\n第三方 MCP 接入 (stdio) 依赖 npx 命令\n下载: https://nodejs.org (推荐 LTS)"
		return st
	}

	// Get Node.js version
	ver := ""
	if nodePath != "" {
		cmd := exec.Command(nodePath, "--version")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.Output()
		if err == nil {
			ver = strings.TrimSpace(string(out))
			// Strip "v" prefix
			ver = strings.TrimPrefix(ver, "v")
		}
	}

	if ver != "" {
		st.Version = ver
	} else {
		st.Version = "已安装"
	}
	st.OK = true

	if npxErr != nil {
		st.Reason = fmt.Sprintf("Node.js %s 已安装，但未找到 npx\n请确保 Node.js 安装完整", ver)
		st.OK = false
	} else {
		// Also get npm version as extra info
		if npmPath, err := exec.LookPath("npm"); err == nil {
			cmd := exec.Command(npmPath, "--version")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			out, err := cmd.Output()
			if err == nil {
				npmVer := strings.TrimSpace(string(out))
				st.Version = fmt.Sprintf("%s · npm %s", ver, npmVer)
			}
		}
		_ = npxPath
	}

	return st
}

// findOllamaExe searches for ollama executable.
func findOllamaExe() string {
	exeName := "ollama"
	if runtime.GOOS == "windows" {
		exeName = "ollama.exe"
	}

	// 1. Check PATH
	if p, err := exec.LookPath(exeName); err == nil {
		return p
	}

	// 2. Common install locations
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		candidates := []string{
			filepath.Join(localAppData, "Programs", "Ollama", "ollama.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Ollama", "ollama.exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "Programs", "Ollama", "ollama.exe"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	} else {
		candidates := []string{
			"/usr/local/bin/ollama",
			"/usr/bin/ollama",
			"/opt/ollama/ollama",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ollamaVersion runs `ollama --version` and returns the version string.
func ollamaVersion(exePath string) string {
	cmd := exec.Command(exePath, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	ver := strings.TrimSpace(string(out))
	// Strip "ollama version " prefix if present
	ver = strings.TrimPrefix(ver, "ollama version ")
	return ver
}

// ollamaRunning checks if Ollama HTTP service is responding.
func ollamaRunning() bool {
	client := httpclient.New(2 * time.Second)
	resp, err := client.Get("http://127.0.0.1:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// ollamaModelCount returns the number of models available in Ollama.
func ollamaModelCount() int {
	client := httpclient.New(2 * time.Second)
	resp, err := client.Get("http://127.0.0.1:11434/api/tags")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}
	var result struct {
		Models []struct{} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0
	}
	return len(result.Models)
}

// detectCUDA performs GPU-first CUDA detection:
// 1. Check for NVIDIA GPU — must be present, otherwise CUDA is unavailable
// 2. Scan for CUDA Runtime DLLs: local directory first, then system paths
func detectCUDA() Status {
	st := Status{Key: "cuda", Name: "CUDA Runtime"}

	gpu := detectGPU()

	if !gpu.Found {
		st.Reason = "未检测到 NVIDIA 显卡\nCUDA 运行时仅支持 NVIDIA GPU，即使存在 DLL 也不可用"
		return st
	}

	// GPU found — scan for CUDA DLLs (local dir priority first, then system)
	dllPath := findCUDADLL()

	if dllPath == "" {
		st.Reason = fmt.Sprintf("检测到 %s\n驱动 %s（支持 CUDA %s）\n缺少 CUDA Runtime DLL\n请将 cudart64_*.dll 放入应用目录，或安装 CUDA Toolkit",
			gpu.Name, gpu.DriverVer, gpu.CUDAVersion)
		return st
	}

	st.OK = true
	st.DLLPath = dllPath
	st.Version = fmt.Sprintf("%s · CUDA %s · %s", gpu.Name, gpu.CUDAVersion, filepath.Base(dllPath))
	return st
}

// findCUDADLL scans for cudart64_*.dll with priority order:
// 1. EXE / install directory (user-placed, highest priority)
// 2. System32 (NVIDIA driver installs runtime there)
// 3. CUDA Toolkit bin directories
func findCUDADLL() string {
	pattern := "cudart64_*.dll"

	// Priority 1: local directories (exe dir, install dir, env dir)
	dirs := searchDirs()
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if dll := scanDirForDLL(dir, pattern); dll != "" {
			return dll
		}
	}

	// Priority 2: CUDA Toolkit installation paths
	for _, p := range cudaToolkitPaths() {
		if dll := scanDirForDLL(p, pattern); dll != "" {
			return dll
		}
	}

	return ""
}

// scanDirForDLL looks for a file matching pattern in a single directory.
func scanDirForDLL(dir, pattern string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if matchPattern(e.Name(), pattern) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// cudaToolkitPaths returns typical CUDA Toolkit installation directories.
func cudaToolkitPaths() []string {
	var paths []string
	if runtime.GOOS == "windows" {
		// CUDA Toolkit default: C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v*\bin
		base := `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA`
		entries, err := os.ReadDir(base)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() && strings.HasPrefix(e.Name(), "v") {
					paths = append(paths, filepath.Join(base, e.Name(), "bin"))
				}
			}
		}
	} else {
		// Linux: /usr/local/cuda/lib64, /usr/lib/x86_64-linux-gnu
		paths = append(paths, "/usr/local/cuda/lib64", "/usr/lib/x86_64-linux-gnu")
	}
	return paths
}

// findDLL searches common paths for the DLL.
// FindDLL searches for a DLL/exe matching pattern in standard locations.
func FindDLL(pattern string) string {
	dirs := searchDirs()
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		// 1. Direct files in this directory
		if dll := matchInDir(dir, pattern); dll != "" {
			return dll
		}
		// 2. One level of subdirectories (e.g., extracted release folders)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(dir, e.Name())
			if dll := matchInDir(sub, pattern); dll != "" {
				return dll
			}
		}
	}
	// Fallback: check PATH
	if runtime.GOOS == "windows" {
		if dll, err := findInPath(pattern); err == nil && dll != "" {
			return dll
		}
	}
	return ""
}

// matchInDir checks for a file matching pattern directly inside dir (no recursion).
func matchInDir(dir, pattern string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if matchPattern(e.Name(), pattern) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func searchDirs() []string {
	var dirs []string
	// 1. EXE directory
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	// 2. Custom env var
	if d := os.Getenv("EVEREVO_BACKEND_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	return dirs
}

func matchPattern(name, pattern string) bool {
	parts := strings.Split(pattern, "*")
	rest := name
	for i, p := range parts {
		if p == "" {
			continue
		}
		idx := strings.Index(strings.ToLower(rest), strings.ToLower(p))
		if idx < 0 {
			return false
		}
		if i < len(parts)-1 {
			rest = rest[idx+len(p):]
		} else {
			if !strings.HasSuffix(strings.ToLower(name), strings.ToLower(p)) {
				return false
			}
		}
	}
	return true
}
