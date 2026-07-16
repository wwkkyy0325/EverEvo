//go:build windows

package plugin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Status represents the runtime state of a plugin process.
type Status struct {
	Name      string `json:"name"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	StartedAt string `json:"startedAt"`
	Error     string `json:"error"`
	Logs      string `json:"logs"`
}

type pluginProc struct {
	spec      Spec
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	stderrBuf *ringBuf
	cancel    context.CancelFunc
	startedAt time.Time
	mu        sync.Mutex
}

// ringBuf is a fixed-size circular byte buffer for capturing recent logs.
type ringBuf struct {
	buf  []byte
	head int
	full bool
	mu   sync.Mutex
}

func newRingBuf(size int) *ringBuf { return &ringBuf{buf: make([]byte, size)} }

func (r *ringBuf) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		r.buf[r.head] = p[i]
		r.head++
		if r.head == len(r.buf) {
			r.head = 0
			r.full = true
		}
	}
	return len(p), nil
}

func (r *ringBuf) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return string(r.buf[:r.head])
	}
	out := make([]byte, len(r.buf))
	n := copy(out, r.buf[r.head:])
	copy(out[n:], r.buf[:r.head])
	return string(out)
}

// Host manages the lifecycle of all plugin subprocesses.
type Host struct {
	dataDir   string
	processes map[string]*pluginProc
	mu        sync.Mutex
}

// NewHost creates a plugin host.
func NewHost(dataDir string) *Host {
	return &Host{
		dataDir:   dataDir,
		processes: make(map[string]*pluginProc),
	}
}

// Start launches a plugin subprocess.
func (h *Host) Start(spec Spec) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.processes[spec.Name]; ok {
		return fmt.Errorf("插件已在运行: %s", spec.Name)
	}

	entry := filepath.Join(spec.Dir, spec.Entry)

	// Resolve runtime executable and arguments.
	//   "python" → python(.exe from venv) entry.py
	//   "node"   → node entry.js
	//   ""       → entry is the compiled binary itself
	//   other    → spec.Runtime entry (custom launcher)
	var exe string
	var cmdArgs []string
	switch spec.Runtime {
	case "python":
		// Resolution order: plugin venv → EVEREVO_PYTHON env override → PATH.
		// EVEREVO_PYTHON lets users point at a specific interpreter (conda,
		// pyenv, custom install) when `python` on PATH is wrong.
		venvPython := filepath.Join(spec.Dir, spec.Env, "Scripts", "python.exe")
		if _, err := os.Stat(venvPython); err == nil {
			exe = venvPython
		} else if envPy := os.Getenv("EVEREVO_PYTHON"); envPy != "" {
			exe = envPy
		} else {
			exe = "python"
		}
		// -u: unbuffered stdin/stdout — critical for JSON-RPC line protocol
		cmdArgs = []string{"-u", entry}
	case "node":
		exe = "node"
		cmdArgs = []string{entry}
	case "":
		// Compiled executable (Go, Rust, etc.) — entry IS the binary.
		exe = entry
	default:
		exe = spec.Runtime
		cmdArgs = []string{entry}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var cmd *exec.Cmd
	if len(cmdArgs) > 0 {
		cmd = exec.CommandContext(ctx, exe, cmdArgs...)
	} else {
		cmd = exec.CommandContext(ctx, exe)
	}
	cmd.Dir = spec.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	// Inject runtime/ into PATH so plugin can use its own DLLs
	envPath := filepath.Join(spec.Dir, spec.Env)
	pathEnv := envPath + ";" + os.Getenv("PATH")
	cmd.Env = append(os.Environ(), "PATH="+pathEnv, "EVEREVO_PLUGIN_DIR="+spec.Dir)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}

	// Create proc early so stderr can tee into its ring buffer
	proc := &pluginProc{
		spec:      spec,
		cmd:       cmd,
		stdin:     stdinPipe,
		stdout:    bufio.NewScanner(stdoutPipe),
		stderrBuf: newRingBuf(64 * 1024),
		cancel:    cancel,
		startedAt: time.Now(),
	}
	// Tee stderr: app console for debugging + ring buffer for log retrieval
	cmd.Stderr = io.MultiWriter(os.Stderr, proc.stderrBuf)

	// Increase scanner buffer for large JSON responses
	proc.stdout.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动插件进程失败: %w", err)
	}

	// Short startup window (600ms): if the process exits immediately it's a
	// launch failure (bad Python/interpreter, missing deps, entry script crash).
	// This surfaces the problem at StartPlugin time instead of later timeout.
	alive := make(chan struct{})
	go func() { cmd.Wait(); close(alive) }()
	select {
	case <-alive:
		cancel()
		return fmt.Errorf("plugin %s exited immediately after start — check entry script and Python environment\nstderr: %s", spec.Name, proc.stderrBuf.String())
	case <-time.After(600 * time.Millisecond):
	}

	h.processes[spec.Name] = proc

	// Monitor process exit in background
	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		defer h.mu.Unlock()
		if p, ok := h.processes[spec.Name]; ok && p == proc {
			if err != nil {
				log.Printf("[plugin] %s 退出: %v", spec.Name, err)
			}
			if p.cancel != nil {
				p.cancel()
			}
		}
	}()

	log.Printf("[plugin] %s 已启动 (PID %d)", spec.Name, cmd.Process.Pid)
	return nil
}

// Stop terminates a plugin subprocess.
func (h *Host) Stop(name string) error {
	h.mu.Lock()
	proc, ok := h.processes[name]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("插件未运行: %s", name)
	}
	delete(h.processes, name)
	h.mu.Unlock()

	// Graceful shutdown: close stdin, send CTRL_BREAK_EVENT, wait 3s, then kill
	if proc.stdin != nil {
		proc.stdin.Close()
	}
	// On Windows, os.Interrupt maps to CTRL_BREAK_EVENT for console processes
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Signal(os.Interrupt)
	}

	done := make(chan struct{})
	go func() {
		proc.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if proc.cmd.Process != nil {
			proc.cmd.Process.Kill()
		}
	}

	if proc.cancel != nil {
		proc.cancel()
	}
	log.Printf("[plugin] %s 已停止", name)
	return nil
}

// Restart stops and restarts a plugin.
func (h *Host) Restart(name string) error {
	spec, err := h.GetSpec(name)
	if err != nil {
		return err
	}
	_ = h.Stop(name)
	time.Sleep(200 * time.Millisecond)
	return h.Start(*spec)
}

// GetStatus returns the runtime status of a plugin.
func (h *Host) GetStatus(name string) Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	proc, ok := h.processes[name]
	if !ok {
		return Status{Name: name, Running: false}
	}
	return Status{
		Name:      name,
		Running:   true,
		PID:       proc.cmd.Process.Pid,
		StartedAt: proc.startedAt.Format(time.RFC3339),
	}
}

// GetLogs returns recent stderr output for a plugin (live or stopped).
func (h *Host) GetLogs(name string) string {
	h.mu.Lock()
	proc, ok := h.processes[name]
	h.mu.Unlock()
	if !ok || proc.stderrBuf == nil {
		return ""
	}
	return proc.stderrBuf.String()
}

// GetSpec returns the spec of a running plugin.
func (h *Host) GetSpec(name string) (*Spec, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	proc, ok := h.processes[name]
	if !ok {
		return nil, fmt.Errorf("插件未运行: %s", name)
	}
	return &proc.spec, nil
}

// ListStatus returns status for all currently managed plugins.
func (h *Host) ListStatus() []Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Status
	for _, proc := range h.processes {
		out = append(out, Status{
			Name:      proc.spec.Name,
			Running:   true,
			PID:       proc.cmd.Process.Pid,
			StartedAt: proc.startedAt.Format(time.RFC3339),
		})
	}
	return out
}

// GetStdout returns the stdout scanner for a running plugin (for RPC client).
func (h *Host) GetStdout(name string) (*bufio.Scanner, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	proc, ok := h.processes[name]
	if !ok {
		return nil, fmt.Errorf("插件未运行: %s", name)
	}
	return proc.stdout, nil
}

// GetStdin returns the stdin writer for a running plugin (for RPC client).
func (h *Host) GetStdin(name string) (io.WriteCloser, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	proc, ok := h.processes[name]
	if !ok {
		return nil, fmt.Errorf("插件未运行: %s", name)
	}
	return proc.stdin, nil
}

// IsRunning returns true if the plugin is currently running.
func (h *Host) IsRunning(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.processes[name]
	return ok
}
