package httpclient

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var (
	userProxy    string
	proxyEnabled = true // global kill-switch; toggled via settings
	lastHealthy  = true // last HealthCheck result; true until proven otherwise
	mu           sync.RWMutex
)

// SetUserProxy sets the user-configured proxy URL (from EverEvo settings UI).
// Pass "" to clear it.
func SetUserProxy(proxyURL string) {
	mu.Lock()
	defer mu.Unlock()
	userProxy = proxyURL
}

// SetEnabled enables or disables the proxy globally.
// When disabled, all requests go direct regardless of config/env/system proxy.
func SetEnabled(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	proxyEnabled = enabled
}

// ProxyStatus describes the current proxy state for display in settings UI.
type ProxyStatus struct {
	URL     string `json:"url"`     // active proxy URL, empty = direct
	Source  string `json:"source"`  // "config" | "env" | "system" | "none"
	Enabled bool   `json:"enabled"` // global kill-switch state
	Healthy bool   `json:"healthy"` // last health-check result
}

// Detect returns the active proxy and its source.
// Precedence: user config > env vars > Windows system proxy > direct.
// When proxyEnabled is false, always returns "none".
func Detect() ProxyStatus {
	mu.RLock()
	enabled := proxyEnabled
	u := userProxy
	healthy := lastHealthy
	mu.RUnlock()

	ps := ProxyStatus{Enabled: enabled, Healthy: healthy}
	if !enabled {
		ps.Source = "none"
		return ps
	}

	// 1. User-configured proxy (EverEvo settings)
	if u != "" {
		ps.URL = u
		ps.Source = "config"
		return ps
	}

	// 2. Environment variables (set by Clash, V2Ray, etc.)
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := os.Getenv(key); v != "" {
			ps.URL = v
			ps.Source = "env"
			return ps
		}
	}

	// 3. Windows system proxy (registry)
	if u := systemProxy(); u != "" {
		ps.URL = u
		ps.Source = "system"
		return ps
	}

	ps.Source = "none"
	return ps
}

// HealthCheck probes the currently active proxy and logs a warning if
// it's unreachable. Non-blocking — call in a goroutine during startup.
// Updates the in-memory health state so the UI reflects reality.
func HealthCheck() {
	ps := Detect()
	if ps.URL == "" || !ps.Enabled {
		return
	}
	if err := Test(ps.URL); err != nil {
		log.Printf("⚠ 代理健康检查失败 [%s]: %v — 请求可能超时。可在设置中禁用代理。", ps.Source, err)
		mu.Lock()
		lastHealthy = false
		mu.Unlock()
	} else {
		log.Printf("✓ 代理可用 [%s]: %s", ps.Source, ps.URL)
		mu.Lock()
		lastHealthy = true
		mu.Unlock()
	}
}

// Test checks if the given proxy URL is reachable.
func Test(proxyURL string) error {
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	if proxy.Scheme == "" {
		proxy.Scheme = "http"
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy),
		},
	}
	resp, err := client.Get("https://www.google.com/generate_204")
	if err != nil {
		return fmt.Errorf("代理不可达: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("代理返回错误状态: %d", resp.StatusCode)
	}
	return nil
}

// proxyFunc returns a proxy function for http.Transport.
func proxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		ps := Detect()
		if ps.URL == "" || !ps.Enabled {
			return nil, nil // direct
		}
		proxy, err := url.Parse(ps.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", ps.URL, err)
		}
		if proxy.Scheme == "" {
			proxy.Scheme = "http"
		}
		return proxy, nil
	}
}
