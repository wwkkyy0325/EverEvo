//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/storage"

	"everevo/internal/acp"
	"everevo/internal/rag"
)

// ─── Self-Evolution: build from source, swap binary, restart ──────

// EvolveCapability describes whether source-code-level self-evolution
// is available on this machine.
type EvolveCapability struct {
	SourceAvailable bool   `json:"sourceAvailable"`
	SourceDir       string `json:"sourceDir"`
	BuildOutput     string `json:"buildOutput"` // where wails build puts the EXE
	CurrentExe      string `json:"currentExe"`
	GoAvailable     bool   `json:"goAvailable"`
	NodeAvailable   bool   `json:"nodeAvailable"`
	WailsAvailable  bool   `json:"wailsAvailable"`
}

// EvolveTask represents a pending or completed self-evolution task.
type EvolveTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // pending / acp_running / building / swapping / done / failed
	CreatedAt string `json:"createdAt"`
	Error     string `json:"error,omitempty"`
	// ACP fields
	AcpMessage   string           `json:"acpMessage,omitempty"`   // natural language task for OpenCode
	AcpSessionID string           `json:"acpSessionId,omitempty"` // OpenCode session ID
	AcpResult    *acp.Result      `json:"acpResult,omitempty"`    // ACP run result (for resume)
	AcpToolCalls []acp.ToolSummary `json:"acpToolCalls,omitempty"` // tools called by OpenCode
	AcpTokens    acp.TokenStats   `json:"acpTokens,omitempty"`    // token usage
	AcpCost      float64          `json:"acpCost,omitempty"`      // cost in USD
	AcpDuration  string           `json:"acpDuration,omitempty"`  // ACP run duration
}

func evolveTaskFile() string {
	dir, err := storage.AppDataDir()
	if err != nil {
		// Fallback: EXE-relative (dev mode / migration in progress).
		exePath, _ := os.Executable()
		return filepath.Join(filepath.Dir(exePath), "data", "evolve_tasks.json")
	}
	return filepath.Join(dir, "evolve_tasks.json")
}

// GetEvolveCapability reports whether the current instance can
// self-compile from source.
func (a *App) GetEvolveCapability() EvolveCapability {
	exe, _ := os.Executable()
	buildOutput := ""
	if a.sourceDir != "" {
		buildOutput = filepath.Join(a.sourceDir, "build", "bin", "everevo.exe")
	}
	return EvolveCapability{
		SourceAvailable: a.sourceDir != "",
		SourceDir:       a.sourceDir,
		BuildOutput:     buildOutput,
		CurrentExe:      exe,
		GoAvailable:     execFound("go"),
		NodeAvailable:   execFound("node"),
		WailsAvailable:  execFound("wails"),
	}
}

// BuildSelf runs the full build pipeline (frontend + wails build) inside
// the current zone's source tree. The new EXE lands at build/bin/everevo.exe
// relative to the source directory.
func (a *App) BuildSelf() (map[string]any, error) {
	if a.sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自编译。请从源码目录运行程序")
	}

	taskID := fmt.Sprintf("build_%d", time.Now().UnixMilli())
	task := EvolveTask{
		ID:        taskID,
		Title:     "构建新版本",
		Status:    "building",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = a.SaveEvolveTask(task)

	return a.buildSelfInternal(task)
}

