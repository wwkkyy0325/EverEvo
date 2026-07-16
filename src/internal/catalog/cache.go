package catalog

import (
	"sync"
	"time"
)

type cacheEntry struct {
	data interface{}
	t    time.Time
	ttl  time.Duration
}

var (
	cacheStore   = map[string]cacheEntry{}
	cacheMu      sync.Mutex
)

// CacheTTL 默认的缓存时效。
const (
	SearchTTL  = 10 * time.Minute // 搜索结果
	FilesTTL   = 30 * time.Minute // 文件列表
	DefaultTTL = 5 * time.Minute
)

// CacheGet 读缓存，未命中返回 nil。
func CacheGet(key string, sample interface{}) interface{} {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cacheStore[key]
	if !ok || time.Since(e.t) >= e.ttl {
		return nil
	}
	return e.data
}

// CacheSet 写缓存。
func CacheSet(key string, data interface{}, ttl time.Duration) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheStore[key] = cacheEntry{data: data, t: time.Now(), ttl: ttl}
}

// Invalidate 清除指定前缀的缓存。
func Invalidate(prefix string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for k := range cacheStore {
		if prefix == "" || (len(prefix) <= len(k) && k[:len(prefix)] == prefix) {
			delete(cacheStore, k)
		}
	}
}

// InvalidateAll 清除全部缓存。
func InvalidateAll() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheStore = map[string]cacheEntry{}
}
