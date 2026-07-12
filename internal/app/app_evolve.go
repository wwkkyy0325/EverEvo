//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/evolve"
	"everevo/internal/rag"
	"everevo/internal/sandbox"
)

// GetEvolveCapability reports whether the current instance can self-compile.
func (a *App) GetEvolveCapability() evolve.Capability {
	exe, _ := os.Executable()
	buildOutput := ""
	if a.sourceDir != "" {
		buildOutput = filepath.Join(a.sourceDir, "build", "bin", "everevo.exe")
	}
	return evolve.Capability{
		SourceAvailable: a.sourceDir != "",
		SourceDir:       a.sourceDir,
		BuildOutput:     buildOutput,
		CurrentExe:      exe,
		GoAvailable:     evolve.ExecFound("go"),
		NodeAvailable:   evolve.ExecFound("node"),
		WailsAvailable:  evolve.ExecFound("wails"),
	}
}

// BuildSelf runs the full build pipeline inside the current zone's source tree.
func (a *App) BuildSelf() (map[string]any, error) {
	if a.sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自编译。请从源码目录运行程序")
	}

	task := evolve.Task{
		ID:        fmt.Sprintf("build_%d", time.Now().UnixMilli()),
		Title:     "构建新版本",
		Status:    "building",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = evolve.SaveTask(task)

	result, err := evolve.BuildSelf(a.sourceDir, task, evolve.SaveTask)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"success":   result.Success,
		"outputExe": result.OutputExe,
		"duration":  result.Duration,
		"steps":     result.Steps,
	}, nil
}

// SwapAndRestart replaces the running EXE with a newly-built one and restarts.
func (a *App) SwapAndRestart() error {
	taskID := fmt.Sprintf("swap_%d", time.Now().UnixMilli())
	task := evolve.Task{
		ID:        taskID,
		Title:     "替换并重启",
		Status:    "pending",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = evolve.SaveTask(task)

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

	zoneName := os.Getenv("EVEREVO_ZONE")
	if zoneName == "" {
		zoneName = "production"
	}

	// If already running from build/bin, just restart in place.
	if strings.EqualFold(exePath, newExe) {
		log.Println("[evolve] EXE 已在 build/bin 位置，直接重启")
		evolve.RestartSelf(exePath, zoneName)
		return nil
	}

	scriptPath, err := evolve.WriteSwapScript(os.Getpid(), newExe, exePath, zoneName)
	if err != nil {
		return err
	}

	log.Printf("[evolve] 交换脚本: %s", scriptPath)
	log.Printf("[evolve] 旧 EXE: %s", exePath)
	log.Printf("[evolve] 新 EXE: %s", newExe)
	log.Printf("[evolve] 即将退出并重启（zone=%s）...", zoneName)

	if err := evolve.LaunchSwapAndExit(scriptPath); err != nil {
		return err
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// ─── ACP-driven self-evolution ────────────────────────────────────

// EvolveWithACP runs the full ACP-driven self-evolution pipeline.
func (a *App) EvolveWithACP(title, message string) (map[string]any, error) {
	if a.sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自进化。请从源码目录运行程序")
	}
	if a.acpBridge == nil {
		return nil, fmt.Errorf("ACP 桥接未初始化")
	}

	taskID := fmt.Sprintf("acp_%d", time.Now().UnixMilli())
	task := evolve.Task{
		ID:         taskID,
		Title:      title,
		Status:     "acp_running",
		CreatedAt:  time.Now().Format(time.RFC3339),
		AcpMessage: message,
	}
	_ = evolve.SaveTask(task)

	log.Printf("[evolve:acp] 启动 ACP 进化任务: %s", title)

	// Phase 1: ACP code modification
	log.Println("[evolve:acp] Phase 1/3: OpenCode 代码修改...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	acpResult, err := a.acpBridge.Run(ctx, a.sourceDir, message, nil)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("ACP 执行失败: %v", err)
		_ = evolve.SaveTask(task)
		return nil, fmt.Errorf("ACP 阶段失败: %w", err)
	}

	task.AcpSessionID = acpResult.SessionID
	task.AcpToolCalls = acpResult.ToolCalls
	task.AcpTokens = acpResult.Tokens
	task.AcpCost = acpResult.Cost
	task.AcpDuration = acpResult.Duration.String()

	if acpResult.Error != "" {
		task.Status = "failed"
		task.Error = fmt.Sprintf("OpenCode 错误: %s", acpResult.Error)
		_ = evolve.SaveTask(task)
		return nil, fmt.Errorf("OpenCode 执行错误: %s", acpResult.Error)
	}

	log.Printf("[evolve:acp] OpenCode 完成: %d 个工具调用, %d tokens, $%.4f, 耗时 %s",
		len(acpResult.ToolCalls), acpResult.Tokens.Total, acpResult.Cost, acpResult.Duration)

	// Phase 2: Build
	task.Status = "building"
	_ = evolve.SaveTask(task)

	log.Println("[evolve:acp] Phase 2/3: 构建新版本...")
	buildResult, err := evolve.BuildSelf(a.sourceDir, task, evolve.SaveTask)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建失败: %v", err)
		_ = evolve.SaveTask(task)
		return nil, fmt.Errorf("构建阶段失败: %w", err)
	}

	// Phase 3: Swap & restart
	task.Status = "swapping"
	_ = evolve.SaveTask(task)

	log.Println("[evolve:acp] Phase 3/3: 替换并重启...")
	if err := a.SwapAndRestart(); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("替换失败: %v", err)
		_ = evolve.SaveTask(task)
		return nil, fmt.Errorf("替换阶段失败: %w", err)
	}

	task.Status = "done"
	_ = evolve.SaveTask(task)

	return map[string]any{
		"success":   true,
		"taskId":    taskID,
		"acpResult": acpResult,
		"build":     buildResult,
	}, nil
}

