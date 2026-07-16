package config

import "strings"

// ModelProfile describes per-model tuning parameters used by the context
// management and task routing layers. This is the backend's single source of
// truth — the frontend consumes it via GetModelRegistry().
type ModelProfile struct {
	Label                  string `json:"label"`
	ContextWindow          int    `json:"contextWindow"`
	MaxOutputTokens        int    `json:"maxOutputTokens"`
	EffectivePct           int    `json:"effectivePct"`           // % of context usable for input
	CompactPct             int    `json:"compactPct"`             // % at which auto-compaction triggers
	SupportsThinkingBudget bool   `json:"supportsThinkingBudget"` // separate thinking token budget
}

// modelPresets is the comprehensive built-in registry. Keys are matched
// case-insensitively as substrings against "providerName modelName".
// First match wins — order matters (specific before generic).
var modelPresets = []struct {
	key     string
	profile ModelProfile
}{
	// ── DeepSeek ──
	{"deepseek-v4-pro", ModelProfile{"DeepSeek V4 Pro", 1_000_000, 0, 95, 90, false}},
	{"deepseek-v4-flash", ModelProfile{"DeepSeek V4 Flash", 1_000_000, 0, 95, 90, false}},
	{"deepseek-chat", ModelProfile{"DeepSeek V4 (legacy)", 1_000_000, 0, 95, 90, false}},
	{"deepseek-reasoner", ModelProfile{"DeepSeek Reasoner", 1_000_000, 0, 95, 90, false}},
	{"deepseek", ModelProfile{"DeepSeek (generic)", 1_000_000, 0, 95, 90, false}},

	// ── Anthropic Claude ──
	{"claude opus", ModelProfile{"Claude Opus 4+", 200_000, 128_000, 95, 90, true}},
	{"claude sonnet", ModelProfile{"Claude Sonnet 4+", 200_000, 64_000, 95, 90, true}},
	{"claude haiku", ModelProfile{"Claude Haiku 4+", 200_000, 64_000, 95, 90, true}},
	{"claude", ModelProfile{"Claude (generic)", 200_000, 64_000, 95, 90, true}},

	// ── OpenAI ──
	{"gpt-5", ModelProfile{"GPT-5", 272_000, 0, 95, 90, false}},
	{"gpt-4o", ModelProfile{"GPT-4o", 128_000, 0, 95, 90, false}},
	{"gpt-4-turbo", ModelProfile{"GPT-4 Turbo", 128_000, 0, 95, 90, false}},
	{"gpt-4", ModelProfile{"GPT-4", 8_192, 0, 95, 90, false}},
	{"gpt-3.5", ModelProfile{"GPT-3.5 Turbo", 16_384, 0, 95, 90, false}},
	{"o4", ModelProfile{"OpenAI o4", 200_000, 0, 95, 90, false}},
	{"o3", ModelProfile{"OpenAI o3", 200_000, 0, 95, 90, false}},
	{"o1", ModelProfile{"OpenAI o1", 200_000, 0, 95, 90, false}},

	// ── Google Gemini ──
	{"gemini-2.5", ModelProfile{"Gemini 2.5+", 1_000_000, 0, 95, 90, true}},
	{"gemini-2", ModelProfile{"Gemini 2", 1_000_000, 0, 95, 90, true}},
	{"gemini-1.5", ModelProfile{"Gemini 1.5", 1_000_000, 0, 95, 90, true}},
	{"gemini", ModelProfile{"Gemini (generic)", 1_000_000, 0, 95, 90, true}},

	// ── Qwen / Tongyi ──
	{"qwen2.5-coder-32b", ModelProfile{"Qwen2.5 Coder 32B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-coder-14b", ModelProfile{"Qwen2.5 Coder 14B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-coder-7b", ModelProfile{"Qwen2.5 Coder 7B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-coder-3b", ModelProfile{"Qwen2.5 Coder 3B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-coder-1.5b", ModelProfile{"Qwen2.5 Coder 1.5B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-coder", ModelProfile{"Qwen2.5 Coder", 32_768, 0, 90, 85, false}},
	{"qwen2.5-72b", ModelProfile{"Qwen2.5 72B", 131_072, 0, 95, 90, false}},
	{"qwen2.5-32b", ModelProfile{"Qwen2.5 32B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-14b", ModelProfile{"Qwen2.5 14B", 32_768, 0, 90, 85, false}},
	{"qwen2.5-7b", ModelProfile{"Qwen2.5 7B", 32_768, 0, 90, 85, false}},
	// Qwen3.5 — before Qwen3 so "qwen3.5" key doesn't match "qwen3" first
	{"qwen3.5-32b", ModelProfile{"Qwen3.5 32B", 65_536, 0, 95, 90, false}},
	{"qwen3.5-14b", ModelProfile{"Qwen3.5 14B", 65_536, 0, 95, 90, false}},
	{"qwen3.5-9b", ModelProfile{"Qwen3.5 9B", 65_536, 0, 95, 90, false}},
	{"qwen3.5-8b", ModelProfile{"Qwen3.5 8B", 65_536, 0, 95, 90, false}},
	{"qwen3.5-0.6b", ModelProfile{"Qwen3.5 0.6B", 65_536, 0, 95, 90, false}},
	{"qwen3.5", ModelProfile{"Qwen3.5", 65_536, 0, 95, 90, false}},
	// Qwen3
	{"qwen3-235b", ModelProfile{"Qwen3 235B", 131_072, 0, 95, 90, false}},
	{"qwen3-32b", ModelProfile{"Qwen3 32B", 131_072, 0, 95, 90, false}},
	{"qwen3-8b", ModelProfile{"Qwen3 8B", 131_072, 0, 95, 90, false}},
	{"qwen3", ModelProfile{"Qwen3", 131_072, 0, 95, 90, false}},
	// Qwen cloud / Tongyi
	{"qwen-max", ModelProfile{"Qwen Max", 1_000_000, 0, 95, 90, false}},
	{"qwen-plus", ModelProfile{"Qwen Plus", 1_000_000, 0, 95, 90, false}},
	{"qwen-turbo", ModelProfile{"Qwen Turbo", 1_000_000, 0, 95, 90, false}},
	{"qwen", ModelProfile{"Qwen (generic)", 131_072, 0, 90, 85, false}},
	{"qwq", ModelProfile{"QwQ", 131_072, 0, 95, 90, false}},

	// ── Meta Llama ──
	{"llama-4", ModelProfile{"Llama 4", 128_000, 0, 95, 90, false}},
	{"llama-3.3", ModelProfile{"Llama 3.3", 128_000, 0, 95, 90, false}},
	{"llama-3.2", ModelProfile{"Llama 3.2", 128_000, 0, 95, 90, false}},
	{"llama-3.1", ModelProfile{"Llama 3.1", 128_000, 0, 95, 90, false}},
	{"llama-3", ModelProfile{"Llama 3", 8_192, 0, 90, 85, false}},
	{"llama", ModelProfile{"Llama (generic)", 8_192, 0, 85, 80, false}},
	{"codellama", ModelProfile{"Code Llama", 16_384, 0, 90, 85, false}},

	// ── Mistral ──
	{"mistral-large", ModelProfile{"Mistral Large", 128_000, 0, 95, 90, false}},
	{"mistral-medium", ModelProfile{"Mistral Medium", 32_000, 0, 90, 85, false}},
	{"mistral-small", ModelProfile{"Mistral Small", 32_000, 0, 90, 85, false}},
	{"mixtral", ModelProfile{"Mixtral", 32_000, 0, 90, 85, false}},
	{"mistral", ModelProfile{"Mistral (generic)", 32_000, 0, 90, 85, false}},
	{"codestral", ModelProfile{"Codestral", 32_000, 0, 90, 85, false}},

	// ── GLM / Zhipu ──
	{"glm-4-plus", ModelProfile{"GLM-4 Plus", 128_000, 0, 95, 90, false}},
	{"glm-4", ModelProfile{"GLM-4", 128_000, 0, 95, 90, false}},
	{"glm", ModelProfile{"GLM (generic)", 32_000, 0, 90, 85, false}},

	// ── Moonshot / Kimi ──
	{"moonshot-v1-128k", ModelProfile{"Moonshot 128K", 128_000, 0, 95, 90, false}},
	{"moonshot-v1-32k", ModelProfile{"Moonshot 32K", 32_000, 0, 90, 85, false}},
	{"moonshot", ModelProfile{"Moonshot (generic)", 128_000, 0, 95, 90, false}},
	{"kimi", ModelProfile{"Kimi", 128_000, 0, 95, 90, false}},

	// ── Google Gemma ──
	{"gemma-3", ModelProfile{"Gemma 3", 128_000, 0, 90, 85, false}},
	{"gemma-2", ModelProfile{"Gemma 2", 8_192, 0, 90, 85, false}},
	{"gemma", ModelProfile{"Gemma (generic)", 8_192, 0, 85, 80, false}},

	// ── Microsoft Phi ──
	{"phi-4", ModelProfile{"Phi-4", 16_384, 0, 90, 85, false}},
	{"phi-3", ModelProfile{"Phi-3", 128_000, 0, 90, 85, false}},
	{"phi", ModelProfile{"Phi (generic)", 16_384, 0, 85, 80, false}},

	// ── Yi / 01.AI ──
	{"yi-lightning", ModelProfile{"Yi Lightning", 16_384, 0, 90, 85, false}},
	{"yi-large", ModelProfile{"Yi Large", 32_768, 0, 95, 90, false}},
	{"yi", ModelProfile{"Yi (generic)", 32_768, 0, 90, 85, false}},

	// ── Grok / xAI ──
	{"grok-3", ModelProfile{"Grok 3", 131_072, 0, 95, 90, false}},
	{"grok-2", ModelProfile{"Grok 2", 131_072, 0, 95, 90, false}},
	{"grok", ModelProfile{"Grok (generic)", 131_072, 0, 95, 90, false}},

	// ── Command R (Cohere) ──
	{"command-r-plus", ModelProfile{"Command R+", 128_000, 0, 95, 90, false}},
	{"command-r", ModelProfile{"Command R", 128_000, 0, 95, 90, false}},

	// ── InternLM ──
	{"internlm2.5", ModelProfile{"InternLM 2.5", 32_768, 0, 90, 85, false}},
	{"internlm", ModelProfile{"InternLM", 32_768, 0, 90, 85, false}},

	// ── Deepseek Coder (legacy) ──
	{"deepseek-coder", ModelProfile{"DeepSeek Coder", 16_384, 0, 90, 85, false}},
}

// fallbackProfile is the conservative default for unknown models.
var fallbackProfile = ModelProfile{
	Label:                  "Unknown Model (conservative fallback)",
	ContextWindow:          32_768,
	MaxOutputTokens:        0,
	EffectivePct:           80,
	CompactPct:             80,
	SupportsThinkingBudget: false,
}

// LookupModelProfile finds the best-matching profile for a provider+model string.
// Matches case-insensitively against keys. First match wins.
func LookupModelProfile(providerName, modelName string) ModelProfile {
	haystack := strings.ToLower(providerName + " " + modelName)
	for _, entry := range modelPresets {
		if strings.Contains(haystack, entry.key) {
			return entry.profile
		}
	}
	return fallbackProfile
}

// MergeWithCapability overlays probed capability data onto a preset profile.
// Probed values win when present (>0); preset values are the fallback.
func MergeWithCapability(profile ModelProfile, cap ModelCapability) ModelProfile {
	if cap.MaxContextTokens > 0 {
		profile.ContextWindow = cap.MaxContextTokens
	}
	return profile
}

// FindBestModel selects the best provider+model for a given task type.
// Returns nil if no suitable provider is found.
// Task types:
//
//	"vision"     — requires SupportsVision
//	"tools"      — requires SupportsTools
//	"reasoning"  — requires SupportsReasoning
//	"extraction" — cheapest option (prefer local), tools not required
//	"chat"       — full-featured, tools + streaming
func FindBestModel(providers []LLMProvider, task string) *LLMProvider {
	type candidate struct {
		provider *LLMProvider
		cap      ModelCapability
		score    int
	}

	var candidates []candidate
	for i := range providers {
		p := &providers[i]
		if !p.Enabled {
			continue
		}
		cap := p.ModelCapabilities[p.Model]
		isLocal := isLocalProvider(p)
		c := candidate{provider: p, cap: cap}

		switch task {
		case "vision":
			if !cap.SupportsVision {
				continue
			}
			c.score = 100
		case "tools":
			if !cap.SupportsTools {
				continue
			}
			c.score = 100
		case "reasoning":
			if !cap.SupportsReasoning {
				continue
			}
			c.score = 100
		case "extraction":
			// Any model works for extraction; prefer local to save credits
			c.score = 10
			if isLocal {
				c.score = 100
			}
		case "chat":
			// Full-featured: tools + streaming
			if cap.SupportsTools {
				c.score += 50
			}
			if cap.SupportsStreaming {
				c.score += 30
			}
			if cap.SupportsVision {
				c.score += 20
			}
			if isLocal {
				c.score += 5 // slight bonus for local (no latency cost)
			}
		default:
			c.score = 50
		}

		candidates = append(candidates, c)
	}

	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.provider
}

// isLocalProvider checks if a provider is a local inference server.
func isLocalProvider(p *LLMProvider) bool {
	return p.Type == "ollama" || p.Type == "llamacpp" ||
		strings.Contains(p.Endpoint, "127.0.0.1") ||
		strings.Contains(p.Endpoint, "localhost")
}

// GetFallbackProfile returns the conservative default for unknown models.
func GetFallbackProfile() ModelProfile {
	return fallbackProfile
}
