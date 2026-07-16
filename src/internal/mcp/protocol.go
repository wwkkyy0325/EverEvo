// Package mcp implements a built-in Model Context Protocol (MCP) server
// for EverEvo. It exposes all app capabilities as MCP Tools, Resources, and
// Prompts over HTTP (Streamable HTTP transport), making EverEvo compatible
// with Claude Desktop, Cursor, Continue, and any MCP-compatible client.
package mcp

// ─── JSON-RPC 2.0 Envelope ──────────────────────────────────────

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no id, no response expected).
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// SuccessResponse is a JSON-RPC 2.0 success response.
type SuccessResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result"`
}

// ErrorResponse is a JSON-RPC 2.0 error response.
type ErrorResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Error   RPCErr `json:"error"`
}

// RPCErr is a JSON-RPC error object.
type RPCErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ─── MCP-Specific Types ─────────────────────────────────────────

// InitializeParams is the params for the "initialize" request.
type InitializeParams struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    ClientCaps   `json:"capabilities"`
	ClientInfo      ClientInfo   `json:"clientInfo"`
}

// ClientCaps declares what the client supports.
type ClientCaps struct{}

// ClientInfo identifies the client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of "initialize".
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    ServerCaps   `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Instructions    string       `json:"instructions,omitempty"`
}

// ServerCaps declares what the server supports.
type ServerCaps struct {
	Tools     *ToolsCaps     `json:"tools,omitempty"`
	Resources *ResourcesCaps `json:"resources,omitempty"`
	Prompts   *PromptsCaps   `json:"prompts,omitempty"`
}

// ToolsCaps is the tools capability declaration.
type ToolsCaps struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCaps is the resources capability declaration.
type ResourcesCaps struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCaps is the prompts capability declaration.
type PromptsCaps struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo identifies the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ─── Tool Types ─────────────────────────────────────────────────

// ToolDef is an MCP-compatible tool definition.
type ToolDef struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description"`
	InputSchema *ToolParams `json:"inputSchema"`
	OutputSchema any        `json:"outputSchema,omitempty"`
	Annotations *ToolAnnot  `json:"annotations,omitempty"`
}

// ToolParams is the JSON Schema for a tool's input.
type ToolParams struct {
	Type       string              `json:"type"`
	Properties map[string]ToolProp `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// ToolProp describes a single parameter.
type ToolProp struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Enum        []string  `json:"enum,omitempty"`
	Default     any       `json:"default,omitempty"`
	Items       *ToolProp `json:"items,omitempty"`
}

// ToolAnnot provides hints about tool behavior.
type ToolAnnot struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   bool   `json:"openWorldHint,omitempty"`
}

// CallToolParams is the params for "tools/call".
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of "tools/call".
type CallToolResult struct {
	Content           []ContentBlock `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

// ListToolsResult is the result of "tools/list".
type ListToolsResult struct {
	Tools      []ToolDef `json:"tools"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

// ─── Content Blocks ─────────────────────────────────────────────

// ContentBlock is a union type for text/image/audio/resource content.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
}

// TextContent creates a text content block.
func TextContent(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// ─── Resource Types ─────────────────────────────────────────────

// ResourceDef is an MCP resource definition.
type ResourceDef struct {
	URI         string        `json:"uri"`
	Name        string        `json:"name"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	MimeType    string        `json:"mimeType,omitempty"`
	Annotations *ToolAnnot    `json:"annotations,omitempty"`
}

// ListResourcesResult is the result of "resources/list".
type ListResourcesResult struct {
	Resources  []ResourceDef `json:"resources"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// ReadResourceParams is the params for "resources/read".
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult is the result of "resources/read".
type ReadResourceResult struct {
	Contents []ContentBlock `json:"contents"`
}

// ─── Prompt Types ───────────────────────────────────────────────

// PromptDef is an MCP prompt or prompt template.
type PromptDef struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a prompt template argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ListPromptsResult is the result of "prompts/list".
type ListPromptsResult struct {
	Prompts    []PromptDef `json:"prompts"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

// GetPromptParams is the params for "prompts/get".
type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptMessage is a message in a prompt result.
type PromptMessage struct {
	Role    string        `json:"role"`
	Content ContentBlock  `json:"content"`
}

// GetPromptResult is the result of "prompts/get".
type GetPromptResult struct {
	Messages    []PromptMessage `json:"messages"`
	Description string          `json:"description,omitempty"`
}