// buildSelfInternal is the internal build logic shared by BuildSelf and EvolveWithACP.
func (a *App) buildSelfInternal(task EvolveTask) (map[string]any, error) {

	start := time.Now()
	var steps []string

	// 1. Build frontend
	frontendDir := filepath.Join(a.sourceDir, "frontend")
	if st, err := os.Stat(filepath.Join(frontendDir, "package.json")); err == nil && !st.IsDir() {
		log.Println("[evolve] 构建前端...")
		steps = append(steps, "npm install")
		cmd := exec.Command("npm", "install")
		cmd.Dir = frontendDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr := fmt.Errorf("npm install 失败: %w\n%s", err, string(out))
			task.Status = "failed"
			task.Error = buildErr.Error()
			_ = a.SaveEvolveTask(task)
			return nil, buildErr
		}

		steps = append(steps, "npm run build")
		cmd = exec.Command("npm", "run", "build")
		cmd.Dir = frontendDir
		out, err = cmd.CombinedOutput()
		if err != nil {
			buildErr := fmt.Errorf("npm run build 失败: %w\n%s", err, string(out))
			task.Status = "failed"
			task.Error = buildErr.Error()
			_ = a.SaveEvolveTask(task)
			return nil, buildErr
		}
		log.Printf("[evolve] 前端构建完成")
	} else {
		steps = append(steps, "(跳过前端——未找到 package.json)")
	}

	// 2. Wails build (compiles Go + bundles frontend)
	log.Println("[evolve] 构建 EXE (wails build -s)...")
	steps = append(steps, "wails build -s")
	cmd := exec.Command("wails", "build", "-s")
	cmd.Dir = a.sourceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		buildErr := fmt.Errorf("wails build 失败: %w\n%s", err, string(out))
		task.Status = "failed"
		task.Error = buildErr.Error()
		_ = a.SaveEvolveTask(task)
		return nil, buildErr
	}
	log.Printf("[evolve] EXE 构建完成")

	// Verify the output.
	newExe := filepath.Join(a.sourceDir, "build", "bin", "everevo.exe")
	if _, err := os.Stat(newExe); err != nil {
		buildErr := fmt.Errorf("构建产物未找到: %s", newExe)
		task.Status = "failed"
		task.Error = buildErr.Error()
		_ = a.SaveEvolveTask(task)
		return nil, buildErr
	}

	elapsed := time.Since(start).Round(time.Second)
	log.Printf("[evolve] 自编译完成，耗时 %v，产物: %s", elapsed, newExe)

	task.Status = "done"
	_ = a.SaveEvolveTask(task)

	return map[string]any{
		"success":   true,
		"outputExe": newExe,
		"duration":  elapsed.String(),
		"steps":     steps,
	}, nil
}

// SwapAndRestart replaces the running EXE with a newly-built one and
// restarts the application. It writes a helper PowerShell script, launches
// it detached, and then exits the current process.
//
// The swap script waits for this process to exit, copies the new EXE over
// the old one, and starts the new instance with the same --zone flag.
//
// Before exiting, it writes a restart marker file so the new instance knows
// this was an intentional evolution restart (not a crash).
func (a *App) SwapAndRestart() error {
	taskID := fmt.Sprintf("swap_%d", time.Now().UnixMilli())
	task := EvolveTask{
		ID:        taskID,
		Title:     "替换并重启",
		Status:    "pending",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = a.SaveEvolveTask(task)

	// 写重启标记文件，包含时间戳和任务 ID，供新实例检测。
	a.writeRestartMarker(taskID)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前 EXE 路径失败: %w", err)
	}

	if a.sourceDir == "" {
		return fmt.Errorf("源码目录未找到")
	}

	newExe := filepath.Join(a.sourceDir, "build", "bin", "everevo.exe")
	if _, err := os.Stat(newExe); err != nil {
		return fmt.Errorf("新 EXE 不存在: %s (请先执行 BuildSelf)", newExe)
	}

	// If already running from build/bin, the "new EXE" is the same file —
	// the build already overwrote it. Just restart.
	if strings.EqualFold(exePath, newExe) {
		log.Println("[evolve] EXE 已在 build/bin 位置，直接重启")
		return a.restartSelf()
	}

	zoneName := os.Getenv("EVEREVO_ZONE")
	if zoneName == "" {
		zoneName = "production"
	}

	// Write the swap script to a temp location.
	// Use safe placeholder replacement to avoid Go fmt conflicts with PowerShell $vars.
	scriptPath := filepath.Join(os.TempDir(), "everevo_swap.ps1")
	script := strings.NewReplacer(
		"{{OLD_PID}}", fmt.Sprintf("%d", os.Getpid()),
		"{{NEW_EXE}}", newExe,
		"{{TARGET_EXE}}", exePath,
		"{{ZONE_NAME}}", zoneName,
	).Replace(swapScript)
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("写入交换脚本失败: %w", err)
	}

	log.Printf("[evolve] 交换脚本: %s", scriptPath)
	log.Printf("[evolve] 旧 EXE: %s", exePath)
	log.Printf("[evolve] 新 EXE: %s", newExe)
	log.Printf("[evolve] 即将退出并重启（zone=%s）...", zoneName)

	// Launch the swap script in a new, detached PowerShell window.
	swapCmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath)
	swapCmd.SysProcAttr = nil // visible window — user sees the restart happen
	if err := swapCmd.Start(); err != nil {
		return fmt.Errorf("启动交换脚本失败: %w", err)
	}

	// Exit after a brief delay so the Wails HTTP response can flush.
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()

	return nil
}

