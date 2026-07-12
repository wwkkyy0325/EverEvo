//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// createShortcut creates a .lnk shortcut via PowerShell COM.
func createShortcut(linkPath, targetExe, workDir, desc string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("create shortcut dir: %w", err)
	}
	ps := fmt.Sprintf(`
$ws = New-Object -ComObject WScript.Shell
$sc = $ws.CreateShortcut('%s')
$sc.TargetPath = '%s'
$sc.WorkingDirectory = '%s'
$sc.Description = '%s'
$sc.IconLocation = '%s'
$sc.Save()
`, escapePS(linkPath), escapePS(targetExe), escapePS(workDir), escapePS(desc), escapePS(targetExe))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create shortcut: %w\n%s", err, string(out))
	}
	return nil
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
