// Package evolve implements the self-evolution subsystem: build from source,
// swap binary, restart, and ACP-bridged code modifications.
package evolve

import "everevo/internal/acp"

// Capability describes whether source-code-level self-evolution is available.
type Capability struct {
	SourceAvailable bool   `json:"sourceAvailable"`
	SourceDir       string `json:"sourceDir"`
	BuildOutput     string `json:"buildOutput"`
	CurrentExe      string `json:"currentExe"`
	GoAvailable     bool   `json:"goAvailable"`
	NodeAvailable   bool   `json:"nodeAvailable"`
	WailsAvailable  bool   `json:"wailsAvailable"`
}

// Task represents a pending or completed self-evolution task.
type Task struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Status      string           `json:"status"` // pending / acp_running / building / swapping / done / failed
	CreatedAt   string           `json:"createdAt"`
	Error       string           `json:"error,omitempty"`
	AcpMessage  string           `json:"acpMessage,omitempty"`
	AcpSessionID string          `json:"acpSessionId,omitempty"`
	AcpResult   *acp.Result      `json:"acpResult,omitempty"`
	AcpToolCalls []acp.ToolSummary `json:"acpToolCalls,omitempty"`
	AcpTokens   acp.TokenStats   `json:"acpTokens,omitempty"`
	AcpCost     float64          `json:"acpCost,omitempty"`
	AcpDuration string           `json:"acpDuration,omitempty"`
}

// RestartMarker is written before exit so the new instance knows this was an
// intentional evolution restart.
type RestartMarker struct {
	TaskID    string `json:"taskId"`
	Timestamp string `json:"timestamp"`
	Reason    string `json:"reason"`
}
