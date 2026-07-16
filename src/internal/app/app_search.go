//go:build windows

// Package app — composite multi-engine web search.
//
// Architecture:
//
//	web_search(query)
//	  └─ compositeSearch(query)
//	       ├─ baiduEngine   (HTML scrape, no API key)
//	       ├─ bingEngine    (HTML scrape, 13 Edge headers from Claude Code)
//	       ├─ sogouEngine   (HTML scrape, no API key)
//	       └─ ddgEngine     (HTML scrape, no API key)
//	       │  goroutine fan-out, 8s per-engine timeout
//	       └─ mergeResults() → URL dedup, round-robin interleave, max 15
//
// Inspired by:
//   - Claude Code's bingAdapter.ts (13 Edge browser headers, base64url URL decode, 3-tier snippet)
//   - OpenClaw's DuckDuckGo provider (uddg URL decode, HTML entity handling, cache pattern)

package app

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"everevo/internal/httpclient"
)

// ── Types ─────────────────────────────────────────────────────────────

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engine  string `json:"engine,omitempty"`
}

type searchEngine interface {
	Name() string
	Search(query string) ([]searchResult, error)
}

// ── Search cache ──────────────────────────────────────────────────────

type cacheEntry struct {
	results   []searchResult
	expiresAt time.Time
}

var (
	searchCache   = make(map[string]cacheEntry)
	cacheMu       sync.RWMutex
	cacheTTL      = 15 * time.Minute
	cacheMaxSize  = 200
)

func cacheGet(key string) ([]searchResult, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	e, ok := searchCache[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.results, true
}

func cacheSet(key string, results []searchResult) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	// Evict oldest if at capacity.
	if len(searchCache) >= cacheMaxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range searchCache {
			if oldestKey == "" || v.expiresAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.expiresAt
			}
		}
		delete(searchCache, oldestKey)
	}
	searchCache[key] = cacheEntry{results: results, expiresAt: time.Now().Add(cacheTTL)}
}

