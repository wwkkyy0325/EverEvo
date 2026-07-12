// Package security provides sandboxing and safety policies for
// system-level operations (shell execution, file access).
package security

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Policy defines safety constraints for AI-initiated operations.
type Policy struct {
	// Shell execution
	ShellEnabled      bool     `json:"shellEnabled"`
	ShellAllowList    []string `json:"shellAllowList"`    // allowed commands (empty = all)
	ShellDenyList     []string `json:"shellDenyList"`     // forbidden patterns
	ShellMaxTimeout   int      `json:"shellMaxTimeout"`   // seconds, default 60
	ShellConfirmFirst bool     `json:"shellConfirmFirst"` // require user confirm for all cmd

	// File system
	FSEnabled       bool     `json:"fsEnabled"`
	FSReadOnlyPaths []string `json:"fsReadOnlyPaths"` // paths allowed for read
	FSWritePaths    []string `json:"fsWritePaths"`    // paths allowed for write

	// Audit
	AuditEnabled bool `json:"auditEnabled"`
}

// DefaultPolicy returns sane defaults.
func DefaultPolicy() Policy {
	exeDir, _ := os.Executable()
	return Policy{
		ShellEnabled:      true,
		ShellDenyList:     []string{"rm -rf /", "format", "shutdown", "del /f /s", "rd /s /q"},
		ShellMaxTimeout:   60,
		ShellConfirmFirst: false,

		FSEnabled:       true,
		FSReadOnlyPaths: []string{exeDir},
		FSWritePaths:    []string{exeDir},

		AuditEnabled: true,
	}
}

// CheckCommand evaluates whether a shell command is allowed.
func (p *Policy) CheckCommand(cmd string) (allowed bool, requireConfirm bool, reason string) {
	if !p.ShellEnabled {
		return false, false, "shell execution is disabled"
	}
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	for _, deny := range p.ShellDenyList {
		if strings.Contains(cmdLower, strings.ToLower(deny)) {
			return false, false, "command matches deny pattern: " + deny
		}
	}
	if p.ShellConfirmFirst {
		return true, true, "confirmation required for all shell commands"
	}
	return true, false, ""
}

// CheckFilePath evaluates whether a file operation is allowed.
func (p *Policy) CheckFilePath(path string, write bool) (allowed bool, reason string) {
	if !p.FSEnabled {
		return false, "file system access is disabled"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, "cannot resolve path: " + err.Error()
	}
	if write {
		for _, wp := range p.FSWritePaths {
			absWp, _ := filepath.Abs(wp)
			if strings.HasPrefix(strings.ToLower(abs), strings.ToLower(absWp)) {
				return true, ""
			}
		}
		return false, "path not in write whitelist"
	}
	// read
	for _, rp := range p.FSReadOnlyPaths {
		absRp, _ := filepath.Abs(rp)
		if strings.HasPrefix(strings.ToLower(abs), strings.ToLower(absRp)) {
			return true, ""
		}
	}
	return false, "path not in read whitelist"
}

// ─── Persistence ────────────────────────────────────────────────

func policyPath() string {
	dir := storage.DataDir()
	return filepath.Join(dir, "security_policy.json")
}

// LoadPolicy reads the security policy from disk or returns defaults.
func LoadPolicy() Policy {
	def := DefaultPolicy()
	data, err := os.ReadFile(policyPath())
	if err != nil {
		return def
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return def
	}
	return p
}

// SavePolicy persists the security policy.
func SavePolicy(p Policy) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return atomic.WriteFile(policyPath(), data, 0644)
}

// ─── Audit ──────────────────────────────────────────────────────

// AuditEntry is a record of a sensitive operation.
type AuditEntry struct {
	Time      time.Time `json:"time"`
	Operation string    `json:"operation"` // "shell" | "file_read" | "file_write"
	Target    string    `json:"target"`    // command or file path
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
}

func auditPath() string {
	dir := storage.DataDir()
	return filepath.Join(dir, "audit.log")
}

// LogAudit appends an audit entry to the log.
func LogAudit(entry AuditEntry) {
	entry.Time = time.Now()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[security] audit marshal: %v", err)
		return
	}

	ap := auditPath()

	// Rotate if the audit log exceeds 1 MB.
	if info, err := os.Stat(ap); err == nil && info.Size() > 1*1024*1024 {
		oldPath := ap + ".old"
		os.Remove(oldPath) // remove any previous .old
		os.Rename(ap, oldPath)
	}

	f, err := os.OpenFile(ap, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[security] audit open: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		log.Printf("[security] audit write: %v", err)
	}
}