// restartSelf is used when the EXE is already in-place (build/bin/everevo.exe).
func (a *App) restartSelf() error {
	exePath, _ := os.Executable()
	zoneName := os.Getenv("EVEREVO_ZONE")
	if zoneName == "" {
		zoneName = "production"
	}

	cmd := exec.Command(exePath, "--zone="+zoneName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新实例失败: %w", err)
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// ─── ACP-driven self-evolution ────────────────────────────────────

// EvolveWithACP runs the full ACP-driven self-evolution pipeline:
//   1. Delegate code modifications to OpenCode via ACP bridge
//   2. Build the modified source (frontend + wails build)
//   3. Swap the binary and restart
//
// This is the primary self-evolution entry point for AI-driven code changes.
// It persists the task at every stage so progress survives crashes.
func (a *App) EvolveWithACP(title, message string) (map[string]any, error) {
	if a.sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自进化。请从源码目录运行程序")
	}
	if a.acpBridge == nil {
		return nil, fmt.Errorf("ACP 桥接未初始化")
	}

	taskID := fmt.Sprintf("acp_%d", time.Now().UnixMilli())
	task := EvolveTask{
		ID:         taskID,
		Title:      title,
		Status:     "acp_running",
		CreatedAt:  time.Now().Format(time.RFC3339),
		AcpMessage: message,
	}
	_ = a.SaveEvolveTask(task)

	log.Printf("[evolve:acp] 启动 ACP 进化任务: %s", title)
	log.Printf("[evolve:acp] 任务描述: %s", message)

	// ── Phase 1: ACP code modification ─────────────────────────────
	log.Println("[evolve:acp] Phase 1/3: OpenCode 代码修改...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	acpResult, err := a.acpBridge.Run(ctx, a.sourceDir, message, nil)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("ACP 执行失败: %v", err)
		_ = a.SaveEvolveTask(task)
		return nil, fmt.Errorf("ACP 阶段失败: %w", err)
	}

	// Persist ACP results into the task.
	task.AcpSessionID = acpResult.SessionID
	task.AcpToolCalls = acpResult.ToolCalls
	task.AcpTokens = acpResult.Tokens
	task.AcpCost = acpResult.Cost
	task.AcpDuration = acpResult.Duration.String()

	if acpResult.Error != "" {
		task.Status = "failed"
		task.Error = fmt.Sprintf("OpenCode 错误: %s", acpResult.Error)
		_ = a.SaveEvolveTask(task)
		return nil, fmt.Errorf("OpenCode 执行错误: %s", acpResult.Error)
	}

	log.Printf("[evolve:acp] OpenCode 完成: %d 个工具调用, %d tokens, $%.4f, 耗时 %s",
		len(acpResult.ToolCalls), acpResult.Tokens.Total, acpResult.Cost, acpResult.Duration)
	log.Printf("[evolve:acp] OpenCode 输出:\n%s", acpResult.Text)

	// ── Phase 2: Build ─────────────────────────────────────────────
	task.Status = "building"
	_ = a.SaveEvolveTask(task)

	log.Println("[evolve:acp] Phase 2/3: 构建新版本...")
	buildResult, err := a.buildSelfInternal(task)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建失败: %v", err)
		_ = a.SaveEvolveTask(task)
		return nil, fmt.Errorf("构建阶段失败: %w", err)
	}

	// ── Phase 3: Swap & restart ────────────────────────────────────
	task.Status = "swapping"
	_ = a.SaveEvolveTask(task)

	log.Println("[evolve:acp] Phase 3/3: 替换并重启...")
	if err := a.SwapAndRestart(); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("替换失败: %v", err)
		_ = a.SaveEvolveTask(task)
		return nil, fmt.Errorf("替换阶段失败: %w", err)
	}

	// SwapAndRestart calls os.Exit — we never reach here.
	// But if we do (e.g. SwapAndRestart returned without exiting), mark done.
	task.Status = "done"
	_ = a.SaveEvolveTask(task)

	return map[string]any{
		"success":   true,
		"taskId":    taskID,
		"acpResult": acpResult,
		"build":     buildResult,
	}, nil
}

// ResumeAcpEvolveTasks attempts to auto-resume ACP evolve tasks that were
// interrupted mid-flight. Called at startup after the ACP bridge is ready.
func (a *App) ResumeAcpEvolveTasks() {
	tasks, err := a.loadEvolveTasks()
	if err != nil {
		return
	}

	for _, t := range tasks {
		if !strings.HasPrefix(t.ID, "acp_") {
			continue
		}
		switch t.Status {
		case "acp_running":
			// ACP was running when we crashed — OpenCode session is lost,
			// but we have the original message. Re-run from scratch.
			log.Printf("[evolve:acp] 🔄 恢复中断的 ACP 任务: %s", t.Title)
			go a.resumeAcpTask(t)
		case "building":
			// Build was in progress — re-run build.
			log.Printf("[evolve:acp] 🔄 恢复中断的构建任务: %s", t.Title)
			go a.resumeBuildTask(t)
		case "swapping":
			// Swap was about to happen — the old swap script may have run.
			// Just mark it done (if we're here, the swap didn't complete).
			log.Printf("[evolve:acp] ⚠ 交换中断: %s (需手动确认)", t.Title)
			t.Status = "failed"
			t.Error = "交换阶段中断，需手动检查"
			_ = a.SaveEvolveTask(t)
		}
	}
}

