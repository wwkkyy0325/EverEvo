//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"everevo/internal/backends"
	"everevo/internal/backends/onnx"
	"everevo/internal/config"
	"everevo/internal/shell"
	"everevo/internal/httpclient"
	"everevo/internal/storage"
	"everevo/internal/sysinfo"
)

// ─── 系统信息 API ──────────────────────────────────────────────

func (a *App) GetDynamicInfo() *sysinfo.DynamicInfo { return sysinfo.CollectDynamic() }

func (a *App) GetSysInfo() *sysinfo.SysInfo {
	if a.sysInfoCache != nil { return a.sysInfoCache }
	info, _ := sysinfo.Collect()
	a.sysInfoCache = info
	return info
}

// GetBackends 返回所有推理后端的检测状态。
func (a *App) GetBackends() []backends.Status {
	list := backends.Detect()
	for i := range list {
		if list[i].Key == "onnx" {
			list[i].OK = onnx.Initialized()
			if !list[i].OK {
				list[i].Reason = "ONNX Runtime 未初始化，请重启应用或检查 DLL 版本"
			}
		}
	}
	return list
}

// GetUserConfigDir returns the user-level config directory (%APPDATA%\EverEvo).
func (a *App) GetUserConfigDir() string {
	return config.UserConfigDir()
}

// GetDataDir returns the root data directory (%APPDATA%/EverEvo/).
func (a *App) GetDataDir() string {
	dir := storage.DataDir()
	return dir
}

// GetModelsDir returns the model storage directory.
func (a *App) GetModelsDir() string {
	dir := storage.ModelsDir()
	return dir
}

// GetBackendDownloadURL returns the platform-specific download URL for a backend.
// variant: "" = default (CPU), "cuda" = CUDA build.
func (a *App) GetBackendDownloadURL(key string, mirror string, variant string) string {
	return backends.GetBackendDownloadURL(key, mirror, variant)
}

// GetPlatformInfo returns the current OS/arch for download selection.
func (a *App) GetPlatformInfo() backends.PlatformInfo {
	return backends.GetPlatform()
}

// GetMirrors returns available download mirrors.
func (a *App) GetMirrors() []backends.Mirror {
	return backends.GetMirrors()
}

// CheckNodeEnv checks if Node.js and npx are available on PATH.
func (a *App) CheckNodeEnv() map[string]bool {
	result := map[string]bool{"node": false, "npx": false}
	if _, err := exec.LookPath("node"); err == nil {
		result["node"] = true
	}
	if _, err := exec.LookPath("npx"); err == nil {
		result["npx"] = true
	}
	return result
}

// ─── File operations ──────────────────────────────────────────

// OpenFileLocation 在资源管理器中打开文件所在目录并选中该文件。
func (a *App) OpenFileLocation(path string) error {
	cmd := exec.Command("explorer", "/select,", path)
	return cmd.Start()
}

// OpenDir 在资源管理器中直接打开指定目录。
func (a *App) OpenDir(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}

// DeleteDownloadedFile 删除指定已下载文件。
func (a *App) DeleteDownloadedFile(relPath string) error {
	target := filepath.Join(storage.ModelsDir(), relPath)
	absModels, _ := filepath.Abs(storage.ModelsDir())
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absModels+string(filepath.Separator)) && absTarget != absModels {
		return fmt.Errorf("路径不安全: %s", relPath)
	}
	return os.Remove(target)
}

// ─── Start Menu shortcuts ────────────────────────────────────

// PinToStartMenu creates a Start Menu shortcut for the current EXE.
func (a *App) PinToStartMenu() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get exe path: %w", err)
	}
	shortcutPath := filepath.Join(os.Getenv("APPDATA"),
		"Microsoft", "Windows", "Start Menu", "Programs", "EverEvo.lnk")
	return createShortcut(shortcutPath, exe, filepath.Dir(exe), "EverEvo")
}