func cacheKey(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// ── Browser headers ───────────────────────────────────────────────────

// edgeHeaders mimics Microsoft Edge on Windows — used for Bing.
// From Claude Code's bingAdapter.ts.
var edgeHeaders = map[string]string{
	"User-Agent":                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
	"Accept-Language":           "en-US,en;q=0.9",
	"Cache-Control":             "no-cache",
	"Pragma":                    "no-cache",
	"Sec-Ch-Ua":                 `"Microsoft Edge";v="131", "Chromium";v="131", "Not_A Brand";v="24"`,
	"Sec-Ch-Ua-Mobile":          "?0",
	"Sec-Ch-Ua-Platform":        `"macOS"`,
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
}

// chromeHeaders mimics Chrome on Windows — used for Baidu/Sogou/DuckDuckGo.
var chromeHeaders = map[string]string{
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	"Cache-Control":   "no-cache",
}

func setHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// ── Composite search ──────────────────────────────────────────────────

func compositeSearch(query string) []searchResult {
	if cached, ok := cacheGet(cacheKey(query)); ok {
		log.Printf("[search] cache hit for %q (%d results)", query, len(cached))
		return cached
	}

	engines := []searchEngine{
		&baiduEngine{},
		&bingEngine{},
		&sogouEngine{},
		&ddgEngine{},
	}

	now := time.Now()

	type engineResult struct {
		name    string
		results []searchResult
	}
	ch := make(chan engineResult, len(engines))

	var wg sync.WaitGroup
	for _, e := range engines {
		wg.Add(1)
		go func(eng searchEngine) {
			defer wg.Done()
			results, err := eng.Search(query)
			if err != nil {
				log.Printf("[search] %s: %v", eng.Name(), err)
				return
			}
			ch <- engineResult{name: eng.Name(), results: results}
		}(e)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var all []searchResult
	for er := range ch {
		log.Printf("[search] %s: %d results (%.1fs)", er.name, len(er.results), time.Since(now).Seconds())
		all = append(all, er.results...)
	}

	merged := mergeResults(all)
	log.Printf("[search] merged: %d results from 4 engines (%.1fs total)", len(merged), time.Since(now).Seconds())

	cacheSet(cacheKey(query), merged)
	return merged
}

// mergeResults deduplicates by normalized URL and returns up to 15 results.
func mergeResults(all []searchResult) []searchResult {
	seen := make(map[string]bool)
	var out []searchResult
	for _, r := range all {
		key := normalizeURL(r.URL)
		if seen[key] {
			// Keep the one with the longer snippet.
			for i, existing := range out {
				if normalizeURL(existing.URL) == key {
					if len(r.Snippet) > len(existing.Snippet) {
						out[i] = r
					}
					break
				}
			}
			continue
		}
		seen[key] = true
		r.Engine = ""
		out = append(out, r)
	}
	if len(out) > 15 {
		out = out[:15]
	}
	return out
}

func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimPrefix(u.Host, "www.")
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// ── Bing engine (Claude Code-style, 13 Edge headers) ──────────────────

type bingEngine struct{}

func (e *bingEngine) Name() string { return "bing" }

func (e *bingEngine) Search(query string) ([]searchResult, error) {
	u := "https://www.bing.com/search?q=" + url.QueryEscape(query) + "&setmkt=zh-CN&count=20"
	req, _ := http.NewRequest("GET", u, nil)
	setHeaders(req, edgeHeaders)

	client := httpclient.New(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	html := string(body)

	if strings.Contains(html, "captcha") || strings.Contains(html, "挑战") {
		return nil, fmt.Errorf("CAPTCHA/rate-limited")
	}

	return parseBingHTML(html), nil
}

// parseBingHTML extracts organic results from Bing's <li class="b_algo"> blocks.
// Strategy from Claude Code's extractBingResults().
func parseBingHTML(html string) []searchResult {
	algoRe := regexp.MustCompile(`<li\s+class="b_algo"[^>]*>([\s\S]*?)</li>`)
	matches := algoRe.FindAllStringSubmatch(html, 20)

	// Link: <h2><a href="...">...</a></h2>
	h2LinkRe := regexp.MustCompile(`<h2[^>]*>\s*<a[^>]+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)

	var results []searchResult
	for _, m := range matches {
		block := m[1]
		linkMatch := h2LinkRe.FindStringSubmatch(block)
		if linkMatch == nil {
			continue
		}
		rawURL := decodeHTMLEntities(linkMatch[1])
		realURL := resolveBingURL(rawURL)
		if realURL == "" {
			continue
		}
		title := stripTags(decodeHTMLEntities(linkMatch[2]))
		snippet := extractBingSnippet(block)

		results = append(results, searchResult{
			Title:   title,
			URL:     realURL,
			Snippet: snippet,
			Engine:  "bing",
		})
	}
	return results
}

// extractBingSnippet implements Claude Code's 3-tier snippet extraction:
// 1. <p class="b_lineclamp...">  2. <div class="b_caption"> → <p>
// 3. <div class="b_caption"> full text.
func extractBingSnippet(block string) string {
	// Tier 1: b_lineclamp
	if re := regexp.MustCompile(`<p[^>]*class="b_lineclamp[^"]*"[^>]*>([\s\S]*?)</p>`); true {
		if m := re.FindStringSubmatch(block); len(m) >= 2 {
			return strings.TrimSpace(stripTags(decodeHTMLEntities(m[1])))
		}
	}
	// Tier 2: b_caption → <p>
	if re := regexp.MustCompile(`<div[^>]*class="b_caption[^"]*"[^>]*>[\s\S]*?<p[^>]*>([\s\S]*?)</p>`); true {
		if m := re.FindStringSubmatch(block); len(m) >= 2 {
			return strings.TrimSpace(stripTags(decodeHTMLEntities(m[1])))
		}
	}
	// Tier 3: b_caption full text
	if re := regexp.MustCompile(`<div[^>]*class="b_caption[^"]*"[^>]*>([\s\S]*?)</div>`); true {
		if m := re.FindStringSubmatch(block); len(m) >= 2 {
			t := stripTags(decodeHTMLEntities(m[1]))
			if t != "" {
				return strings.TrimSpace(t)
			}
		}
	}
	return ""
}

// resolveBingURL decodes Bing's redirect URLs. From Claude Code's resolveBingUrl().
// Format: https://www.bing.com/ck/a?!...&u=a1BASE64URL...
// The 'u' parameter: 2-char prefix (a1=https, a0=http) + base64url-encoded URL.
func resolveBingURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, "#") {
		return ""
	}
	// Try to extract the 'u' parameter from redirect URLs.
	re := regexp.MustCompile(`[?&]u=([a-zA-Z0-9+/_=-]+)`)
	if m := re.FindStringSubmatch(rawURL); len(m) >= 2 {
		encoded := m[1]
		if len(encoded) >= 3 {
			b64 := encoded[2:] // skip "a1" or "a0" prefix
			// Convert base64url → standard base64
			padded := strings.NewReplacer("-", "+", "_", "/").Replace(b64)
			// Pad to multiple of 4.
			if rem := len(padded) % 4; rem != 0 {
				padded += strings.Repeat("=", 4-rem)
			}
			decoded, err := base64.StdEncoding.DecodeString(padded)
			if err == nil && strings.HasPrefix(string(decoded), "http") {
				return string(decoded)
			}
		}
	}
	// Direct external URL (not Bing-internal).
	if !strings.Contains(rawURL, "bing.com") {
		return rawURL
	}
	return ""
}

