package catalog

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

const hfAPI = "https://huggingface.co/api"

// HuggingFace 源。
type HuggingFace struct{}

func (h *HuggingFace) Name() string { return "Hugging Face" }

// Search 搜索模型。query 为空时返回热门模型。支持 task/library/sort 筛选。
func (h *HuggingFace) Search(query string, limit int, filter *SearchFilter) (*SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if filter == nil {
		filter = &SearchFilter{}
	}
	url := fmt.Sprintf("%s/models?limit=%d&full=false", hfAPI, limit)
	if filter.Sort != "" {
		url += "&sort=" + filter.Sort + "&direction=-1"
	} else {
		url += "&sort=downloads&direction=-1"
	}
	if query != "" {
		url += "&search=" + query
	}
	if filter.Task != "" {
		url += "&pipeline_tag=" + filter.Task
	}
	if filter.Library != "" {
		url += "&library=" + filter.Library
	}
	if filter.Offset > 0 {
		url += fmt.Sprintf("&offset=%d", filter.Offset)
	}

	var models []struct {
		ID           string   `json:"id"`
		Downloads    int      `json:"downloads"`
		Likes        int      `json:"likes"`
		PipelineTag  string   `json:"pipeline_tag"`
		Author       string   `json:"author"`
		LibraryName  string   `json:"library_name"`
		Tags         []string `json:"tags"`
		LastModified string   `json:"lastModified"`
	}
	log.Printf("[HF] Search URL: %s", url)
	if err := httpGetJSON(url, &models); err != nil {
		return nil, fmt.Errorf("搜索 Hugging Face 失败: %w", err)
	}
	log.Printf("[HF] Search got %d models", len(models))

	result := &SearchResult{Total: len(models)}
	for _, m := range models {
		result.Models = append(result.Models, ModelEntry{
			ID:        m.ID,
			Name:      m.ID,
			Downloads: m.Downloads,
			Task:      TranslateTask(m.PipelineTag),
			Author:    m.Author,
			URL:       "https://huggingface.co/" + m.ID,
		})
	}
	return result, nil
}

// ListFiles 列出 repo 中所有条目（含目录），递归展开。
func (h *HuggingFace) ListFiles(repoID string) ([]FileEntry, error) {
	url := fmt.Sprintf("%s/models/%s/tree/main?recursive=true", hfAPI, repoID)
	var raw []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	if err := httpGetJSON(url, &raw); err != nil {
		return nil, fmt.Errorf("列出文件失败: %w", err)
	}
	var files []FileEntry
	for _, f := range raw {
		files = append(files, FileEntry{Path: f.Path, Type: f.Type, Size: f.Size})
	}
	return files, nil
}

// ListRevisions 获取模型的分支/tag 列表。
func (h *HuggingFace) ListRevisions(repoID string) ([]string, error) {
	url := fmt.Sprintf("%s/models/%s?blobs=true", hfAPI, repoID)
	var m struct {
		Siblings []struct {
			Rfilename string `json:"rfilename"`
		} `json:"siblings"`
		Tags []string `json:"tags"`
	}
	if err := httpGetJSON(url, &m); err != nil {
		return nil, fmt.Errorf("获取版本列表失败: %w", err)
	}
	revisions := []string{"main"}
	for _, tag := range m.Tags {
		revisions = append(revisions, tag)
	}
	return revisions, nil
}

// DownloadFile 下载指定文件。
func (h *HuggingFace) DownloadFile(repoID, filename, destDir string, onProgress func(pct int)) (string, error) {
	url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repoID, filename)
	return DownloadWithProgress(url, destDir, filename, onProgress)
}

// GetModelInfo 获取单个模型的详细信息（含描述）。使用 ?full=true 一次拿到 siblings 文件列表。
func (h *HuggingFace) GetModelInfo(repoID string) (*ModelEntry, error) {
	u := fmt.Sprintf("%s/models/%s?full=true", hfAPI, repoID)
	var m struct {
		ID          string   `json:"id"`
		Downloads   int      `json:"downloads"`
		Likes       int      `json:"likes"`
		PipelineTag string   `json:"pipeline_tag"`
		Author      string   `json:"author"`
		LibraryName string   `json:"library_name"`
		Tags        []string `json:"tags"`
		Siblings    []struct {
			Rfilename string `json:"rfilename"`
			Size      int64  `json:"size"`
		} `json:"siblings"`
	}
	if err := httpGetJSON(u, &m); err != nil {
		return nil, err
	}

	// 从 siblings 构造文件列表（full=true 已包含完整文件信息）
	var fileEntries []FileEntry
	for _, s := range m.Siblings {
		entryType := "file"
		if strings.HasSuffix(s.Rfilename, "/") {
			entryType = "directory"
		}
		fileEntries = append(fileEntries, FileEntry{
			Path: s.Rfilename,
			Type: entryType,
			Size: s.Size,
		})
	}

	// 抓 README 前 1000 字，跳过 license header，提取实际描述
	desc := ""
	readmeURL := fmt.Sprintf("https://huggingface.co/%s/raw/main/README.md", repoID)
	req, _ := http.NewRequest("GET", readmeURL, nil)
	req.Header.Set("User-Agent", "everevo/0.1")
	if resp, err := httpClient.Do(req); err == nil {
		if resp.StatusCode == 200 {
			buf := make([]byte, 2000)
			n, _ := resp.Body.Read(buf)
			raw := string(buf[:n])
			lines := strings.Split(raw, "\n")
			var contentLines []string
			inYAML := false
			yamlCount := 0
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "---" {
					yamlCount++
					inYAML = yamlCount == 1
					continue
				}
				if inYAML {
					continue
				}
				if trimmed == "" && len(contentLines) == 0 {
					continue
				}
				contentLines = append(contentLines, line)
				if len(contentLines) >= 10 {
					break
				}
			}
			desc = strings.TrimSpace(strings.Join(contentLines, "\n"))
		}
		resp.Body.Close()
	}

	return &ModelEntry{
		ID:          m.ID,
		Name:        m.ID,
		Description: desc,
		Downloads:   m.Downloads,
		Task:        TranslateTask(m.PipelineTag),
		Author:      m.Author,
		URL:         "https://huggingface.co/" + m.ID,
	}, nil
}

// FullFileEntries 返回从 full=true 响应中已解析的文件列表。
func (h *HuggingFace) FullFileEntries(repoID string) ([]FileEntry, error) {
	u := fmt.Sprintf("%s/models/%s?full=true", hfAPI, repoID)
	var m struct {
		Siblings []struct {
			Rfilename string `json:"rfilename"`
			Size      int64  `json:"size"`
		} `json:"siblings"`
	}
	if err := httpGetJSON(u, &m); err != nil {
		return nil, err
	}
	var entries []FileEntry
	for _, s := range m.Siblings {
		entryType := "file"
		if strings.HasSuffix(s.Rfilename, "/") {
			entryType = "directory"
		}
		entries = append(entries, FileEntry{
			Path: s.Rfilename,
			Type: entryType,
			Size: s.Size,
		})
	}
	return entries, nil
}
