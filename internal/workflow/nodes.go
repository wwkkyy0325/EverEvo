package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// agentNodeTimeout is the per-agent-node execution budget, mirrored in
// Engine.executeNode's timeout switch. Defined here so executeAgent can derive a
// matching deadline context: without it, runAgentLoop would run on eng.ctx
// (canceled only on engine Cancel/shutdown) and keep going after the node's
// timeout fires.
const agentNodeTimeout = 300 * time.Second

// ─── Node Executor Interface ─────────────────────────────────────

// NodeExecutor executes a single node and returns its output.
// The engine passes itself so executors can access shared state.
type NodeExecutor func(eng *Engine, node *WorkflowNode) (any, error)

// GetNodeExecutor returns the executor function for the given node type.
func GetNodeExecutor(t NodeType) (NodeExecutor, bool) {
	switch t {
	case NodeInput:
		return executeInput, true
	case NodeLLM:
		return executeLLM, true
	case NodeTool:
		return executeTool, true
	case NodeCondition:
		return executeCondition, true
	case NodeCode:
		return executeCode, true
	case NodeLoop:
		return executeLoop, true
	case NodeAgent:
		return executeAgent, true
	case NodeOutput:
		return executeOutput, true
	case NodeParallel:
		return executeParallel, true
	case NodeMap:
		return executeMap, true
	default:
		return nil, false
	}
}

// ─── Individual Executors ────────────────────────────────────────

func executeInput(eng *Engine, node *WorkflowNode) (any, error) {
	fields, _ := getSliceMap(node.Config, "fields")
	out := make(map[string]any, len(fields))
	// Start from declared fields with their configured defaults.
	for _, f := range fields {
		name := getString(f, "name")
		if name == "" {
			continue
		}
		out[name] = f["default"]
	}
	// Override with the actual execution inputs (real values win over defaults)
	// and pass through any extra inputs not declared as fields.
	for k, v := range eng.inputs {
		out[k] = v
	}
	return out, nil
}

func executeLLM(eng *Engine, node *WorkflowNode) (any, error) {
	systemPrompt := getString(node.Config, "systemPrompt")
	userPrompt := getString(node.Config, "userPrompt")
	if userPrompt == "" {
		// Accept "prompt" as an alias — authored workflows often use it.
		userPrompt = getString(node.Config, "prompt")
	}
	temperature := 0.7
	if t, ok := getFloat(node.Config, "temperature"); ok {
		temperature = t
	}

	resolved, err := ResolveTemplate(userPrompt, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve userPrompt: %w", err)
	}
	resolvedSystem, err := ResolveTemplate(systemPrompt, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve systemPrompt: %w", err)
	}

	messages := []map[string]any{
		{"role": "system", "content": resolvedSystem},
		{"role": "user", "content": resolved},
	}

	// Build tools array if requested
	toolNames := getStringSlice(node.Config, "tools")
	var toolsJSON json.RawMessage
	if len(toolNames) > 0 && eng.toolProvider != nil {
		tools := eng.toolProvider.GetToolDefs(toolNames)
		if len(tools) > 0 {
			type toolFn struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  any    `json:"parameters"`
			}
			type toolWrap struct {
				Type     string `json:"type"`
				Function toolFn `json:"function"`
			}
			var wrapped []toolWrap
			for _, t := range tools {
				params := t.Parameters
				if len(t.RawParameters) > 0 {
					params = t.RawParameters
				}
				wrapped = append(wrapped, toolWrap{
					Type: "function",
					Function: toolFn{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  params,
					},
				})
			}
			toolsJSON, _ = json.Marshal(wrapped)
		}
	}

	// Build request body
	body := map[string]any{
		"messages":    messages,
		"temperature": temperature,
		"stream":      false,
	}
	if toolsJSON != nil {
		body["tools"] = json.RawMessage(toolsJSON)
		body["tool_choice"] = "auto"
	}

	messagesJSON, _ := json.Marshal(messages)
	var tJSON json.RawMessage
	if toolsJSON != nil {
		tJSON = toolsJSON
	}

	_ = temperature
	log.Printf("[workflow] LLM node %s: calling ChatStream (%d messages, %d tools)", node.ID, len(messages), len(toolNames))

	// Stream partial text to the UI via the progress channel, throttled so a
	// fast token stream doesn't flood the event bridge. The final assembled
	// text still comes from `result` below.
	var sb strings.Builder
	var lastEmit time.Time
	onChunk := func(text string) {
		if text == "" {
			return
		}
		sb.WriteString(text)
		if time.Since(lastEmit) >= 100*time.Millisecond {
			lastEmit = time.Now()
			cum := sb.String()
			if len(cum) > 400 {
				cum = "…" + cum[len(cum)-400:]
			}
			eng.emitProgress(node.ID, cum)
		}
	}

	result, err := eng.llmCaller.ChatStream(eng.ctx, messagesJSON, tJSON, onChunk)
	if err != nil {
		return nil, fmt.Errorf("ChatStream: %w", err)
	}
	// Final flush so the UI shows the complete text before the node finishes.
	if cum := sb.String(); cum != "" {
		eng.emitProgress(node.ID, cum)
	}
	// Normalize: expose the assistant text under a stable "output" field so
	// downstream nodes can reference it as {{llmNode.output}} without digging
	// into the raw API response shape (choices[0].message.content). The full
	// response is kept under "raw" for advanced use.
	text := extractAssistantText(result)
	return map[string]any{
		"output":  text,
		"content": text,
		"raw":     result,
	}, nil
}

