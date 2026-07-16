// Package a2a implements a minimal Google Agent-to-Agent (A2A) protocol layer:
// agent-card discovery, JSON-RPC 2.0 task messaging, and a loopback-capable
// server + client. Phase 1 covers synchronous tasks/send (no SSE/push yet).
package a2a

import "encoding/json"

// Task lifecycle states (A2A spec).
const (
	StateSubmitted     = "submitted"
	StateWorking       = "working"
	StateInputRequired = "input-required"
	StateCompleted     = "completed"
	StateCanceled      = "canceled"
	StateFailed        = "failed"
)

// JSON-RPC method names.
const (
	MethodSend   = "tasks/send"
	MethodGet    = "tasks/get"
	MethodCancel = "tasks/cancel"
)

// AgentCard is served at /.well-known/agent-card.json and describes an agent.
type AgentCard struct {
	Name               string            `json:"name"`
	Description        string            `json:"description,omitempty"`
	Version            string            `json:"version,omitempty"`
	URL                string            `json:"url,omitempty"`
	Capabilities       AgentCapabilities `json:"capabilities"`
	DefaultInputModes  []string          `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string          `json:"defaultOutputModes,omitempty"`
	Skills             []AgentSkill      `json:"skills,omitempty"`
}

// AgentCapabilities advertises optional protocol features.
type AgentCapabilities struct {
	Streaming              bool `json:"streaming"`
	PushNotifications      bool `json:"pushNotifications"`
	StateTransitionHistory bool `json:"stateTransitionHistory"`
}

// AgentSkill is one capability an agent exposes.
type AgentSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Message is a turn in an A2A conversation.
type Message struct {
	Role  string `json:"role"` // "user" | "agent"
	Parts []Part `json:"parts"`
}

// Part is one fragment of a message. Phase 1 supports text only.
type Part struct {
	Kind string `json:"kind"` // "text" | "data" | "file"
	Text string `json:"text,omitempty"`
	Data any    `json:"data,omitempty"`
}

// TextMessage is a convenience constructor for a single text-part message.
func TextMessage(role, text string) Message {
	return Message{Role: role, Parts: []Part{{Kind: "text", Text: text}}}
}

// TaskStatus is the status of a task at a point in time.
type TaskStatus struct {
	State     string   `json:"state"`
	Timestamp string   `json:"timestamp,omitempty"`
	Message   *Message `json:"message,omitempty"`
}

// Task is the unit of work in A2A.
type Task struct {
	ID        string     `json:"id"`
	SessionID string     `json:"sessionId,omitempty"`
	Status    TaskStatus `json:"status"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	History   []Message  `json:"history,omitempty"`
}

// Artifact is an output produced by a task.
type Artifact struct {
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

// JSONRPCRequest is a JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SendParams is the params object for tasks/send.
type SendParams struct {
	ID        string  `json:"id"`
	SessionID string  `json:"sessionId,omitempty"`
	Message   Message `json:"message"`
}
