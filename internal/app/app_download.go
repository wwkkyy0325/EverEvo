//go:build windows

package app

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/backends"
	"everevo/internal/catalog"
	"everevo/internal/downloader"
	"everevo/internal/httpclient"
	"everevo/internal/storage"
)

// ─── Download URL helpers ──────────────────────────────────────

// localDownloadPath computes the on-disk path for a downloaded model file.
func localDownloadPath(repoID, filename string) (string, error) {
	packageDir := filepath.Join(storage.ModelsDir(), sanitizeDir(repoID))
	return filepath.Join(packageDir, filepath.FromSlash(filename)), nil
}

// IsFileDownloaded checks whether a model file exists on disk (and is non-empty).
// This is the source of truth for dedup — no in-memory cache.
func (a *App) IsFileDownloaded(source, repoID, filename string) bool {
	path, err := localDownloadPath(repoID, filename)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// resolveDownloadURL builds the raw file download URL for a given source.
func resolveDownloadURL(source, repoID, filename string) string {
	switch source {
	case "modelscope":
		return fmt.Sprintf("https://www.modelscope.cn/models/%s/resolve/master/%s", repoID, filename)
	default:
		return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repoID, filename)
	}
}

// ─── Download orchestrators ─────────────────────────────────────

// startDownload 启动单文件下载，统一规则：
//   - 远程 URL 与事件 file 字段使用正斜杠 filename（与前端树 path 一致）
//   - 本地保存到 modelsDir/{repoDir}/{filename}，保留目录结构
//   - filename 仅用于 URL/事件标识，destPath 用 OS 分隔符
func (a *App) startDownload(dlID, source, repoID, filename string) {
	dlURL := resolveDownloadURL(source, repoID, filename)
	modelsDir := storage.ModelsDir()
	packageDir := filepath.Join(modelsDir, sanitizeDir(repoID))
	destPath := filepath.Join(packageDir, filepath.FromSlash(filename))
	os.MkdirAll(filepath.Dir(destPath), 0755)

	// Inject catalog auth credentials as HTTP headers for the download.
	var headers map[string]string
	if cred, ok := catalog.Credentials[source]; ok {
		headers = make(map[string]string)
		if cred.Cookie != "" {
			headers["Cookie"] = cred.Cookie
		}
		if cred.Token != "" {
			headers["Authorization"] = "Bearer " + cred.Token
		}
	}

	log.Printf("[下载] 启动: source=%s file=%s url=%s", source, filename, dlURL)
	task := a.dlManager.Start(dlID, dlURL, destPath, filename)
	task.Source = source
	if len(headers) > 0 {
		task.Headers = headers
	}
}

// DownloadModelFile 异步下载单个文件到模型子目录。返回 download ID。
// Returns an error if the file already exists on disk.
func (a *App) DownloadModelFile(source, repoID, filename string) (string, error) {
	if a.IsFileDownloaded(source, repoID, filename) {
		return "", fmt.Errorf("文件已下载: %s", filename)
	}
	dlID := source + "|" + filename
	a.startDownload(dlID, source, repoID, filename)
	return dlID, nil
}

// DownloadModelPackage 一键下载模型仓库全部文件到子目录。
func (a *App) DownloadModelPackage(source, repoID string) (string, error) {
	entries := a.ListModelFiles(source, repoID)
	var filePaths []string
	for _, fe := range entries {
		if fe.Type == "file" || fe.Type == "" {
			filePaths = append(filePaths, fe.Path)
		}
	}
	return a.DownloadSelectedFiles(source, repoID, filePaths)
}

// DownloadSelectedFiles 下载用户勾选的文件到模型子目录（并发启动）。
// Skips files already on disk.
func (a *App) DownloadSelectedFiles(source, repoID string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("未选中任何文件")
	}
	pkgID := source + "|pkg|" + repoID + "|" + time.Now().Format("150405")
	go func() {
		skipped := 0
		for _, f := range files {
			if a.IsFileDownloaded(source, repoID, f) {
				skipped++
				continue
			}
			fileID := pkgID + "|" + f
			a.startDownload(fileID, source, repoID, f)
		}
		if skipped > 0 {
			log.Printf("[下载] 跳过 %d 个已下载文件 (repo=%s)", skipped, repoID)
		}
	}()
	return pkgID, nil
}

