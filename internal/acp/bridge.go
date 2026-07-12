// Package acp implements a bridge to OpenCode's ACP (Agent Client Protocol),
// allowing EverEvo to programmatically delegate code modification tasks to
// OpenCode as a subprocess. Communication is via JSON-RPC 2.0 style JSON Lines
// over stdout (opencode run --format json).
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultExe is the default executable name for opencode.
const DefaultExe = "opencode"

// Event types emitted by opencode run --format json.
const (
	EventStepStart  = "step_start"
	EventStepFinish = "step_finish"
	EventToolUse    = "tool_use"
	EventText       = "text"
	EventError      = "error"
)

// RawEvent is the raw JSON line from opencode stdout.
type RawEvent struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"`
	Error     *struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error,omitempty"`
}

// TextPart is extracted from a "text" event.
type TextPart struct {
	Text string `json:"text"`
}

// ToolPart is extracted from a "tool_use" event.
type ToolPart struct {
	Tool   string `json:"tool"`
	CallID string `json:"callID"`
	State  struct {
		Status string `json:"status"`
		Input  struct {
			Pattern  string `json:"pattern,omitempty"`
			FilePath string `json:"filePath,omitempty"`
			Limit    int    `json:"limit,omitempty"`
		} `json:"input"`
		Output string `json:"output"`
	} `json:"state"`
}

// StepFinishPart is extracted from a "step_finish" event.
type StepFinishPart struct {
	Reason   string `json:"reason"`
	Snapshot string `json:"snapshot"`
	Tokens   struct {
		Total     int `json:"total"`
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
	} `json:"tokens"`
	Cost float64 `json:"cost"`
}

// Result summarises a completed opencode run.
type Result struct {
	SessionID string        `json:"sessionId"`
	Text      string        `json:"text"`
	ToolCalls []ToolSummary `json:"toolCalls"`
	Snapshots []string      `json:"snapshots"`
	Tokens    TokenStats    `json:"tokens"`
	Cost      float64       `json:"cost"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
}

// ToolSummary is a concise record of one tool call.
type ToolSummary struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Status string `json:"status"`
}

// TokenStats aggregates token usage across all steps.
type TokenStats struct {
	Total     int `json:"total"`
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
}

// EventHandler is called for each parsed event. Return an error to abort.
type EventHandler func(event RawEvent) error

// Bridge manages an OpenCode subprocess for code modification tasks.
type Bridge struct {
	mu          sync.Mutex
	opencodeExe string
}

// NewBridge creates a new ACP bridge.
func NewBridge(opencodeExe string) *Bridge {
	if opencodeExe == "" {
		opencodeExe = "opencode"
	}
	return &Bridge{opencodeExe: opencodeExe}
}

// Run executes an opencode task in the given project directory and returns a
// structured result. It streams JSON events from the subprocess stdout.
func (b *Bridge) Run(ctx context.Context, projectDir, message string, onEvent EventHandler) (*Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	startTime := time.Now()

	cmd := exec.CommandContext(ctx, b.opencodeExe,
		"run", "--format", "json",
		"--dir", projectDir,
		message,
	)
	cmd.Stderr = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start opencode: %w", err)
	}

	result := &Result{}
	var textParts []string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if ev.SessionID != "" && result.SessionID == "" {
			result.SessionID = ev.SessionID
		}

		switch ev.Type {
		case EventText:
			var tp TextPart
			if err := json.Unmarshal(ev.Part, &tp); err == nil && tp.Text != "" {
				textParts = append(textParts, tp.Text)
			}
		case EventToolUse:
			var tp ToolPart
			if err := json.Unmarshal(ev.Part, &tp); err == nil {
				inputDesc := tp.State.Input.Pattern
				if inputDesc == "" {
					inputDesc = tp.State.Input.FilePath
				}
				result.ToolCalls = append(result.ToolCalls, ToolSummary{
					Tool:   tp.Tool,
					Input:  inputDesc,
					Status: tp.State.Status,
				})
			}
		case EventStepFinish:
			var sp StepFinishPart
			if err := json.Unmarshal(ev.Part, &sp); err == nil {
				if sp.Snapshot != "" {
					result.Snapshots = append(result.Snapshots, sp.Snapshot)
				}
				result.Tokens.Total += sp.Tokens.Total
				result.Tokens.Input += sp.Tokens.Input
				result.Tokens.Output += sp.Tokens.Output
				result.Tokens.Reasoning += sp.Tokens.Reasoning
				result.Cost += sp.Cost
			}
		case EventError:
			if ev.Error != nil {
				result.Error = ev.Error.Data.Message
			}
		}

		if onEvent != nil {
			if err := onEvent(ev); err != nil {
				cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, err
			}
		}
	}

	waitErr := cmd.Wait()
	result.Text = strings.TrimSpace(strings.Join(textParts, "\n"))
	result.Duration = time.Since(startTime).Round(time.Millisecond)

	if waitErr != nil && result.Error == "" {
		result.Error = waitErr.Error()
	}
	if err := scanner.Err(); err != nil && result.Error == "" {
		result.Error = fmt.Sprintf("stdout read error: %v", err)
	}

	return result, nil
}

// RunSimple is a convenience wrapper that just returns the text output.
func (b *Bridge) RunSimple(ctx context.Context, projectDir, message string) (string, error) {
	result, err := b.Run(ctx, projectDir, message, nil)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return result.Text, fmt.Errorf("opencode: %s", result.Error)
	}
	return result.Text, nil
}

// Check verifies that opencode is available and functional.
func (b *Bridge) Check(ctx context.Context) error {
	if _, err := exec.LookPath(b.opencodeExe); err != nil {
		return fmt.Errorf("opencode not found: %w", err)
	}
	return nil
}

// Version returns the opencode version string.
func (b *Bridge) Version() (string, error) {
	cmd := exec.Command(b.opencodeExe, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
