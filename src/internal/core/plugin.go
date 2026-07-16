package core

import "context"

// ─── Manifest ────────────────────────────────────────────────────────

// PluginManifest describes a plugin's identity and capabilities.
// This is the static metadata that the UI shows and the registry indexes.
type PluginManifest struct {
	ID          string `json:"id"`          // unique, e.g. "openai", "deepseek"
	Name        string `json:"name"`        // human-readable, e.g. "OpenAI"
	Version     string `json:"version"`     // semver
	Description string `json:"description"` // one-line summary
	Author      string `json:"author"`      // "EverEvo" for built-in
	Type        string `json:"type"`        // "provider" | "toolset" | "channel" | "memory"
}

// ─── Base Plugin ─────────────────────────────────────────────────────

// Plugin is the minimal contract every plugin must satisfy.
// Most plugins will implement a more specific extension interface.
type Plugin interface {
	// Manifest returns static metadata. Called once at registration time.
	Manifest() PluginManifest
}

// ─── Extension Point: AI Provider ────────────────────────────────────

// ChatRequest is a provider-agnostic chat request.
type ChatRequest struct {
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	ThinkEffort string    `json:"think_effort,omitempty"` // "", "low", "high", "max"
}

// Message is a single turn in the conversation.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function-call from the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name + arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDef is a tool definition for LLM function calling.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
	ReadOnly    bool           `json:"read_only,omitempty"`
}

// Chunk is a streaming response delta from a provider.
type Chunk struct {
	Text      string `json:"text,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Done      bool   `json:"done,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ContextWindow  int    `json:"context_window"`
	MaxOutputToken int    `json:"max_output_tokens"`
	SupportsVision bool   `json:"supports_vision"`
	SupportsTools  bool   `json:"supports_tools"`
}

// ProviderPlugin is the extension point for AI model providers.
// Implement this to add a new LLM backend (OpenAI, DeepSeek, Ollama, etc.).
type ProviderPlugin interface {
	Plugin

	// Chat sends a chat request and returns a stream of response chunks.
	Chat(ctx context.Context, req ChatRequest) (<-chan Chunk, error)

	// ListModels returns the models available from this provider.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// Supports checks whether this provider supports a given feature.
	Supports(feature string) bool
}

// ─── Extension Point: Tool Set ───────────────────────────────────────

// ToolResult is the result of calling a tool.
type ToolResult struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ToolPlugin is the extension point for callable tools.
// A single ToolPlugin can provide multiple tools (e.g., "fileops" provides
// read_file, write_file, list_dir).
type ToolPlugin interface {
	Plugin

	// ToolDefs returns the tool schemas for LLM function calling.
	ToolDefs() []ToolDef

	// CallTool executes a named tool with the given arguments.
	CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error)
}

// ─── Extension Point: Messaging Channel ──────────────────────────────

// ChannelPlugin is the extension point for messaging channels
// (Feishu, Discord, Telegram, etc.).
type ChannelPlugin interface {
	Plugin

	// Connect establishes the channel connection.
	Connect(ctx context.Context) error

	// Disconnect closes the channel connection.
	Disconnect() error

	// OnMessage registers a handler for inbound messages.
	OnMessage(handler func(ChannelMessage) error)

	// Send delivers an outbound message to a target.
	Send(ctx context.Context, target string, msg ChannelMessage) error
}

// ChannelMessage is a channel-agnostic message.
type ChannelMessage struct {
	Text     string            `json:"text"`
	Sender   string            `json:"sender"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ─── Extension Point: Memory Backend ─────────────────────────────────

// MemoryPlugin is the extension point for memory backends.
type MemoryPlugin interface {
	Plugin
	Store() Store
	VectorSearch(ctx context.Context, query string, k int) ([]VectorResult, error)
}

// VectorResult is a single semantic search hit.
type VectorResult struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
