package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// ModelCapability describes what a specific model can do.
type ModelCapability struct {
	SupportsVision    bool `json:"supportsVision"`
	SupportsTools     bool `json:"supportsTools"`
	SupportsStreaming bool `json:"supportsStreaming"`
	SupportsReasoning bool `json:"supportsReasoning"`
	SupportsJSON      bool `json:"supportsJSON"`  // native JSON mode (response_format json_object)
	SupportsFIM       bool `json:"supportsFIM"`   // Fill-in-the-Middle completion (DeepSeek /beta/completions)
	MaxContextTokens  int  `json:"maxContextTokens"`
}

// LLMProvider represents a single LLM API provider configuration.
type LLMProvider struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Icon              string                     `json:"icon,omitempty"`
	Notes             string                     `json:"notes,omitempty"`
	Type              string                     `json:"type"`      // preset key or "custom"
	APIFormat         string                     `json:"apiFormat"` // "openai" | "anthropic" | "openai-compat"
	Endpoint          string                     `json:"endpoint"`
	APIKey            string                     `json:"apiKey"`
	Model             string                     `json:"model"`
	Models            []string                   `json:"models,omitempty"`            // available model names
	ModelCapabilities map[string]ModelCapability `json:"modelCapabilities,omitempty"` // per-model capability info
	EnableJSONOutput  bool                       `json:"enableJSONOutput"`            // user toggle: force JSON structured output
	Website           string                     `json:"website,omitempty"`
	Enabled           bool                       `json:"enabled"`
	CreatedAt         int64                      `json:"createdAt"`
}

