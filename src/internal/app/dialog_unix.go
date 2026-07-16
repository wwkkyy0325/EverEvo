//go:build linux || darwin

package app

import (
	"os/exec"
	"runtime"
	"strings"
)

func pickFolderDialog() (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("zenity", "--file-selection", "--directory",
			"--title=选择安装目录")
	case "darwin":
		script := `
tell application "System Events"
    activate
    set dir to choose folder with prompt "选择安装目录"
    POSIX path of dir
end tell
`
		cmd = exec.Command("osascript", "-e", script)
	}
	if cmd == nil {
		return "", nil
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
