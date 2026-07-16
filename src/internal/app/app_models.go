//go:build windows

package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"everevo/internal/model"
	"everevo/internal/storage"
	"everevo/internal/toolbox"
)

// ─── 模型 API ────────────────────────────────────────────────

func (a *App) LoadModel(id string, name string) (model.ModelInfo, error) {
	return a.manager.LoadModel(id, name)
}

// LoadModelFile 加载模型文件（自动检测类型）。
func (a *App) LoadModelFile(id string, name string, modelPath string) (model.ModelInfo, error) {
	info, err := a.manager.LoadModelFile(id, name, modelPath)
	if err != nil {
		return info, err
	}
	a.emitChanged("models:changed", "update", id)
	return info, nil
}

// PickModelFile 打开文件选择对话框，选 ONNX 模型文件。
func (a *App) PickModelFile() string {
	path, _ := pickModelDialog()
	return path
}
func (a *App) UnloadModel(id string) error {
	if err := a.manager.UnloadModel(id); err != nil {
		return err
	}
	a.emitChanged("models:changed", "update", id)
	return nil
}
func (a *App) ListModels() []model.ModelInfo { return a.manager.List() }
func (a *App) RunModel(id string, input string) (string, error) {
	output, err := a.manager.RunModel(a.ctx, id, []byte(input))
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (a *App) LogToTerminal(msg string) { log.Printf("[界面] %s", msg) }

// ─── Downloaded file management ─────────────────────────────────

// DownloadedFile 已下载的模型文件/目录。
type DownloadedFile struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Ext   string `json:"ext"`
	IsDir bool   `json:"isDir"`
}

// ListDownloadedModels 递归扫描 data/models/ 目录，返回已下载的模型文件。
// 同时扫描子目录本身作为条目，确保空目录也能在"我的模型"中显示。
func (a *App) ListDownloadedModels() []DownloadedFile {
	list := []DownloadedFile{}
	modelsDir := storage.ModelsDir()
	log.Printf("[models] scanning %s", modelsDir)
	// 递归扫描所有目录和文件。
	// 目录全部显示（即使空目录），文件跳过 .part .meta。
	filepath.Walk(modelsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(modelsDir, path)
		if rel == "." {
			return nil // skip models/ itself
		}
		if info.IsDir() {
			list = append(list, DownloadedFile{
				Name:  rel,
				Path:  path,
				IsDir: true,
			})
			return nil
		}
		// 文件：跳过临时文件
		if strings.HasSuffix(info.Name(), ".part") || strings.HasSuffix(info.Name(), ".meta") {
			return nil
		}
		ext := ""
		if i := strings.LastIndex(info.Name(), "."); i >= 0 {
			ext = strings.ToLower(info.Name()[i:])
		}
		list = append(list, DownloadedFile{
			Name: rel,
			Path: path,
			Size: info.Size(),
			Ext:  ext,
		})
		return nil
	})
	return list
}

// ─── Toolbox model discovery ───────────────────────────────────

// ToolModel 是工具箱里一个可用的模型（已按类型探测归类）。
type ToolModel struct {
	RepoID string `json:"repoId"`
	Name   string `json:"name"`
	Type   string `json:"type"` // toolbox.Type
	Dir    string `json:"dir"`
	Hidden int    `json:"hidden"`
}

// ListToolModels 扫描模型库，返回已识别类型（句向量/图像分类）的模型，供工具箱展示。
func (a *App) ListToolModels() []ToolModel {
	var list []ToolModel
	modelsDir := storage.ModelsDir()
	entries, _ := os.ReadDir(modelsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgDir := filepath.Join(modelsDir, e.Name())
		meta := toolbox.Detect(pkgDir)
		if meta.Type == toolbox.TypeUnknown {
			continue
		}
		list = append(list, ToolModel{
			RepoID: e.Name(),
			Name:   e.Name(),
			Type:   string(meta.Type),
			Dir:    pkgDir,
			Hidden: meta.Hidden,
		})
	}
	return list
}

// EmbedTexts 用指定句向量模型把多段文本编码为嵌入向量。
func (a *App) EmbedTexts(modelDir string, texts []string) ([][]float32, error) {
	onnxPath, err := findOnnxInDir(modelDir)
	if err != nil {
		return nil, err
	}
	meta := toolbox.Detect(modelDir)
	emb, err := toolbox.NewEmbedder(onnxPath, meta.Hidden)
	if err != nil {
		return nil, err
	}
	defer emb.Close()
	return emb.Embed(texts)
}

// findOnnxInDir 在模型包内查找 ONNX 文件（优先 model.onnx / onnx/model.onnx）。
func findOnnxInDir(dir string) (string, error) {
	for _, c := range []string{filepath.Join(dir, "model.onnx"), filepath.Join(dir, "onnx", "model.onnx")} {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	var found string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if found == "" && strings.EqualFold(filepath.Ext(p), ".onnx") {
			found = p
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("未在 %s 找到 .onnx 文件", dir)
}
