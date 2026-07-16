//go:build windows

package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/evolve"
	"everevo/internal/rag"

	evolvePlugin "everevo/plugins/tools/evolve"
)

// GetEvolveCapability reports whether the current instance can self-compile.
func (a *App) GetEvolveCapability() evolve.Capability {
	exe, _ := os.Executable()
	buildOutput := ""
	if a.sourceDir != "" {
		buildOutput = filepath.Join(a.sourceDir, "dist", "bin", "everevo.exe")
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
	return evolvePlugin.Get().BuildSelf()
}

// SwapAndRestart replaces the running EXE with a newly-built one and restarts.
func (a *App) SwapAndRestart() error {
	return evolvePlugin.Get().SwapAndRestart()
}

// ─── ACP-driven self-evolution ────────────────────────────────────

// EvolveWithACP runs the full ACP-driven self-evolution pipeline.
func (a *App) EvolveWithACP(title, message string) (map[string]any, error) {
	return evolvePlugin.Get().EvolveWithACP(title, message)
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
	return evolvePlugin.Get().EvolveSandbox()
}

// AcceptSandbox promotes the verified sandbox as the new primary.
func (a *App) AcceptSandbox(name string) error {
	return evolvePlugin.Get().AcceptSandbox(name)
}

// RejectSandbox stops the failed sandbox.
func (a *App) RejectSandbox(name, reason string) error {
	return evolvePlugin.Get().RejectSandbox(name, reason)
}
