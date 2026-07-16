// Package shell provides cross-platform command execution with automatic
// strategy selection: direct execution (os/exec) for simple commands,
// shell invocation only when needed for pipes, redirects, or chaining.
//
// Architecture inspired by Claude Code's Bash tool and Go security best practices:
//   - Avoid shell interpreters whenever possible (prevents quoting bugs, CWE-78)
//   - Direct os/exec with separate arguments = no quoting, no injection
//   - Shell mode only for commands that actually need pipes/redirects/chaining
//   - Concurrent stdout/stderr capture to prevent pipe buffer deadlocks
//   - Process group cleanup on timeout/cancellation (Setpgid on Unix)
//   - Quote-aware shell detection to reduce false-positive shell dispatch
//
// Strategy selection:
//
//	Direct execution (≈80% of commands):
//	  echo hello  → exec.Command("echo", "hello")
//	  python -c "print(123)"  → exec.Command("python", "-c", "print(123)")
//	  git status  → exec.Command("git", "status")
//
//	Shell execution (≈20% of commands):
//	  dir | findstr foo  → cmd /c "dir | findstr foo"
//	  echo hello > f.txt → cmd /c "echo hello > f.txt"
//	  npm i && npm test  → cmd /c "npm i && npm test"
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// Options configures command execution.
type Options struct {
	Cwd     string        // working directory; empty = current
	Env     []string      // extra env vars (appended to os.Environ())
	Timeout time.Duration // max execution time; 0 = default 30s
}

