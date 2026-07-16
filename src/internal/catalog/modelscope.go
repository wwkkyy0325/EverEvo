package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

const msAPI = "https://www.modelscope.cn/api/v1"

// ModelScope 源。
type ModelScope struct{}

func (ms *ModelScope) Name() string { return "ModelScope" }

// Search 搜索模型。MS 用 PUT /api/v1/models/ + JSON body。
func (ms *ModelScope) Search(query string, limit int, filter *SearchFilter) (*SearchResult, error) {
	if limit <= 0 {
		limit = 15
	}
	page := 1
	if filter != nil && filter.Offset > 0 && limit > 0 {
		page = 1 + filter.Offset/limit
	}
	body := map[string]interface{}{
		"PageNumber": page,
		"PageSize":   limit,
	}
	if query != "" {
		body["Name"] = query
	}
	if filter != nil {
		if filter.Task != "" {
			body["Task"] = filter.Task
		}
		if filter.Language != "" {
			body["Language"] = filter.Language
		}
		if filter.Sort == "lastModified" {
			body["SortBy"] = "GmtModified"
		}
	}
	bodyBytes, _ := json.Marshal(body)

	req, _ := http.NewRequest("PUT", msAPI+"/models/", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://modelscope.cn/models")
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[MS] 请求失败: %v", err)
		return nil, fmt.Errorf("搜索 ModelScope 失败: %w", err)
	}
	defer resp.Body.Close()

	bodyData, _ := io.ReadAll(resp.Body)
	log.Printf("[MS] HTTP %d, body 长度=%d, 前200字符=%s", resp.StatusCode, len(bodyData), truncate(string(bodyData), 200))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ModelScope HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Code int `json:"Code"`
		Data struct {
			Models []map[string]interface{} `json:"Models"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(bodyData, &raw); err != nil {
		log.Printf("[MS] JSON 解析失败: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 200 && raw.Code != 0 {
		log.Printf("[MS] Code=%d", raw.Code)
		return &SearchResult{}, nil
	}

	result := &SearchResult{Total: len(raw.Data.Models)}
	for _, m := range raw.Data.Models {
		owner := getStr(m, "Path")
		name := getStr(m, "Name")
		if owner == "" && name == "" {
			continue
		}
		id := owner
		if name != "" {
			id = owner + "/" + name
		}
		display := getStr(m, "ChineseName", "NickName", "Name")
		if display == "" {
			display = name
		}
		if display == "" {
			display = id
		}
		dl := int(getNum(m, "Downloads"))
		task := ""
		if tasks, ok := m["Tasks"].([]interface{}); ok {
			for _, t := range tasks {
				if ts, ok := t.(string); ok && ts != "" {
					task = ts
					break
				}
			}
		}
		author := ""
		if org, ok := m["Organization"].(map[string]interface{}); ok {
			author = getStr(org, "FullName", "Name", "Path")
		}
		desc := getStr(m, "Description")
		result.Models = append(result.Models, ModelEntry{
			ID:          id,
			Name:        display,
			Description: desc,
			Downloads:   dl,
			Task:        TranslateTask(task),
			Author:      author,
			URL:         "https://modelscope.cn/models/" + id,
		})
	}
	return result, nil
}

// ListFiles 列出仓库所有条目（含目录），递归展开。
func (ms *ModelScope) ListFiles(repoID string) ([]FileEntry, error) {
	u := fmt.Sprintf("%s/models/%s/repo/files?Revision=master&Recursive=true", msAPI, repoID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyData, _ := io.ReadAll(resp.Body)
	log.Printf("[MS] ListFiles %s HTTP %d body=%s", repoID, resp.StatusCode, truncate(string(bodyData), 300))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Data struct {
			Files []struct {
				Path string `json:"Path"`
				Type string `json:"Type"`
				Size int64  `json:"Size"`
			} `json:"Files"`
		} `json:"Data"`
	}
	json.Unmarshal(bodyData, &raw)

	var files []FileEntry
	for _, f := range raw.Data.Files {
		if f.Path == "" {
			continue
		}
		entryType := "file"
		if f.Type == "tree" {
			entryType = "directory"
		}
		files = append(files, FileEntry{Path: f.Path, Type: entryType, Size: f.Size})
	}
	return files, nil
}

// ListRevisions 获取模型仓库的版本列表。
func (ms *ModelScope) ListRevisions(repoID string) ([]string, error) {
	u := fmt.Sprintf("%s/models/%s/revisions", msAPI, repoID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyData, _ := io.ReadAll(resp.Body)
	log.Printf("[MS] ListRevisions %s HTTP %d body=%s", repoID, resp.StatusCode, truncate(string(bodyData), 300))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Code int `json:"Code"`
		Data struct {
			Revisions []struct {
				Revision string `json:"Revision"`
			} `json:"Revisions"`
		} `json:"Data"`
	}
	json.Unmarshal(bodyData, &raw)

	var revisions []string
	for _, r := range raw.Data.Revisions {
		if r.Revision != "" {
			revisions = append(revisions, r.Revision)
		}
	}
	if len(revisions) == 0 {
		revisions = []string{"master"}
	}
	return revisions, nil
}

// DownloadFile ModelScope 文件下载（支持 Cookie 认证）。
func (ms *ModelScope) DownloadFile(repoID, filename, destDir string, onProgress func(pct int)) (string, error) {
	url := fmt.Sprintf("https://www.modelscope.cn/models/%s/resolve/master/%s", repoID, filename)
	return DownloadWithProgress(url, destDir, filename, onProgress)
}

// GetModelInfo 获取单个模型的详细信息。
func (ms *ModelScope) GetModelInfo(repoID string) (*ModelEntry, error) {
	u := fmt.Sprintf("%s/models/%s?Revision=master", msAPI, repoID)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	bodyData, _ := io.ReadAll(resp.Body)
	log.Printf("[MS] GetModelInfo body=%s", truncate(string(bodyData), 300))

	var raw struct {
		Code int                    `json:"Code"`
		Data map[string]interface{} `json:"Data"`
	}
	json.Unmarshal(bodyData, &raw)

	m := raw.Data
	if m == nil {
		return nil, fmt.Errorf("Data 为空")
	}
	owner := getStr(m, "Path")
	name := getStr(m, "Name")
	id := owner + "/" + name
	display := getStr(m, "ChineseName", "NickName", "Name")
	if display == "" {
		display = name
	}
	if display == "" {
		display = repoID
	}
	dl := int(getNum(m, "Downloads"))
	desc := getStr(m, "Description", "Summary")
	author := ""
	if org, ok := m["Organization"].(map[string]interface{}); ok {
		author = getStr(org, "FullName", "Name")
	}
	task := ""
	if tasks, ok := m["Tasks"].([]interface{}); ok {
		for _, t := range tasks {
			if ts, ok := t.(string); ok && ts != "" {
				task = ts
				break
			}
		}
	}

	return &ModelEntry{
		ID:          id,
		Name:        display,
		Description: desc,
		Downloads:   dl,
		Task:        TranslateTask(task),
		Author:      author,
		URL:         "https://modelscope.cn/models/" + id,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func getStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func getNum(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return 0
}
