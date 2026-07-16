// Package models: catalog logic — model marketplace search and file listing.
package models

import (
	"fmt"
	"log"
	"strings"

	"everevo/internal/catalog"
)

// ─── Types ──────────────────────────────────────────────────────────

// ModelDetail holds model metadata and file listing for the frontend.
type ModelDetail struct {
	Source      string                  `json:"source"`
	RepoID      string                  `json:"repoId"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Author      string                  `json:"author"`
	Task        string                  `json:"task"`
	Downloads   int                     `json:"downloads"`
	URL         string                  `json:"url"`
	Files       []string                `json:"files"`
	FileEntries []catalog.FileEntry     `json:"fileEntries"`
	FileTree    []catalog.FileTreeEntry `json:"fileTree"`
	InfoError   string                  `json:"infoError,omitempty"`
	FilesError  string                  `json:"filesError,omitempty"`
}

// ─── Source listing ─────────────────────────────────────────────────

// GetCatalogSources returns the names of all available catalog sources.
func GetCatalogSources() []string { return catalog.SourceNames() }

// ─── Model detail ──────────────────────────────────────────────────

// GetModelDetail fetches model info (API) + file listing (cached).
// Falls back to search results for missing fields (author/task/description).
func GetModelDetail(source, repoID string) *ModelDetail {
	s, ok := catalog.Sources[source]
	if !ok {
		return nil
	}

	detail := &ModelDetail{Source: source, RepoID: repoID, Name: repoID}

	// 1. Primary: detail API (precise repoID lookup)
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

	// 2. Fallback: fill missing fields from search results
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

	// File listing (cached)
	entries, ferr := listModelFilesErr(source, repoID)
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

// classifyNetErr normalizes catalog/HTTP errors for the frontend.
func classifyNetErr(err error) string {
	if err == nil {
		return ""
	}
	return "网络错误：" + err.Error()
}

// ─── Search ────────────────────────────────────────────────────────

// SearchAllCatalog performs multi-source concurrent search with dedup.
func SearchAllCatalog(query string, filter *catalog.SearchFilter) *catalog.SearchResult {
	key := "agg:" + query + ":" + filterKey(filter)
	if data := catalog.CacheGet(key, &catalog.SearchResult{}); data != nil {
		return data.(*catalog.SearchResult)
	}
	r := catalog.AggregatedSearch(query, 30, filter)
	catalog.CacheSet(key, r, catalog.SearchTTL)
	return r
}

// SearchCatalog searches a single source (10-minute cache).
func SearchCatalog(source, query string, filter *catalog.SearchFilter) *catalog.SearchResult {
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

// ─── Revisions ─────────────────────────────────────────────────────

// ListModelRevisions returns available revisions (branches/tags) for a model repo.
func ListModelRevisions(source, repoID string) []string {
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

// ─── File listing ──────────────────────────────────────────────────

// ListModelFiles lists all files in a model repo (30-minute cache).
func ListModelFiles(source, repoID string) []catalog.FileEntry {
	files, _ := listModelFilesErr(source, repoID)
	return files
}

// listModelFilesErr returns files or an error for upper-layer distinction.
func listModelFilesErr(source, repoID string) ([]catalog.FileEntry, error) {
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
		return nil, err
	}
	catalog.CacheSet(key, &files, catalog.FilesTTL)
	return files, nil
}

// ─── Cache ─────────────────────────────────────────────────────────

// InvalidateCache clears a specific cache key or all keys.
func InvalidateCache(key string) {
	if key == "" {
		catalog.InvalidateAll()
	} else {
		catalog.Invalidate(key)
	}
}
