package evolve

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecFound checks whether a command is available on PATH.
func ExecFound(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SwapScript is a PowerShell script that waits for the old process, copies the
// new EXE, and launches with the same --zone flag. Template placeholders:
// {{OLD_PID}}, {{NEW_EXE}}, {{TARGET_EXE}}, {{ZONE_NAME}}.
const SwapScript = `
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

// BuildResult holds the outcome of a BuildSelf run.
type BuildResult struct {
	Success   bool     `json:"success"`
	OutputExe string   `json:"outputExe"`
	Duration  string   `json:"duration"`
	Steps     []string `json:"steps"`
}

// BuildSelf runs frontend + wails build inside sourceDir. Persists task state
// via saveTask callback at each stage.
func BuildSelf(sourceDir string, task Task, saveTask func(Task) error) (BuildResult, error) {
	start := time.Now()
	var steps []string

	// 1. Build frontend
	frontendDir := filepath.Join(sourceDir, "frontend")
	if st, err := os.Stat(filepath.Join(frontendDir, "package.json")); err == nil && !st.IsDir() {
		log.Println("[evolve] 构建前端...")
		steps = append(steps, "npm install")
		cmd := exec.Command("npm", "install")
		cmd.Dir = frontendDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			task.Status = "failed"
			task.Error = fmt.Sprintf("npm install 失败: %v\n%s", err, string(out))
			_ = saveTask(task)
			return BuildResult{}, fmt.Errorf("npm install 失败: %w\n%s", err, string(out))
		}

		steps = append(steps, "npm run build")
		cmd = exec.Command("npm", "run", "build")
		cmd.Dir = frontendDir
		out, err = cmd.CombinedOutput()
		if err != nil {
			task.Status = "failed"
			task.Error = fmt.Sprintf("npm run build 失败: %v\n%s", err, string(out))
			_ = saveTask(task)
			return BuildResult{}, fmt.Errorf("npm run build 失败: %w\n%s", err, string(out))
		}
		log.Println("[evolve] 前端构建完成")
	} else {
		steps = append(steps, "(跳过前端——未找到 package.json)")
	}

	// 2. Wails build
	log.Println("[evolve] 构建 EXE (wails build -s)...")
	steps = append(steps, "wails build -s")
	cmd := exec.Command("wails", "build", "-s")
	cmd.Dir = sourceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("wails build 失败: %v\n%s", err, string(out))
		_ = saveTask(task)
		return BuildResult{}, fmt.Errorf("wails build 失败: %w\n%s", err, string(out))
	}
	log.Println("[evolve] EXE 构建完成")

	newExe := filepath.Join(sourceDir, "dist", "bin", "everevo.exe")
	if _, err := os.Stat(newExe); err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("构建产物未找到: %s", newExe)
		_ = saveTask(task)
		return BuildResult{}, fmt.Errorf("构建产物未找到: %s", newExe)
	}

	elapsed := time.Since(start).Round(time.Second)
	log.Printf("[evolve] 自编译完成，耗时 %v，产物: %s", elapsed, newExe)

	task.Status = "done"
	_ = saveTask(task)

	return BuildResult{
		Success:   true,
		OutputExe: newExe,
		Duration:  elapsed.String(),
		Steps:     steps,
	}, nil
}

// WriteSwapScript writes the swap PowerShell script to a temp location, with
// template placeholders replaced.
func WriteSwapScript(oldPID int, newExe, targetExe, zoneName string) (string, error) {
	scriptPath := filepath.Join(os.TempDir(), "everevo_swap.ps1")
	script := strings.NewReplacer(
		"{{OLD_PID}}", fmt.Sprintf("%d", oldPID),
		"{{NEW_EXE}}", newExe,
		"{{TARGET_EXE}}", targetExe,
		"{{ZONE_NAME}}", zoneName,
	).Replace(SwapScript)
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("写入交换脚本失败: %w", err)
	}
	return scriptPath, nil
}

// LaunchSwapAndExit launches the swap script and returns (caller should then
// os.Exit). Returns an error if the script couldn't be started.
func LaunchSwapAndExit(scriptPath string) error {
	swapCmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath)
	if err := swapCmd.Start(); err != nil {
		return fmt.Errorf("启动交换脚本失败: %w", err)
	}
	return nil
}

// RestartSelf launches a new instance and exits the current process.
func RestartSelf(exePath, zoneName string) {
	cmd := exec.Command(exePath, "--zone="+zoneName)
	_ = cmd.Start()
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}
