package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

// --- test mocks ---

type mockLLM struct {
	lastMessages []map[string]any
	resp         map[string]any
}

func (m *mockLLM) ChatProxy(messagesJSON, toolsJSON json.RawMessage) (map[string]any, error) {
	var msgs []map[string]any
	_ = json.Unmarshal(messagesJSON, &msgs)
	m.lastMessages = msgs
	return m.resp, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, messagesJSON, toolsJSON json.RawMessage, onChunk func(string)) (map[string]any, error) {
	return m.ChatProxy(messagesJSON, toolsJSON)
}

type noopEmitter struct{}

func (noopEmitter) Emit(event string, data any) {}

type noopTracker struct{}

func (noopTracker) TrackExecution(state *ExecutionState) {}

// userMessage returns the content of the last "user" role message seen by the LLM.
func (m *mockLLM) userMessage() string {
	for _, msg := range m.lastMessages {
		if msg["role"] == "user" {
			s, _ := msg["content"].(string)
			return s
		}
	}
	return ""
}

// TestInputLLMOutputDataFlow exercises the full data path the user reported broken:
// workflow_execute inputs → input node output → LLM user prompt template → LLM output
// → output node source template. Covers Bug1 (input output), Bug2/3 (template resolution)
// and the LLM output normalization + title alias.
func TestInputLLMOutputDataFlow(t *testing.T) {
	llm := &mockLLM{resp: map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"role": "assistant", "content": "analysis-text"}},
		},
		"usage": map[string]any{"prompt_tokens": 12, "total_tokens": 34},
	}}

	wf := &WorkflowDef{
		ID:   "wf_test",
		Name: "test",
		Nodes: []WorkflowNode{
			{ID: "in", Type: NodeInput, Title: "决策问题", Config: map[string]any{
				"fields": []any{map[string]any{"name": "problem"}},
			}},
			{ID: "llm1", Type: NodeLLM, Title: "选项分析", Config: map[string]any{
				"systemPrompt": "sys",
				"userPrompt":   "分析: {{in.problem}}",
			}},
			{ID: "out", Type: NodeOutput, Title: "结果", Config: map[string]any{
				"fields": []any{map[string]any{"name": "answer", "source": "{{llm1.output}}"}},
			}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "in", Target: "llm1"},
			{ID: "e2", Source: "llm1", Target: "out"},
		},
	}

	eng := NewEngine(wf, llm, nil, nil, noopEmitter{}, noopTracker{})
	eng.Run(map[string]any{"problem": "world"})

	if eng.execState.Status != ExecDone {
		t.Fatalf("exec status = %q, want done (err=%v)", eng.execState.Status, eng.execState.Error)
	}

	// Bug1: input node output must carry the real exec input value, not null.
	inOut, ok := eng.execState.NodeStates["in"].Output.(map[string]any)
	if !ok || inOut["problem"] != "world" {
		t.Fatalf("input node output = %#v, want problem=\"world\"", eng.execState.NodeStates["in"].Output)
	}

	// Bug3 (title alias): upstream nodes are also reachable in templates by Title.
	if alias, ok := eng.nodeOutputs["决策问题"].(map[string]any); !ok || alias["problem"] != "world" {
		t.Fatalf("title alias \"决策问题\" missing/wrong: %#v", eng.nodeOutputs["决策问题"])
	}

	// Bug2+Bug3: the LLM user message is the rendered userPrompt (template resolved).
	if got := llm.userMessage(); got != "分析: world" {
		t.Fatalf("LLM user message = %q, want %q", got, "分析: world")
	}

	// LLM node output is normalized to a stable {output, content, raw} shape.
	llmOut, ok := eng.execState.NodeStates["llm1"].Output.(map[string]any)
	if !ok || llmOut["output"] != "analysis-text" {
		t.Fatalf("llm node output = %#v, want output=\"analysis-text\"", eng.execState.NodeStates["llm1"].Output)
	}

	// Output node resolved {{llm1.output}} to the assistant text end-to-end.
	outOut, ok := eng.execState.NodeStates["out"].Output.(map[string]any)
	if !ok || outOut["answer"] != "analysis-text" {
		t.Fatalf("output node = %#v, want answer=\"analysis-text\"", eng.execState.NodeStates["out"].Output)
	}
}

// TestLLMPromptAlias verifies the LLM node accepts "prompt" as an alias for
// "userPrompt" — the user reported hardcoding a "prompt" field that never reached the API.
func TestLLMPromptAlias(t *testing.T) {
	llm := &mockLLM{resp: map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
	}}
	wf := &WorkflowDef{
		ID:   "wf_alias",
		Nodes: []WorkflowNode{
			{ID: "in", Type: NodeInput, Config: map[string]any{"fields": []any{map[string]any{"name": "x"}}}},
			{ID: "l", Type: NodeLLM, Config: map[string]any{"prompt": "hello {{in.x}}"}},
		},
		Edges: []WorkflowEdge{{ID: "e", Source: "in", Target: "l"}},
	}
	eng := NewEngine(wf, llm, nil, nil, noopEmitter{}, noopTracker{})
	eng.Run(map[string]any{"x": "there"})

	if got := llm.userMessage(); got != "hello there" {
		t.Fatalf("LLM user message via \"prompt\" alias = %q, want %q", got, "hello there")
	}
}

