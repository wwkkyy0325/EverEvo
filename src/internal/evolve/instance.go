// Package evolve manages the two alternating evolution sandbox instances
// (alpha / beta). Each instance is a self-contained EverEvo process running
// in its own zone with dedicated MCP and A2A ports.
//
// Lifecycle:
//   - Prepare: copy new EXE to sandbox/<name>/
//   - Launch: start the instance as a subprocess
//   - WaitReady: poll A2A health endpoint until the instance responds
//   - Verify: the orchestrator (old instance) sends A2A verification tasks
//   - Accept: the old instance exits gracefully, the new one becomes primary
//   - Reject: the old instance kills the new one and reports failure
package evolve

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Name identifies a sandbox slot.
type Name string

const (
	Alpha Name = "alpha"
	Beta  Name = "beta"
)

// Ports for each sandbox instance.
const (
	AlphaMCPPort = 19401
	AlphaA2APort = 19802
	BetaMCPPort  = 19402
	BetaA2APort  = 19803
)

// Instance represents a sandboxed EverEvo process.
type Instance struct {
	Name    Name   `json:"name"`
	Dir     string `json:"dir"`     // sandbox/<name>/
	ExePath string `json:"exePath"` // sandbox/<name>/everevo.exe
	Zone    string `json:"zone"`    // zone name (same as Name)
	MCPPort int    `json:"mcpPort"`
	A2APort int    `json:"a2aPort"`
	PID     int    `json:"pid"`
	cmd     *exec.Cmd
}

// MCPPortFor returns the MCP port for a named sandbox.
func MCPPortFor(n Name) int {
	switch n {
	case Alpha:
		return AlphaMCPPort
	case Beta:
		return BetaMCPPort
	default:
		return AlphaMCPPort
	}
}

// A2APortFor returns the A2A port for a named sandbox.
func A2APortFor(n Name) int {
	switch n {
	case Alpha:
		return AlphaA2APort
	case Beta:
		return BetaA2APort
	default:
		return AlphaA2APort
	}
}

// Other returns the opposite sandbox name.
func Other(n Name) Name {
	if n == Alpha {
		return Beta
	}
	return Alpha
}

// Prepare creates the sandbox directory and copies the EXE into it.
// buildExePath is the path to the newly-built EXE (e.g. build/bin/everevo.exe).
// sandboxRoot is the project root (e.g. F:/EverEvo).
func Prepare(sandboxRoot string, n Name, buildExePath string) (*Instance, error) {
	dir := filepath.Join(sandboxRoot, "sandbox", string(n))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create sandbox dir: %w", err)
	}

	dst := filepath.Join(dir, "everevo.exe")
	log.Printf("[sandbox:%s] 准备沙箱: %s → %s", n, buildExePath, dst)

	srcData, err := os.ReadFile(buildExePath)
	if err != nil {
		return nil, fmt.Errorf("read build EXE: %w", err)
	}
	if err := os.WriteFile(dst, srcData, 0755); err != nil {
		return nil, fmt.Errorf("copy EXE: %w", err)
	}

	return &Instance{
		Name:    n,
		Dir:     dir,
		ExePath: dst,
		Zone:    string(n),
		MCPPort: MCPPortFor(n),
		A2APort: A2APortFor(n),
	}, nil
}

// Launch starts the sandbox instance. The process runs detached (no window).
// zoneName and ports are passed as command-line flags.
func (s *Instance) Launch() error {
	if s.cmd != nil && s.PID > 0 {
		return fmt.Errorf("[sandbox:%s] 已在运行 (PID %d)", s.Name, s.PID)
	}

	log.Printf("[sandbox:%s] 启动: %s --zone=%s --mcp-port=%d --a2a-port=%d",
		s.Name, s.ExePath, s.Zone, s.MCPPort, s.A2APort)

	s.cmd = exec.Command(s.ExePath,
		"--zone="+s.Zone,
		"--sandbox",
		fmt.Sprintf("--mcp-port=%d", s.MCPPort),
		fmt.Sprintf("--a2a-port=%d", s.A2APort),
	)
	s.cmd.Dir = s.Dir
	s.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	// Redirect stdout/stderr to log files for debugging.
	logDir := filepath.Join(s.Dir, "logs")
	os.MkdirAll(logDir, 0755)
	stdout, _ := os.Create(filepath.Join(logDir, "stdout.log"))
	stderr, _ := os.Create(filepath.Join(logDir, "stderr.log"))
	s.cmd.Stdout = stdout
	s.cmd.Stderr = stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("[sandbox:%s] 启动失败: %w", s.Name, err)
	}

	s.PID = s.cmd.Process.Pid
	log.Printf("[sandbox:%s] 已启动 (PID %d)", s.Name, s.PID)
	return nil
}

// WaitReady polls the instance until it responds on its A2A port or times out.
func (s *Instance) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/.well-known/agent.json", s.A2APort)

	for time.Now().Before(deadline) {
		if s.IsRunning() {
			// Try the A2A agent card endpoint.
			resp, err := httpGet(url)
			if err == nil && resp {
				log.Printf("[sandbox:%s] A2A 就绪 (port %d)", s.Name, s.A2APort)
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("[sandbox:%s] 启动超时 (%v)", s.Name, timeout)
}

// IsRunning reports whether the sandbox process is still alive.
func (s *Instance) IsRunning() bool {
	if s.cmd == nil || s.cmd.Process == nil {
		return false
	}
	return isPIDAlive(s.PID)
}

// Stop sends a graceful shutdown (SIGINT via CTRL_BREAK on Windows) and
// waits up to 5s for the process to exit. Falls back to kill.
func (s *Instance) Stop() error {
	if !s.IsRunning() {
		s.PID = 0
		return nil
	}
	log.Printf("[sandbox:%s] 停止 (PID %d)...", s.Name, s.PID)

	// Send CTRL_BREAK_EVENT on Windows.
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGINT)
	}

	// Wait for graceful exit.
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-done:
		log.Printf("[sandbox:%s] 已退出", s.Name)
	case <-time.After(5 * time.Second):
		log.Printf("[sandbox:%s] 强制终止", s.Name)
		_ = s.cmd.Process.Kill()
		<-done
	}

	s.PID = 0
	s.cmd = nil
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────────

func httpGet(url string) (bool, error) {
	// Simple HTTP GET without importing net/http to avoid pulling in
	// unnecessary dependencies in the sandbox package context.
	// In practice this delegates to httpclient.
	return httpGetInternal(url)
}