// ── Baidu engine ───────────────────────────────────────────────────────

type baiduEngine struct{}

func (e *baiduEngine) Name() string { return "baidu" }

func (e *baiduEngine) Search(query string) ([]searchResult, error) {
	u := "https://www.baidu.com/s?wd=" + url.QueryEscape(query) + "&rn=20"
	req, _ := http.NewRequest("GET", u, nil)
	setHeaders(req, chromeHeaders)

	client := httpclient.New(8 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	html := string(body)

	if strings.Contains(html, "百度安全验证") {
		return nil, fmt.Errorf("CAPTCHA/rate-limited")
	}

	return parseBaiduHTML(html), nil
}

func parseBaiduHTML(html string) []searchResult {
	// Baidu result blocks: <div class="result c-container" ...>
	blockRe := regexp.MustCompile(`<div[^>]*class="[^"]*c-container[^"]*"[^>]*>([\s\S]*?)</div>\s*(?:<div[^>]*class="[^"]*c-container|$)`)
	blocks := blockRe.FindAllStringSubmatch(html, 20)

	// Title: first <a> with href="http..." inside a heading or standalone.
	linkRe := regexp.MustCompile(`<a[^>]*href="(https?://[^"]*)"[^>]*>([\s\S]*?)</a>`)
	snippetRe := regexp.MustCompile(`<span[^>]*class="[^"]*content-right[^"]*"[^>]*>([\s\S]*?)</span>`)
	altSnippetRe := regexp.MustCompile(`<div[^>]*class="[^"]*c-abstract[^"]*"[^>]*>([\s\S]*?)</div>`)

	var results []searchResult
	for _, block := range blocks {
		content := block[0]
		if len(block) > 1 {
			content = block[1]
		}

		// Find first external link.
		linkMatches := linkRe.FindAllStringSubmatch(content, 3)
		var r searchResult
		for _, lm := range linkMatches {
			href := lm[1]
			if strings.Contains(href, "baidu.com") && !strings.Contains(href, "baidu.com/link") {
				continue
			}
			r.URL = decodeBaiduURL(href)
			r.Title = stripTags(lm[2])
			break
		}
		if r.URL == "" || r.Title == "" {
			continue
		}

		if m := snippetRe.FindStringSubmatch(content); len(m) >= 2 {
			r.Snippet = stripTags(m[1])
		} else if m := altSnippetRe.FindStringSubmatch(content); len(m) >= 2 {
			r.Snippet = stripTags(m[1])
		}
		r.Engine = "baidu"
		results = append(results, r)
	}
	return results
}

// decodeBaiduURL extracts the real URL from Baidu's redirect wrapper.
func decodeBaiduURL(raw string) string {
	if !strings.Contains(raw, "baidu.com/link") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if realURL := u.Query().Get("url"); realURL != "" {
		decoded, err := url.QueryUnescape(realURL)
		if err == nil && strings.HasPrefix(decoded, "http") {
			return decoded
		}
	}
	return raw
}

// ── Sogou engine ───────────────────────────────────────────────────────

type sogouEngine struct{}

func (e *sogouEngine) Name() string { return "sogou" }

func (e *sogouEngine) Search(query string) ([]searchResult, error) {
	u := "https://www.sogou.com/web?query=" + url.QueryEscape(query) + "&num=20"
	req, _ := http.NewRequest("GET", u, nil)
	setHeaders(req, chromeHeaders)

	client := httpclient.New(8 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	html := string(body)

	if strings.Contains(html, "请输入验证码") || strings.Contains(html, "异常访问") {
		return nil, fmt.Errorf("CAPTCHA/rate-limited")
	}

	return parseSogouHTML(html), nil
}

func parseSogouHTML(html string) []searchResult {
	blockRe := regexp.MustCompile(`<div[^>]*class="[^"]*\brb\b[^"]*"[^>]*>([\s\S]*?)</div>\s*<div[^>]*class="[^"]*\brb\b`)
	blocks := blockRe.FindAllStringSubmatch(html, 20)

	titleRe := regexp.MustCompile(`<h3[^>]*class="[^"]*\bpt\b[^"]*"[^>]*>\s*<a[^>]*href="([^"]*)"[^>]*>([\s\S]*?)</a>`)
	altLinkRe := regexp.MustCompile(`<a[^>]*href="(https?://[^"]*)"[^>]*>([\s\S]*?)</a>`)
	snippetRe := regexp.MustCompile(`<p[^>]*class="[^"]*str_info[^"]*"[^>]*>([\s\S]*?)</p>`)
	altSnippetRe := regexp.MustCompile(`<div[^>]*class="[^"]*\bft\b[^"]*"[^>]*>([\s\S]*?)</div>`)

	var results []searchResult
	for _, block := range blocks {
		content := block[0]
		if len(block) > 1 {
			content = block[1]
		}

		var r searchResult
		if m := titleRe.FindStringSubmatch(content); len(m) >= 3 {
			r.URL = decodeSogouURL(m[1])
			r.Title = stripTags(m[2])
		} else {
			// Fallback: find first external link.
			for _, lm := range altLinkRe.FindAllStringSubmatch(content, 3) {
				href := lm[1]
				if !strings.HasPrefix(href, "http") || strings.Contains(href, "sogou.com") {
					continue
				}
				r.URL = href
				r.Title = stripTags(lm[2])
				break
			}
		}
		if r.URL == "" || r.Title == "" {
			continue
		}

		if m := snippetRe.FindStringSubmatch(content); len(m) >= 2 {
			r.Snippet = stripTags(m[1])
		} else if m := altSnippetRe.FindStringSubmatch(content); len(m) >= 2 {
			r.Snippet = stripTags(m[1])
		}
		r.Engine = "sogou"
		results = append(results, r)
	}
	return results
}

// decodeSogouURL extracts the real URL from Sogou's redirect format.
func decodeSogouURL(raw string) string {
	if !strings.Contains(raw, "sogou.com/link") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if realURL := u.Query().Get("url"); realURL != "" {
		decoded, err := url.QueryUnescape(realURL)
		if err == nil && strings.HasPrefix(decoded, "http") {
			return decoded
		}
	}
	return raw
}

// ── DuckDuckGo engine ──────────────────────────────────────────────────

type ddgEngine struct{}

func (e *ddgEngine) Name() string { return "ddg" }

func (e *ddgEngine) Search(query string) ([]searchResult, error) {
	u := "https://html.duckduckgo.com/html/?q=" + strings.ReplaceAll(query, " ", "+")
	req, _ := http.NewRequest("GET", u, nil)
	setHeaders(req, chromeHeaders)

	client := httpclient.New(8 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	html := string(body)

	// DDG bot challenge detection (from OpenClaw).
	if strings.Contains(html, "g-recaptcha") || strings.Contains(html, "challenge-form") {
		return nil, fmt.Errorf("bot challenge detected")
	}

	return parseDDGHTML(html), nil
}

func parseDDGHTML(html string) []searchResult {
	linkRe := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>([^<]*)</a>`)
	snippetRe := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>([^<]*)</a>`)

	links := linkRe.FindAllStringSubmatch(html, 10)
	snippets := snippetRe.FindAllStringSubmatch(html, 10)

	var results []searchResult
	for i, l := range links {
		if len(l) < 3 {
			continue
		}
		rawURL := decodeHTMLEntities(l[1])
		realURL := decodeDDGURL(rawURL)
		if realURL == "" {
			continue
		}

		snippet := ""
		if i < len(snippets) && len(snippets[i]) > 1 {
			snippet = decodeHTMLEntities(strings.TrimSpace(snippets[i][1]))
		}

		results = append(results, searchResult{
			Title:   decodeHTMLEntities(strings.TrimSpace(l[2])),
			URL:     realURL,
			Snippet: snippet,
			Engine:  "ddg",
		})
	}
	return results
}

// decodeDDGURL extracts the real URL from DuckDuckGo's redirect (uddg parameter).
// From OpenClaw's ddg-client.ts.
func decodeDDGURL(raw string) string {
	if strings.Contains(raw, "duckduckgo.com/l/?uddg=") {
		u, err := url.Parse(raw)
		if err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				decoded, err := url.QueryUnescape(uddg)
				if err == nil && strings.HasPrefix(decoded, "http") {
					return decoded
				}
			}
		}
	}
	// Skip DDG internal links.
	if strings.Contains(raw, "duckduckgo.com") && !strings.Contains(raw, "/l/?uddg=") {
		return ""
	}
	return raw
}

// ── HTML utilities ────────────────────────────────────────────────────

func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, "")
	return decodeHTMLEntities(s)
}

// decodeHTMLEntities handles common HTML entities. From OpenClaw's decoder.
func decodeHTMLEntities(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
		"&ndash;", "–",
		"&mdash;", "—",
		"&hellip;", "…",
		"&#x27;", "'",
		"&#x2F;", "/",
	)
	s = replacer.Replace(s)
	// Collapse whitespace.
	ws := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(ws.ReplaceAllString(s, " "))
}