// extractAssistantText pulls the assistant message text out of an OpenAI-shaped
// ChatProxy response ({choices:[{message:{content}}]}). Returns "" if absent.
func extractAssistantText(resp map[string]any) string {
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := first["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
}

func executeTool(eng *Engine, node *WorkflowNode) (any, error) {
	toolName := getString(node.Config, "toolName")
	if toolName == "" {
		return nil, fmt.Errorf("toolName is required")
	}

	// Block orchestration / dangerous tools to prevent unbounded recursion and
	// privilege escalation from a workflow Tool node. A workflow must not be able
	// to spawn another workflow (workflow_execute), delegate to agents that
	// themselves recurse (agent_run), rewrite its own definition (workflow_*),
	// or invoke destructive system tools (shell_exec/evolve_swap/zone_*).
	if isWorkflowBlockedTool(toolName) {
		return nil, fmt.Errorf("工具节点禁止调用编排/危险工具: %s", toolName)
	}

	rawParams := getMap(node.Config, "params")
	params, err := ResolveMap(rawParams, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve params: %w", err)
	}
	// Auto-parse JSON-stringified values so arrays/objects survive serialization.
	for k, v := range params {
		if s, ok := v.(string); ok {
			trimmed := strings.TrimSpace(s)
			if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
				var parsed any
				if json.Unmarshal([]byte(trimmed), &parsed) == nil {
					params[k] = parsed
				}
			}
		}
	}

	log.Printf("[workflow] Tool node %s: calling %s", node.ID, toolName)
	result := eng.toolCaller.CallTool(toolName, params)
	return map[string]any{
		"success": result.Success,
		"data":    string(result.Data),
		"error":   result.Error,
	}, nil
}

func executeCondition(eng *Engine, node *WorkflowNode) (any, error) {
	expr := getString(node.Config, "expression")
	if expr == "" {
		return nil, fmt.Errorf("expression is required")
	}

	resolved, err := ResolveTemplate(expr, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve expression: %w", err)
	}

	result, err := EvaluateCondition(resolved, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}

	branch := "false"
	if result {
		branch = "true"
	}
	log.Printf("[workflow] Condition %s: %s → %s", node.ID, resolved, branch)
	return map[string]any{"result": result, "branch": branch}, nil
}

