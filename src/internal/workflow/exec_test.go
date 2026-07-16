package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// recordingEmitter captures emitted event names for assertions.
type recordingEmitter struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingEmitter) Emit(event string, data any) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *recordingEmitter) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// streamingMockLLM calls onChunk once to simulate a streamed token, then returns resp.
type streamingMockLLM struct{ resp map[string]any }

func (m *streamingMockLLM) ChatProxy(messagesJSON, toolsJSON json.RawMessage) (map[string]any, error) {
	return m.resp, nil
}
func (m *streamingMockLLM) ChatStream(ctx context.Context, messagesJSON, toolsJSON json.RawMessage, onChunk func(string)) (map[string]any, error) {
	if onChunk != nil {
		onChunk("partial")
	}
	return m.resp, nil
}

// TestEngineDoneClosesOnSuccess verifies the Done channel closes on normal completion.
func TestEngineDoneClosesOnSuccess(t *testing.T) {
	wf := WorkflowDef{ID: "wf_done"}
	eng := NewEngine(&wf, &streamingMockLLM{}, nil, nil, &recordingEmitter{}, noopTracker{})
	eng.Run(nil)
	if eng.State().Status != ExecDone {
		t.Errorf("status: want done, got %s", eng.State().Status)
	}
	select {
	case <-eng.State().Done:
	default:
		t.Error("Done channel not closed after successful run")
	}
}

// TestEngineCancelClosesDone verifies cancel sets Cancelled and closes Done.
func TestEngineCancelClosesDone(t *testing.T) {
	wf := WorkflowDef{ID: "wf_cancel"}
	eng := NewEngine(&wf, &streamingMockLLM{}, nil, nil, &recordingEmitter{}, noopTracker{})
	eng.Cancel()
	if eng.State().Status != ExecCancelled {
		t.Errorf("status: want cancelled, got %s", eng.State().Status)
	}
	select {
	case <-eng.State().Done:
	default:
		t.Error("Done channel not closed after cancel")
	}
}

// TestEngineLLMStreamsProgress verifies an LLM node forwards streamed text via the
// workflow-progress-<execId> event.
func TestEngineLLMStreamsProgress(t *testing.T) {
	wf := WorkflowDef{
		ID:    "wf_stream",
		Nodes: []WorkflowNode{{ID: "n1", Type: NodeLLM, Config: map[string]any{"userPrompt": "hi"}}},
	}
	mock := &streamingMockLLM{resp: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "hello"}}}}}
	emitter := &recordingEmitter{}
	eng := NewEngine(&wf, mock, nil, nil, emitter, noopTracker{})
	eng.Run(nil)
	if !emitter.has("workflow-progress-") {
		t.Error("expected a workflow-progress-* event from the streaming LLM node")
	}
}

// TestOutputAutoFromActiveBranch verifies an output node with no hand-picked
// source automatically picks up the active (non-skipped) branch's output at a
// converge point (condition → A/B → output).
func TestOutputAutoFromActiveBranch(t *testing.T) {
	wf := WorkflowDef{
		ID:    "wf_converge",
		Nodes: []WorkflowNode{{ID: "out", Type: NodeOutput, Config: map[string]any{}}},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "a", Target: "out"},
			{ID: "e2", Source: "b", Target: "out"},
		},
	}
	eng := NewEngine(&wf, &streamingMockLLM{}, nil, nil, &recordingEmitter{}, noopTracker{})
	eng.nodeOutputs["a"] = "A-result"
	eng.nodeOutputs["b"] = "B-result"

	// True branch taken → b is skipped → output should auto-pick a.
	eng.skipped["b"] = true
	got, err := executeOutput(eng, &wf.Nodes[0])
	if err != nil {
		t.Fatalf("executeOutput: %v", err)
	}
	if got != "A-result" {
		t.Errorf("auto output (true branch): want A-result, got %v", got)
	}

	// False branch taken → a is skipped → output should auto-pick b.
	eng.skipped["b"] = false
	eng.skipped["a"] = true
	got, err = executeOutput(eng, &wf.Nodes[0])
	if err != nil {
		t.Fatalf("executeOutput: %v", err)
	}
	if got != "B-result" {
		t.Errorf("auto output (false branch): want B-result, got %v", got)
	}
}