// ─── Task persistence (thin wrappers) ──────────────────────────────

func (a *App) SaveEvolveTask(task evolve.Task) error { return evolve.SaveTask(task) }

func (a *App) ListEvolveTasks() []evolve.Task {
	tasks, err := evolve.LoadTasks()
	if err != nil {
		return []evolve.Task{}
	}
	return tasks
}

// ─── Resume / recovery ────────────────────────────────────────────

// ResumeAcpEvolveTasks auto-resumes ACP evolve tasks interrupted mid-flight.
func (a *App) ResumeAcpEvolveTasks() {
	tasks, err := evolve.LoadTasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		if !strings.HasPrefix(t.ID, "acp_") {
			continue
		}
		switch t.Status {
		case "acp_running":
			log.Printf("[evolve:acp] 🔄 恢复中断的 ACP 任务: %s", t.Title)
			go a.resumeAcpTask(t)
		case "building":
			log.Printf("[evolve:acp] 🔄 恢复中断的构建任务: %s", t.Title)
			go a.resumeBuildTask(t)
		case "swapping":
			log.Printf("[evolve:acp] ⚠ 交换中断: %s (需手动确认)", t.Title)
			t.Status = "failed"
			t.Error = "交换阶段中断，需手动检查"
			_ = evolve.SaveTask(t)
		}
	}
}

func (a *App) resumeAcpTask(task evolve.Task) {
	if task.AcpMessage == "" {
		task.Status = "failed"
		task.Error = "无法恢复: 缺少原始任务描述"
		_ = evolve.SaveTask(task)
		return
	}
	result, err := a.EvolveWithACP(task.Title+" (恢复)", task.AcpMessage)
	if err != nil {
		log.Printf("[evolve:acp] 恢复失败: %v", err)
		task.Status = "failed"
		task.Error = fmt.Sprintf("恢复失败: %v", err)
		_ = evolve.SaveTask(task)
		return
	}
	log.Printf("[evolve:acp] 恢复成功: %v", result)
}

func (a *App) resumeBuildTask(task evolve.Task) {
	_, err := evolve.BuildSelf(a.sourceDir, task, evolve.SaveTask)
	if err != nil {
		log.Printf("[evolve:acp] 构建恢复失败: %v", err)
		return
	}
	if err := a.SwapAndRestart(); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建后替换失败: %v", err)
		_ = evolve.SaveTask(task)
	}
}