func executeCode(eng *Engine, node *WorkflowNode) (any, error) {
	tmpl := getString(node.Config, "template")
	if tmpl == "" {
		return nil, fmt.Errorf("template is required")
	}

	resolved, err := ResolveTemplate(tmpl, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve template: %w", err)
	}

	// Auto-parse JSON strings so arrays/objects don't get double-stringified.
	if trimmed := strings.TrimSpace(resolved); len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		var parsed any
		if json.Unmarshal([]byte(trimmed), &parsed) == nil {
			outputField := getString(node.Config, "outputField")
			if outputField == "" {
				outputField = "output"
			}
			return map[string]any{outputField: parsed}, nil
		}
	}

	outputField := getString(node.Config, "outputField")
	if outputField == "" {
		outputField = "output"
	}
	return map[string]any{outputField: resolved}, nil
}

func executeLoop(eng *Engine, node *WorkflowNode) (any, error) {
	sourceExpr := getString(node.Config, "sourceExpression")
	itemVar := getString(node.Config, "itemVariable")
	if itemVar == "" {
		itemVar = "item"
	}
	maxIter := 100
	if n, ok := getInt(node.Config, "maxIterations"); ok {
		maxIter = n
	}
	// Hard cap to prevent context/memory explosion from LLM-authored loops.
	const loopHardCap = 100
	if maxIter > loopHardCap {
		maxIter = loopHardCap
	}
	if maxIter <= 0 {
		maxIter = 1
	}

	raw, err := ResolveExpression(sourceExpr, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}

	// Auto-parse JSON-stringified arrays (common when params flow through serialization).
	if s, ok := raw.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			raw = parsed
		}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("source is not an array: %T", raw)
	}

	subIDs := getStringSlice(node.Config, "subNodeIDs")
	if len(subIDs) == 0 {
		return nil, fmt.Errorf("subNodeIDs is required for loop")
	}

	// Build sub-graph
	subSet := make(map[string]bool, len(subIDs))
	for _, id := range subIDs {
		subSet[id] = true
	}

	var results []any
	for i, item := range arr {
		if i >= maxIter {
			break
		}
		// Inject loop variable
		eng.nodeOutputs[itemVar] = item

		// Execute sub-nodes in topological order (filtered to sub-graph)
		subOrder := filterTopo(eng.topoOrder, subSet)
		for _, nid := range subOrder {
			subNode := eng.wf.NodeByID(nid)
			if subNode == nil {
				continue
			}
			eng.emitNodeStart(nid, subNode.Title)
			output, err := eng.executeNode(subNode)
			if err != nil {
				eng.emitNodeError(nid, subNode.Title, err.Error())
				return nil, fmt.Errorf("loop iteration %d, node %s: %w", i, nid, err)
			}
			eng.nodeOutputs[nid] = output
			eng.emitNodeDone(nid, subNode.Title, output)
		}

		// Collect outputs from sub-graph
		iterOut := make(map[string]any)
		for _, nid := range subIDs {
			iterOut[nid] = eng.nodeOutputs[nid]
		}
		results = append(results, iterOut)
	}

	// Clean up loop variable
	delete(eng.nodeOutputs, itemVar)
	return results, nil
}

// executeAgent runs a local Agent persona on a task. It reads agentId + a prompt
// (userPrompt, or "prompt" as an alias), resolves the prompt template against
// upstream outputs, delegates to the AgentRunner (App.runAgentLoop), and returns
// the agent's final text in the same {output, content, raw} shape as executeLLM
// so downstream nodes can reference {{agentNode.output}}.
func executeAgent(eng *Engine, node *WorkflowNode) (any, error) {
	agentID := getString(node.Config, "agentId")
	if agentID == "" {
		return nil, fmt.Errorf("agentId is required")
	}
	userPrompt := getString(node.Config, "userPrompt")
	if userPrompt == "" {
		// Accept "prompt" as an alias, mirroring the LLM node.
		userPrompt = getString(node.Config, "prompt")
	}
	resolved, err := ResolveTemplate(userPrompt, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve prompt: %w", err)
	}
	if eng.agentRunner == nil {
		return nil, fmt.Errorf("agent runner not configured")
	}
	log.Printf("[workflow] Agent node %s: running agent %s", node.ID, agentID)
	// Derive a deadline matching the node timeout so runAgentLoop's between-round
	// ctx.Done() check actually stops the agent when the node times out. eng.ctx
	// alone is only canceled on engine Cancel/shutdown — a node timeout would
	// otherwise leave the agent running for its remaining rounds.
	ctx, cancel := context.WithTimeout(eng.ctx, agentNodeTimeout)
	defer cancel()
	text, err := eng.agentRunner.RunAgent(ctx, agentID, resolved)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", agentID, err)
	}
	return map[string]any{
		"output":  text,
		"content": text,
		"raw":     text,
	}, nil
}

