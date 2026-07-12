package marketplace

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
	"everevo/internal/httpclient"
)

// Default market source URL (GitHub Raw).
const MarketURL = "https://raw.githubusercontent.com/EverEvo-marketplace/skills/main/marketplace.json"

// MarketCache is the on-disk cache format.
type MarketCache struct {
	UpdatedAt string         `json:"updatedAt"`
	Packages  []SkillPackage `json:"packages"`
}

func cachePath() string {
	dir := storage.DataDir()
	return filepath.Join(dir, "marketplace_cache.json")
}

// LoadMarketCache reads the cached marketplace from disk.
func LoadMarketCache() ([]SkillPackage, time.Time) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, time.Time{}
	}
	var cache MarketCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, time.Time{}
	}
	t, err := time.Parse(time.RFC3339, cache.UpdatedAt)
	if err != nil {
		return cache.Packages, time.Time{}
	}
	return cache.Packages, t
}

// SaveMarketCache persists marketplace data to disk.
func SaveMarketCache(pkgs []SkillPackage) error {
	cache := MarketCache{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Packages:  pkgs,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(cachePath())
	os.MkdirAll(dir, 0755)
	return atomic.WriteFile(cachePath(), data, 0644)
}

// FetchMarket downloads the marketplace JSON from the remote source.
// Falls back to cached data if online fetch fails. Returns nil if nothing available.
func FetchMarket() ([]SkillPackage, error) {
	pkgs, err := fetchFromURL(MarketURL)
	if err != nil {
		log.Printf("[marketplace] fetch from remote failed: %v", err)
		// Try cache — this is real data from a previous successful fetch
		cached, t := LoadMarketCache()
		if cached != nil {
			log.Printf("[marketplace] using cached data from %s", t.Format(time.RFC3339))
			return cached, nil
		}
		// No real data available
		log.Printf("[marketplace] no market data available")
		return nil, nil
	}

	// Save to cache
	if err := SaveMarketCache(pkgs); err != nil {
		log.Printf("[marketplace] save cache: %v", err)
	}
	return pkgs, nil
}

// RefreshMarket forces a re-fetch from the remote source.
func RefreshMarket() ([]SkillPackage, error) {
	pkgs, err := fetchFromURL(MarketURL)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if err := SaveMarketCache(pkgs); err != nil {
		return pkgs, err
	}
	return pkgs, nil
}

// fetchFromURL downloads and parses the marketplace JSON.
func fetchFromURL(url string) ([]SkillPackage, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("only https URLs allowed")
	}

	client := httpclient.New(10 * time.Second)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB max
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	// Try array format first, then cache format
	var pkgs []SkillPackage
	if err := json.Unmarshal(body, &pkgs); err != nil {
		// Try MarketCache format
		var cache MarketCache
		if err2 := json.Unmarshal(body, &cache); err2 != nil {
			return nil, fmt.Errorf("parse: %w / %w", err, err2)
		}
		pkgs = cache.Packages
	}

	return pkgs, nil
}