// ResumeEvolveTasks auto-resolves stale task states on restart.
func (a *App) ResumeEvolveTasks() []evolve.Task {
	tasks, err := evolve.LoadTasks()
	if err != nil {
		log.Printf("[evolve] 加载进化任务失败: %v", err)
		return nil
	}
	var pending []evolve.Task
	dirty := false
	for i, t := range tasks {
		switch {
		case t.Status == "pending" && strings.HasPrefix(t.ID, "swap_"):
			tasks[i].Status = "done"
			tasks[i].Error = ""
			dirty = true
			log.Printf("[evolve] ✓ 交换完成 (%s)", t.Title)
		case t.Status == "acp_running", (t.Status == "building" && strings.HasPrefix(t.ID, "acp_")):
			pending = append(pending, t)
			continue
		case t.Status == "building":
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
		if err := evolve.PersistTasks(tasks); err != nil {
			log.Printf("[evolve] 持久化任务状态失败: %v", err)
		}
	}
	a.syncEvolveResumeWiki(pending)
	return pending
}

func (a *App) syncEvolveResumeWiki(pending []evolve.Task) {
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

// ─── Restart marker ────────────────────────────────────────────────

func (a *App) writeRestartMarker(taskID string) {
	m := evolve.RestartMarker{TaskID: taskID, Timestamp: time.Now().Format(time.RFC3339), Reason: "evolve_swap"}
	data, _ := json.MarshalIndent(m, "", "  ")
	path := evolve.RestartMarkerPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
	log.Printf("[evolve] 写入重启标记: %s", path)
}

func (a *App) readRestartMarker() *evolve.RestartMarker { return evolve.ReadRestartMarker() }

// ─── ACP Bridge ────────────────────────────────────────────────────

// AcpCheck verifies that OpenCode is available on this system.
func (a *App) AcpCheck() map[string]any {
	if a.acpBridge == nil {
		return map[string]any{"ok": false, "error": "ACP 桥接未初始化"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if err := a.acpBridge.Check(ctx); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	ver, _ := a.acpBridge.Version()
	return map[string]any{"ok": true, "version": ver}
}

// AcpRun delegates a natural-language task to OpenCode and returns results.
func (a *App) AcpRun(message string) map[string]any {
	if a.acpBridge == nil {
		return map[string]any{"ok": false, "error": "ACP 桥接未初始化"}
	}
	if a.sourceDir == "" {
		return map[string]any{"ok": false, "error": "源码目录未找到"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	result, err := a.acpBridge.Run(ctx, a.sourceDir, message, nil)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{
		"ok":        result.Error == "",
		"text":      result.Text,
		"sessionId": result.SessionID,
		"toolCalls": result.ToolCalls,
		"snapshots": result.Snapshots,
		"tokens": map[string]int{
			"total": result.Tokens.Total, "input": result.Tokens.Input,
			"output": result.Tokens.Output, "reasoning": result.Tokens.Reasoning,
		},
		"cost": result.Cost, "duration": result.Duration.String(),
		"error": result.Error,
	}
}

// ─── Sandbox evolution ─────────────────────────────────────────────

// EvolveSandbox runs a complete sandbox evolution cycle:
// build → prepare → launch → return sandbox info for A2A verification.
// The caller (AI or frontend) is responsible for A2A-verifying the fix.
func (a *App) EvolveSandbox() (map[string]any, error) {
	if a.sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法进化")
	}

	buildExe := evolve.BuildPath(a.sourceDir)
	if _, err := os.Stat(buildExe); err != nil {
		return nil, fmt.Errorf("构建产物未找到: %s (请先执行 BuildSelf)", buildExe)
	}

	inst, err := evolve.RunEvolutionCycle(a.sourceDir, buildExe)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"ok":       true,
		"instance": inst.Name,
		"exePath":  inst.ExePath,
		"zone":     inst.Zone,
		"mcpPort":  inst.MCPPort,
		"a2aPort":  inst.A2APort,
		"pid":      inst.PID,
		"status":   "running",
	}, nil
}

// AcceptSandbox promotes the verified sandbox as the new primary.
func (a *App) AcceptSandbox(name string) error {
	inst := &sandbox.Instance{Name: sandbox.Name(name)}
	return evolve.AcceptEvolution(a.sourceDir, inst)
}

// RejectSandbox stops the failed sandbox.
func (a *App) RejectSandbox(name, reason string) error {
	inst := &sandbox.Instance{Name: sandbox.Name(name)}
	return evolve.RejectEvolution(inst, reason)
}