// parallelConcurrency caps how many branches/items run at once.
const parallelConcurrency = 4

// executeParallel runs multiple named branches concurrently, gathering each
// branch's final sub-node output into a map keyed by alias. Each branch gets
// an isolated copy of nodeOutputs (snapshot at dispatch) so concurrent writes
// don't race; results are merged back after all branches finish.
//
// config:
//
//	branches: [{ nodeId, alias }]   alias defaults to nodeId
//	failFast: bool (default false — collect all, including errors)
func executeParallel(eng *Engine, node *WorkflowNode) (any, error) {
	rawBranches, _ := node.Config["branches"].([]any)
	if len(rawBranches) == 0 {
		return nil, fmt.Errorf("parallel node requires branches")
	}
	type branch struct {
		nodeID string
		alias  string
	}
	var branches []branch
	for _, rb := range rawBranches {
		m, ok := rb.(map[string]any)
		if !ok {
			continue
		}
		nid, _ := m["nodeId"].(string)
		alias, _ := m["alias"].(string)
		if alias == "" {
			alias = nid
		}
		if nid != "" {
			branches = append(branches, branch{nodeID: nid, alias: alias})
		}
	}
	if len(branches) == 0 {
		return nil, fmt.Errorf("parallel node has no valid branches")
	}

	results := make(map[string]any, len(branches))
	errs := make(map[string]string, len(branches))
	var mu sync.Mutex
	sem := make(chan struct{}, parallelConcurrency)
	var wg sync.WaitGroup

	for _, b := range branches {
		wg.Add(1)
		go func(br branch) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			subNode := eng.wf.NodeByID(br.nodeID)
			if subNode == nil {
				mu.Lock()
				errs[br.alias] = "node not found: " + br.nodeID
				mu.Unlock()
				return
			}
			eng.emitNodeStart(br.nodeID, subNode.Title)
			output, err := eng.executeNode(subNode)
			mu.Lock()
			if err != nil {
				errs[br.alias] = err.Error()
			} else {
				results[br.alias] = output
			}
			mu.Unlock()
			if err == nil {
				// Publish to nodeOutputs under the sub-node ID so downstream
				// nodes can reference it (write under the engine lock).
				eng.SetOutput(br.nodeID, output)
			}
			if err != nil {
				eng.emitNodeError(br.nodeID, subNode.Title, err.Error())
			} else {
				eng.emitNodeDone(br.nodeID, subNode.Title, output)
			}
		}(b)
	}
	wg.Wait()

	out := map[string]any{"results": results}
	if len(errs) > 0 {
		out["errors"] = errs
	}
	return out, nil
}

