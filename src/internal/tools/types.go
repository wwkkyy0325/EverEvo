// Package tools defines the LLM function-calling tool system for EverEvo.
// Every functional interface in the app is encapsulated as a Tool with
// JSON Schema parameters, compatible with OpenAI/Anthropic function calling
// and the MCP (Model Context Protocol) tools/list specification.
//
// Deprecated: This is the legacy tool system. New tools should implement
// core.ToolPlugin and self-register via core.GlobalTools.Register() in init().
// Tool dispatch in CallTool() checks the new plugin engine first, so migrated
// tools automatically take priority over legacy handlers.
package tools

import "encoding/json"

// ToolDef describes one callable tool in OpenAI Function Calling / MCP format.
type ToolDef struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`       // human-readable short name (MCP)
	Description string      `json:"description"`
	Parameters  *ToolParams `json:"parameters"`
	Category    string      `json:"category"` // model | plugin | kb | catalog | download | system | guide | workflow | mcp | provider | a2a | agent
	// RawParameters preserves the original MCP inputSchema JSON for external tools,
	// avoiding JSON Schema fidelity loss from the typed Parameters round-trip.
	// When set, the frontend and agent-execution paths prefer it over Parameters.
	RawParameters json.RawMessage `json:"rawParameters,omitempty"`
	// MCP-specific fields
	OutputSchema *ToolOutputSchema      `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations       `json:"annotations,omitempty"`
}

// ToolOutputSchema describes the expected output structure (MCP).
type ToolOutputSchema struct {
	Type       string                       `json:"type"`
	Properties map[string]ToolOutputProp    `json:"properties,omitempty"`
}

// ToolOutputProp describes a single output field.
type ToolOutputProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolAnnotations provides hints about tool behavior (MCP).
type ToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint,omitempty"`
	DestructiveHint bool `json:"destructiveHint,omitempty"`
	IdempotentHint  bool `json:"idempotentHint,omitempty"`
}

// ToolParams is a JSON Schema object with properties and required fields.
type ToolParams struct {
	Type       string              `json:"type"` // always "object"
	Properties map[string]ToolProp `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// ToolProp describes a single parameter.
type ToolProp struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Enum        []string  `json:"enum,omitempty"`
	Default     any       `json:"default,omitempty"`
	Items       *ToolProp `json:"items,omitempty"` // for array types
}

// ToolResult is the unified return value from CallTool.
type ToolResult struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}
