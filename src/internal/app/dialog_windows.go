package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// pickFolderDialog 调用 PowerShell 打开 Windows 原生文件夹选择对话框。
func pickFolderDialog() (string, error) {
	tmpFile, err := os.CreateTemp("", "EverEvo-dir-*.txt")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// 用 New UTF8Encoding($false) 避免 BOM
	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.FolderBrowserDialog
$f.Description = '选择安装目录'
$f.ShowNewFolderButton = $true
if ($f.ShowDialog() -eq 'OK') {
    $utf8nobom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText('%s', $f.SelectedPath, $utf8nobom)
}
`, tmpPath)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("文件夹选择失败: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return "", nil
	}
	// 剔除 BOM + 首尾空白和换行
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	data = bytes.TrimSpace(data)
	return string(data), nil
}

// pickArchiveDialog 打开文件选择对话框，选 .zip 引擎包。
func pickArchiveDialog() (string, error) {
	tmpFile, err := os.CreateTemp("", "EverEvo-zip-*.txt")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.OpenFileDialog
$f.Title = '选择引擎压缩包'
$f.Filter = '引擎包 (*.zip)|*.zip'
# 置顶对话框，避免弹在主窗口后台
$topmost = New-Object System.Windows.Forms.Form; $topmost.TopMost = $true; $topmost.MinimizeBox = $false; $topmost.MaximizeBox = $false
$topmost.ShowInTaskbar = $false; $topmost.WindowState = 'Minimized'
$topmost.Show(); $topmost.Hide()
if ($f.ShowDialog($topmost) -eq 'OK') {
    $utf8nobom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText('%s', $f.FileName, $utf8nobom)
}
$topmost.Dispose()
`, tmpPath)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return "", nil
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return string(bytes.TrimSpace(data)), nil
}

// pickPluginDialog 打开文件选择对话框，选 .zip 插件包。
func pickPluginDialog() (string, error) {
	tmpFile, err := os.CreateTemp("", "EverEvo-plugin-*.txt")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.OpenFileDialog
$f.Title = '选择插件包'
$f.Filter = '插件包 (*.zip)|*.zip|所有文件 (*.*)|*.*'
$topmost = New-Object System.Windows.Forms.Form; $topmost.TopMost = $true; $topmost.MinimizeBox = $false; $topmost.MaximizeBox = $false
$topmost.ShowInTaskbar = $false; $topmost.WindowState = 'Minimized'
$topmost.Show(); $topmost.Hide()
if ($f.ShowDialog($topmost) -eq 'OK') {
    $utf8nobom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText('%s', $f.FileName, $utf8nobom)
}
$topmost.Dispose()
`, tmpPath)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return "", nil
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return string(bytes.TrimSpace(data)), nil
}

// pickModelDialog 打开文件选择对话框，选 .onnx 模型文件。
func pickModelDialog() (string, error) {
	tmpFile, err := os.CreateTemp("", "EverEvo-onnx-*.txt")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.OpenFileDialog
$f.Title = '选择 ONNX 模型文件'
$f.Filter = '模型文件 (*.onnx;*.gguf;*.safetensors;*.bin;*.pt;*.pth)|*.onnx;*.gguf;*.safetensors;*.bin;*.pt;*.pth|ONNX (*.onnx)|*.onnx|GGUF (*.gguf)|*.gguf|SafeTensors (*.safetensors)|*.safetensors|PyTorch (*.pt;*.pth;*.bin)|*.pt;*.pth;*.bin|所有文件 (*.*)|*.*'
$topmost = New-Object System.Windows.Forms.Form; $topmost.TopMost = $true; $topmost.MinimizeBox = $false; $topmost.MaximizeBox = $false
$topmost.ShowInTaskbar = $false; $topmost.WindowState = 'Minimized'
$topmost.Show(); $topmost.Hide()
if ($f.ShowDialog($topmost) -eq 'OK') {
    $utf8nobom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText('%s', $f.FileName, $utf8nobom)
}
$topmost.Dispose()
`, tmpPath)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return "", nil
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return string(bytes.TrimSpace(data)), nil
}
