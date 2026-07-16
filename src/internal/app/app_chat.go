//go:build windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"sync/atomic"

	"everevo/internal/config"
	"everevo/internal/httpclient"
)

// ─── Shared Anthropic conversion ────────────────────────────────

// anthropicRequest holds the pre-built request components for Anthropic API.
type anthropicRequest struct {
	SystemContent string
	Messages      []map[string]any
	Tools         []map[string]any
}

// convertToAnthropicRequest converts OpenAI-format messages and tools to Anthropic format.
// Used by both ChatProxy (non-streaming) and runChatStream (streaming) to eliminate ~200 lines of duplication.
func convertToAnthropicRequest(messagesJSON, toolsJSON json.RawMessage, streaming bool) *anthropicRequest {
	// Convert tools
	type anthropicTool struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"input_schema"`
	}
	var rawTools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		} `json:"function"`
	}
	json.Unmarshal(toolsJSON, &rawTools)
	aTools := make([]map[string]any, len(rawTools))
	for i, t := range rawTools {
		aTools[i] = map[string]any{
			"name":         t.Function.Name,
			"description":  t.Function.Description,
			"input_schema": t.Function.Parameters,
		}
	}

	// Convert messages
	var msgsArr []map[string]any
	json.Unmarshal(messagesJSON, &msgsArr)

	var systemContent string
	var anthropicMsgs []map[string]any

	for i := 0; i < len(msgsArr); i++ {
		m := msgsArr[i]
		role, _ := m["role"].(string)
		switch role {
		case "system":
			if s, ok := m["content"].(string); ok {
				systemContent = s
			}
		case "tool":
			if streaming {
				// Streaming: one tool_result per message (simple)
				toolCallID, _ := m["tool_call_id"].(string)
				content, _ := m["content"].(string)
				anthropicMsgs = append(anthropicMsgs, map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": toolCallID, "content": content},
					},
				})
			} else {
				// Non-streaming: collect consecutive tool results into a single user message
				// (Anthropic requires all tool_results in one user message)
				var toolResults []map[string]any
				for j := i; j < len(msgsArr); j++ {
					rj, _ := msgsArr[j]["role"].(string)
					if rj != "tool" {
						break
					}
					toolCallID, _ := msgsArr[j]["tool_call_id"].(string)
					content, _ := msgsArr[j]["content"].(string)
					toolResults = append(toolResults, map[string]any{
						"type": "tool_result", "tool_use_id": toolCallID, "content": content,
					})
					i = j
				}
				anthropicMsgs = append(anthropicMsgs, map[string]any{
					"role": "user", "content": toolResults,
				})
			}
		case "assistant":
			if tcs, ok := m["tool_calls"]; ok {
				tcList, _ := tcs.([]any)
				var blocks []map[string]any
				for _, tc := range tcList {
					tcm, _ := tc.(map[string]any)
					id, _ := tcm["id"].(string)
					fn, _ := tcm["function"].(map[string]any)
					name, _ := fn["name"].(string)
					argsStr, _ := fn["arguments"].(string)
					var args map[string]any
					json.Unmarshal([]byte(argsStr), &args)
					if args == nil {
						args = map[string]any{}
					}
					blocks = append(blocks, map[string]any{
						"type": "tool_use", "id": id, "name": name, "input": args,
					})
				}
				anthropicMsgs = append(anthropicMsgs, map[string]any{
					"role": "assistant", "content": blocks,
				})
			} else {
				anthropicMsgs = append(anthropicMsgs, map[string]any{
					"role": "assistant", "content": m["content"],
				})
			}
		default:
			anthropicMsgs = append(anthropicMsgs, m)
		}
	}

	return &anthropicRequest{
		SystemContent: systemContent,
		Messages:      anthropicMsgs,
		Tools:         aTools,
	}
}

// resolveActiveProvider returns the active LLM provider or an error.
func (a *App) resolveActiveProvider() (*config.LLMProvider, error) {
	activeID := a.cfg.LLM.ActiveProvider
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == activeID && a.cfg.LLM.Providers[i].Enabled {
			return &a.cfg.LLM.Providers[i], nil
		}
	}
	return nil, fmt.Errorf("没有活动的供应商")
}

// resolveExtractionProvider returns the provider used for memory fact/graph
// extraction (LLMConfig.ExtractionProvider), falling back to the active provider
// when unset or unavailable. Lets extraction target a cheaper model than chat.
// When no explicit extraction provider is set, uses task routing to prefer
// local models (saves cloud credits).
func (a *App) resolveExtractionProvider() (*config.LLMProvider, error) {
	if id := a.cfg.LLM.ExtractionProvider; id != "" {
		for i := range a.cfg.LLM.Providers {
			if a.cfg.LLM.Providers[i].ID == id && a.cfg.LLM.Providers[i].Enabled {
				return &a.cfg.LLM.Providers[i], nil
			}
		}
	}
	// No explicit extraction provider — try task routing (prefer local models)
	if best := config.FindBestModel(a.cfg.LLM.Providers, "extraction"); best != nil {
		return best, nil
	}
	return a.resolveActiveProvider()
}

// ─── ChatProxy (non-streaming) ──────────────────────────────────

// ChatProxy proxies the LLM chat request through the Go backend to avoid
// CORS / fetch issues in the Wails WebView. Returns the normalized response.
func (a *App) ChatProxy(messagesJSON json.RawMessage, toolsJSON json.RawMessage) (map[string]any, error) {
	messagesJSON = a.injectTaskBoard(messagesJSON)
	messagesJSON = a.injectThinkLang(messagesJSON)
	if paradigmForceMode != "" {
		messagesJSON = a.injectParadigmForce(messagesJSON, paradigmForceMode)
	}
	p, err := a.resolveActiveProvider()
	if err != nil {
		log.Printf("[chat] ChatProxy called activeID=%s providers=%d", a.cfg.LLM.ActiveProvider, len(a.cfg.LLM.Providers))
		return nil, err
	}
	return a.chatCompletion(p, messagesJSON, toolsJSON, chatOpts{})
}

// chatOpts carries optional per-call overrides for the LLM request body
// (used by local-agent execution to apply temperature / maxTokens).
type chatOpts struct {
	Temperature *float64
	MaxTokens   int
	// ThinkBudget, when > 0, enables extended thinking with given token budget.
	// (Anthropic: budget_tokens; OpenAI: thinking.type enabled).
	ThinkEffort string
	// OnChunk, when set, receives each streamed text delta. Used by the workflow
	// engine to surface LLM progress to the editor without polling.
	OnChunk func(text string)
	// Ctx, when set, replaces the global chat ctx for body-cancellation so a
	// workflow node's stream stops when the engine is cancelled.
	Ctx context.Context
}

// chatCompletion sends a non-streaming chat request to an explicit provider,
// building the request body (OpenAI or Anthropic) and normalizing the response
// to OpenAI shape. Shared by ChatProxy (active provider, default opts) and the
// local-agent execution loop (provider/model override + custom opts).
// safeRawJSON validates that data is valid JSON before wrapping in json.RawMessage.
// json.RawMessage.MarshalJSON() causes a fatal crash on invalid input; this prevents that.
func safeRawJSON(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return json.RawMessage("null")
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		log.Printf("[chat] WARNING: invalid JSON passed as RawMessage: %v (first 200 chars: %.200q)", err, string(data))
		return json.RawMessage("null")
	}
	// Re-marshal to a fresh []byte — json.RawMessage.MarshalJSON() returns
	// the underlying slice directly. If the original was part of a larger
	// buffer (e.g. a shared HTTP body), it may contain trailing garbage
	// that reads as unescaped control characters. Round-tripping through
	// Marshal guarantees a clean, independent, compact JSON copy.
	clean, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(clean)
}

// mustMarshal marshals v to JSON. On error it logs and returns "{}" instead of
// crashing (json.RawMessage.MarshalJSON fires a fatal error on invalid input).
func mustMarshal(v any) []byte {
	bodyBytes, err := json.Marshal(v)
	if err != nil {
		log.Printf("[chat] json.Marshal failed (returning {}): %v", err)
		return []byte("{}")
	}
	return bodyBytes
}

// mustMarshalCtx is like mustMarshal but includes provider/model context in the
// error log so we can trace which LLM call produced the invalid JSON.
func mustMarshalCtx(v any, p *config.LLMProvider, apiFormat string) []byte {
	bodyBytes, err := json.Marshal(v)
	if err != nil {
		log.Printf("[chat] json.Marshal failed (provider=%s model=%s fmt=%s, returning {}): %v",
			p.Name, p.Model, apiFormat, err)
		return []byte("{}")
	}
	return bodyBytes
}

func (a *App) chatCompletion(p *config.LLMProvider, messagesJSON, toolsJSON json.RawMessage, opts chatOpts) (map[string]any, error) {
	base := strings.TrimRight(p.Endpoint, "/")

	// Anthropic requires max_tokens; default to 4096 when not specified.
	anthropicMax := opts.MaxTokens
	if anthropicMax <= 0 {
		anthropicMax = 4096
	}

	// Build request body
	var reqBody string
	if p.APIFormat == "anthropic" {
		ar := convertToAnthropicRequest(messagesJSON, toolsJSON, false)
		bodyMap := map[string]any{
			"model":      p.Model,
			"messages":   ar.Messages,
			"tools":      ar.Tools,
			"max_tokens": anthropicMax,
			"stream":     false,
		}
		if ar.SystemContent != "" {
			bodyMap["system"] = ar.SystemContent
		}
		if opts.Temperature != nil {
			bodyMap["temperature"] = *opts.Temperature
		}
		bodyBytes := mustMarshal(bodyMap)
		reqBody = string(bodyBytes)
	} else {
		// OpenAI — pass through with explicit stream: false
		var msgsArr []map[string]any
		json.Unmarshal(messagesJSON, &msgsArr)
		bodyMap := map[string]any{
			"model":    p.Model,
			"messages": msgsArr,
			"stream":   false,
		}
		// Only attach tools / tool_choice when tools are present — an empty
		// tools array with tool_choice:auto is rejected by some providers.
		toolsTrimmed := strings.TrimSpace(string(toolsJSON))
		if toolsTrimmed != "" && toolsTrimmed != "null" && toolsTrimmed != "[]" {
			bodyMap["tools"] = safeRawJSON(toolsJSON)
			bodyMap["tool_choice"] = "auto"
		}
		if opts.Temperature != nil {
			bodyMap["temperature"] = *opts.Temperature
		}
		if opts.MaxTokens > 0 {
			bodyMap["max_tokens"] = opts.MaxTokens
		}
		bodyBytes := mustMarshal(bodyMap)
		reqBody = string(bodyBytes)
	}

	// Build URL + headers
	var url, authHeader, authValue string
	switch p.APIFormat {
	case "anthropic":
		url = base + "/messages"
		authHeader = "x-api-key"
		authValue = p.APIKey
	default:
		url = base + "/chat/completions"
		authHeader = "Authorization"
		authValue = "Bearer " + p.APIKey
	}

	// Make the HTTP request
	log.Printf("[chat] chatCompletion POST url=%s fmt=%s model=%s bodyLen=%d", url, p.APIFormat, p.Model, len(reqBody))
	httpReq, err := http.NewRequestWithContext(a.chatCtx, "POST", url, strings.NewReader(reqBody))
	if err != nil {
		log.Printf("[chat] ChatProxy build request error: %v", err)
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(authHeader, authValue)
	if p.APIFormat == "anthropic" {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Use longer timeout for local endpoints (CPU inference on large prompts).
		timeout := 60 * time.Second
		if strings.Contains(p.Endpoint, "127.0.0.1") || strings.Contains(p.Endpoint, "localhost") {
			timeout = 300 * time.Second
		}
		client := httpclient.New(timeout)
	// Retry on transient network failures (proxy resets, connection drops)
	var resp *http.Response
	var doErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, doErr = client.Do(httpReq)
		if doErr == nil {
			break
		}
		if attempt < 2 && strings.Contains(doErr.Error(), "forcibly closed") {
			log.Printf("[chat] retry %d/3 after: %v", attempt+1, doErr)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			httpReq, _ = http.NewRequestWithContext(a.chatCtx, "POST", url, strings.NewReader(reqBody))
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set(authHeader, authValue)
			if p.APIFormat == "anthropic" {
				httpReq.Header.Set("anthropic-version", "2023-06-01")
			}
			continue
		}
		break
	}
	if doErr != nil {
		log.Printf("[chat] HTTP error: %v", doErr)
		return nil, fmt.Errorf("请求失败: %w", doErr)
	}
	log.Printf("[chat] HTTP %d url=%s", resp.StatusCode, url)
	defer resp.Body.Close()

	// Read full response (io.ReadAll avoids the manual buffer loop bug)
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[chat] read error: %v", err)
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	log.Printf("[chat] response bodyLen=%d", len(buf))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(buf)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, detail)
	}

	// Parse response
	var data map[string]any
	if err := json.Unmarshal(buf, &data); err != nil {
		preview := string(buf)
		if len(preview) > 300 { preview = preview[:300] }
		log.Printf("[chat] JSON parse error: %v body=%s", err, preview)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// Normalize Anthropic response to OpenAI shape
	if p.APIFormat == "anthropic" {
		return normalizeAnthropicResponse(data), nil
	}
	return data, nil
}

// normalizeAnthropicResponse converts an Anthropic Messages response to OpenAI shape.
func normalizeAnthropicResponse(data map[string]any) map[string]any {
	msg := map[string]any{"role": "assistant", "content": ""}
	var toolCalls []map[string]any
	if content, ok := data["content"].([]any); ok {
		for _, c := range content {
			cb, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := cb["type"].(string); t == "text" {
				txt, _ := cb["text"].(string)
				if s, _ := msg["content"].(string); s != "" {
					msg["content"] = s + "\n" + txt
				} else {
					msg["content"] = txt
				}
			} else if t == "tool_use" {
				inputJSON, _ := json.Marshal(cb["input"])
				toolCalls = append(toolCalls, map[string]any{
					"id":   cb["id"],
					"type": "function",
					"function": map[string]any{
						"name":      cb["name"],
						"arguments": string(inputJSON),
					},
				})
			}
		}
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return map[string]any{"choices": []any{map[string]any{"message": msg}}}
}

// ─── ChatStream (SSE streaming) ─────────────────────────────────

// ChatStream starts an SSE streaming chat request in a background goroutine.
// The method returns immediately. Events are emitted to the frontend:
//
//	chat-chunk-<streamID>  — {text: "..."} for each text delta
//	chat-done-<streamID>   — {choices: [{message: {content, tool_calls}}]} on completion
//	chat-err-<streamID>    — {error: "..."} on failure
//
// ChatStreamCancel cancels a running stream by its ID.
func (a *App) ChatStreamCancel(streamID string) {
	a.streamCancelMu.Lock()
	if cancel, ok := a.streamCancels[streamID]; ok {
		cancel()
		delete(a.streamCancels, streamID)
	}
	a.streamCancelMu.Unlock()
}

// The frontend must register EventsOn listeners BEFORE calling this method.
func (a *App) ChatStream(streamID string, messagesJSON json.RawMessage, toolsJSON json.RawMessage, thinkEffort string) {
	messagesJSON = a.injectTaskBoard(messagesJSON)
	messagesJSON = a.injectThinkLang(messagesJSON)
	if paradigmForceMode != "" {
		messagesJSON = a.injectParadigmForce(messagesJSON, paradigmForceMode)
	}
	go func() {
		// Recover from any panic during SSE parsing/provider resolution so the
		// frontend always gets a terminal event — otherwise a malformed chunk
		// (e.g. unexpected tool_calls shape from a small local model) crashes
		// this goroutine silently and the chat hangs forever.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[chat] stream=%s PANIC recovered: %v", streamID, r)
				wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": fmt.Sprintf("内部错误（解析中断）: %v", r)})
			}
		}()
		// Create a cancellable context for this stream.
		streamCtx, streamCancel := context.WithCancel(a.chatCtx)
		a.streamCancelMu.Lock()
		if a.streamCancels == nil { a.streamCancels = make(map[string]context.CancelFunc) }
		a.streamCancels[streamID] = streamCancel
		a.streamCancelMu.Unlock()
		defer func() {
			a.streamCancelMu.Lock()
			delete(a.streamCancels, streamID)
			a.streamCancelMu.Unlock()
			streamCancel()
		}()

		p, err := a.resolveActiveProvider()
		if err != nil {
			log.Printf("[chat] stream=%s NO ACTIVE PROVIDER", streamID)
			wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": err.Error()})
			return
		}
		result, err := a.runChatStream(streamID, messagesJSON, toolsJSON, p, chatOpts{ThinkEffort: thinkEffort, Ctx: streamCtx})
		if err != nil {
			if streamCtx.Err() != nil {
				wailsRuntime.EventsEmit(a.ctx, "chat-done-"+streamID, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": ""}}}, "cancelled": true})
				return
			}
			log.Printf("[chat] stream=%s ERROR: %v", streamID, err)
			wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "chat-done-"+streamID, result)
	}()
}

// ChatStreamAs is like ChatStream but targets a specific provider (with optional
// model override) and applies call options. Used when chatting under a local
// Agent persona that overrides the provider/model or sets temperature/maxTokens.
// Pass temperature < 0 to omit it (use provider default) and maxTokens <= 0 to omit.
func (a *App) ChatStreamAs(streamID string, messagesJSON json.RawMessage, toolsJSON json.RawMessage, providerId, model string, temperature float64, maxTokens int, thinkEffort string) {
	messagesJSON = a.injectTaskBoard(messagesJSON)
	messagesJSON = a.injectThinkLang(messagesJSON)
	if paradigmForceMode != "" {
		messagesJSON = a.injectParadigmForce(messagesJSON, paradigmForceMode)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[chat] stream=%s PANIC recovered: %v", streamID, r)
				wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": fmt.Sprintf("内部错误（解析中断）: %v", r)})
			}
		}()
		streamCtx, streamCancel := context.WithCancel(a.chatCtx)
		a.streamCancelMu.Lock()
		if a.streamCancels == nil { a.streamCancels = make(map[string]context.CancelFunc) }
		a.streamCancels[streamID] = streamCancel
		a.streamCancelMu.Unlock()
		defer func() {
			a.streamCancelMu.Lock()
			delete(a.streamCancels, streamID)
			a.streamCancelMu.Unlock()
			streamCancel()
		}()

		p, err := a.resolveProviderForStream(providerId, model)
		if err != nil {
			log.Printf("[chat] stream=%s provider resolve error: %v", streamID, err)
			wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": err.Error()})
			return
		}
		opts := chatOpts{MaxTokens: maxTokens, ThinkEffort: thinkEffort, Ctx: streamCtx}
		if temperature >= 0 {
			t := temperature
			opts.Temperature = &t
		}
		result, err := a.runChatStream(streamID, messagesJSON, toolsJSON, p, opts)
		if err != nil {
			if streamCtx.Err() != nil {
				wailsRuntime.EventsEmit(a.ctx, "chat-done-"+streamID, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": ""}}}, "cancelled": true})
				return
			}
			log.Printf("[chat] stream=%s ERROR: %v", streamID, err)
			wailsRuntime.EventsEmit(a.ctx, "chat-err-"+streamID, map[string]any{"error": err.Error()})
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "chat-done-"+streamID, result)
	}()
}

// resolveProviderForStream resolves a provider by ID (with optional model
// override) for ChatStreamAs. Falls back to the active provider when providerId
// is empty. Returns a copy when the model is overridden.
func (a *App) resolveProviderForStream(providerId, model string) (*config.LLMProvider, error) {
	if providerId == "" {
		ap, err := a.resolveActiveProvider()
		if err != nil {
			return nil, err
		}
		if model != "" {
			clone := *ap
			clone.Model = model
			return &clone, nil
		}
		return ap, nil
	}
	for i := range a.cfg.LLM.Providers {
		p := &a.cfg.LLM.Providers[i]
		if p.ID == providerId {
			if !p.Enabled {
				return nil, fmt.Errorf("供应商 %q 未启用", p.Name)
			}
			clone := *p
			if model != "" {
				clone.Model = model
			}
			return &clone, nil
		}
	}
	return nil, fmt.Errorf("供应商 %q 不存在", providerId)
}

// runChatStream does the actual SSE streaming work (runs in a goroutine).
func (a *App) runChatStream(streamID string, messagesJSON json.RawMessage, toolsJSON json.RawMessage, p *config.LLMProvider, opts chatOpts) (map[string]any, error) {
	// Track active streams so background extraction jobs don't saturate the same model.
	atomic.AddInt32(&a.activeStreams, 1)
	defer atomic.AddInt32(&a.activeStreams, -1)

	log.Printf("[chat] stream=%s provider=%s fmt=%s ep=%s model=%s", streamID, p.Name, p.APIFormat, p.Endpoint, p.Model)

	base := strings.TrimRight(p.Endpoint, "/")

	// Build request body — same as chatCompletion but stream:true
	anthropicMax := opts.MaxTokens
	if anthropicMax <= 0 {
		anthropicMax = 4096
	}
	var reqBody string
	if p.APIFormat == "anthropic" {
		ar := convertToAnthropicRequest(messagesJSON, toolsJSON, true)
		bodyMap := map[string]any{
			"model":      p.Model,
			"messages":   ar.Messages,
			"tools":      ar.Tools,
			"max_tokens": anthropicMax,
			"stream":     true,
		}
		if ar.SystemContent != "" {
			bodyMap["system"] = ar.SystemContent
		}
		if opts.Temperature != nil {
			bodyMap["temperature"] = *opts.Temperature
		}
		thinkApplyBody(bodyMap, p.APIFormat, opts.ThinkEffort)
		bodyBytes := mustMarshal(bodyMap)
		reqBody = string(bodyBytes)
	} else {
		var msgsArr []map[string]any
		json.Unmarshal(messagesJSON, &msgsArr)
		bodyMap := map[string]any{
			"model":    p.Model,
			"messages": msgsArr,
			"stream":   true,
		}
		toolsTrimmed := strings.TrimSpace(string(toolsJSON))
		if toolsTrimmed != "" && toolsTrimmed != "null" && toolsTrimmed != "[]" {
			bodyMap["tools"] = safeRawJSON(toolsJSON)
			bodyMap["tool_choice"] = "auto"
		}
		if opts.Temperature != nil {
			bodyMap["temperature"] = *opts.Temperature
		}
		if opts.MaxTokens > 0 {
			bodyMap["max_tokens"] = opts.MaxTokens
		}
		thinkApplyBody(bodyMap, p.APIFormat, opts.ThinkEffort)
		bodyBytes := mustMarshal(bodyMap)
		reqBody = string(bodyBytes)
	}

	var url, authHeader, authValue string
	if p.APIFormat == "anthropic" {
		url = base + "/messages"
		authHeader = "x-api-key"
		authValue = p.APIKey
	} else {
		url = base + "/chat/completions"
		authHeader = "Authorization"
		authValue = "Bearer " + p.APIKey
	}

	// SSE read loop — close body when the governing context is cancelled.
	// Workflow nodes pass their engine ctx (so cancel stops the stream); chat
	// passes the global chat ctx (shutdown only).
	streamCtx := opts.Ctx
	if streamCtx == nil {
		streamCtx = a.chatCtx
	}

	httpReq, err := http.NewRequestWithContext(streamCtx, "POST", url, strings.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(authHeader, authValue)
	if p.APIFormat == "anthropic" {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

		client := httpclient.New(300 * time.Second)
			log.Printf("[chat] stream=%s POST %s bodyLen=%d", streamID, url, len(reqBody))
	// Retry on transient network failures (proxy resets, connection drops)
	var resp *http.Response
	var doErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, doErr = client.Do(httpReq)
		if doErr == nil {
			break
		}
		if attempt < 2 && strings.Contains(doErr.Error(), "forcibly closed") {
			log.Printf("[chat] stream interrupted, retry %d/3 after: %v", attempt+1, doErr)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			httpReq, _ = http.NewRequestWithContext(streamCtx, "POST", url, strings.NewReader(reqBody))
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set(authHeader, authValue)
			if p.APIFormat == "anthropic" {
				httpReq.Header.Set("anthropic-version", "2023-06-01")
			}
			continue
		}
		break
	}
	if doErr != nil {
		log.Printf("[chat] stream=%s HTTP ERROR: %v", streamID, doErr)
		return nil, fmt.Errorf("请求失败: %w", doErr)
	}
	defer resp.Body.Close()

	log.Printf("[chat] stream=%s HTTP %d", streamID, resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		detail := string(buf)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		log.Printf("[chat] stream=%s API ERROR: %s", streamID, detail)
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, detail)
	}

	go func() {
		<-streamCtx.Done()
		resp.Body.Close()
	}()

	// Read SSE stream
	eventName := "chat-chunk-" + streamID
	reader := bufio.NewReader(resp.Body)
	result := map[string]any{"role": "assistant", "content": ""}
	var toolCalls []map[string]any
	var currentToolID, currentToolName, currentToolArgs string
	var lineCount int
	log.Printf("[chat] stream=%s SSE parsing started fmt=%s", streamID, p.APIFormat)
	if p.APIFormat == "anthropic" {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimSpace(line)
			lineCount++
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				continue // event type handled by next data line
			}
			if strings.HasPrefix(line, "data: ") {
				raw := strings.TrimPrefix(line, "data: ")
				var obj map[string]any
				if json.Unmarshal([]byte(raw), &obj) != nil {
					continue
				}
				t, _ := obj["type"].(string)
				switch t {
				case "content_block_start":
					if cb, ok := obj["content_block"].(map[string]any); ok {
						if cb["type"] == "tool_use" {
							currentToolID, _ = cb["id"].(string)
							currentToolName, _ = cb["name"].(string)
							currentToolArgs = ""
						}
					}
				case "content_block_delta":
					if delta, ok := obj["delta"].(map[string]any); ok {
						if delta["type"] == "text_delta" {
							text, _ := delta["text"].(string)
							if text != "" {
								wailsRuntime.EventsEmit(a.ctx, eventName, map[string]any{"text": text})
								if opts.OnChunk != nil {
									opts.OnChunk(text)
								}
								s, _ := result["content"].(string)
								result["content"] = s + text
							}
					} else if delta["type"] == "thinking_delta" {
						thinkText, _ := delta["thinking"].(string)
						if thinkText != "" {
							wailsRuntime.EventsEmit(a.ctx, eventName, map[string]any{"reasoning": thinkText})
						}
					} else if delta["type"] == "signature_delta" {
					} else if thinkSSEDelta(a.ctx, eventName, delta) {
						// thinking/signature delta
						} else if delta["type"] == "input_json_delta" {
							partial, _ := delta["partial_json"].(string)
							currentToolArgs += partial
						}
					}
				case "content_block_stop":
					if currentToolID != "" {
						toolCalls = append(toolCalls, map[string]any{
							"id": currentToolID, "type": "function",
							"function": map[string]any{
								"name": currentToolName, "arguments": currentToolArgs,
							},
						})
						currentToolID = ""
					}
				case "message_stop":
					// stream complete
				}
			}
		}
	} else {
		// OpenAI SSE format
		logFirst := 3 // diagnostic: log keys for first N chunks
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Printf("[chat] stream=%s SSE read error: %v", streamID, err)
				}
				break
			}
			line = strings.TrimSpace(line)
			lineCount++
			// Accept both "data: {...}" and "data:{...}" (llama.cpp may omit space)
			var raw string
			if strings.HasPrefix(line, "data: ") {
				raw = line[6:]
			} else if strings.HasPrefix(line, "data:") {
				raw = line[5:]
			} else {
				continue
			}
			if raw == "[DONE]" {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(raw), &obj) != nil {
				continue
			}
			choices, _ := obj["choices"].([]any)
			if len(choices) == 0 {
				continue
			}
			ch, _ := choices[0].(map[string]any)
			delta, _ := ch["delta"].(map[string]any)
			// Fallback: some servers return message instead of delta in stream
			if delta == nil {
				delta, _ = ch["message"].(map[string]any)
			}
			if delta == nil {
				continue
			}
			if logFirst > 0 {
				logFirst--
				keys := make([]string, 0, len(delta))
				for k := range delta { keys = append(keys, k) }
				log.Printf("[chat] stream=%s SSE chunk keys=%v", streamID, keys)
			}
			// Text delta
			if text, ok := delta["content"].(string); ok && text != "" {
				wailsRuntime.EventsEmit(a.ctx, eventName, map[string]any{"text": text})
				if opts.OnChunk != nil {
					opts.OnChunk(text)
				}
				s, _ := result["content"].(string)
				result["content"] = s + text
			}
			// Reasoning / chain-of-thought (DeepSeek, Qwen, etc.)
			if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
				wailsRuntime.EventsEmit(a.ctx, eventName, map[string]any{"reasoning": rc})
			}
			// Tool call deltas
			if tcs, ok := delta["tool_calls"].([]any); ok {
				for _, tc := range tcs {
					tcm, _ := tc.(map[string]any)
					if tcm == nil {
						continue
					}
					idx := 0
					if iv, ok := tcm["index"].(float64); ok {
						idx = int(iv)
					}
					for len(toolCalls) <= idx {
						toolCalls = append(toolCalls, map[string]any{
							"id": "", "type": "function",
							"function": map[string]any{"name": "", "arguments": ""},
						})
					}
					if id, ok := tcm["id"].(string); ok && id != "" {
						toolCalls[idx]["id"] = id
					}
					if fn, ok := tcm["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok && name != "" {
							fnMap, _ := toolCalls[idx]["function"].(map[string]any)
							// Set on first chunk only (avoid duplicating name on re-sends)
							if cur, _ := fnMap["name"].(string); cur == "" {
								fnMap["name"] = name
							}
						}
						if args, ok := fn["arguments"].(string); ok && args != "" {
							fnMap, _ := toolCalls[idx]["function"].(map[string]any)
							if cur, _ := fnMap["arguments"].(string); true {
								fnMap["arguments"] = cur + args
							}
						}
					}
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		result["tool_calls"] = toolCalls
	}
	// Diagnostic: surface empty/short responses (common when a small local
	// model's context window is overloaded — it returns 200 + empty SSE).
	contentLen := len(result["content"].(string))
	if contentLen == 0 && len(toolCalls) == 0 {
		log.Printf("[chat] stream=%s WARNING: empty response (0 content, 0 tool_calls, %d lines) — likely context overflow on the model", streamID, lineCount)
	} else {
		log.Printf("[chat] stream=%s SSE done: %d chars, %d tool_calls, %d lines", streamID, contentLen, len(toolCalls), lineCount)
	}
	return map[string]any{"choices": []any{map[string]any{"message": result}}}, nil
}

// ─── Thinking configuration ────────────────────────────────────────

// thinkApplyBody merges thinking configuration into the request body map.
func thinkApplyBody(bodyMap map[string]any, apiFormat, effort string) {
	if effort == "" {
		return
	}
	switch apiFormat {
	case "anthropic":
		bt := 4000
		if effort == "max" {
			bt = 16000
		}
		bodyMap["thinking"] = map[string]any{"type": "enabled", "budget_tokens": bt}
		bodyMap["output_config"] = map[string]string{"effort": effort}
	default:
		bodyMap["thinking"] = map[string]string{"type": "enabled"}
		bodyMap["reasoning_effort"] = effort
	}
}

// thinkSSEDelta handles a content_block_delta event for thinking/signature.
func thinkSSEDelta(ctx context.Context, eventName string, delta map[string]any) bool {
	dt, _ := delta["type"].(string)
	switch dt {
	case "thinking_delta":
		if text, _ := delta["thinking"].(string); text != "" {
			wailsRuntime.EventsEmit(ctx, eventName, map[string]any{"reasoning": text})
		}
		return true
	case "signature_delta":
		return true
	}
	return false
}