// TestNormalizeEdgesAssignsIDs verifies edges authored without an id (e.g. by the
// LLM via workflow_update) get a deterministic id, and existing ids are preserved.
func TestNormalizeEdgesAssignsIDs(t *testing.T) {
	edges := []WorkflowEdge{
		{Source: "n1", Target: "n2"},                         // no id, no handle
		{Source: "n2", Target: "n3", SourceHandle: "true"},   // no id, handle
		{ID: "custom", Source: "n3", Target: "n4"},           // has id — preserved
	}
	normalizeEdges(edges)
	if edges[0].ID != "n1→output→n2" {
		t.Errorf("edge0 id = %q, want n1→output→n2", edges[0].ID)
	}
	if edges[1].ID != "n2→true→n3" {
		t.Errorf("edge1 id = %q, want n2→true→n3", edges[1].ID)
	}
	if edges[2].ID != "custom" {
		t.Errorf("edge2 id = %q, want custom (preserved)", edges[2].ID)
	}
}
// (the engine injects them under bare keys too).
func TestBareInputReference(t *testing.T) {
	llm := &mockLLM{resp: map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
	}}
	wf := &WorkflowDef{
		ID:   "wf_bare",
		Nodes: []WorkflowNode{
			{ID: "in", Type: NodeInput, Config: map[string]any{"fields": []any{map[string]any{"name": "q"}}}},
			{ID: "l", Type: NodeLLM, Config: map[string]any{"userPrompt": "Q: {{q}}"}},
		},
		Edges: []WorkflowEdge{{ID: "e", Source: "in", Target: "l"}},
	}
	eng := NewEngine(wf, llm, nil, nil, noopEmitter{}, noopTracker{})
	eng.Run(map[string]any{"q": "meaning"})

	if got := llm.userMessage(); got != "Q: meaning" {
		t.Fatalf("bare {{q}} reference = %q, want %q", got, "Q: meaning")
	}
}

// --- agent node + output bare-path tests ---

type mockAgentRunner struct {
	lastID   string
	lastText string
	resp     string
}

func (m *mockAgentRunner) RunAgent(_ context.Context, agentID, userText string) (string, error) {
	m.lastID = agentID
	m.lastText = userText
	return m.resp, nil
}

// TestAgentNodeRunsRunner verifies the "agent" node type dispatches to the
// AgentRunner, resolves its prompt template, and normalizes output to
// {output, content, raw} like the LLM node.
func TestAgentNodeRunsRunner(t *testing.T) {
	runner := &mockAgentRunner{resp: "agent-reply"}
	wf := &WorkflowDef{
		ID: "wf_agent",
		Nodes: []WorkflowNode{
			{ID: "in", Type: NodeInput, Config: map[string]any{"fields": []any{map[string]any{"name": "q"}}}},
			{ID: "a", Type: NodeAgent, Title: "审计", Config: map[string]any{"agentId": "ag-1", "userPrompt": "do: {{in.q}}"}},
			{ID: "out", Type: NodeOutput, Config: map[string]any{"fields": []any{map[string]any{"name": "answer", "source": "a.output"}}}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "in", Target: "a"},
			{ID: "e2", Source: "a", Target: "out"},
		},
	}
	eng := NewEngine(wf, &mockLLM{resp: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "x"}}}}}, nil, nil, noopEmitter{}, noopTracker{})
	eng.SetAgentRunner(runner)
	eng.Run(map[string]any{"q": "stuff"})

	if eng.execState.Status != ExecDone {
		t.Fatalf("exec status = %q, want done (err=%v)", eng.execState.Status, eng.execState.Error)
	}
	if runner.lastID != "ag-1" || runner.lastText != "do: stuff" {
		t.Fatalf("agent runner got id=%q text=%q, want ag-1 / %q", runner.lastID, runner.lastText, "do: stuff")
	}
	aOut, ok := eng.execState.NodeStates["a"].Output.(map[string]any)
	if !ok || aOut["output"] != "agent-reply" {
		t.Fatalf("agent node output = %#v, want output=%q", eng.execState.NodeStates["a"].Output, "agent-reply")
	}
}

// TestOutputBarePathResolves verifies the output node resolves a BARE "node.field"
// source (no {{}} braces) — the regression the user reported — falling back to
// the literal only when the path is invalid.
func TestOutputBarePathResolves(t *testing.T) {
	wf := &WorkflowDef{
		ID: "wf_bareout",
		Nodes: []WorkflowNode{
			{ID: "llm1", Type: NodeLLM, Config: map[string]any{"userPrompt": "hi"}},
			{ID: "out", Type: NodeOutput, Config: map[string]any{"fields": []any{
				map[string]any{"name": "resolved", "source": "llm1.output"},   // bare path → resolves
				map[string]any{"name": "literal", "source": "plain text"},    // not a path → literal kept
			}}},
		},
		Edges: []WorkflowEdge{{ID: "e", Source: "llm1", Target: "out"}},
	}
	llm := &mockLLM{resp: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "analysis-text"}}}}}
	eng := NewEngine(wf, llm, nil, nil, noopEmitter{}, noopTracker{})
	eng.Run(nil)

	out, ok := eng.execState.NodeStates["out"].Output.(map[string]any)
	if !ok {
		t.Fatalf("output node = %#v, want map", eng.execState.NodeStates["out"].Output)
	}
	if out["resolved"] != "analysis-text" {
		t.Errorf("bare path llm1.output = %#v, want %q", out["resolved"], "analysis-text")
	}
	if out["literal"] != "plain text" {
		t.Errorf("non-path source = %#v, want literal %q", out["literal"], "plain text")
	}
}
