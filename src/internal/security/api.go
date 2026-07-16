// Package security provides safety policies for AI-initiated operations.
//
// Public API: Policy, DefaultPolicy, LoadPolicy, SavePolicy, CheckCommand, CheckFilePath.
// Audit implementation lives in internal/.
package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Policy defines safety constraints for AI-initiated operations.
type Policy struct {
	ShellEnabled      bool     `json:"shellEnabled"`
	ShellAllowList    []string `json:"shellAllowList"`
	ShellDenyList     []string `json:"shellDenyList"`
	ShellMaxTimeout   int      `json:"shellMaxTimeout"`
	ShellConfirmFirst bool     `json:"shellConfirmFirst"`

	FSEnabled       bool     `json:"fsEnabled"`
	FSReadOnlyPaths []string `json:"fsReadOnlyPaths"`
	FSWritePaths    []string `json:"fsWritePaths"`

	AuditEnabled bool `json:"auditEnabled"`
}

// DefaultPolicy returns sane defaults.
func DefaultPolicy() Policy {
	exeDir, _ := os.Executable()
	dataDir := storage.DataDir()
	return Policy{
		ShellEnabled:    true,
		ShellDenyList:   []string{"rm -rf /", "format", "shutdown", "del /f /s", "rd /s /q"},
		ShellMaxTimeout: 60,

		FSEnabled:       true,
		FSReadOnlyPaths: []string{exeDir, dataDir},
		FSWritePaths:    []string{storage.SessionsDir(), storage.ModelsDir(), storage.DownloadsDir()},

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
	return filepath.Join(storage.DataDir(), "security_policy.json")
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