// resumeAcpTask re-runs an interrupted ACP evolution task from scratch.
func (a *App) resumeAcpTask(task EvolveTask) {
	if task.AcpMessage == "" {
		task.Status = "failed"
		task.Error = "无法恢复: 缺少原始任务描述"
		_ = a.SaveEvolveTask(task)
		return
	}
	// Re-run the full pipeline (will create a new taskID internally).
	result, err := a.EvolveWithACP(task.Title+" (恢复)", task.AcpMessage)
	if err != nil {
		log.Printf("[evolve:acp] 恢复失败: %v", err)
		// Mark original task as failed.
		task.Status = "failed"
		task.Error = fmt.Sprintf("恢复失败: %v", err)
		_ = a.SaveEvolveTask(task)
		return
	}
	log.Printf("[evolve:acp] 恢复成功: %v", result)
}

// resumeBuildTask re-runs a build that was interrupted.
func (a *App) resumeBuildTask(task EvolveTask) {
	_, err := a.buildSelfInternal(task)
	if err != nil {
		log.Printf("[evolve:acp] 构建恢复失败: %v", err)
		return
	}
	// After build, try swap.
	if err := a.SwapAndRestart(); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建后替换失败: %v", err)
		_ = a.SaveEvolveTask(task)
	}
}

func execFound(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (a *App) SaveEvolveTask(task EvolveTask) error {
	tasks, err := a.loadEvolveTasks()
	if err != nil {
		tasks = []EvolveTask{}
	}
	found := false
	for i, t := range tasks {
		if t.ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, task)
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	taskFile := evolveTaskFile()
	if err := os.MkdirAll(filepath.Dir(taskFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(taskFile, data, 0644)
}

func (a *App) ListEvolveTasks() []EvolveTask {
	tasks, err := a.loadEvolveTasks()
	if err != nil {
		return []EvolveTask{}
	}
	return tasks
}

func (a *App) loadEvolveTasks() ([]EvolveTask, error) {
	data, err := os.ReadFile(evolveTaskFile())
	if err != nil {
		return nil, err
	}
	var tasks []EvolveTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ResumeEvolveTasks auto-resolves stale task states on restart and writes
// pending tasks to the wiki page "evolve/resume" so the AI's system prompt
// picks them up on the next conversation.
func (a *App) ResumeEvolveTasks() []EvolveTask {
	tasks, err := a.loadEvolveTasks()
	if err != nil {
		log.Printf("[evolve] 加载进化任务失败: %v", err)
		return nil
	}
	var pending []EvolveTask
	dirty := false
	for i, t := range tasks {
		switch {
		case t.Status == "pending" && strings.HasPrefix(t.ID, "swap_"):
			tasks[i].Status = "done"
			tasks[i].Error = ""
			dirty = true
			log.Printf("[evolve] ✓ 交换完成 (%s)", t.Title)
		case t.Status == "acp_running":
			// ACP was running — handled by ResumeAcpEvolveTasks.
			pending = append(pending, t)
			continue
		case t.Status == "building":
			// For ACP tasks, handled by ResumeAcpEvolveTasks.
			if strings.HasPrefix(t.ID, "acp_") {
				pending = append(pending, t)
				continue
			}
			tasks[i].Status = "failed"
			tasks[i].Error = "进程重启中断"
			dirty = true
			log.Printf("[evolve] ⚠ 构建中断: %s", t.Title)
		default:
			if t.Status == "pending" {
				pending = append(pending, t)
				log.Printf("[evolve] ⚠ 待处理任务: %s (%s)", t.Title, t.Status)
			}
		}
	}
	if dirty {
		if err := a.persistEvolveTasks(tasks); err != nil {
			log.Printf("[evolve] 持久化任务状态失败: %v", err)
		}
	}
	// 将未解决的残留在务写入 wiki 页，AI 下次对话会自动感知。
	a.syncEvolveResumeWiki(pending)
	return pending
}

// syncEvolveResumeWiki 把残留在务写入 wiki 页 evolve/resume，
// 供系统提示词自动注入——AI 下次对话开篇就能看到。
func (a *App) syncEvolveResumeWiki(pending []EvolveTask) {
	if a.memoryStore == nil {
		return
	}
	libID, err := a.memoryStore.DefaultLibrary()
	if err != nil || libID == "" {
		return
	}
	ws := a.getWikiStore(libID)
	if ws == nil {
		return
	}
	if len(pending) == 0 {
		// 无残留在务，清理旧页面。
		_ = ws.DeletePage("evolve/resume")
		return
	}
	var lines []string
	lines = append(lines, "# 🔄 进化残留在务（重启后待跟进）", "")
	lines = append(lines, "以下任务在上次重启前未完成，请优先处理：", "")
	for _, t := range pending {
		lines = append(lines, fmt.Sprintf("- **[%s]** `%s` — 状态: %s, 创建于 %s",
			t.Title, t.ID, t.Status, t.CreatedAt))
		if t.Error != "" {
			lines = append(lines, fmt.Sprintf("  - 错误: %s", t.Error))
		}
	}
	lines = append(lines, "", "---", "", "*此页面由系统自动生成，任务解决后自动清除。*")
	content := strings.Join(lines, "\n")

	dir := a.memoryStore.EmbeddingModelDir()
	if dir == "" {
		// 无嵌入模型可用，只存 DB 不建索引（下次加载模型后手动 reindex）。
		_ = ws.SavePageRaw("evolve/resume", "进化残留在务", content)
		log.Printf("[evolve] 📋 残留在务已写入 wiki (无索引): evolve/resume (%d 项)", len(pending))
		return
	}
	embedFn := func(texts []string) ([][]float32, error) {
		return rag.EmbedChunks(dir, texts)
	}
	if err := ws.SavePage("evolve/resume", "进化残留在务", content, embedFn); err != nil {
		log.Printf("[evolve] ⚠ 写入 resume wiki 失败: %v", err)
	} else {
		log.Printf("[evolve] 📋 残留在务已写入 wiki: evolve/resume (%d 项)", len(pending))
	}
}

// persistEvolveTasks 将任务列表写回磁盘。
func (a *App) persistEvolveTasks(tasks []EvolveTask) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	taskFile := evolveTaskFile()
	if err := os.MkdirAll(filepath.Dir(taskFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(taskFile, data, 0644)
}

// RestartMarker 是重启标记文件的内容。
type RestartMarker struct {
	TaskID    string `json:"taskId"`
	Timestamp string `json:"timestamp"`
	Reason    string `json:"reason"` // "evolve_swap" / "manual_restart"
}

func restartMarkerPath() string {
	exePath, _ := os.Executable()
	return filepath.Join(filepath.Dir(exePath), "data", "restart_marker.json")
}

// writeRestartMarker 在退出前写入重启标记，供新实例读取。
func (a *App) writeRestartMarker(taskID string) {
	m := RestartMarker{
		TaskID:    taskID,
		Timestamp: time.Now().Format(time.RFC3339),
		Reason:    "evolve_swap",
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	path := restartMarkerPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
	log.Printf("[evolve] 写入重启标记: %s", path)
}

// readRestartMarker 读取重启标记（如果存在），读取后自动清除。
func (a *App) readRestartMarker() *RestartMarker {
	path := restartMarkerPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m RestartMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	// 读取后清除，避免下次启动误判。
	_ = os.Remove(path)
	return &m
}

// swapScript is a PowerShell script that:
// 1. Waits for the old process (PID) to exit
// 2. Copies the new EXE over the old EXE
// 3. Launches the new EXE with the same --zone flag
const swapScript = `
param(
    [int]$OldPid = {{OLD_PID}},
    [string]$NewExe = "{{NEW_EXE}}",
    [string]$TargetExe = "{{TARGET_EXE}}",
    [string]$ZoneName = "{{ZONE_NAME}}"
)

Write-Host "EverEvo 自进化: 等待旧进程 (PID $OldPid) 退出..."
try {
    $proc = Get-Process -Id $OldPid -ErrorAction SilentlyContinue
    if ($proc) {
        $proc.WaitForExit()
    }
} catch {
    # Process already gone.
}
Start-Sleep -Seconds 1

Write-Host "正在替换 EXE..."
try {
    Copy-Item -Path $NewExe -Destination $TargetExe -Force
    Write-Host "已替换: $TargetExe"
} catch {
    Write-Host "替换失败: $_"
    Read-Host "按 Enter 退出"
    exit 1
}

Write-Host "正在启动新版本 (zone=$ZoneName)..."
$args = "--zone=$ZoneName"
try {
    Start-Process -FilePath $TargetExe -ArgumentList $args
    Write-Host "启动成功!"
} catch {
    Write-Host "启动失败: $_"
    Read-Host "按 Enter 退出"
    exit 1
}

Start-Sleep -Seconds 2
`
