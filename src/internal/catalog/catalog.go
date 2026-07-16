package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/httpclient"
)

// ModelEntry 一个可下载的模型条目。
type ModelEntry struct {
	ID          string `json:"id"`          // 如 "huggingface/microsoft/resnet-50"（含来源前缀）
	Name        string `json:"name"`        // 显示名
	Description string `json:"description"` // 简介
	Downloads   int    `json:"downloads"`   // 下载量
	Task        string `json:"task"`        // 任务类型（image-classification 等）
	Author      string `json:"author"`      // 作者
	URL         string `json:"url"`         // 模型页面链接
	Source      string `json:"source"`      // 来源标识：huggingface / modelscope / onnxzoo
}

// SearchResult 搜索结果。
type SearchResult struct {
	Models []ModelEntry `json:"models"`
	Total  int          `json:"total"`
	Error  string       `json:"error,omitempty"` // 非空表示请求失败（多为网络错误），前端据此区分"无结果"
}

// FileEntry 仓库中的单个条目（文件或目录）。
type FileEntry struct {
	Path string `json:"path"` // 相对路径
	Type string `json:"type"` // "file" 或 "directory"
	Size int64  `json:"size"` // 文件大小（字节），目录为 0，-1 表示未知
}

// FileTreeEntry 前端目录树节点。
type FileTreeEntry struct {
	Name     string          `json:"name"`               // 显示名（最后一段）
	Path     string          `json:"path"`               // 完整相对路径
	Type     string          `json:"type"`               // "file" 或 "directory"
	Size     int64           `json:"size"`               // 文件大小
	Children []FileTreeEntry `json:"children,omitempty"` // 子节点（仅目录）
}

// BuildFileTree 将扁平文件列表转为嵌套目录树（支持任意深度）。
// 使用指针中间结构构建，最后一次性物化为值，避免值拷贝丢失深层子节点。
func BuildFileTree(flat []FileEntry) []FileTreeEntry {
	type node struct {
		name     string
		path     string
		isDir    bool
		size     int64
		children []*node
	}

	dirs := map[string]*node{} // path -> 目录节点

	// getOrCreateDir 取得或隐式创建一个目录节点（补全中间目录）。
	getOrCreateDir := func(path string) *node {
		if d, ok := dirs[path]; ok {
			return d
		}
		name := path
		if idx := lastSlash(path); idx >= 0 {
			name = path[idx+1:]
		}
		d := &node{name: name, path: path, isDir: true}
		dirs[path] = d
		return d
	}

	// 第一遍：创建所有显式目录节点
	for _, f := range flat {
		if f.Type == "directory" {
			getOrCreateDir(f.Path)
		}
	}

	var roots []*node
	attach := func(n *node) {
		parentPath := parentDir(n.path)
		if parentPath == "" {
			roots = append(roots, n)
		} else {
			parent := getOrCreateDir(parentPath)
			parent.children = append(parent.children, n)
		}
	}

	// 第二遍：挂目录（保证父目录存在）
	for _, f := range flat {
		if f.Type == "directory" {
			attach(dirs[f.Path])
		}
	}
	// 第三遍：挂文件
	for _, f := range flat {
		if f.Type == "directory" {
			continue
		}
		name := f.Path
		if idx := lastSlash(f.Path); idx >= 0 {
			name = f.Path[idx+1:]
		}
		attach(&node{name: name, path: f.Path, isDir: false, size: f.Size})
	}

	// 递归排序 + 物化为 FileTreeEntry 值
	var materialize func([]*node) []FileTreeEntry
	materialize = func(nodes []*node) []FileTreeEntry {
		// 目录在前，文件在后，各自按名称排序
		dirNodes := make([]*node, 0)
		fileNodes := make([]*node, 0)
		for _, n := range nodes {
			if n.isDir {
				dirNodes = append(dirNodes, n)
			} else {
				fileNodes = append(fileNodes, n)
			}
		}
		sortByName := func(ns []*node) {
			for i := 1; i < len(ns); i++ {
				j := i
				for j > 0 && ns[j-1].name > ns[j].name {
					ns[j-1], ns[j] = ns[j], ns[j-1]
					j--
				}
			}
		}
		sortByName(dirNodes)
		sortByName(fileNodes)

		out := make([]FileTreeEntry, 0, len(nodes))
		for _, n := range dirNodes {
			out = append(out, FileTreeEntry{
				Name:     n.name,
				Path:     n.path,
				Type:     "directory",
				Children: materialize(n.children),
			})
		}
		for _, n := range fileNodes {
			out = append(out, FileTreeEntry{
				Name: n.name,
				Path: n.path,
				Type: "file",
				Size: n.size,
			})
		}
		return out
	}

	return materialize(roots)
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func parentDir(path string) string {
	if idx := lastSlash(path); idx >= 0 {
		return path[:idx]
	}
	return ""
}

// SearchFilter 搜索筛选参数。
type SearchFilter struct {
	Task     string `json:"task,omitempty"`     // 按任务类型筛选
	Library  string `json:"library,omitempty"`  // 按框架/库筛选（HF: transformers, diffusers, ggml...）
	Language string `json:"language,omitempty"` // 按语言筛选（MS: zh, en）
	Sort     string `json:"sort,omitempty"`     // 排序: downloads, likes, lastModified, created
	Offset   int    `json:"offset,omitempty"`    // 分页偏移
}

// Source 模型源接口。新增源只需实现此接口。
type Source interface {
	Name() string
	Search(query string, limit int, filter *SearchFilter) (*SearchResult, error)
	DownloadFile(repoID, filename, destDir string, onProgress func(pct int)) (string, error)
	ListFiles(repoID string) ([]FileEntry, error)
	ListRevisions(repoID string) ([]string, error)
	GetModelInfo(repoID string) (*ModelEntry, error)
}

// FilterKey returns a compact string for cache key inclusion.
func (f *SearchFilter) FilterKey() string {
	if f == nil {
		return ""
	}
	var parts []string
	if f.Task != "" {
		parts = append(parts, "t="+f.Task)
	}
	if f.Library != "" {
		parts = append(parts, "l="+f.Library)
	}
	if f.Language != "" {
		parts = append(parts, "la="+f.Language)
	}
	if f.Sort != "" {
		parts = append(parts, "s="+f.Sort)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}

// DownloadTarget 下载目标信息。
type DownloadTarget struct {
	Source   string `json:"source"`   // huggingface / modelscope
	RepoID   string `json:"repoId"`   // 仓库 ID
	Filename string `json:"filename"` // 文件名
	DestPath string `json:"destPath"` // 保存路径
}

// ─── HTTP 工具 ────────────────────────────────────────────

var httpClient = httpclient.New(10 * time.Second)

func httpGetJSON(url string, out interface{}) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// DownloadWithProgress 下载文件，支持进度回调（0-100）。
func DownloadWithProgress(url, destDir, filename string, onProgress func(pct int)) (string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if filename == "" {
		filename = filepath.Base(url)
	}
	dest := filepath.Join(destDir, filename)

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			downloaded += int64(n)
			if onProgress != nil && total > 0 {
				pct := int(downloaded * 100 / total)
				onProgress(pct)
			}
		}
		if err != nil {
			break
		}
	}
	f.Close()

	if err == io.EOF {
		os.Rename(tmp, dest)
		return dest, nil
	}
	os.Remove(tmp)
	return "", err
}