// UnpinFromStartMenu removes the Start Menu shortcut.
func (a *App) UnpinFromStartMenu() error {
	shortcutPath := filepath.Join(os.Getenv("APPDATA"),
		"Microsoft", "Windows", "Start Menu", "Programs", "EverEvo.lnk")
	if _, err := os.Stat(shortcutPath); os.IsNotExist(err) {
		return fmt.Errorf("快捷方式不存在")
	}
	return os.Remove(shortcutPath)
}

// HasStartMenuShortcut checks if the Start Menu shortcut exists.
func (a *App) HasStartMenuShortcut() bool {
	shortcutPath := filepath.Join(os.Getenv("APPDATA"),
		"Microsoft", "Windows", "Start Menu", "Programs", "EverEvo.lnk")
	_, err := os.Stat(shortcutPath)
	return err == nil
}

// DeleteDownloadedDir 删除指定已下载目录及其所有内容。
func (a *App) DeleteDownloadedDir(dirName string) error {
	target := filepath.Join(storage.ModelsDir(), dirName)
	absModels, _ := filepath.Abs(storage.ModelsDir())
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absModels+string(filepath.Separator)) && absTarget != absModels {
		return fmt.Errorf("路径不安全: %s", dirName)
	}
	return os.RemoveAll(target)
}

// ─── Proxy API ──────────────────────────────────────────────

// GetProxyStatus returns the current proxy detection result.
func (a *App) GetProxyStatus() httpclient.ProxyStatus {
	return httpclient.Detect()
}

// SetProxy sets the user-configured proxy URL, persists it, and applies it.
// Pass "" to clear.
func (a *App) SetProxy(proxyURL string) error {
	a.cfg.LLM.HTTPProxy = proxyURL
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	httpclient.SetUserProxy(proxyURL)
	return nil
}

// TestProxy attempts a quick connectivity test through the given proxy URL.
// Returns nil if the proxy is reachable, or an error message otherwise.
func (a *App) TestProxy(proxyURL string) error {
	return httpclient.Test(proxyURL)
}

// SetProxyEnabled toggles the global proxy kill-switch.
// When false, all HTTP requests use direct connection.
// Persists the setting so it survives restarts.
func (a *App) SetProxyEnabled(enabled bool) {
	httpclient.SetEnabled(enabled)
	a.cfg.LLM.ProxyEnabled = &enabled
	config.Save(a.cfg)
}

// ─── Shell Execution ────────────────────────────────────────────

