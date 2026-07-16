//go:build windows

package app

import (
	"everevo/internal/downloader"
	"everevo/internal/plugins/tools/models"
)

// ─── Download API ───────────────────────────────────────────────
//
// All download logic now lives in internal/plugins/tools/models/.
// App methods below are thin delegations for the Wails frontend.

func (a *App) IsFileDownloaded(source, repoID, filename string) bool {
	return models.IsFileDownloaded(source, repoID, filename)
}

func (a *App) DownloadModelFile(source, repoID, filename string) (string, error) {
	return models.DownloadModelFile(a, source, repoID, filename)
}

func (a *App) DownloadModelPackage(source, repoID string) (string, error) {
	return models.DownloadModelPackage(a, source, repoID)
}

func (a *App) DownloadSelectedFiles(source, repoID string, files []string) (string, error) {
	return models.DownloadSelectedFiles(a, source, repoID, files)
}

func (a *App) PauseDownload(id string) { models.PauseDownload(a, id) }

func (a *App) ResumeDownload(id string) { models.ResumeDownload(a, id) }

func (a *App) GetDownloadTasks() []downloader.TaskInfo {
	return models.GetDownloadTasks(a)
}

func (a *App) GetDownloadHistory() []downloader.TaskInfo {
	return models.GetDownloadHistory(a)
}

func (a *App) CancelDownload(id string) error {
	return models.CancelDownload(a, id)
}

func (a *App) RetryDownload(id string) {
	models.RetryDownload(a, id)
}

func (a *App) ClearDownloadHistory() {
	models.ClearDownloadHistory(a)
}

func (a *App) GetDownloadDir() string {
	return models.GetDownloadDir()
}

func (a *App) OpenDownloadedFileDir(filename string) {
	models.OpenDownloadedFileDir(filename)
}

func (a *App) OpenDownloadDir() {
	models.OpenDownloadDir()
}

func (a *App) DownloadEngineFile(key string, mirror string, variant string) (string, error) {
	return models.DownloadEngineFile(a, key, mirror, variant)
}

func (a *App) GetExeDir() string {
	return models.GetExeDir()
}

func (a *App) InstallPythonPortable() error {
	return models.InstallPythonPortable()
}
