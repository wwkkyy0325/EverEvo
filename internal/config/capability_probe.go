package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"everevo/internal/httpclient"
	"sync"
	"time"
)

// ProbeModelCapability sends real API requests to detect what a model can actually do.
// Returns nil if probing failed entirely (network error, auth error, etc.).
// Uses context for cancellation — caller can abort mid-probe.
func ProbeModelCapability(endpoint, apiKey, model, apiFormat string) *ModelCapability {
	ctx := context.Background()
	return ProbeModelCapabilityCtx(ctx, endpoint, apiKey, model, apiFormat)
}

// ProbeModelCapabilityCtx is the cancellable version.
func ProbeModelCapabilityCtx(ctx context.Context, endpoint, apiKey, model, apiFormat string) *ModelCapability {
	if apiFormat == "anthropic" {
		return probeAnthropicAPI(ctx, endpoint, apiKey, model)
	}
	return probeOpenAIAPI(ctx, endpoint, apiKey, model)
}

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func probeOpenAIAPI(ctx context.Context, endpoint, apiKey, model string) *ModelCapability {
	cap := ModelCapability{MaxContextTokens: 0}
	base := strings.TrimRight(endpoint, "/")

	// Reduced timeouts: 15s cloud, 30s local (was 30s/120s)
	timeout := 15 * time.Second
	if isLocalEndpoint(endpoint) {
		timeout = 30 * time.Second
	}
	client := httpclient.New(timeout)

	log.Printf("[cap] probing %s at %s (timeout=%v) …", model, base, timeout)

	// Quick pre-check: verify API key + model name (blocking, must pass first)
	preBody := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1,"stream":false}`, model)
	resp := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, preBody)
	if resp == nil {
		log.Printf("[cap] %s FAIL: cannot reach endpoint", model)
		return nil
	}
	resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		log.Printf("[cap] %s FAIL: auth error HTTP %d", model, resp.StatusCode)
		return nil
	}
	if resp.StatusCode == 404 {
		log.Printf("[cap] %s FAIL: model not found HTTP %d", model, resp.StatusCode)
		return nil
	}

	// Parallel probes: tools, vision, streaming, json, fim run concurrently
	var wg sync.WaitGroup
	wg.Add(5)

	// Probe tools — try without tool_choice first (most compatible).
	// tool_choice:"required" causes 400 on many non-OpenAI backends (DeepSeek, etc.).
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
		}
		body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"system","content":"You are a function-calling assistant. You MUST call the test_probe function now. Do not output any text — ONLY call the function."},{"role":"user","content":"call test_probe now"}],"tools":[{"type":"function","function":{"name":"test_probe","description":"test probe","parameters":{"type":"object","properties":{}}}}],"max_tokens":50,"stream":false}`, model)
		r := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, body)
		if r == nil {
			log.Printf("[cap] %s tools probe: request failed", model)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Printf("[cap] %s tools probe: HTTP %d", model, r.StatusCode)
			return
		}
		var result map[string]any
		if json.NewDecoder(r.Body).Decode(&result) == nil {
			if choices, ok := result["choices"].([]any); ok && len(choices) > 0 {
				msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
				if msg != nil {
					if tc, ok := msg["tool_calls"]; ok && tc != nil {
						if arr, ok := tc.([]any); ok && len(arr) > 0 {
							cap.SupportsTools = true
						}
					}
					if rc, ok := msg["reasoning_content"]; ok && rc != nil && rc != "" {
						cap.SupportsReasoning = true
					}
				}
			}
		}
		// Fallback: try with tool_choice if first attempt didn't yield tool calls
		if !cap.SupportsTools {
			body2 := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"call test_probe"}],"tools":[{"type":"function","function":{"name":"test_probe","description":"test","parameters":{"type":"object","properties":{}}}}],"tool_choice":"required","max_tokens":50,"stream":false}`, model)
			r2 := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, body2)
			if r2 != nil {
				defer r2.Body.Close()
				if r2.StatusCode == 200 {
					var result2 map[string]any
					if json.NewDecoder(r2.Body).Decode(&result2) == nil {
						if choices, ok := result2["choices"].([]any); ok && len(choices) > 0 {
							msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
							if msg != nil {
								if tc, ok := msg["tool_calls"]; ok && tc != nil {
									if arr, ok := tc.([]any); ok && len(arr) > 0 {
										cap.SupportsTools = true
									}
								}
							}
						}
					}
				} else {
					log.Printf("[cap] %s tools fallback (tool_choice=required): HTTP %d", model, r2.StatusCode)
				}
			}
		}
	}()

	// Probe vision
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
		}
		body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":[{"type":"text","text":"What is in this image? Reply with exactly one word."},{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]}],"max_tokens":10,"stream":false}`, model, tinyPNG)
		r := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, body)
		if r == nil {
			log.Printf("[cap] %s vision probe: request failed", model)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Printf("[cap] %s vision probe: HTTP %d", model, r.StatusCode)
			return
		}
		cap.SupportsVision = true
	}()

	// Probe streaming
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
		}
		body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":5,"stream":true}`, model)
		r := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, body)
		if r == nil {
			log.Printf("[cap] %s stream probe: request failed", model)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Printf("[cap] %s stream probe: HTTP %d", model, r.StatusCode)
			return
		}
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		if n > 0 && strings.HasPrefix(strings.TrimSpace(string(buf[:n])), "data:") {
			cap.SupportsStreaming = true
		}
	}()

	// Probe JSON mode (response_format json_object)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
		}
		body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"Return JSON: {\"ok\":true}"}],"response_format":{"type":"json_object"},"max_tokens":20,"stream":false}`, model)
		r := doProbeRequestCtx(ctx, client, base, "/chat/completions", apiKey, body)
		if r == nil {
			log.Printf("[cap] %s json probe: request failed", model)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Printf("[cap] %s json probe: HTTP %d", model, r.StatusCode)
			return
		}
		cap.SupportsJSON = true
	}()

	// Probe FIM (Fill-in-the-Middle via /beta/completions)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
		}
		body := fmt.Sprintf(`{"model":"%s","prompt":"function add(a,b){","suffix":"}","max_tokens":10,"stream":false}`, model)
		r := doProbeRequestCtx(ctx, client, base, "/beta/completions", apiKey, body)
		if r == nil {
			log.Printf("[cap] %s fim probe: request failed", model)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Printf("[cap] %s fim probe: HTTP %d", model, r.StatusCode)
			return
		}
		cap.SupportsFIM = true
	}()

	wg.Wait()

	// Context window probe (fast, sequential is fine).
	// Only return what the API actually reports — do NOT merge with
	// knownContextDefault here. That fallback lives in LookupModelProfile
	// and is used by the context management layer. Merging here would
	// overwrite a user's manually-configured context (e.g. 24K for a
	// model whose family default is 128K).
	if ctx.Err() == nil {
		if c := probeContextOpenAI(client, base, apiKey, model); c > 0 {
			cap.MaxContextTokens = c
		}
	}

	log.Printf("[cap] %s done: stream=%v tools=%v vision=%v reason=%v json=%v fim=%v ctx=%d",
		model, cap.SupportsStreaming, cap.SupportsTools, cap.SupportsVision, cap.SupportsReasoning, cap.SupportsJSON, cap.SupportsFIM, cap.MaxContextTokens)
	return &cap
}

func doProbeRequestCtx(ctx context.Context, client *http.Client, base, path, apiKey, body string) *http.Response {
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, strings.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	return resp
}

func doProbeRequest(client *http.Client, base, path, apiKey, body string) *http.Response {
	return doProbeRequestCtx(context.Background(), client, base, path, apiKey, body)
}

func probeContextOpenAI(client *http.Client, base, apiKey, model string) int {
	// Strategy 1: GET /models/{model} (works on OpenAI, fails on DeepSeek & most compat APIs)
	if c := tryGetModelInfo(client, base, apiKey, model); c > 0 {
		return c
	}

	// Strategy 2: GET /models (list all) — find model in the list and read its metadata
	if c := tryListModelsForContext(client, base, apiKey, model); c > 0 {
		return c
	}

	// Strategy 3: known defaults based on model name prefix / family
	if c := knownContextDefault(model); c > 0 {
		log.Printf("[cap] %s context: using known default %d", model, c)
		return c
	}

	return 0
}

// tryGetModelInfo queries GET /models/{model} for context window info.
func tryGetModelInfo(client *http.Client, base, apiKey, model string) int {
	url := strings.TrimRight(base, "/v1") + "/models/" + model
	req, _ := http.NewRequest("GET", url, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}
	return parseModelInfo(resp)
}

// tryListModelsForContext queries GET /models and searches for the model.
func tryListModelsForContext(client *http.Client, base, apiKey, model string) int {
	url := strings.TrimRight(base, "/v1") + "/models"
	req, _ := http.NewRequest("GET", url, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}

	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return 0
	}

	for _, m := range list.Data {
		id, _ := m["id"].(string)
		if id == model {
			// Found the model entry — check its metadata
			if ctx, ok := m["context_window"].(float64); ok {
				return int(ctx)
			}
			if ctx, ok := m["max_input_tokens"].(float64); ok {
				return int(ctx)
			}
			if meta, ok := m["meta"].(map[string]any); ok {
				if ctx, ok := meta["context_window"].(float64); ok {
					return int(ctx)
				}
			}
			break
		}
		// Also check partial match (e.g. "deepseek-chat" matches "deepseek-chat-0125")
		if strings.Contains(id, model) || strings.Contains(model, id) {
			if ctx, ok := m["context_window"].(float64); ok {
				return int(ctx)
			}
			if ctx, ok := m["max_input_tokens"].(float64); ok {
				return int(ctx)
			}
		}
	}
	return 0
}

// parseModelInfo extracts context window from a single model info response body.
func parseModelInfo(resp *http.Response) int {
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return 0
	}
	if ctx, ok := info["context_window"].(float64); ok {
		return int(ctx)
	}
	if ctx, ok := info["max_input_tokens"].(float64); ok {
		return int(ctx)
	}
	if meta, ok := info["meta"].(map[string]any); ok {
		if ctx, ok := meta["context_window"].(float64); ok {
			return int(ctx)
		}
	}
	return 0
}

// knownContextDefault returns a plausible context window for well-known model families.
// These are conservative defaults; the API probe is always preferred when available.
func knownContextDefault(model string) int {
	lower := strings.ToLower(model)

	// OpenAI family
	if strings.Contains(lower, "gpt-4o") || strings.Contains(lower, "gpt-4.5") {
		return 128_000
	}
	if strings.Contains(lower, "gpt-4-turbo") || strings.Contains(lower, "gpt-4-0125") || strings.Contains(lower, "gpt-4-1106") {
		return 128_000
	}
	if strings.Contains(lower, "gpt-4") {
		return 8_192
	}
	if strings.Contains(lower, "gpt-3.5-turbo-16k") {
		return 16_384
	}
	if strings.Contains(lower, "gpt-3.5") {
		return 4_096
	}
	if strings.Contains(lower, "o1") || strings.Contains(lower, "o3") || strings.Contains(lower, "o4") {
		return 200_000
	}

	// Anthropic family
	if strings.Contains(lower, "claude-3.5") || strings.Contains(lower, "claude-4") || strings.Contains(lower, "claude-opus") {
		return 200_000
	}
	if strings.Contains(lower, "claude-3") {
		return 200_000
	}
	if strings.Contains(lower, "claude") {
		return 200_000
	}

	// DeepSeek family
	if strings.Contains(lower, "deepseek-r1") || strings.Contains(lower, "deepseek-v3") || strings.Contains(lower, "deepseek-v4") || strings.Contains(lower, "deepseek-chat") {
		return 1_000_000 // DeepSeek models typically support 1M tokens
	}
	if strings.Contains(lower, "deepseek") {
		return 128_000
	}

	// Qwen / Tongyi family
	if strings.Contains(lower, "qwen") || strings.Contains(lower, "tongyi") {
		if strings.Contains(lower, "max") || strings.Contains(lower, "plus") {
			return 1_000_000
		}
		return 128_000
	}

	// GLM family
	if strings.Contains(lower, "glm-4") || strings.Contains(lower, "glm4") {
		return 128_000
	}
	if strings.Contains(lower, "glm") {
		return 32_000
	}

	// Moonshot / Kimi
	if strings.Contains(lower, "moonshot") || strings.Contains(lower, "kimi") {
		return 128_000
	}

	// Mistral
	if strings.Contains(lower, "mistral-large") || strings.Contains(lower, "mistral-medium") {
		return 128_000
	}
	if strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral") {
		return 32_000
	}

	// Llama (local)
	if strings.Contains(lower, "llama-3") || strings.Contains(lower, "llama-4") {
		return 128_000
	}
	if strings.Contains(lower, "llama") {
		return 8_192
	}

	// Gemini
	if strings.Contains(lower, "gemini-2") || strings.Contains(lower, "gemini-1.5") {
		return 1_000_000
	}
	if strings.Contains(lower, "gemini") {
		return 32_000
	}

	// Grok
	if strings.Contains(lower, "grok") {
		return 128_000
	}

	return 0
}

func isLocalEndpoint(endpoint string) bool {
	return strings.Contains(endpoint, "127.0.0.1") ||
		strings.Contains(endpoint, "localhost") ||
		strings.Contains(endpoint, "0.0.0.0")
}

func probeAnthropicAPI(ctx context.Context, endpoint, apiKey, model string) *ModelCapability {
	cap := ModelCapability{SupportsStreaming: true, MaxContextTokens: 200000}
	client := httpclient.New(10 * time.Second)
	base := strings.TrimRight(endpoint, "/")

	body := fmt.Sprintf(`{"model":"%s","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, model)
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	cap.SupportsVision = true
	cap.SupportsTools = true
	return &cap
}
