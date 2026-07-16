package backends

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// BestPython returns the path to the best available Python executable.
func BestPython() string {
	spec := BackendSpec{}
	st := detectPython(spec)
	if st.OK && st.DLLPath != "" {
		return st.DLLPath
	}
	return ""
}

// CreateVenv creates a virtual environment at the given path.
// Uses portable Python if available, falls back to system/conda.
func CreateVenv(venvPath, pythonExe string) error {
	if pythonExe == "" {
		pythonExe = BestPython()
	}
	if pythonExe == "" {
		return fmt.Errorf("未找到可用的 Python")
	}
	// Remove existing venv to ensure clean state
	_ = os.RemoveAll(venvPath)
	cmd := exec.Command(pythonExe, "-m", "venv", venvPath)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建虚拟环境失败: %v\n%s", err, string(out))
	}
	return nil
}

// RunInVenv executes a Python script inside a virtual environment.
func RunInVenv(venvPath, script string, args []string) (string, error) {
	var pythonBin string
	if runtime.GOOS == "windows" {
		pythonBin = filepath.Join(venvPath, "Scripts", "python.exe")
	} else {
		pythonBin = filepath.Join(venvPath, "bin", "python")
	}
	if _, err := os.Stat(pythonBin); os.IsNotExist(err) {
		return "", fmt.Errorf("虚拟环境未就绪: %s", venvPath)
	}
	cmd := exec.Command(pythonBin, append([]string{"-I", script}, args...)...)
	cmd.Env = []string{} // isolate from user env
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%v: %s", err, string(out))
	}
	return string(out), nil
}

// PipInstall installs packages to a virtual environment.
func PipInstall(venvPath string, packages []string) error {
	var pipBin string
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvPath, "Scripts", "pip.exe")
	} else {
		pipBin = filepath.Join(venvPath, "bin", "pip")
	}
	if _, err := os.Stat(pipBin); os.IsNotExist(err) {
		return fmt.Errorf("虚拟环境未就绪，请先创建: %s", venvPath)
	}
	args := append([]string{"install", "--quiet"}, packages...)
	cmd := exec.Command(pipBin, args...)
	cmd.Env = []string{}
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install 失败: %v\n%s", err, string(out))
	}
	return nil
}
