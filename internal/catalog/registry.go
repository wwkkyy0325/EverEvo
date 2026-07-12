package catalog

// Sources 已注册的模型源。
var Sources = map[string]Source{
	"huggingface": &HuggingFace{},
	"modelscope":  &ModelScope{},
}

// AggregatedSearch 多源并发搜索，去重合并。
func AggregatedSearch(query string, limit int, filter *SearchFilter) *SearchResult {
	if filter == nil {
		filter = &SearchFilter{}
	}
	type srcResult struct {
		source string
		result *SearchResult
	}
	ch := make(chan srcResult, len(Sources))
	for key, src := range Sources {
		go func(k string, s Source) {
			// Recover from panics in any single source so one buggy source
			// can't crash the whole aggregate (a nil-deref in HF Search did
			// exactly this before the in-source nil guard was added).
			defer func() {
				if r := recover(); r != nil {
					ch <- srcResult{source: k, result: &SearchResult{}}
				}
			}()
			r, err := s.Search(query, limit, filter)
			if err != nil || r == nil {
				r = &SearchResult{}
			}
			ch <- srcResult{source: k, result: r}
		}(key, src)
	}

	seen := map[string]bool{}
	merged := &SearchResult{}
	for range Sources {
		sr := <-ch
		for _, m := range sr.result.Models {
			// 去重：按原名去重
			key := m.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			m.Source = sr.source
			m.ID = sr.source + "|" + m.ID
			merged.Models = append(merged.Models, m)
		}
	}
	merged.Total = len(merged.Models)
	return merged
}

// SourceNames 返回已注册的源名列表。
func SourceNames() []string {
	keys := make([]string, 0, len(Sources))
	for k := range Sources {
		keys = append(keys, k)
	}
	return keys
}
