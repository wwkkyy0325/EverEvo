//go:build !windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// runViaShell executes the command through sh -c on Unix systems.
func runViaShell(ctx context.Context, command string, opts Options) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = opts.Cwd
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}

	return finishCommand(cmd, opts, cancel)
}

// prepareCommand sets platform-specific process attributes before starting.
// On Unix: Setpgid=true so we can kill the entire process tree on timeout.
// Also sets PWD env var when a custom working directory is used.
func prepareCommand(cmd *exec.Cmd, opts Options) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Setsid = false

	// When using a custom Cwd, child processes may fail to resolve relative
	// paths if PWD is not set. Explicitly set PWD to the working directory.
	if opts.Cwd != "" {
		cmd.Env = append(cmd.Env, "PWD="+opts.Cwd)
	}
}

// terminateProcessGroup kills the entire process group to prevent zombie
// grandchildren when a command times out or is cancelled.
func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Negative PID sends the signal to the entire process group.
	// Setpgid=true ensures the child is the group leader.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