// executeMap runs a sub-graph concurrently over each element of an array.
// It is the concurrent counterpart of executeLoop. Each iteration gets an
// isolated nodeOutputs snapshot seeded with the item variable; per-iteration
// results are collected into an output array.
//
// config:
//
//	sourceExpression: template resolving to an array
//	itemVariable: name for the current item (default "item")
//	subNodeIDs: sub-graph node IDs to run per item
//	concurrency: optional cap (default 4)
func executeMap(eng *Engine, node *WorkflowNode) (any, error) {
	sourceExpr := getString(node.Config, "sourceExpression")
	itemVar := getString(node.Config, "itemVariable")
	if itemVar == "" {
		itemVar = "item"
	}
	subIDs := getStringSlice(node.Config, "subNodeIDs")
	if len(subIDs) == 0 {
		return nil, fmt.Errorf("map node requires subNodeIDs")
	}

	raw, err := ResolveExpression(sourceExpr, eng.nodeOutputs)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	if s, ok := raw.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			raw = parsed
		}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("source is not an array: %T", raw)
	}

	conc := parallelConcurrency
	if n, ok := getInt(node.Config, "concurrency"); ok && n > 0 && n < conc {
		conc = n
	}

	subSet := make(map[string]bool, len(subIDs))
	for _, id := range subIDs {
		subSet[id] = true
	}
	subOrder := filterTopo(eng.topoOrder, subSet)

	type iterResult struct {
		index  int
		output map[string]any
		err    string
	}
	results := make([]map[string]any, len(arr))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i, item := range arr {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Isolated per-iteration view: snapshot + inject item.
			iterView := eng.SnapshotOutputs()
			iterView[itemVar] = it

			iterOut := map[string]any{"index": idx, itemVar: it}
			eng.WithOutputs(iterView, func() {
				for _, nid := range subOrder {
					subNode := eng.wf.NodeByID(nid)
					if subNode == nil {
						continue
					}
					output, err := eng.executeNode(subNode)
					if err != nil {
						results[idx] = map[string]any{"index": idx, "error": err.Error()}
						return
					}
					iterView[nid] = output
					iterOut[nid] = output
				}
				results[idx] = iterOut
			})
		}(i, item)
	}
	wg.Wait()

	// Flatten to a plain slice for downstream consumption.
	out := make([]any, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out, nil
}

func executeOutput(eng *Engine, node *WorkflowNode) (any, error) {
	// "Incoming" is the output of the active (non-skipped) predecessor wired into
	// this output node. For a branch converge (condition → A/B → output) it is
	// whichever branch actually ran — so an output can collect the active branch
	// without hand-picking a source.
	var incoming any
	for _, e := range eng.wf.InEdges(node.ID) {
		if eng.skipped[e.Source] {
			continue
		}
		if out, ok := eng.nodeOutputs[e.Source]; ok {
			incoming = out
			break
		}
	}

	fields, _ := getSliceMap(node.Config, "fields")
	// No fields configured → pass the active incoming output through wholesale.
	if len(fields) == 0 {
		return incoming, nil
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		name := getString(f, "name")
		if name == "" {
			continue
		}
		source := getString(f, "source")
		// Empty / "@incoming" source → the active branch's output (auto).
		if source == "" || source == "@incoming" {
			out[name] = incoming
			continue
		}
		var resolved any
		var err error
		if HasTemplates(source) {
			// "{{node.field}}" (possibly embedded in surrounding text) → template resolver
			resolved, err = ResolveExpression(source, eng.nodeOutputs)
		} else {
			// Bare "node.field" path → resolve directly. If it isn't a valid path,
			// fall back to the literal string so static text values still pass through.
			resolved, err = resolvePath(source, eng.nodeOutputs)
			if err != nil {
				resolved = source
				err = nil
			}
		}
		if err != nil {
			out[name] = fmt.Sprintf("ERROR: %v", err)
			continue
		}
		out[name] = resolved
	}
	return out, nil
}

// ─── Config Helpers ──────────────────────────────────────────────

func getString(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return v
}