// A2AAgentConfig holds the local agent identity and server settings.
type A2AAgentConfig struct {
	Enabled     bool   `json:"enabled"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Port        int    `json:"port"`
	Secret      string `json:"secret,omitempty"` // Feishu-style HMAC secret; when set, inbound task requests must be signed
}

// LLMConfig holds all LLM providers, the active one, and global MCP port.
type LLMConfig struct {
	Providers          []LLMProvider `json:"providers"`
	ActiveProvider     string        `json:"activeProvider"`
	ExtractionProvider string        `json:"extractionProvider"` // provider ID for memory fact/graph extraction; "" → active
	MCPPort            int           `json:"mcpPort"`             // global MCP server port
	HTTPProxy          string        `json:"httpProxy"`           // user-configured proxy URL (e.g. "http://127.0.0.1:7890")
	ProxyEnabled       *bool         `json:"proxyEnabled"`        // proxy kill-switch; nil → true (default on)
	A2AConfig          A2AAgentConfig `json:"a2aConfig"`
	FeishuConfig       FeishuConfig   `json:"feishuConfig"`
}

// FeishuConfig holds credentials for the Feishu self-built-app bot (WebSocket
// long-connection mode). Persisted as part of LLMConfig.
type FeishuConfig struct {
	Enabled           bool   `json:"enabled"`
	AppID             string `json:"appId"`
	AppSecret         string `json:"appSecret"`
	VerificationToken string `json:"verificationToken"`
}

// Config 用户配置。
type Config struct {
	Version        string            `json:"version"`
	ModelDirs      []string          `json:"model_dirs"`
	DefaultBackend string            `json:"default_backend"`
	Theme          string            `json:"theme"`
	Language       string            `json:"language"`
	Credentials    map[string]string `json:"credentials"`
	LLM            LLMConfig         `json:"llm"`
}

// Preset defines a built-in LLM provider preset.
type Preset struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	APIFormat string  `json:"apiFormat"`
	Endpoint string   `json:"endpoint"`
	Models   []string `json:"models"`
	Icon     string   `json:"icon,omitempty"`
}

// Presets returns all built-in provider presets.
func Presets() []Preset {
	return []Preset{
		{Name: "OpenAI", Type: "openai", APIFormat: "openai", Endpoint: "https://api.openai.com/v1", Icon: "◎"},
		{Name: "DeepSeek", Type: "deepseek", APIFormat: "openai", Endpoint: "https://api.deepseek.com", Icon: "◈"},
		{Name: "通义千问", Type: "qwen", APIFormat: "openai", Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", Icon: "◇"},
		{Name: "智谱 GLM", Type: "glm", APIFormat: "openai", Endpoint: "https://open.bigmodel.cn/api/paas/v4", Icon: "□"},
		{Name: "Moonshot", Type: "moonshot", APIFormat: "openai", Endpoint: "https://api.moonshot.cn/v1", Icon: "◉"},
			{Name: "Ollama 本地", Type: "ollama", APIFormat: "openai-compat", Endpoint: "http://127.0.0.1:11434/v1", Icon: "●"},
			{Name: "llama.cpp 本地", Type: "llamacpp", APIFormat: "openai-compat", Endpoint: "http://127.0.0.1:8080/v1", Icon: "◆"},
		}
	}

// UnknownCapability returns the default capability for an unprobed model.
// All capabilities are false; clients must call ProbeModelCapability to get real data.
func UnknownCapability() ModelCapability {
	return ModelCapability{MaxContextTokens: 0}
}

// Defaults 返回默认配置。
func Defaults() *Config {
	return &Config{
		Version:        "0.1.0",
		ModelDirs:      []string{},
		DefaultBackend: "auto",
		Theme:          "system",
		Language:       "zh-CN",
		Credentials:    map[string]string{},
		LLM: LLMConfig{
			Providers:      []LLMProvider{},
			ActiveProvider: "",
			A2AConfig: A2AAgentConfig{
				Name:    "EverEvo Agent",
				Version: "0.1.0",
				Port:    19801,
			},
			FeishuConfig: FeishuConfig{},
		},
	}
}

// Path 返回当前 zone 内的用户配置文件路径。
func Path() string {
	return filepath.Join(UserConfigDir(), "config.json")
}

// GlobalPath returns the path to the global (cross-zone) config file.
// This holds settings shared by all zones: theme, language, active zone.
func GlobalPath() string {
	root, err := storage.RootAppDataDir()
	if err != nil {
		root = filepath.Join(os.Getenv("APPDATA"), "EverEvo")
	}
	return filepath.Join(root, "global_config.json")
}

// UserConfigDir returns the current zone's config directory.
// Delegates to storage.AppDataDir() which respects EVEREVO_ZONE.
func UserConfigDir() string {
	dir, err := storage.AppDataDir()
	if err != nil {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "EverEvo", "zones", "production")
		}
		return ""
	}
	return dir
}

// Load reads the user config; if the file does not exist, returns defaults.
// On first launch after the zone system is introduced, the legacy
// %APPDATA%\EverEvo\user_config.json is automatically copied into the
// production zone.
func Load() (*Config, error) {
	cfgPath := Path()

	// Auto-migration: copy old root-level user_config.json into the zone.
	migrateLegacyConfig(cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	migrateOldLLM(cfg, data)

	if cfg.LLM.Providers == nil {
		cfg.LLM.Providers = []LLMProvider{}
	}

	return cfg, nil
}

// migrateLegacyConfig copies ALL data from the old %APPDATA%/EverEvo/ root
// into the production zone on first launch. This includes config, memory DB,
// wiki, knowledge, workflows, agents — not just config.json.
func migrateLegacyConfig(zoneCfgPath string) {
	root, err := storage.RootAppDataDir()
	if err != nil {
		return
	}

	zoneDir := filepath.Dir(zoneCfgPath)

	// Check if old root-level data exists at all.
	oldCfgPath := filepath.Join(root, "user_config.json")
	if _, err := os.Stat(oldCfgPath); os.IsNotExist(err) {
		return // nothing to migrate
	}

	// Already migrated — but check if old root has SUBSTANTIALLY more data
	// than the zone (e.g., only config was migrated before — bugfix for v1).
	if _, err := os.Stat(zoneCfgPath); err == nil {
		zoneMem := filepath.Join(zoneDir, "memory")
		oldMem := filepath.Join(root, "memory")
		zne := dirSizeBytes(zoneMem)
		old := dirSizeBytes(oldMem)
		// If old memory dir is > 10x larger than zone, the zone is essentially
		// empty and old data was never migrated. Force recovery.
		if old > 10*zne && old > 10*1024 {
			log.Printf("[migrate] 检测到旧数据(%d bytes)远大于新区(%d bytes)，开始恢复...", old, zne)
			// Backup the near-empty zone data first.
			backupZoneData(zoneDir)
			migrateAllData(root, zoneDir, zoneCfgPath)
		}
		return
	}

	// First migration — zone doesn't exist yet.
	log.Printf("[migrate] 检测到旧版数据，开始迁移: %s → %s", root, zoneDir)
	migrateAllData(root, zoneDir, zoneCfgPath)
}

func migrateAllData(root, zoneDir, zoneCfgPath string) {
	if err := os.MkdirAll(zoneDir, 0755); err != nil {
		log.Printf("[migrate] 创建 zone 目录失败: %v", err)
		return
	}

	migrateEntries := []string{
		"user_config.json",
		"agents.json",
		"memory",
		"wiki",
		"knowledge",
		"workflows",
		"download_history.json",
		"skills",
		"EverEvo.log",
	}

	for _, entry := range migrateEntries {
		oldPath := filepath.Join(root, entry)
		newPath := filepath.Join(zoneDir, entry)
		if entry == "user_config.json" {
			newPath = zoneCfgPath
		}

		if _, err := os.Stat(oldPath); os.IsNotExist(err) {
			continue
		}

		if err := moveOrCopyPath(oldPath, newPath); err != nil {
			log.Printf("[migrate] %s 迁移失败: %v", entry, err)
		} else {
			log.Printf("[migrate] %s ✓", entry)
		}
	}

	log.Printf("[migrate] 迁移完成 —— 请重启应用")
}

func isDirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

func dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// dirSizeBytes returns the total byte size of all files under a directory.
func dirSizeBytes(dir string) int64 {
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// backupZoneData moves the zone's existing data dirs to *.bak to make room
// for the old root data during forced recovery.
func backupZoneData(zoneDir string) {
	for _, d := range []string{"memory", "wiki", "knowledge", "workflows"} {
		src := filepath.Join(zoneDir, d)
		dst := src + ".bak-" + time.Now().Format("20060102-150405")
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err == nil {
				log.Printf("[migrate] 备份新区数据: %s", filepath.Base(dst))
			}
		}
	}
}

// moveOrCopyPath moves a file or directory from src to dst. Tries os.Rename
// (atomic, same-filesystem) first; falls back to copy+delete for cross-volume.
func moveOrCopyPath(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // destination already exists, don't overwrite
	}
	// Try atomic rename first (fast, same volume).
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-volume or permission issue — copy + delete.
	return copyAndDelete(src, dst)
}

// copyAndDelete recursively copies src to dst, then removes src.
func copyAndDelete(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyAndDelete(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.Remove(src)
	}
	// Single file.
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := atomic.WriteFile(dst, data, srcInfo.Mode()); err != nil {
		return err
	}
	return os.Remove(src)
}

// migrateOldLLM detects and migrates old LLM config format.
func migrateOldLLM(cfg *Config, raw []byte) {
	// Old format had "endpoint"/"apiKey"/"model"/"mcpPort" directly under "llm"
	var legacy struct {
		LLM struct {
			Endpoint string `json:"endpoint"`
			APIKey   string `json:"apiKey"`
			Model    string `json:"model"`
			MCPPort  int    `json:"mcpPort"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return
	}
	// If old fields exist and no providers, migrate
	if legacy.LLM.Endpoint != "" && len(cfg.LLM.Providers) == 0 {
		id := fmt.Sprintf("migrated-%d", time.Now().Unix())
		cfg.LLM.Providers = []LLMProvider{{
			ID:        id,
			Name:      "默认供应商",
			Type:      "custom",
			APIFormat: "openai",
			Endpoint:  legacy.LLM.Endpoint,
			APIKey:    legacy.LLM.APIKey,
			Model:     legacy.LLM.Model,
			Models:    []string{legacy.LLM.Model},
			Enabled:   true,
			CreatedAt: time.Now().UnixMilli(),
		}}
		cfg.LLM.MCPPort = legacy.LLM.MCPPort
		cfg.LLM.ActiveProvider = id
	}
}

// Save 保存用户配置，自动创建目录。
func Save(cfg *Config) error {
	cfgPath := Path()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return atomic.WriteFile(cfgPath, data, 0644)
}
