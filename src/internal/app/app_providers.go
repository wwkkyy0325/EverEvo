//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"everevo/internal/config"
)

// ─── LLM Provider CRUD ─────────────────────────────────────────

func (a *App) GetConfig() *config.Config { return a.cfg }

func (a *App) ListPresets() []config.Preset { return config.Presets() }

func (a *App) ListProviders() []config.LLMProvider {
	if a.cfg == nil { return []config.LLMProvider{} }
	return a.cfg.LLM.Providers
}

func (a *App) GetActiveProvider() *config.LLMProvider {
	if a.cfg == nil { return nil }
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == a.cfg.LLM.ActiveProvider && a.cfg.LLM.Providers[i].Enabled {
			return &a.cfg.LLM.Providers[i]
		}
	}
	return nil
}

func (a *App) CreateProvider(p config.LLMProvider) error {
	p.ID = uuid.New().String()
	p.CreatedAt = time.Now().UnixMilli()
	if p.Models == nil { p.Models = []string{} }
	a.cfg.LLM.Providers = append(a.cfg.LLM.Providers, p)
	// Auto-set as active if first provider
	if a.cfg.LLM.ActiveProvider == "" {
		a.cfg.LLM.ActiveProvider = p.ID
	}
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	a.emitChanged("providers:changed", "update", p.ID)
	return nil
}

func (a *App) UpdateProvider(id string, p config.LLMProvider) error {
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == id {
			p.ID = id
			p.CreatedAt = a.cfg.LLM.Providers[i].CreatedAt
			if p.Models == nil { p.Models = []string{} }
			a.cfg.LLM.Providers[i] = p
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			a.emitChanged("providers:changed", "update", id)
			return nil
		}
	}
	return fmt.Errorf("供应商 %s 不存在", id)
}

func (a *App) DeleteProvider(id string) error {
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == id {
			a.cfg.LLM.Providers = append(a.cfg.LLM.Providers[:i], a.cfg.LLM.Providers[i+1:]...)
			if a.cfg.LLM.ActiveProvider == id {
				a.cfg.LLM.ActiveProvider = ""
				if len(a.cfg.LLM.Providers) > 0 {
					a.cfg.LLM.ActiveProvider = a.cfg.LLM.Providers[0].ID
				}
			}
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			a.emitChanged("providers:changed", "update", id)
			return nil
		}
	}
	return fmt.Errorf("供应商 %s 不存在", id)
}

func (a *App) SetActiveProvider(id string) error {
	a.cfg.LLM.ActiveProvider = id
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	a.emitChanged("providers:changed", "update", id)
	return nil
}

// SetExtractionProvider sets the provider used for memory fact/graph extraction
// (empty → fall back to the active provider).
func (a *App) SetExtractionProvider(id string) error {
	a.cfg.LLM.ExtractionProvider = id
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	return nil
}

// TestProviderConnection tries a simple API call to verify the provider config.
func (a *App) TestProviderConnection(id string) (string, error) {
	for _, p := range a.cfg.LLM.Providers {
		if p.ID == id {
			base := strings.TrimRight(p.Endpoint, "/")
			var url, reqBody, authHeader, authValue string

			switch p.APIFormat {
			case "anthropic":
				url = base + "/messages"
				reqBody = fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":5}`, p.Model)
				authHeader = "x-api-key"
				authValue = p.APIKey
			default: // openai, openai-compat
				url = base + "/chat/completions"
				reqBody = fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":5}`, p.Model)
				authHeader = "Authorization"
				authValue = "Bearer " + p.APIKey
			}

			code, err := httpPost(url, authHeader, authValue, reqBody)
			if err != nil {
				return "", fmt.Errorf("连接失败: %w", err)
			}
			return fmt.Sprintf("HTTP %d — 连接成功", code), nil
		}
	}
	return "", fmt.Errorf("供应商 %s 不存在", id)
}

// httpPost does a minimal HTTP POST and returns the status code.
// Returns an error for network failures or non-2xx responses.
func httpPost(url, authHeader, authValue, body string) (int, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authHeader, authValue)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a bit of the response body for error context
		buf := make([]byte, 256)
		n, _ := resp.Body.Read(buf)
		detail := strings.TrimSpace(string(buf[:n]))
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}
	return resp.StatusCode, nil
}

