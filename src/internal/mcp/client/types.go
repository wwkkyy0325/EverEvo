// Package client implements an MCP client that connects to external
// MCP servers (stdio subprocess or HTTP), discovers their tools, and
// exposes them for use in EverEvo's tool system.
package client

import (
	"encoding/json"
	"sync"

	"everevo/internal/tools"
)

// ─── Connection Config ──────────────────────────────────────────

// ServerConfig is the persistent configuration for one external MCP server.
type ServerConfig struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Transport string   `json:"transport"`        // "stdio" | "http"
	Command   string   `json:"command,omitempty"` // stdio: executable
	Args      []string `json:"args,omitempty"`    // stdio: arguments
	Env       []string `json:"env,omitempty"`     // stdio: extra env (KEY=VALUE)
	URL       string   `json:"url,omitempty"`     // http: base URL
	LibraryID string   `json:"libraryId,omitempty"` // domain library this server belongs to

	// Runtime state (not persisted)
	Status    string `json:"status"` // "disconnected" | "connecting" | "connected" | "error"
	Error     string `json:"error,omitempty"`
	ToolCount int    `json:"toolCount"` // runtime, not persisted
}

// Connection represents an active connection to an external MCP server.
type Connection struct {
	Cfg   ServerConfig
	Tools []*tools.ToolDef

	mu       sync.Mutex
	transport Transport
	nextID   int64
}

// ─── JSON-RPC 2.0 (MCP wire format) ─────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP Protocol Types ─────────────────────────────────────────

type initializeParams struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    clientCaps   `json:"capabilities"`
	ClientInfo      clientInfo   `json:"clientInfo"`
}

type clientCaps struct{}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    serverCaps `json:"capabilities"`
	ServerInfo      serverInfo `json:"serverInfo"`
}

type serverCaps struct {
	Tools *struct{ ListChanged bool } `json:"tools,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type listToolsResult struct {
	Tools []mcpToolDef `json:"tools"`
}

type mcpToolDef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpInputSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]mcpParamProp  `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

type mcpParamProp struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ─── Persistent Store ───────────────────────────────────────────

// Store is the on-disk format for mcp_servers.json.
type Store struct {
	Servers []ServerConfig `json:"servers"`
}

// ─── Recommended MCP Servers ────────────────────────────────────

// RecommendInfo describes a recommended MCP server for the UI.
type RecommendInfo struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Transport   string   `json:"transport"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
	Category    string   `json:"category"` // "filesystem" | "shell" | "browser" | "data" | "dev"
}
