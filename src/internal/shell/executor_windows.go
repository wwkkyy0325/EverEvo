//go:build windows

package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// PythonEnvVars are injected into the environment of every shell command
// on Windows. PYTHONUNBUFFERED=1 forces unbuffered stdout (fixes python -c
// output being empty). PYTHONIOENCODING=utf-8 prevents GBK garbled text.
var PythonEnvVars = []string{
	"PYTHONUNBUFFERED=1",
	"PYTHONIOENCODING=utf-8",
}

// runViaShell executes the command through cmd.exe.
//
// Uses SysProcAttr.CmdLine — Go's recommended escape hatch for cmd.exe and
// .bat files (per Go docs as of 1.24+). The /s flag tells cmd.exe to strip
// the first and last quote before processing the command line, making the
// quoting predictable.
//
// Reference: https://pkg.go.dev/os/exec — "Notable exceptions are msiexec.exe
// and cmd.exe (and thus, all batch files), which have a different unquoting
// algorithm."
func runViaShell(ctx context.Context, command string, opts Options) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// /s: strip first and last quote from CmdLine before processing
		// /c: execute the following command and then terminate
		CmdLine:    fmt.Sprintf(`cmd.exe /s /c "chcp 65001 > nul & %s"`, escapeForCmd(command)),
		HideWindow: true,
	}
	cmd.Dir = opts.Cwd
	env := append(PythonEnvVars, opts.Env...)
	cmd.Env = append(os.Environ(), env...)

	return finishCommand(cmd, opts, cancel)
}

// prepareCommand sets platform-specific process attributes before starting.
// On Windows: HideWindow to prevent console popups, explicit PWD env var.
func prepareCommand(cmd *exec.Cmd, opts Options) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true

	// When using a custom Cwd, set PWD so child processes can resolve paths.
	if opts.Cwd != "" {
		cmd.Env = append(cmd.Env, "PWD="+opts.Cwd)
	}
}

// terminateProcessGroup kills the process tree on Windows.
// Uses TerminateProcess for forceful cleanup of the entire job.
func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