// Result holds the outcome of a command execution.
type Result struct {
	ExitCode   int           `json:"exitCode"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	Cwd        string        `json:"cwd"`
	Duration   time.Duration `json:"-"`
	DurationMs string        `json:"duration"`
}

// Execute runs a command string with automatic strategy selection.
//
// Strategy 1 — Direct execution: When the command contains no shell
// metacharacters (pipes, redirects, chaining) outside of quotes,
// it is parsed into program + arguments and executed directly via os/exec.
// This eliminates ALL quoting issues because arguments are passed as
// separate strings to the OS, never going through shell interpretation.
//
// Strategy 2 — Shell execution: When shell features are detected, the
// command is passed to cmd.exe (Windows) or sh (Unix).
func Execute(ctx context.Context, command string, opts Options) (Result, error) {
	// Normalize options.
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Timeout > 300*time.Second {
		opts.Timeout = 300 * time.Second
	}
	if opts.Cwd == "" {
		opts.Cwd, _ = os.Getwd()
	}

	// Validate — reject dangerous control characters.
	if err := validateCommand(command); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(command) == "" {
		return Result{}, fmt.Errorf("command is empty")
	}

	// Choose strategy: quote-aware shell metacharacter detection.
	if needsShell(command) {
		return runViaShell(ctx, command, opts)
	}
	return runDirect(ctx, command, opts)
}

// validateCommand rejects commands containing null bytes (C string
// terminators), ANSI escape sequences, and other control characters
// that could be used for terminal injection.
func validateCommand(command string) error {
	for _, r := range command {
		if r == 0 {
			return fmt.Errorf("command contains null byte (possible injection)")
		}
		if r == '\x1b' {
			return fmt.Errorf("command contains ANSI escape character")
		}
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("command contains control character U+%04X", r)
		}
	}
	return nil
}

// needsShell returns true when the command contains shell metacharacters
// OUTSIDE of quotes that require interpretation by cmd.exe or sh.
//
// Shell metacharacters: | (pipe), >/< (redirection), & (chaining),
// %/$ (variable expansion), *? (globbing).
//
// Quote-awareness: metacharacters inside double-quoted strings are
// treated as literal text, not shell operators. This correctly classifies
// echo "hello|world" as a direct-execution candidate.
func needsShell(command string) bool {
	inDoubleQuote := false
	inSingleQuote := false

	for i := 0; i < len(command); i++ {
		ch := command[i]

		// Track quote state.
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}

		// Inside quotes: characters are literal, not shell metacharacters.
		if inDoubleQuote || inSingleQuote {
			continue
		}

		switch ch {
		case '|', '>', '<': // pipes and redirects
			return true
		case '&': // command chaining (&&, &) or background
			return true
		case '%', '$': // variable expansion (%VAR%, $VAR, ${VAR})
			return true
		case '*', '?': // globbing wildcards
			return true
		case '\\':
			// Backslash escapes the next character — skip it.
			if i+1 < len(command) {
				i++
			}
		}
	}
	return false
}

// runDirect parses the command string into program + args and executes
// directly via os/exec — no shell interpreter involved. This is the
// recommended approach from Go security documentation (avoids CWE-78).
//
// If the program cannot be found via exec.LookPath (e.g., shell builtins
// like "echo", "dir", "type" on Windows), it falls back to shell execution.
// This provides the best of both worlds: direct execution for real binaries,
// shell execution for builtins.
func runDirect(ctx context.Context, command string, opts Options) (Result, error) {
	parts := parseCommand(command)
	if len(parts) == 0 {
		return Result{}, fmt.Errorf("empty command")
	}

	// If the program is a shell builtin (not a real executable on disk),
	// fall back to shell execution automatically. This handles Windows
	// cmd.exe builtins (echo, dir, type, set, etc.) and Unix shell builtins.
	if _, err := exec.LookPath(parts[0]); err != nil {
		return runViaShell(ctx, command, opts)
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = opts.Cwd
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	prepareCommand(cmd, opts)

	return finishCommand(cmd, opts, cancel)
}

// finishCommand starts the command, captures output concurrently,
// and builds the Result from the outcome.
//
// On timeout, the entire process group is terminated (Setpgid on Unix,
// TerminateProcess on Windows) to prevent zombie child processes.
func finishCommand(cmd *exec.Cmd, opts Options, cancel context.CancelFunc) (Result, error) {
	defer cancel()

	result := Result{Cwd: opts.Cwd, ExitCode: 0}

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	start := time.Now()
	if err := cmd.Start(); err != nil {
		result.Duration = time.Since(start)
		result.DurationMs = fmt.Sprintf("%dms", result.Duration.Milliseconds())
		result.Stderr = err.Error()
		result.ExitCode = -1
		return result, nil
	}

	// Concurrent reads — prevents pipe buffer deadlocks.
	type pipeResult struct{ data string }
	stdoutCh := make(chan pipeResult, 1)
	stderrCh := make(chan pipeResult, 1)

	// Timeout watchdog: after the deadline, kill the entire process group.
	// exec.CommandContext only kills the direct child via os.Process.Kill();
	// this goroutine calls platform-specific terminateProcessGroup to ensure
	// no zombie grandchildren survive the timeout.
	done := make(chan struct{})
	deadline := time.Now().Add(opts.Timeout)
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-timer.C:
			terminateProcessGroup(cmd)
		case <-done:
			return
		}
	}()

	go func() {
		var buf strings.Builder
		b := make([]byte, 4096)
		for {
			n, err := stdoutPipe.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		stdoutCh <- pipeResult{buf.String()}
	}()

	go func() {
		var buf strings.Builder
		b := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		stderrCh <- pipeResult{buf.String()}
	}()

	waitErr := cmd.Wait()
	close(done)
	result.Duration = time.Since(start)
	result.DurationMs = fmt.Sprintf("%dms", result.Duration.Milliseconds())
	result.Stdout = (<-stdoutCh).data
	result.Stderr = (<-stderrCh).data

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			if result.Stderr == "" {
				result.Stderr = waitErr.Error()
			}
		}
	}

	return result, nil
}

// IsDirectExecutable returns true if the command can be executed without
// a shell. Exported for use by safety checks and tool callers.
func IsDirectExecutable(command string) bool {
	return !needsShell(command)
}

// truncateForDisplay truncates output to maxLen bytes, appending a
// truncation marker. Exported for use by app-layer output formatting.
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n\n…(输出已截断，共 %d bytes)", len(s))
}

// countShellChars counts consecutive non-space, non-punctuation characters
// used in command-line arguments (for estimating non-alphanum complexity).
func countShellChars(s string) int {
	count := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			count++
		}
	}
	return count
}
