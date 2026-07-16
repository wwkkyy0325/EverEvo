//go:build windows

// Package evolve implements self-evolution workflows: build, swap, ACP-driven
// evolution, and sandbox evolution. Formerly part of the monolithic App struct;
// now encapsulated as a standalone plugin with a minimal delegate interface.
package evolve

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/acp"
	evolvepkg "everevo/internal/evolve"
)

// ─── Delegate ────────────────────────────────────────────────────────

// Delegate provides the runtime dependencies the plugin needs from App.
type Delegate interface {
	SourceDir() string
	AcpBridge() *acp.Bridge
}

// ─── Plugin ──────────────────────────────────────────────────────────

// Plugin manages self-evolution workflows.
type Plugin struct {
	delegate Delegate
}

var instance *Plugin

// New creates the plugin with the given delegate and stores it as the singleton.
func New(d Delegate) *Plugin {
	p := &Plugin{delegate: d}
	instance = p
	return p
}

// Get returns the singleton plugin instance, or nil if New has not been called.
func Get() *Plugin { return instance }

// ─── Build ───────────────────────────────────────────────────────────

// BuildSelf runs the full build pipeline inside the source tree.
func (p *Plugin) BuildSelf() (map[string]any, error) {
	sourceDir := p.delegate.SourceDir()
	if sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自编译。请从源码目录运行程序")
	}

	task := evolvepkg.Task{
		ID:        fmt.Sprintf("build_%d", time.Now().UnixMilli()),
		Title:     "构建新版本",
		Status:    "building",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = evolvepkg.SaveTask(task)

	result, err := evolvepkg.BuildSelf(sourceDir, task, evolvepkg.SaveTask)
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

// ─── Swap ────────────────────────────────────────────────────────────

// SwapAndRestart replaces the running EXE with a newly-built one and restarts.
func (p *Plugin) SwapAndRestart() error {
	taskID := fmt.Sprintf("swap_%d", time.Now().UnixMilli())
	task := evolvepkg.Task{
		ID:        taskID,
		Title:     "替换并重启",
		Status:    "pending",
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	_ = evolvepkg.SaveTask(task)

	p.writeRestartMarker(taskID)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前 EXE 路径失败: %w", err)
	}
	sourceDir := p.delegate.SourceDir()
	if sourceDir == "" {
		return fmt.Errorf("源码目录未找到")
	}

	newExe := filepath.Join(sourceDir, "dist", "bin", "everevo.exe")
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
		evolvepkg.RestartSelf(exePath, zoneName)
		return nil
	}

	scriptPath, err := evolvepkg.WriteSwapScript(os.Getpid(), newExe, exePath, zoneName)
	if err != nil {
		return err
	}

	log.Printf("[evolve] 交换脚本: %s", scriptPath)
	log.Printf("[evolve] 旧 EXE: %s", exePath)
	log.Printf("[evolve] 新 EXE: %s", newExe)
	log.Printf("[evolve] 即将退出并重启（zone=%s）...", zoneName)

	if err := evolvepkg.LaunchSwapAndExit(scriptPath); err != nil {
		return err
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// writeRestartMarker writes a restart marker before the process exits.
func (p *Plugin) writeRestartMarker(taskID string) {
	m := evolvepkg.RestartMarker{TaskID: taskID, Timestamp: time.Now().Format(time.RFC3339), Reason: "evolve_swap"}
	data, _ := json.MarshalIndent(m, "", "  ")
	path := evolvepkg.RestartMarkerPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
	log.Printf("[evolve] 写入重启标记: %s", path)
}

// ─── ACP-driven evolution ────────────────────────────────────────────

// EvolveWithACP runs the full ACP-driven self-evolution pipeline.
func (p *Plugin) EvolveWithACP(title, message string) (map[string]any, error) {
	sourceDir := p.delegate.SourceDir()
	if sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法自进化。请从源码目录运行程序")
	}
	acpBridge := p.delegate.AcpBridge()
	if acpBridge == nil {
		return nil, fmt.Errorf("ACP 桥接未初始化")
	}

	taskID := fmt.Sprintf("acp_%d", time.Now().UnixMilli())
	task := evolvepkg.Task{
		ID:         taskID,
		Title:      title,
		Status:     "acp_running",
		CreatedAt:  time.Now().Format(time.RFC3339),
		AcpMessage: message,
	}
	_ = evolvepkg.SaveTask(task)

	log.Printf("[evolve:acp] 启动 ACP 进化任务: %s", title)

	// Phase 1: ACP code modification
	log.Println("[evolve:acp] Phase 1/3: OpenCode 代码修改...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	acpResult, err := acpBridge.Run(ctx, sourceDir, message, nil)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("ACP 执行失败: %v", err)
		_ = evolvepkg.SaveTask(task)
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
		_ = evolvepkg.SaveTask(task)
		return nil, fmt.Errorf("OpenCode 执行错误: %s", acpResult.Error)
	}

	log.Printf("[evolve:acp] OpenCode 完成: %d 个工具调用, %d tokens, $%.4f, 耗时 %s",
		len(acpResult.ToolCalls), acpResult.Tokens.Total, acpResult.Cost, acpResult.Duration)

	// Phase 2: Build
	task.Status = "building"
	_ = evolvepkg.SaveTask(task)

	log.Println("[evolve:acp] Phase 2/3: 构建新版本...")
	buildResult, err := evolvepkg.BuildSelf(sourceDir, task, evolvepkg.SaveTask)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建失败: %v", err)
		_ = evolvepkg.SaveTask(task)
		return nil, fmt.Errorf("构建阶段失败: %w", err)
	}

	// Phase 3: Swap & restart
	task.Status = "swapping"
	_ = evolvepkg.SaveTask(task)

	log.Println("[evolve:acp] Phase 3/3: 替换并重启...")
	if err := p.SwapAndRestart(); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("替换失败: %v", err)
		_ = evolvepkg.SaveTask(task)
		return nil, fmt.Errorf("替换阶段失败: %w", err)
	}

	task.Status = "done"
	_ = evolvepkg.SaveTask(task)

	return map[string]any{
		"success":   true,
		"taskId":    taskID,
		"acpResult": acpResult,
		"build":     buildResult,
	}, nil
}

// ─── Sandbox evolution ───────────────────────────────────────────────

// EvolveSandbox runs a complete sandbox evolution cycle:
// build -> prepare -> launch -> return sandbox info for A2A verification.
func (p *Plugin) EvolveSandbox() (map[string]any, error) {
	sourceDir := p.delegate.SourceDir()
	if sourceDir == "" {
		return nil, fmt.Errorf("源码目录未找到，无法进化")
	}

	buildExe := evolvepkg.BuildPath(sourceDir)
	if _, err := os.Stat(buildExe); err != nil {
		return nil, fmt.Errorf("构建产物未找到: %s (请先执行 BuildSelf)", buildExe)
	}

	inst, err := evolvepkg.RunEvolutionCycle(sourceDir, buildExe)
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
func (p *Plugin) AcceptSandbox(name string) error {
	sourceDir := p.delegate.SourceDir()
	inst := &evolvepkg.Instance{Name: evolvepkg.Name(name)}
	return evolvepkg.AcceptEvolution(sourceDir, inst)
}

// RejectSandbox stops the failed evolvepkg.
func (p *Plugin) RejectSandbox(name, reason string) error {
	inst := &evolvepkg.Instance{Name: evolvepkg.Name(name)}
	return evolvepkg.RejectEvolution(inst, reason)
}