// dangerousCmdPatterns are regex patterns that match potentially destructive
// commands. The LLM can still run these if needed — they produce an error that
// explains WHY the command was blocked and what to use instead.
var dangerousCmdPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	// Destructive disk operations
	{regexp.MustCompile(`(?i)\bformat\s+[a-z]:`), "格式化磁盘操作被拦截。如需清理文件，请使用 del 或 Remove-Item 指定具体路径。"},
	{regexp.MustCompile(`(?i)\bdiskpart\b`), "diskpart 分区操作被拦截。"},
	// Recursive delete of system roots
	{regexp.MustCompile(`(?i)\b(rm|del|rd|rmdir)\s+(-[rRf]+\s+)*[/\\]\b`), "递归删除根目录被拦截。请指定具体子目录。"},
	{regexp.MustCompile(`(?i)\b(del|rd|rmdir)\s+/[sq]\s+%SystemDrive%`), "删除系统盘被拦截。"},
	{regexp.MustCompile(`(?i)\brm\s+-rf\s+~?\s*$`), "rm -rf 无指定路径被拦截。"},
	// System state destruction
	{regexp.MustCompile(`(?i)\bwmic\s+.*\bdelete\b`), "WMIC delete 操作被拦截。"},
	{regexp.MustCompile(`(?i)\bsc\s+delete\b`), "服务删除操作被拦截。"},
	// Fork bomb patterns
	{regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:&\s*\}`), "fork bomb 模式被拦截。"},
	{regexp.MustCompile(`(?i)%0\|%0`), "fork bomb 模式被拦截。"},
}

// ShellExec runs a command through the OS shell with safety guards:
// dangerous-pattern blocking, timeout enforcement, and output truncation.
//
// Execution strategy (via internal/shell package):
//   - Direct os/exec when no shell features detected (≈80% of commands)
//     Arguments parsed and passed separately — no quoting issues, no CWE-78.
//   - Shell via SysProcAttr.CmdLine when pipes/redirects/chaining needed
//     Uses Go-recommended cmd.exe /s /c approach for correct quoting.
func (a *App) ShellExec(command string, cwd string, timeoutSec int) (map[string]any, error) {
	// ── Safety gate 1: dangerous pattern check ──
	for _, d := range dangerousCmdPatterns {
		if d.pattern.MatchString(command) {
			return nil, fmt.Errorf("安全拦截: %s", d.reason)
		}
	}

	// ── Sanitise timeout ──
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	// ── Resolve working directory ──
	if cwd == "" {
		if exeDir := storage.ExeDir(); exeDir != "" {
			cwd = exeDir
		} else {
			cwd, _ = os.Getwd()
		}
	}

	// ── Execute via shell package ──
	sr, err := shell.Execute(context.Background(), command, shell.Options{
		Cwd:     cwd,
		Timeout: time.Duration(timeoutSec) * time.Second,
	})
	if err != nil {
		return nil, err
	}

	// ── Build result (backward-compatible format) ──
	result := map[string]any{
		"exitCode": sr.ExitCode,
		"stdout":   sr.Stdout,
		"stderr":   sr.Stderr,
		"cwd":      sr.Cwd,
		"duration": sr.DurationMs,
	}

	// Truncate output to avoid flooding context (50KB max).
	const maxOutput = 50 * 1024
	if len(sr.Stdout) > maxOutput {
		result["stdout"] = sr.Stdout[:maxOutput] + fmt.Sprintf("\n\n…(输出已截断，共 %d bytes)", len(sr.Stdout))
	}

	return result, nil
}


// ─── Web Search (DuckDuckGo) ─────────────────────────────────

// WebSearch uses DuckDuckGo's HTML instant answer API (free, no key needed).
func (a *App) WebSearch(query string) ([]map[string]any, error) {
	url := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", strings.ReplaceAll(query, " ", "+"))
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client := httpclient.New(15 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	htmlStr := string(body)

	// Extract results with simple regex — robust enough for DuckDuckGo's HTML.
	linkRe := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>([^<]*)</a>`)
	snippetRe := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>([^<]*)</a>`)

	links := linkRe.FindAllStringSubmatch(htmlStr, 10)
	snippets := snippetRe.FindAllStringSubmatch(htmlStr, 10)

	var results []map[string]any
	for i, l := range links {
		if len(l) < 3 { continue }
		snippet := ""
		if i < len(snippets) && len(snippets[i]) > 1 {
			snippet = strings.TrimSpace(snippets[i][1])
		}
		results = append(results, map[string]any{
			"title":   strings.TrimSpace(l[2]),
			"url":     l[1],
			"snippet": snippet,
		})
	}
	return results, nil
}

// ─── Web Fetch (URL → text) ─────────────────────────────────

// WebFetch fetches a URL and extracts usable text content, stripping HTML when
// detected. Optional prompt extracts a targeted excerpt (2KB around matching
// keywords). Limits response body to 256KB.
func (a *App) WebFetch(url, prompt, depth string) (map[string]any, error) {
	if depth == "" {
		depth = "summary"
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client := httpclient.New(15 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	raw := string(body)
	contentType := resp.Header.Get("Content-Type")
	text := raw
	if strings.HasPrefix(strings.TrimSpace(raw), "<") || strings.Contains(contentType, "text/html") {
		text = stripHTML(raw)
	}
	text = strings.TrimSpace(text)

	// For large pages in summary mode, attempt LLM summarization via a
	// lightweight provider to keep context impact minimal (Haiku-gate pattern).
	originalSize := len(text)
	if depth == "summary" && originalSize > 4096 {
		if summary := a.trySummarizePage(url, text, prompt); summary != "" {
			return map[string]any{
				"url":            url,
				"contentType":    contentType,
				"text":           summary,
				"size":           len(body),
				"originalSize":   originalSize,
				"summarized":     true,
			}, nil
		}
	}

	// Fallback: return truncated raw text (full mode or summarization unavailable).
	if depth != "full" && len(text) > 8192 {
		head := text[:4096]
		tail := text[len(text)-2048:]
		text = head + fmt.Sprintf("\n\n─── [页面内容截断: %d chars 总计. 使用 depth=full 获取完整内容] ───\n\n", len(text)) + tail
	}

	result := map[string]any{
		"url":         url,
		"contentType": contentType,
		"text":        text,
		"size":        len(body),
	}
	if prompt != "" && depth != "summary" {
		result["excerpt"] = excerptAround(text, prompt, 2048)
	}
	return result, nil
}

// trySummarizePage attempts to summarize page content using a lightweight LLM.
// Returns "" if no suitable provider is available or summarization fails.
func (a *App) trySummarizePage(url, content, prompt string) string {
	prov, err := a.resolveExtractionProvider()
	if err != nil || prov == nil {
		return ""
	}

	// Cap content sent to summarizer at 32KB to stay within cheap model limits.
	contentToSummarize := content
	if len(contentToSummarize) > 32768 {
		contentToSummarize = contentToSummarize[:32768]
	}

	sumPrompt := "Summarize this web page content concisely in Chinese (max 500 chars). Focus on key facts, data, and actionable information. Skip navigation, ads, and boilerplate."
	if prompt != "" {
		sumPrompt = fmt.Sprintf("Extract information relevant to this query from the web page: %s. Respond concisely in Chinese (max 500 chars). Only include information that answers the query.", prompt)
	}

	msgs := []map[string]string{
		{"role": "user", "content": fmt.Sprintf("URL: %s\n\nContent:\n%s\n\n---\n%s", url, contentToSummarize, sumPrompt)},
	}
	msgsJSON, _ := json.Marshal(msgs)

	t := 0.1
	resp, err := a.chatCompletion(prov, msgsJSON, nil, chatOpts{MaxTokens: 600, Temperature: &t})
	if err != nil {
		return ""
	}

	if text := extractChatText(resp); text != "" {
		return text
	}
	return ""
}

// stripHTML removes all HTML tags and decodes common entities.
func stripHTML(s string) string {
	// Remove scripts, styles, comments (no backreferences — RE2 doesn't support \1)
	re := regexp.MustCompile(`(?is)<(?:script|style|noscript)[^>]*>.*?</(?:script|style|noscript)>`)
	s = re.ReplaceAllString(s, "")
	re = regexp.MustCompile(`<!--.*?-->`)
	s = re.ReplaceAllString(s, "")
	// Strip all remaining tags
	re = regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, " ")
	// Decode common entities
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	// Collapse whitespace
	re = regexp.MustCompile(`\s+`)
	s = re.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// excerptAround returns up to maxLen characters around the first occurrence of
// any keyword in text.
func excerptAround(text, prompt string, maxLen int) string {
	if text == "" || prompt == "" {
		return ""
	}
	words := strings.Fields(prompt)
	best := strings.Index(strings.ToLower(text), strings.ToLower(prompt))
	if best == -1 {
		for _, w := range words {
			if pos := strings.Index(strings.ToLower(text), strings.ToLower(w)); pos >= 0 {
				best = pos
				break
			}
		}
	}
	if best == -1 {
		if len(text) > maxLen {
			return text[:maxLen] + "…"
		}
		return text
	}
	start := best - maxLen/2
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(text) {
		end = len(text)
	}
	out := text[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out = out + "…"
	}
	return out
}