// sanitizeDir 将 repoID 转为安全的目录名。
func sanitizeDir(repoID string) string {
	s := strings.ReplaceAll(repoID, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// ─── Download Center APIs ────────────────────────────────────────

// PauseDownload 暂停下载。
func (a *App) PauseDownload(id string) { a.dlManager.Pause(id) }

// ResumeDownload 恢复下载。
func (a *App) ResumeDownload(id string) { a.dlManager.Resume(id) }

// GetDownloadTasks returns all active download tasks.
func (a *App) GetDownloadTasks() []downloader.TaskInfo {
	if a.dlManager == nil { return []downloader.TaskInfo{} }
	return a.dlManager.List()
}

// GetDownloadHistory returns completed and failed download tasks.
func (a *App) GetDownloadHistory() []downloader.TaskInfo {
	if a.dlManager == nil { return []downloader.TaskInfo{} }
	return a.dlManager.History()
}

// CancelDownload cancels a download and removes it from all lists.
func (a *App) CancelDownload(id string) error {
	if a.dlManager == nil {
		return fmt.Errorf("下载管理器未初始化")
	}
	log.Printf("[下载] 取消: %s", id)
	a.dlManager.Remove(id)
	return nil
}

// RetryDownload re-queues a failed download from history.
func (a *App) RetryDownload(id string) {
	a.dlManager.Retry(id)
}

// ClearDownloadHistory removes all completed/failed entries.
func (a *App) ClearDownloadHistory() {
	a.dlManager.ClearHistory()
}

// GetDownloadDir returns the download directory path.
func (a *App) GetDownloadDir() string {
	dir := storage.ModelsDir()
	return dir
}

// OpenDownloadedFileDir opens the folder containing a downloaded file.
func (a *App) OpenDownloadedFileDir(filename string) {
	dir := storage.ModelsDir()
	fullPath := filepath.Join(dir, filename)
	if _, err := os.Stat(fullPath); err == nil {
		a.OpenDir(filepath.Dir(fullPath))
		return
	}
	a.OpenDir(dir)
}

// OpenDownloadDir opens the download directory in the system file explorer.
func (a *App) OpenDownloadDir() {
	dir := storage.ModelsDir()
	cmd := exec.Command("explorer", dir)
	cmd.Start()
}

// ─── Engine download ────────────────────────────────────────────

// DownloadEngineFile starts a download for a backend engine file.
// key: "onnx", "llama", or "cuda" — variant: "cpu" or "cuda" for llama.
func (a *App) DownloadEngineFile(key string, mirror string, variant string) (string, error) {
	url := backends.GetBackendDownloadURL(key, mirror, variant)
	if url == "" {
		return "", fmt.Errorf("无法获取 %s 的下载链接", key)
	}

	parts := strings.Split(url, "/")
	filename := parts[len(parts)-1]
	if filename == "" || !strings.Contains(filename, ".") {
		filename = key + "-download"
	}

	exeDir := filepath.Dir(os.Args[0])
	if idx := strings.Index(filename, "?"); idx >= 0 {
		filename = filename[:idx]
	}
	destPath := filepath.Join(exeDir, filename)

	dlID := "engine|" + key + "|" + variant + "|" + time.Now().Format("150405")
	a.dlManager.Start(dlID, url, destPath, filename)
	return dlID, nil
}

// ─── Path utilities ─────────────────────────────────────────────

// GetExeDir returns the directory containing the running executable.
func (a *App) GetExeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

// InstallPythonPortable downloads and extracts the portable Python to AppData.
func (a *App) InstallPythonPortable() error {
	appDir, err := pythonAppDataDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(appDir, "python")
	// Remove existing installation to ensure clean state.
	_ = os.RemoveAll(dest)

	url := backends.GetBackendDownloadURL("python", "", "")
	zipPath := filepath.Join(appDir, "python-portable.zip")

	log.Printf("[python] 下载便携 Python: %s", url)
	if err := downloadFile(url, zipPath); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer os.Remove(zipPath)

	log.Printf("[python] 解压到 %s", dest)
	if err := unzipFile(zipPath, dest); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	log.Printf("[python] 便携 Python 安装完成: %s", dest)
	return nil
}

func pythonAppDataDir() (string, error) {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(dir, "EverEvo"), nil
}

func downloadFile(url, dest string) error {
	os.MkdirAll(filepath.Dir(dest), 0755)
	resp, err := httpclient.New(300 * time.Second).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func unzipFile(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	os.MkdirAll(dest, 0755)
	for _, f := range r.File {
		path := filepath.Join(dest, f.Name)
		// Prevent zip-slip
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		w, err := os.Create(path)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(w, rc)
		w.Close()
		rc.Close()
	}
	return nil
}
