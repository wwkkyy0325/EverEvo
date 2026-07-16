//go:build windows

package app

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// FileControlMode defines the file access policy for write_file and shell_exec.
type FileControlMode string

const (
	ModeReadOnly FileControlMode = "readonly" // can read, cannot write/execute
	ModeAudit    FileControlMode = "audit"    // writes need frontend confirmation
	ModeFull     FileControlMode = "full"     // unrestricted
)

// AuditRequest is sent to the frontend when a write/exec needs approval.
type AuditRequest struct {
	ID        string `json:"id"`
	Tool      string `json:"tool"`      // "write_file" | "shell_exec"
	Path      string `json:"path"`      // target file path or command
	Content   string `json:"content"`   // file content preview (first 500 chars) or full command
	Size      int    `json:"size"`      // total content size in bytes
	CreatedAt int64  `json:"createdAt"` // unix millis
}

// AuditResponse is the user's decision.
type AuditResponse struct {
	RequestID string `json:"requestId"`
	Approved  bool   `json:"approved"`
	Permanent bool   `json:"permanent"` // apply to all future requests this session
}

// FileCtl holds the file control state.
type FileCtl struct {
	mu            sync.Mutex
	mode          FileControlMode
	pending       map[string]chan AuditResponse // request ID → response channel
}

func (fc *FileCtl) init() {
	fc.mode = ModeFull
	fc.pending = make(map[string]chan AuditResponse)
}

// SetMode updates the file control mode.
func (fc *FileCtl) SetMode(m FileControlMode) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.mode = m
	log.Printf("[filectl] 模式切换: %s", m)
}

// Mode returns the current file control mode.
func (fc *FileCtl) Mode() FileControlMode {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.mode
}

// CheckWrite returns nil if the write is allowed, or an error describing why not.
// In audit mode, it sends an AuditRequest to the frontend and blocks until the
// user responds.
func (fc *FileCtl) CheckWrite(tool, path, content string, emitFn func(string, any)) error {
	fc.mu.Lock()
	mode := fc.mode
	fc.mu.Unlock()

	switch mode {
	case ModeReadOnly:
		return errReadOnly
	case ModeFull:
		return nil
	case ModeAudit:
		return fc.auditWait(tool, path, content, emitFn)
	default:
		return nil
	}
}

// CheckShell returns nil if shell execution is allowed.
// In read-only mode, only non-destructive commands are allowed.
// In audit mode, destructive commands need confirmation.
func (fc *FileCtl) CheckShell(command string, emitFn func(string, any)) error {
	fc.mu.Lock()
	mode := fc.mode
	fc.mu.Unlock()

	switch mode {
	case ModeReadOnly:
		if isDestructiveCmd(command) {
			return errReadOnly
		}
		return nil
	case ModeFull:
		return nil
	case ModeAudit:
		if isDestructiveCmd(command) {
			return fc.auditWait("shell_exec", command, "", emitFn)
		}
		return nil
	default:
		return nil
	}
}

func (fc *FileCtl) auditWait(tool, path, content string, emitFn func(string, any)) error {
	req := AuditRequest{
		ID:        "audit_" + time.Now().Format("150405.000"),
		Tool:      tool,
		Path:      path,
		Content:   content,
		Size:      len(content),
		CreatedAt: time.Now().UnixMilli(),
	}

	ch := make(chan AuditResponse, 1)
	fc.mu.Lock()
	fc.pending[req.ID] = ch
	fc.mu.Unlock()

	// Send to frontend.
	emitFn("filectl:audit-request", req)

	// Wait for response (30s timeout).
	select {
	case resp := <-ch:
		if resp.Permanent {
			fc.mu.Lock()
			if resp.Approved {
				fc.mode = ModeFull
				log.Printf("[filectl] 用户批准并设为完全控制模式")
			} else {
				fc.mode = ModeReadOnly
				log.Printf("[filectl] 用户拒绝并设为只读模式")
			}
			fc.mu.Unlock()
		}
		if !resp.Approved {
			return errAuditDenied
		}
		return nil
	case <-time.After(30 * time.Second):
		fc.mu.Lock()
		delete(fc.pending, req.ID)
		fc.mu.Unlock()
		return errAuditTimeout
	}
}

// ResolveAudit is called from the frontend to answer a pending audit request.
func (fc *FileCtl) ResolveAudit(requestID string, approved, permanent bool) {
	fc.mu.Lock()
	ch, ok := fc.pending[requestID]
	if ok {
		delete(fc.pending, requestID)
	}
	fc.mu.Unlock()
	if ok {
		ch <- AuditResponse{RequestID: requestID, Approved: approved, Permanent: permanent}
	}
}

// isDestructiveCmd returns true for commands that modify filesystem or system state.
func isDestructiveCmd(cmd string) bool {
	destructive := []string{
		"rm ", "del ", "rmdir", "move ", "mv ", "copy ", "cp ",
		"mkdir", "New-Item", "Remove-Item", "Set-Content", "Out-File",
		"npm install", "pip install", "go install", "choco install",
		"Write-", ">", ">>",
	}
	for _, d := range destructive {
		if len(cmd) >= len(d) && cmd[:len(d)] == d {
			return true
		}
	}
	return false
}

var (
	errReadOnly     = &fileCtlError{"只读模式：不允许写文件或执行危险命令。请切换到审计模式或完全控制模式。"}
	errAuditDenied  = &fileCtlError{"审计模式：用户拒绝了此次操作。"}
	errAuditTimeout = &fileCtlError{"审计模式：等待用户确认超时 (30s)。"}
)

type fileCtlError struct{ msg string }

func (e *fileCtlError) Error() string { return e.msg }

// ─── Wails bindings ────────────────────────────────────────────────

// GetFileControlMode returns the current file access mode.
func (a *App) GetFileControlMode() map[string]any {
	return map[string]any{"mode": string(a.fileCtl.Mode())}
}

// SetFileControlMode switches the file access mode.
// Valid values: "readonly", "audit", "full".
func (a *App) SetFileControlMode(mode string) error {
	switch FileControlMode(mode) {
	case ModeReadOnly, ModeAudit, ModeFull:
		a.fileCtl.SetMode(FileControlMode(mode))
		return nil
	default:
		return fmt.Errorf("invalid mode: %s (valid: readonly, audit, full)", mode)
	}
}

// ResolveAudit answers a pending audit request from the frontend.
func (a *App) ResolveAudit(requestID string, approved, permanent bool) {
	a.fileCtl.ResolveAudit(requestID, approved, permanent)
}