// ─── DeepSeek-specific integration ─────────────────────────────

// DeepSeekModel represents a model entry from the DeepSeek API.
type DeepSeekModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// FetchDeepSeekModels fetches available models from the DeepSeek API.
func (a *App) FetchDeepSeekModels(apiKey string) ([]DeepSeekModel, error) {
	url := "https://api.deepseek.com/models"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 DeepSeek API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DeepSeek 返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data []DeepSeekModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	return result.Data, nil
}

// BalanceInfo holds balance data from DeepSeek's /user/balance endpoint.
type BalanceInfo struct {
	IsAvailable   bool            `json:"isAvailable"`
	BalanceInfos  []BalanceEntry  `json:"balanceInfos"`
}

// BalanceEntry is a single currency balance line.
type BalanceEntry struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"totalBalance"`
	GrantedBalance  string `json:"grantedBalance"`
	ToppedUpBalance string `json:"toppedUpBalance"`
}

// QueryBalance queries the DeepSeek account balance.
func (a *App) QueryBalance(providerID string) (*BalanceInfo, error) {
	var prov *config.LLMProvider
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == providerID {
			prov = &a.cfg.LLM.Providers[i]
			break
		}
	}
	if prov == nil {
		return nil, fmt.Errorf("供应商 %s 不存在", providerID)
	}

	url := "https://api.deepseek.com/user/balance"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+prov.APIKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询余额失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("查询余额返回 HTTP %d", resp.StatusCode)
	}

	// Raw response has snake_case keys
	var raw struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析余额数据失败: %w", err)
	}

	info := &BalanceInfo{
		IsAvailable: raw.IsAvailable,
	}
	for _, b := range raw.BalanceInfos {
		info.BalanceInfos = append(info.BalanceInfos, BalanceEntry{
			Currency:        b.Currency,
			TotalBalance:    b.TotalBalance,
			GrantedBalance:  b.GrantedBalance,
			ToppedUpBalance: b.ToppedUpBalance,
		})
	}
	return info, nil
}
// ─── 本地模型能力检测 ─────────────────────────────────────────────

// FetchOllamaModels 从 Ollama 服务拉取已安装的模型列表。
func (a *App) FetchOllamaModels(endpoint string) ([]map[string]any, error) {
	base := strings.TrimRight(endpoint, "/")
	// endpoint is like http://127.0.0.1:11434/v1 — strip /v1 to get base
	base = strings.TrimSuffix(base, "/v1")
	url := base + "/api/tags"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Ollama (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Ollama 返回 %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
			Digest     string `json:"digest"`
			Details    struct {
				Format            string `json:"format"`
				Family            string `json:"family"`
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Ollama 响应失败: %w", err)
	}

	var models []map[string]any
	for _, m := range result.Models {
		info := map[string]any{
			"name":       m.Name,
			"size":       m.Size,
			"modifiedAt": m.ModifiedAt,
			"family":     m.Details.Family,
			"params":     m.Details.ParameterSize,
			"quant":      m.Details.QuantizationLevel,
		}
		models = append(models, info)
	}
	return models, nil
}

// FetchOpenAIModels fetches available models from an OpenAI-compatible /v1/models endpoint.
// Used for llama.cpp and other local servers. Also probes real capabilities for each model.
func (a *App) FetchOpenAIModels(endpoint string, apiKey string) ([]map[string]any, error) {
	base := strings.TrimRight(endpoint, "/")
	url := base + "/models"

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("返回 %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	var models []map[string]any
	for _, m := range result.Data {
		info := map[string]any{
			"name":    m.ID,
			"ownedBy": m.OwnedBy,
		}
		models = append(models, info)
	}
	return models, nil
}

// DetectModelCapability probes the model API to detect real capabilities.
func (a *App) DetectModelCapability(providerID string, modelName string) config.ModelCapability {
	if providerID != "" {
		for i := range a.cfg.LLM.Providers {
			if a.cfg.LLM.Providers[i].ID == providerID {
				prov := &a.cfg.LLM.Providers[i]
				endpoint := strings.TrimRight(prov.Endpoint, "/")
				cap := config.ProbeModelCapability(endpoint, prov.APIKey, modelName, prov.APIFormat)
				if cap != nil {
					log.Printf("[cap] %s via API: vision=%v tools=%v stream=%v reason=%v ctx=%d",
						modelName, cap.SupportsVision, cap.SupportsTools, cap.SupportsStreaming, cap.SupportsReasoning, cap.MaxContextTokens)
					return *cap
				}
				break
			}
		}
	}
	return config.UnknownCapability()
}

// ProbeModelCap always performs a real API probe regardless of cached data.
// Takes raw endpoint/apiKey/model/apiFormat — works without a saved provider.
func (a *App) ProbeModelCap(endpoint, apiKey, model, apiFormat string) config.ModelCapability {
	endpoint = strings.TrimRight(endpoint, "/")
	cap := config.ProbeModelCapability(endpoint, apiKey, model, apiFormat)
	if cap != nil {
		log.Printf("[cap] %s probed: vision=%v tools=%v stream=%v reason=%v ctx=%d",
			model, cap.SupportsVision, cap.SupportsTools, cap.SupportsStreaming, cap.SupportsReasoning, cap.MaxContextTokens)
		return *cap
	}
	return config.UnknownCapability()
}

// ProbeAllModels probes every model in the provider's model list via real API calls.
func (a *App) ProbeAllModels(providerID string) map[string]config.ModelCapability {
	result := make(map[string]config.ModelCapability)
	if providerID == "" {
		return result
	}
	var prov *config.LLMProvider
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == providerID {
			prov = &a.cfg.LLM.Providers[i]
			break
		}
	}
	if prov == nil {
		return result
	}
	endpoint := strings.TrimRight(prov.Endpoint, "/")
	for _, m := range prov.Models {
		cap := config.ProbeModelCapability(endpoint, prov.APIKey, m, prov.APIFormat)
		if cap != nil {
			result[m] = *cap
		} else {
			result[m] = config.UnknownCapability()
		}
	}
	return result
}

// ModelRegistryEntry is a single entry in the model registry returned to the
// frontend. It merges the preset profile with any probed capabilities.
type ModelRegistryEntry struct {
	config.ModelProfile
	ProviderID   string                `json:"providerId"`
	ProviderName string                `json:"providerName"`
	ModelName    string                `json:"modelName"`
	Capabilities config.ModelCapability `json:"capabilities"`
}

// GetModelRegistry returns the unified model registry: every model across all
// providers, with preset profiles merged with probed capabilities. The frontend
// uses this as its single source of truth for context management.
func (a *App) GetModelRegistry() []ModelRegistryEntry {
	if a.cfg == nil {
		return nil
	}
	var entries []ModelRegistryEntry
	for _, p := range a.cfg.LLM.Providers {
		if !p.Enabled {
			continue
		}
		// If provider has an explicit model list, register each
		if len(p.Models) > 0 {
			for _, modelName := range p.Models {
				profile := config.LookupModelProfile(p.Name, modelName)
				cap := p.ModelCapabilities[modelName]
				profile = config.MergeWithCapability(profile, cap)
				entries = append(entries, ModelRegistryEntry{
					ModelProfile: profile,
					ProviderID:   p.ID,
					ProviderName: p.Name,
					ModelName:    modelName,
					Capabilities: cap,
				})
			}
		} else if p.Model != "" {
			// Single-model provider (no model list)
			profile := config.LookupModelProfile(p.Name, p.Model)
			cap := p.ModelCapabilities[p.Model]
			profile = config.MergeWithCapability(profile, cap)
			entries = append(entries, ModelRegistryEntry{
				ModelProfile: profile,
				ProviderID:   p.ID,
				ProviderName: p.Name,
				ModelName:    p.Model,
				Capabilities: cap,
			})
		}
	}
	return entries
}

// FindBestModelForTask selects the best provider for a task type.
// Returns nil if no suitable provider is found.
// Task types: "vision", "tools", "reasoning", "extraction", "chat".
func (a *App) FindBestModelForTask(task string) *config.LLMProvider {
	if a.cfg == nil {
		return nil
	}
	return config.FindBestModel(a.cfg.LLM.Providers, task)
}
