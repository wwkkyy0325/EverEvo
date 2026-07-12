//go:build windows

package app

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/auth"
	"everevo/internal/catalog"
	"everevo/internal/config"
)

// ─── 模型市场 API ──────────────────────────────────────────────

func (a *App) GetCatalogSources() []string { return catalog.SourceNames() }

// ModelDetail 模型详情（卡片点击后从右侧面板展示）。
type ModelDetail struct {
	Source      string                  `json:"source"`
	RepoID      string                  `json:"repoId"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Author      string                  `json:"author"`
	Task        string                  `json:"task"`
	Downloads   int                     `json:"downloads"`
	URL         string                  `json:"url"`
	Files       []string                `json:"files"`       // 所有文件路径
	FileEntries []catalog.FileEntry     `json:"fileEntries"` // 所有条目（含大小和类型）
	FileTree    []catalog.FileTreeEntry `json:"fileTree"`    // 目录树（前端渲染用）
	InfoError   string                  `json:"infoError,omitempty"`   // 基本信息/描述请求失败
	FilesError  string                  `json:"filesError,omitempty"`  // 文件列表请求失败（多为网络错误）
}

// GetModelDetail 获取模型详情 + 完整文件列表。
// 数据源优先级：GetModelInfo（按 repoID 精确查询）为主，Search（按名搜索）为兜底。
func (a *App) GetModelDetail(source, repoID string) *ModelDetail {
	s, ok := catalog.Sources[source]
	if !ok {
		return nil
	}

	detail := &ModelDetail{Source: source, RepoID: repoID, Name: repoID}

	// 1. 主数据源：详情 API（精确 repoID 查询，名称/作者/任务/下载量/描述都可靠）
	info, ierr := s.GetModelInfo(repoID)
	if ierr != nil {
		detail.InfoError = classifyNetErr(ierr)
	} else if info != nil {
		if info.Name != "" {
			detail.Name = info.Name
		}
		detail.Author = info.Author
		detail.Task = info.Task
		detail.Downloads = info.Downloads
		if info.Description != "" {
			detail.Description = info.Description
		}
		if info.URL != "" {
			detail.URL = info.URL
		}
	}

	// 2. 兜底：详情 API 没拿到的字段，用搜索结果补（需精确匹配 repoID，避免串台）
	if detail.Downloads == 0 || detail.Description == "" || detail.Author == "" || detail.Task == "" {
		if result, serr := s.Search(repoID, 5, nil); serr == nil && result != nil {
			for _, m := range result.Models {
				id := m.ID
				if strings.Contains(id, "|") {
					parts := strings.SplitN(id, "|", 2)
					id = parts[1]
				}
				if id == repoID || strings.HasSuffix(id, "/"+repoID) || strings.HasSuffix(repoID, "/"+id) {
					if detail.Name == repoID && m.Name != "" {
						detail.Name = m.Name
					}
					if detail.Author == "" {
						detail.Author = m.Author
					}
					if detail.Task == "" {
						detail.Task = m.Task
					}
					if detail.Downloads == 0 {
						detail.Downloads = m.Downloads
					}
					if detail.Description == "" {
						detail.Description = m.Description
					}
					if detail.URL == "" {
						detail.URL = m.URL
					}
					break
				}
			}
		}
	}

	if detail.URL == "" {
		if source == "huggingface" {
			detail.URL = "https://huggingface.co/" + repoID
		} else {
			detail.URL = "https://modelscope.cn/models/" + repoID
		}
	}

	// 文件列表（走缓存，显示全部文件不再过滤）
	entries, ferr := a.listModelFilesErr(source, repoID)
	if ferr != nil {
		detail.FilesError = classifyNetErr(ferr)
	} else {
		detail.FileEntries = entries
		for _, fe := range entries {
			detail.Files = append(detail.Files, fe.Path)
		}
		detail.FileTree = catalog.BuildFileTree(entries)
	}
	return detail
}

// classifyNetErr 把 catalog/HTTP 错误归一为面向用户的网络错误提示。
func classifyNetErr(err error) string {
	if err == nil {
		return ""
	}
	return "网络错误：" + err.Error()
}

// AccountInfo 账号状态。
type AccountInfo struct {
	Source    string `json:"source"`
	Name      string `json:"name"`
	HasToken  bool   `json:"hasToken"`
	LoginURL  string `json:"loginUrl"`
	Username  string `json:"username"`  // 验证后的用户名（空=未验证）
	Valid     bool   `json:"valid"`     // 凭证是否有效
	Reason    string `json:"reason"`    // 无效原因
	Verifying bool   `json:"verifying"` // 正在后台验证中
}

var loginURLs = map[string]string{
	"huggingface": "https://huggingface.co/settings/tokens",
	"modelscope":  "https://modelscope.cn/my/myaccesstoken",
}
var sourceNames = map[string]string{
	"huggingface": "Hugging Face",
	"modelscope":  "ModelScope",
}

// GetAccounts 立即返回各平台账号（HasToken 来自配置，不阻塞）。
// 有凭证的平台在后台并行验证，结果通过 account-verified 事件推送给前端。
func (a *App) GetAccounts() []AccountInfo {
	var list []AccountInfo
	for _, src := range catalog.SourceNames() {
		name := sourceNames[src]
		if name == "" {
			name = src
		}
		cred, has := a.cfg.Credentials[src]
		acc := AccountInfo{Source: src, Name: name, HasToken: has, LoginURL: loginURLs[src], Verifying: has}
		list = append(list, acc)
		if has {
			go a.verifyAndEmit(src, cred)
		}
	}
	return list
}

// verifyAndEmit 后台验证单个平台，结果通过事件推送（并行、互不阻塞）。
func (a *App) verifyAndEmit(source, credential string) {
	info := auth.Verify(source, credential)
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "account-verified", map[string]interface{}{
			"source":   source,
			"valid":    info.Valid,
			"username": info.Username,
			"reason":   info.Reason,
		})
	}
}

// VerifyAccount 手动验证指定平台凭证（阻塞，用于手动刷新）。
func (a *App) VerifyAccount(source string) *auth.UserInfo {
	cred, ok := a.cfg.Credentials[source]
	if !ok {
		return &auth.UserInfo{Valid: false, Reason: "未登录"}
	}
	return auth.Verify(source, cred)
}

// SetAccountToken 保存平台 token。
func (a *App) SetAccountToken(source, token string) error {
	if a.cfg.Credentials == nil {
		a.cfg.Credentials = map[string]string{}
	}
	if token == "" {
		delete(a.cfg.Credentials, source)
		catalog.SetCredential(source, catalog.Credential{})
	} else {
		a.cfg.Credentials[source] = token
		catalog.SetCredential(source, catalog.Credential{Token: token})
	}
	return config.Save(a.cfg)
}

// OpenLoginPage 在默认浏览器打开平台 token 获取页。
func (a *App) OpenLoginPage(source string) {
	url := loginURLs[source]
	if url == "" { return }
	openBrowser(url)
}

// LoginToSource 在软件内弹出浏览器窗口，用户登录后自动截取 cookie。
// 阻塞直到登录成功或超时（5 分钟）。
func (a *App) LoginToSource(source string) (string, error) {
	log.Printf("[API] LoginToSource %s — 正在打开浏览器…", source)
	result, err := auth.Login(source, 5*time.Minute)
	if err != nil {
		log.Printf("[API] LoginToSource 失败: %v", err)
		return "", err
	}
	// 保存 cookie 作为凭证
	if a.cfg.Credentials == nil {
		a.cfg.Credentials = map[string]string{}
	}
	a.cfg.Credentials[source] = result.Cookies
	catalog.SetCredential(source, catalog.Credential{Cookie: result.Cookies})
	config.Save(a.cfg)
	log.Printf("[API] LoginToSource 成功: %s", source)
	// Notify frontend to refresh the account panel.
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "account-verified", map[string]interface{}{
			"source": source, "valid": true, "username": "", "reason": "",
		})
	}
	return result.Source + " 登录成功", nil
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

// SetCatalogCredential 设置平台凭证（Cookie 或 API Token）。
func (a *App) SetCatalogCredential(source, token, cookie string) {
	catalog.SetCredential(source, catalog.Credential{Token: token, Cookie: cookie})
	log.Printf("[API] 已为 %s 设置凭证", source)
}

// SearchAllCatalog 多源汇总搜索（并发 + 去重）。
func (a *App) SearchAllCatalog(query string, filter *catalog.SearchFilter) *catalog.SearchResult {
	key := "agg:" + query + ":" + filterKey(filter)
	if data := catalog.CacheGet(key, &catalog.SearchResult{}); data != nil {
		return data.(*catalog.SearchResult)
	}
	r := catalog.AggregatedSearch(query, 30, filter)
	catalog.CacheSet(key, r, catalog.SearchTTL)
	return r
}

// SearchCatalog 在指定源搜索模型（10 分钟缓存）。失败时返回带 Error 的结果，不缓存。
func (a *App) SearchCatalog(source, query string, filter *catalog.SearchFilter) *catalog.SearchResult {
	log.Printf("[API] SearchCatalog source=%s query=%q filter=%+v", source, query, filter)
	key := "search:" + source + ":" + query + ":" + filterKey(filter)
	if data := catalog.CacheGet(key, &catalog.SearchResult{}); data != nil {
		return data.(*catalog.SearchResult)
	}
	s, ok := catalog.Sources[source]
	if !ok {
		return &catalog.SearchResult{Error: "未知来源: " + source}
	}
	r, err := s.Search(query, 20, filter)
	if err != nil {
		log.Printf("[API] SearchCatalog 失败: %v", err)
		return &catalog.SearchResult{Error: classifyNetErr(err)}
	}
	catalog.CacheSet(key, r, catalog.SearchTTL)
	return r
}

// ListModelRevisions returns available revisions (branches/tags) for a model repo.
func (a *App) ListModelRevisions(source, repoID string) []string {
	s, ok := catalog.Sources[source]
	if !ok {
		return nil
	}
	revs, err := s.ListRevisions(repoID)
	if err != nil {
		log.Printf("[API] ListRevisions failed for %s/%s: %v", source, repoID, err)
		return nil
	}
	names := make([]string, len(revs))
	copy(names, revs)
	return names
}

func filterKey(f *catalog.SearchFilter) string {
	if f == nil {
		return ""
	}
	return f.FilterKey()
}

// ListModelFiles 列出仓库所有文件（30 分钟缓存）。前端兼容入口（吞掉错误）。
func (a *App) ListModelFiles(source, repoID string) []catalog.FileEntry {
	files, _ := a.listModelFilesErr(source, repoID)
	return files
}

// listModelFilesErr 列出仓库所有文件，返回错误以便上层区分网络错误。
func (a *App) listModelFilesErr(source, repoID string) ([]catalog.FileEntry, error) {
	key := "files:" + source + ":" + repoID
	if data := catalog.CacheGet(key, &[]catalog.FileEntry{}); data != nil {
		return *(data.(*[]catalog.FileEntry)), nil
	}
	s, ok := catalog.Sources[source]
	if !ok {
		return nil, fmt.Errorf("未知来源: %s", source)
	}
	files, err := s.ListFiles(repoID)
	if err != nil {
		return nil, err // 不缓存失败结果
	}
	catalog.CacheSet(key, &files, catalog.FilesTTL)
	return files, nil
}

// InvalidateCache 手动刷新缓存（清除特定 key 或全部）。
func (a *App) InvalidateCache(key string) {
	if key == "" {
		catalog.InvalidateAll()
	} else {
		catalog.Invalidate(key)
	}
}