func getStringSlice(cfg map[string]any, key string) []string {
	raw, ok := cfg[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func getMap(cfg map[string]any, key string) map[string]any {
	m, _ := cfg[key].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func getSliceMap(cfg map[string]any, key string) ([]map[string]any, bool) {
	raw, ok := cfg[key]
	if !ok {
		return nil, false
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	var out []map[string]any
	for _, v := range arr {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, true
}

func getFloat(cfg map[string]any, key string) (float64, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, _ := n.Float64()
		return f, true
	}
	return 0, false
}

func getInt(cfg map[string]any, key string) (int, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, _ := n.Int64()
		return int(i), true
	}
	return 0, false
}

func filterTopo(order []string, include map[string]bool) []string {
	var out []string
	for _, id := range order {
		if include[id] {
			out = append(out, id)
		}
	}
	return out
}

// ─── Edge helpers for engine ─────────────────────────────────────

// skippedByCondition returns node IDs that should be skipped because
// a condition node took a branch that doesn't lead to them.
func computeSkipped(wf *WorkflowDef, condNodeID string, takenBranch string) map[string]bool {
	skipped := make(map[string]bool)

	// Find the edge for the NOT-taken branch
	notTaken := "true"
	if takenBranch == "true" {
		notTaken = "false"
	}

	// Collect all nodes reachable from the not-taken branch
	var notTakenTargets []string
	for _, e := range wf.OutEdges(condNodeID) {
		if e.SourceHandle == notTaken {
			notTakenTargets = append(notTakenTargets, e.Target)
		}
	}

	// BFS from not-taken targets
	visited := make(map[string]bool)
	queue := append([]string{}, notTakenTargets...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		skipped[id] = true
		for _, e := range wf.OutEdges(id) {
			if !visited[e.Target] {
				queue = append(queue, e.Target)
			}
		}
	}

	// Don't skip nodes that are ALSO reachable from the taken branch
	takenTargets := make(map[string]bool)
	for _, e := range wf.OutEdges(condNodeID) {
		if e.SourceHandle == takenBranch {
			takenTargets[e.Target] = true
		}
	}
	queue = nil
	for t := range takenTargets {
		queue = append(queue, t)
	}
	visited = make(map[string]bool)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		delete(skipped, id) // reachable from taken branch → not skipped
		for _, e := range wf.OutEdges(id) {
			if !visited[e.Target] {
				queue = append(queue, e.Target)
			}
		}
	}

	return skipped
}

// topologicalSort returns a linear order of node IDs, or an error on cycles.
func topologicalSort(wf *WorkflowDef) ([]string, error) {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for _, node := range wf.Nodes {
		inDegree[node.ID] = 0
		adj[node.ID] = nil
	}
	for _, edge := range wf.Edges {
		adj[edge.Source] = append(adj[edge.Source], edge.Target)
		inDegree[edge.Target]++
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, t := range adj[id] {
			inDegree[t]--
			if inDegree[t] == 0 {
				queue = append(queue, t)
			}
		}
	}

	if len(order) != len(wf.Nodes) {
		return nil, fmt.Errorf("cycle detected in workflow graph")
	}
	return order, nil
}

// ─── Interfaces for engine dependencies ──────────────────────────

// LLMCaller abstracts the LLM API call (implemented by App.ChatProxy).
type LLMCaller interface {
	ChatProxy(messagesJSON, toolsJSON json.RawMessage) (map[string]any, error)
	// ChatStream is like ChatProxy but delivers partial text to onChunk as it
	// arrives. Implementations may fall back to non-streaming (calling onChunk
	// once with the full text, or not at all) when the provider can't stream.
	ChatStream(ctx context.Context, messagesJSON, toolsJSON json.RawMessage, onChunk func(string)) (map[string]any, error)
}

// AgentRunner abstracts running a local Agent persona on a task (implemented
// by App via runAgentLoop). Used by the "agent" workflow node type.
type AgentRunner interface {
	RunAgent(ctx context.Context, agentID, userText string) (string, error)
}

// ToolCaller abstracts the tool dispatch (implemented by App.CallTool).
type ToolCaller interface {
	CallTool(name string, params map[string]any) ToolResult
}

// ToolResult mirrors tools.ToolResult to avoid a circular import.
// Must stay field-identical with tools.ToolResult.
type ToolResult struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ToolProvider provides tool definitions for LLM function calling.
type ToolProvider interface {
	GetToolDefs(names []string) []ToolDefInfo
}

// ToolDefInfo is a lightweight tool definition for the engine.
type ToolDefInfo struct {
	Name          string
	Description   string
	Parameters    any
	RawParameters json.RawMessage // preserved MCP inputSchema when available
}

// EventEmitter emits workflow execution events.
type EventEmitter interface {
	Emit(event string, data any)
}

// contextKey is used for engine context values.
type contextKey string

var engineKey contextKey = "engine"
