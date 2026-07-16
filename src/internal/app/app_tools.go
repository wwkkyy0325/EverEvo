//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
agentPlugin "everevo/internal/plugins/tools/agents"

	"everevo/internal/a2a"
	"everevo/internal/agents"
	"everevo/internal/core"
	"everevo/internal/guides"
	mcpclient "everevo/internal/mcp/client"
	"everevo/internal/tools"
	"everevo/internal/workflow"
)

// ─── LLM 工具封装 API ──────────────────────────────────────────

// toolHandlers maps tool names to their handler functions.
// Populated by registerToolHandlers() which is called during startup.
var toolHandlers map[string]func(a *App, params map[string]any) tools.ToolResult

func init() {
	toolHandlers = map[string]func(a *App, params map[string]any) tools.ToolResult{
		// ── knowledge base ──
		"kb_list":         hKBList,
		"kb_create":       hKBCreate,
		"kb_add_texts":    hKBAddTexts,
		"kb_search":       hKBSearch,
		"kb_delete":       hKBDelete,
		"kb_clear":        hKBClear,
		"kb_set_library":  hKBSetLibrary,
		"kb_list_docs":    hKBListDocs,
		"read_file":        hReadFile,
		"read_media_file":  hReadMediaFile,
		"kb_delete_chunks": hKBDeleteChunks,

		// ── download cleanup ──
		"download_delete_file": hDownloadDeleteFile,
		"download_delete_dir": hDownloadDeleteDir,

		// ── system ──
		"system_info":    hSystemInfo,
		"system_dynamic": hSystemDynamic,
		"system_backends": hSystemBackends,
		"system_config":   hSystemConfig,

		// ── guide ──
		"guide_list":    hGuideList,
		"guide_read":    hGuideRead,
		"guide_search":  hGuideSearch,
		"guide_sync":    hGuideSync,
		"guide_sources": hGuideSources,

		// ── workflow ──
		"workflow_list":     hWorkflowList,
		"workflow_get":      hWorkflowGet,
		"workflow_create":   hWorkflowCreate,
		"workflow_update":   hWorkflowUpdate,
		"workflow_delete":   hWorkflowDelete,
		"workflow_execute":  hWorkflowExecute,
		"workflow_status":   hWorkflowStatus,
		"workflow_validate": hWorkflowValidate,

		// ── mcp ──
		"mcp_list_servers":     hMCPListServers,
		"mcp_add_server":       hMCPAddServer,
		"mcp_remove_server":    hMCPRemoveServer,
		"mcp_connect_server":   hMCPConnectServer,
		"mcp_disconnect_server": hMCPDisconnectServer,
		"mcp_get_server_tools": hMCPGetServerTools,
		"mcp_status":           hMCPStatus,

		// ── a2a ──
		"a2a_list_agents":      hA2AListAgents,
		"a2a_connect_agent":    hA2AConnectAgent,
		"a2a_disconnect_agent": hA2ADisconnectAgent,
		"a2a_send_to_agent":    hA2ASendToAgent,
		"a2a_agent_status":     hA2AAgentStatus,

		// ── local agent (personas) ──
		"agent_list":                hAgentList,
		"agent_create":              hAgentCreate,
		"agent_run":                 hAgentRun,
			"agent_run_async":           hAgentRunAsync,
		"library_list":                  hLibraryList,
		"agent_delegate_to_domain":      hAgentDelegateToDomain,
		"agent_delegate_multi_domain":   hAgentDelegateMultiDomain,
		"agent_synthesize_tool":         hAgentSynthesizeTool,
		"agent_set_library":             hAgentSetLibrary,

		// ── proxy ──
		"proxy_get":    hProxyGet,
		"proxy_set":    hProxySet,
		"proxy_test":   hProxyTest,
		"proxy_toggle": hProxyToggle,

		// ── shell ──
		"shell_exec":      hShellExec,
		"runtime_install": hRuntimeInstall,
		"system_diag":     hSystemDiag,
		"web_search":      hWebSearch,
		"web_fetch":       hWebFetch,

		// ── context: lazy tool discovery ──
			"codebase_import":    hCodebaseImport,
			"domain_search":       hDomainSearch,
		"tool_search": hToolSearch,
		// ── paradigm ──
		"paradigm_list":     hParadigmList,
		"paradigm_select":   hParadigmSelect,
		"paradigm_feedback": hParadigmFeedback,
		"paradigm_refine":    hParadigmRefine,
		"paradigm_distill":   hParadigmDistill,
		"paradigm_match":     hParadigmMatch,
	}
}

// ─── Lazy tool discovery ─────────────────────────────────────────

// hToolSearch implements the tool_search meta-tool for on-demand schema loading.
// The LLM calls this to discover and fetch full tool schemas instead of having
// all ~110 tool definitions loaded in every request.
func hToolSearch(a *App, p map[string]any) tools.ToolResult {
	query := tools.GetString(p, "query")
	category := tools.GetString(p, "category")

	results := tools.SearchTools(query, category)

	// Cache fetched schemas for the remainder of this turn
	for _, t := range results {
		tools.CacheSchema(t)
	}

	type searchResult struct {
		Query    string          `json:"query"`
		Category string          `json:"category,omitempty"`
		Tools    []*tools.ToolDef `json:"tools"`
		Count    int             `json:"count"`
		Hint     string          `json:"hint,omitempty"`
	}

	sr := searchResult{
		Query: query,
		Category: category,
		Tools: results,
		Count: len(results),
	}
	if len(results) == 0 {
		sr.Hint = "No tools matched. Try a broader query, use '*' to list all, or specify a category."
	} else if len(results) >= 15 {
		sr.Hint = "Results capped at 15. Use a more specific query or category filter for narrower results."
	}

	return tools.OkResult(sr)
}

// ListTools returns all LLM-callable tool definitions (for MCP tools/list).
func (a *App) ListTools() []*tools.ToolDef { return tools.List() }

// ListToolsLazy returns core tools only — the lightweight set sent to the LLM
// in every request. Full tool schemas are fetched on-demand via tool_search.
func (a *App) ListToolsLazy() []*tools.ToolDef { return tools.CoreToolsDef() }

// ─── Plugin tool dispatch ────────────────────────────────────────

// pluginToolMap maps tool names to core.ToolPlugin instances that handle them.
// Populated at startup by BuildPluginToolMap(). When a tool name is found here,
// CallTool dispatches through the plugin engine instead of the legacy toolHandlers.
var pluginToolMap map[string]core.ToolPlugin

// BuildPluginToolMap scans all registered core.ToolPlugin instances and indexes
// their tools by name. Call once during startup after plugins have registered.
func BuildPluginToolMap() {
	pluginToolMap = map[string]core.ToolPlugin{}
	for _, entry := range core.GlobalTools.List() {
		tp, ok := entry.Plugin.(core.ToolPlugin)
		if !ok {
			continue
		}
		for _, td := range tp.ToolDefs() {
			pluginToolMap[td.Name] = tp
		}
	}
}

// callToolViaPlugins tries to dispatch a tool call through registered core.ToolPlugin
// instances. Returns the result and true if a plugin handled it, or zero/ false.
// If a plugin indicates it is not yet wired (delegate not set), false is returned
// so the caller can fall through to legacy handlers.
func callToolViaPlugins(name string, params map[string]any) (tools.ToolResult, bool) {
	if pluginToolMap == nil {
		return tools.ToolResult{}, false
	}
	tp, ok := pluginToolMap[name]
	if !ok {
		return tools.ToolResult{}, false
	}
	result, err := tp.CallTool(context.Background(), name, params)
	if err != nil {
		return tools.ErrResult(err), true
	}
	// If the plugin is not wired yet, fall through to legacy handlers so
	// tools continue to work while plugins are being gradually migrated.
	if !result.Success && (strings.Contains(result.Error, "delegate not wired") ||
		strings.Contains(result.Error, "not configured") ||
		strings.Contains(result.Error, "service not configured")) {
		return tools.ToolResult{}, false
	}
	data, _ := json.Marshal(result.Data)
	return tools.ToolResult{Success: result.Success, Data: data, Error: result.Error}, true
}

// CallTool dispatches a named tool call from the LLM to the appropriate backend method.
// Priority: plugin engine → legacy toolHandlers → external MCP server tools.
// Unwired plugins fall through to legacy handlers for backward compatibility.
// Tool results are automatically truncated per the tool's OutputPolicy.
func (a *App) CallTool(name string, params map[string]any) tools.ToolResult {
	var result tools.ToolResult
	if tr, ok := callToolViaPlugins(name, params); ok {
		result = tr
	} else if h, ok := toolHandlers[name]; ok {
		result = h(a, params)
	} else if tools.IsExternal(name) && a.mcpClient != nil {
		// Fallback: try external MCP server tools
		r, err := a.mcpClient.CallTool(name, params)
		if err != nil {
			return tools.ErrResult(err)
		}
		if r != nil {
			result = *r
		}
	} else {
		return tools.ErrMsg("未知工具: " + name)
	}

	// Apply output truncation for large results
	if compacted, did := tools.CompactResult(name, result); did {
		return compacted
	}
	return result
}

// ─── KB handlers ──────────────────────────────────────────────

func hKBList(a *App, _ map[string]any) tools.ToolResult {
	kbs, err := a.ListKnowledgeBases("")
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(kbs)
}

func hKBCreate(a *App, p map[string]any) tools.ToolResult {
	name, modelDir := tools.GetString(p, "name"), tools.GetString(p, "modelDir")
	if name == "" || modelDir == "" {
		return tools.ErrMsg("缺少必填参数: name, modelDir")
	}
	libraryID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	kb, err := a.CreateKnowledgeBase(name, modelDir, libraryID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(kb)
}

func hKBAddTexts(a *App, p map[string]any) tools.ToolResult {
	kbID, texts := tools.GetString(p, "kbId"), tools.GetStringSlice(p, "texts")
	if kbID == "" || len(texts) == 0 {
		return tools.ErrMsg("缺少必填参数: kbId, texts")
	}
	n, err := a.AddTexts(kbID, texts, tools.GetMap(p, "metadata"))
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]int{"added": n})
}

func hKBSearch(a *App, p map[string]any) tools.ToolResult {
	kbID, query := tools.GetString(p, "kbId"), tools.GetString(p, "query")
	if kbID == "" || query == "" {
		return tools.ErrMsg("缺少必填参数: kbId, query")
	}
	k := tools.GetInt(p, "k")
	if k <= 0 {
		k = 5
	}
	results, err := a.SearchKnowledge(kbID, query, k, tools.GetMap(p, "filter"))
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(results)
}

func hKBDelete(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.DeleteKnowledgeBase(tools.GetString(p, "kbId")))
}

func hKBClear(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.ClearKnowledgeBase(tools.GetString(p, "kbId")))
}

func hKBSetLibrary(a *App, p map[string]any) tools.ToolResult {
	kbID := tools.GetString(p, "kbId")
	libraryID := tools.GetString(p, "libraryId")
	if kbID == "" {
		return tools.ErrMsg("缺少必填参数: kbId")
	}
	if err := a.SetKnowledgeBaseLibrary(kbID, libraryID); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"status": "ok"})
}

func hKBListDocs(a *App, p map[string]any) tools.ToolResult {
	docs, err := a.ListKBDocuments(tools.GetString(p, "kbId"))
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(docs)
}

func hReadFile(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("缺少必填参数: path")
	}
	content, isScanned, err := a.ReadChatFile(path)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"content":   content,
		"isScanned": isScanned,
		"path":      path,
	})
}

func hReadMediaFile(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("缺少必填参数: path")
	}
	result, err := a.ReadMediaFile(path)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hKBDeleteChunks(a *App, p map[string]any) tools.ToolResult {
	kbID, ids := tools.GetString(p, "kbId"), tools.GetStringSlice(p, "ids")
	if kbID == "" || len(ids) == 0 {
		return tools.ErrMsg("缺少必填参数: kbId, ids")
	}
	n, err := a.DeleteKBChunks(kbID, ids)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]int{"deleted": n})
}

func hDownloadDeleteFile(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.DeleteDownloadedFile(tools.GetString(p, "relPath")))
}

func hDownloadDeleteDir(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.DeleteDownloadedDir(tools.GetString(p, "dirName")))
}

// ─── System handlers ──────────────────────────────────────────

func hSystemInfo(a *App, _ map[string]any) tools.ToolResult    { return tools.OkResult(a.GetSysInfo()) }
func hSystemDynamic(a *App, _ map[string]any) tools.ToolResult { return tools.OkResult(a.GetDynamicInfo()) }
func hSystemBackends(a *App, _ map[string]any) tools.ToolResult { return tools.OkResult(a.GetBackends()) }
func hSystemConfig(a *App, _ map[string]any) tools.ToolResult  { return tools.OkResult(a.GetConfig()) }

// ─── Guide handlers ───────────────────────────────────────────

func hGuideList(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.guideManager.SearchGuides(tools.GetString(p, "query")))
}

func hGuideRead(a *App, p map[string]any) tools.ToolResult {
	content, err := a.guideManager.ReadGuide(tools.GetString(p, "id"))
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"content": content})
}

func hGuideSearch(a *App, p map[string]any) tools.ToolResult {
	results := a.guideManager.SearchGuides(tools.GetString(p, "query"))
	type guideHit struct {
		guides.Guide
		Snippet string `json:"snippet"`
	}
	var hits []guideHit
	for _, g := range results {
		content, _ := a.guideManager.ReadGuide(g.ID)
		snippet := content
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		hits = append(hits, guideHit{Guide: g, Snippet: snippet})
	}
	if len(hits) == 0 {
		// Return an explicit empty array + hint instead of null, so the caller
		// can tell "no matches" apart from "tool produced nothing".
		return tools.OkResult(map[string]any{
			"results": []any{},
			"hint":    "无匹配攻略。攻略可能未同步——先调用 guide_sync 拉取来源。",
		})
	}
	return tools.OkResult(hits)
}

func hGuideSync(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.guideManager.SyncAll())
}

func hGuideSources(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.guideManager.ListSources())
}

// ─── Workflow handlers ────────────────────────────────────────

func hWorkflowList(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.ListWorkflows())
}

func hWorkflowGet(a *App, p map[string]any) tools.ToolResult {
	wf, err := a.GetWorkflow(tools.GetString(p, "id"))
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(wf)
}

func hWorkflowCreate(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("缺少必填参数: name")
	}
	wf := workflow.NewWorkflow(name)
	if desc := tools.GetString(p, "description"); desc != "" {
		wf.Description = desc
	}
	if err := a.CreateWorkflow(wf); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"id": wf.ID, "name": wf.Name})
}

func hWorkflowUpdate(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("缺少必填参数: id")
	}
	existing, err := a.workflowManager.Get(id)
	if err != nil {
		return tools.ErrResult(err)
	}
	clone := *existing
	if n := tools.GetString(p, "name"); n != "" {
		clone.Name = n
	}
	if d := tools.GetString(p, "description"); d != "" {
		clone.Description = d
	}
	if rawNodes, ok := p["nodes"]; ok {
		data, _ := json.Marshal(rawNodes)
		var nodes []workflow.WorkflowNode
		if err := json.Unmarshal(data, &nodes); err == nil {
			// The LLM never sends coordinates; carry over positions of nodes
			// that already existed (by ID) so editing doesn't wipe the layout.
			clone.Nodes = workflow.MergePositions(existing.Nodes, nodes)
		}
	}
	if rawEdges, ok := p["edges"]; ok {
		data, _ := json.Marshal(rawEdges)
		var edges []workflow.WorkflowEdge
		if err := json.Unmarshal(data, &edges); err == nil {
			clone.Edges = edges
		}
	}
	clone.UpdatedAt = time.Now().UnixMilli()
	if err := a.UpdateWorkflow(clone.ID, clone); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"id": clone.ID, "name": clone.Name})
}

func hWorkflowDelete(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("缺少必填参数: id")
	}
	if err := a.DeleteWorkflow(id); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult("已删除: " + id)
}

func hWorkflowExecute(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("缺少必填参数: id")
	}
	inputs := map[string]any{}
	if raw, ok := p["inputs"]; ok {
		if m, ok := raw.(map[string]any); ok {
			inputs = m
		}
	}
	execID, err := a.ExecuteWorkflow(id, inputs)
	if err != nil {
		return tools.ErrResult(err)
	}
	// Block until the run finishes (done/error/cancelled), the timeout elapses,
	// or the app shuts down — returning the final result in one call so the
	// agent doesn't need to poll workflow_status.
	state := a.workflowManager.GetExecution(execID)
	if state == nil {
		return tools.OkResult(map[string]any{"execId": execID, "status": "unknown"})
	}
	timeout := 10 * time.Minute
	if secs, ok := p["timeout"].(float64); ok && secs > 0 {
		timeout = time.Duration(secs) * time.Second
	}
	select {
	case <-state.Done:
	case <-time.After(timeout):
	case <-a.chatCtx.Done():
	}
	return tools.OkResult(map[string]any{
		"execId":  execID,
		"status":  string(state.Status),
		"outputs": state.Outputs,
		"error":   state.Error,
	})
}

func hWorkflowStatus(a *App, p map[string]any) tools.ToolResult {
	execID := tools.GetString(p, "execId")
	if execID == "" {
		return tools.ErrMsg("缺少必填参数: execId")
	}
	state := a.workflowManager.GetExecution(execID)
	if state == nil {
		return tools.ErrMsg("未找到执行记录: " + execID)
	}
	return tools.OkResult(state)
}

func hWorkflowValidate(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		// No id → validate nothing, return a helpful empty result instead of
		// an error (a bare validate call shouldn't read as "tool failed").
		return tools.OkResult(map[string]any{"valid": true, "issues": []string{}, "hint": "未指定 id；传入 workflow id 以校验"})
	}
	wf, err := a.workflowManager.Get(id)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(workflow.Validate(wf))
}

// ─── MCP handlers ────────────────────────────────────────────

func hMCPListServers(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.ListMCPServers(""))
}
func hMCPAddServer(a *App, p map[string]any) tools.ToolResult {
	cfg := mcpclient.ServerConfig{
		Name:      tools.GetString(p, "name"),
		Transport: tools.GetString(p, "transport"),
		Command:   tools.GetString(p, "command"),
		URL:       tools.GetString(p, "url"),
	}
	if cfg.Name == "" || cfg.Transport == "" {
		return tools.ErrMsg("缺少必填参数: name, transport")
	}
	return tools.OkResult(a.AddMCPServer(cfg))
}
func hMCPRemoveServer(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.RemoveMCPServer(tools.GetString(p, "id")))
}
func hMCPConnectServer(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.ConnectMCPServer(tools.GetString(p, "id")))
}
func hMCPDisconnectServer(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.DisconnectMCPServer(tools.GetString(p, "id")))
}
func hMCPGetServerTools(a *App, p map[string]any) tools.ToolResult {
	toolList, err := a.GetMCPServerTools(tools.GetString(p, "id"))
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(toolList)
}
func hMCPStatus(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.GetMCPStatus())
}

// ─── Proxy handlers ──────────────────────────────────────────

func hProxyGet(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.GetProxyStatus())
}
func hProxySet(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.SetProxy(tools.GetString(p, "url")))
}
func hProxyTest(a *App, p map[string]any) tools.ToolResult {
	return tools.OkResult(a.TestProxy(tools.GetString(p, "url")))
}
func hProxyToggle(a *App, p map[string]any) tools.ToolResult {
	enabled := true
	if v, ok := p["enabled"]; ok {
		if b, ok := v.(bool); ok { enabled = b }
	}
	a.SetProxyEnabled(enabled)
	return tools.OkResult(map[string]bool{"enabled": enabled})
}

// ─── Web search handler ──────────────────────────────────────

func hWebSearch(a *App, p map[string]any) tools.ToolResult {
	query := tools.GetString(p, "query")
	if query == "" {
		return tools.ErrMsg("缺少必填参数: query")
	}
	results, err := a.WebSearch(query)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(results)
}

func hWebFetch(a *App, p map[string]any) tools.ToolResult {
	url := tools.GetString(p, "url")
	if url == "" {
		return tools.ErrMsg("缺少必填参数: url")
	}
	prompt := tools.GetString(p, "prompt")
	depth := tools.GetString(p, "depth") // "summary" | "detailed" | "full"; default "summary"
	result, err := a.WebFetch(url, prompt, depth)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

// ─── Shell exec handler ──────────────────────────────────────

func hRuntimeInstall(a *App, p map[string]any) tools.ToolResult {
	kind := tools.GetString(p, "kind")
	if kind == "" {
		kind = "all"
	}
	return tools.OkResult(a.RuntimeInstall(kind))
}

func hSystemDiag(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.SystemDiag())
}

func hShellExec(a *App, p map[string]any) tools.ToolResult {
	command := tools.GetString(p, "command")
	if command == "" {
		return tools.ErrMsg("缺少必填参数: command")
	}
	// File control check for destructive commands.
	if err := a.fileCtl.CheckShell(command, a.emitAudit); err != nil {
		return tools.ErrResult(err)
	}
	cwd := tools.GetString(p, "cwd")
	timeout := tools.GetInt(p, "timeout")
	sessionID := tools.GetString(p, "sessionId")
	result, err := a.ShellExec(command, cwd, timeout, sessionID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

// ─── A2A Agent handlers ─────────────────────────────────────────

func hA2AListAgents(a *App, _ map[string]any) tools.ToolResult {
	if a.a2aManager == nil {
		return tools.OkResult([]a2a.RemoteAgentConfig{})
	}
	return tools.OkResult(a.a2aManager.ListRemoteAgents())
}

func hA2AConnectAgent(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("缺少必填参数: id")
	}
	if err := a.a2aManager.ConnectRemoteAgent(id); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"status": "connected", "id": id})
}

func hA2ADisconnectAgent(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("缺少必填参数: id")
	}
	if err := a.a2aManager.DisconnectRemoteAgent(id); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"status": "disconnected", "id": id})
}

func hA2ASendToAgent(a *App, p map[string]any) tools.ToolResult {
	agentID := tools.GetString(p, "agentId")
	message := tools.GetString(p, "message")
	if agentID == "" || message == "" {
		return tools.ErrMsg("缺少必填参数: agentId 和 message")
	}
	task, err := a.a2aManager.SendTask(agentID, message)
	if err != nil {
		return tools.ErrResult(err)
	}

	// Extract text content from the task result
	var text string
	for _, art := range task.Artifacts {
		for _, part := range art.Parts {
			if part.Kind == "text" {
				text += part.Text
			}
		}
	}
	if text == "" && task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			if part.Kind == "text" {
				text += part.Text
			}
		}
	}
	if text == "" {
		text = task.Status.State
	}

	return tools.OkResult(map[string]any{
		"taskId": task.ID,
		"status": task.Status.State,
		"text":   text,
	})
}

func hA2AAgentStatus(a *App, _ map[string]any) tools.ToolResult {
	if a.a2aManager == nil {
		return tools.OkResult(map[string]any{"running": false})
	}
	return tools.OkResult(a.a2aManager.ServerStatus())
}

// ─── Local Agent (persona) handlers ───────────────────────────

// hAgentList returns a slim summary of every local agent for the LLM.
func hAgentList(a *App, _ map[string]any) tools.ToolResult {
	if a.agentManager == nil {
		return tools.OkResult([]any{})
	}
	type agentSummary struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		IsDefault   bool     `json:"isDefault"`
		InheritAll  bool     `json:"inheritAll"`
		Skills      []string `json:"skills"`
	}
	out := make([]agentSummary, 0, len(a.agentManager.List()))
	for _, ag := range a.agentManager.List() {
		out = append(out, agentSummary{
			ID:          ag.ID,
			Name:        ag.Name,
			Description: ag.Description,
			IsDefault:   ag.IsDefault,
			InheritAll:  ag.InheritSkills,
			Skills:      ag.Skills,
		})
	}
	return tools.OkResult(out)
}

// hAgentCreate creates a new local agent at runtime (LLM-defined persona).
func hAgentCreate(a *App, p map[string]any) tools.ToolResult {
	if a.agentManager == nil {
		return tools.ErrMsg("agent 管理器未初始化")
	}
	name := tools.GetString(p, "name")
	systemPrompt := tools.GetString(p, "systemPrompt")
	if name == "" || systemPrompt == "" {
		return tools.ErrMsg("缺少必填参数: name, systemPrompt")
	}
	agent := agents.Agent{
		Name:         name,
		Description:  tools.GetString(p, "description"),
		Icon:         "◉",
		SystemPrompt: systemPrompt,
		Skills:       tools.GetStringSlice(p, "skills"),
		LibraryID:    a.resolveLibraryID(tools.GetString(p, "libraryId")),
	}
	created, err := a.agentManager.Create(agent)
	if err != nil {
		return tools.ErrResult(err)
	}
	a.emitChanged("agents:changed", "create", created.ID)
	return tools.OkResult(map[string]any{"id": created.ID, "name": created.Name})
}

// hAgentRun delegates a task to a local agent and returns its final reply.
func hAgentRun(a *App, p map[string]any) tools.ToolResult {
	if a.agentManager == nil {
		return tools.ErrMsg("agent 管理器未初始化")
	}
	agentID := tools.GetString(p, "agentId")
	name := tools.GetString(p, "name")
	task := tools.GetString(p, "task")
	if task == "" {
		return tools.ErrMsg("缺少必填参数: task")
	}
	if agentID == "" && name == "" {
		return tools.ErrMsg("需要指定 agentId 或 name")
	}

	// Resilient lookup: try both fields regardless of which one the LLM used.
	// LLMs sometimes put agent names into the agentId field.
	var agent *agents.Agent
	var err error
	if agentID != "" {
		agent, err = a.agentManager.Get(agentID)
		if err != nil {
			// Fallback: maybe the LLM put a name in the agentId field.
			agent, err = a.agentManager.FindByName(agentID)
		}
	}
	if agent == nil && name != "" {
		agent, err = a.agentManager.FindByName(name)
		if err != nil {
			// Fallback: maybe the LLM put an ID in the name field.
			agent, err = a.agentManager.Get(name)
		}
	}
	if err != nil || agent == nil {
		if err == nil {
			err = fmt.Errorf("agent 未找到")
		}
		log.Printf("[agent_run] lookup failed: agentId=%q name=%q err=%v", agentID, name, err)
		return tools.ErrResult(err)
	}

	log.Printf("[agent_run] launching agent: id=%s name=%s", agent.ID, agent.Name)
	// Claude Code pattern: coordinator forces ALL agent spawns async.
	// Sync execution blocks the main conversation — fire-and-forget instead.
	taskID, err := agentPlugin.RunAgentLoopAsync(agent, task, "")
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"status":    "async_launched",
		"taskId":    taskID,
		"agentName": agent.Name,
		"hint":      "Agent " + agent.Name + " 已启动后台执行。结果将在后续自动注入对话。",
	})
}


// hLibraryList returns all domain libraries with their agents for the core agent to discover.
func hLibraryList(a *App, _ map[string]any) tools.ToolResult {
	if a.memoryStore == nil {
		return tools.ErrResult(fmt.Errorf("memory store not initialized"))
	}
	libs, err := a.memoryStore.LibraryList()
	if err != nil {
		return tools.ErrResult(err)
	}
	type libEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Agents []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Desc string `json:"description"`
		} `json:"agents"`
	}
	var out []libEntry
	for _, l := range libs {
		entry := libEntry{ID: l.ID, Name: l.Name}
		if a.agentManager != nil {
			agents := a.agentManager.ListByLibrary(l.ID)
			for _, ag := range agents {
				entry.Agents = append(entry.Agents, struct {
					ID   string `json:"id"`
					Name string `json:"name"`
					Desc string `json:"description"`
				}{ID: ag.ID, Name: ag.Name, Desc: ag.Description})
			}
		}
		out = append(out, entry)
	}
	return tools.OkResult(out)
}

// hAgentDelegateToDomain delegates a task to a domain library's agent.
func hAgentDelegateToDomain(a *App, p map[string]any) tools.ToolResult {
	libraryID := tools.GetString(p, "libraryId")
	libraryName := tools.GetString(p, "libraryName")
	agentID := tools.GetString(p, "agentId")
	task := tools.GetString(p, "task")

	if task == "" {
		return tools.ErrResult(fmt.Errorf("task cannot be empty"))
	}

	var libID string
	if libraryID != "" {
		libID = libraryID
	} else if libraryName != "" {
		libs, _ := a.memoryStore.LibraryList()
		for _, l := range libs {
			if strings.EqualFold(l.Name, libraryName) {
				libID = l.ID
				break
			}
		}
		if libID == "" {
			return tools.ErrResult(fmt.Errorf("library not found: %s", libraryName))
		}
	} else {
		if a.memoryStore != nil {
			libID, _ = a.memoryStore.DefaultLibrary()
		}
	}

	var target *agents.Agent
	if agentID != "" {
		target, _ = a.agentManager.Get(agentID)
	}
	if target == nil && a.agentManager != nil {
		agents := a.agentManager.ListByLibrary(libID)
		for i := range agents {
			if agents[i].IsDefault {
				target = &agents[i]
				break
			}
		}
		if target == nil && len(agents) > 0 {
			target = &agents[0]
		}
	}
	if target == nil {
		return tools.ErrResult(fmt.Errorf("no agent available in this library"))
	}

	text, err := a.runAgentLoop(a.chatCtx, target, task)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"agent":   target.Name,
		"library": libraryName,
		"text":    text,
	})
}

// hAgentDelegateMultiDomain concurrently delegates a task to agents in multiple
// domain libraries, then aggregates their responses. Used by the core agent for
// cross-domain federated queries.
func hAgentDelegateMultiDomain(a *App, p map[string]any) tools.ToolResult {
	task := tools.GetString(p, "task")
	if task == "" {
		return tools.ErrResult(fmt.Errorf("task cannot be empty"))
	}
	rawDomains, _ := p["domains"].([]any)
	if len(rawDomains) == 0 {
		return tools.ErrResult(fmt.Errorf("domains list cannot be empty"))
	}
	var domainNames []string
	for _, d := range rawDomains {
		if s, ok := d.(string); ok && s != "" {
			domainNames = append(domainNames, s)
		}
	}
	if len(domainNames) == 0 {
		return tools.ErrResult(fmt.Errorf("no valid domain names"))
	}

	// Resolve libraries and their default agents in parallel.
	type domainResult struct {
		Library string `json:"library"`
		Agent   string `json:"agent"`
		Text    string `json:"text"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]domainResult, len(domainNames))
	var wg sync.WaitGroup
	for i, name := range domainNames {
		wg.Add(1)
		go func(idx int, libName string) {
			defer wg.Done()
			// Resolve library by name
			var libID string
			libs, _ := a.memoryStore.LibraryList()
			for _, l := range libs {
				if strings.EqualFold(l.Name, libName) {
					libID = l.ID
					break
				}
			}
			if libID == "" {
				results[idx] = domainResult{Library: libName, Error: "library not found"}
				return
			}
			// Find default agent for this library
			var target *agents.Agent
			list := a.agentManager.ListByLibrary(libID)
			for j := range list {
				if list[j].IsDefault {
					target = &list[j]
					break
				}
			}
			if target == nil && len(list) > 0 {
				target = &list[0]
			}
			if target == nil {
				results[idx] = domainResult{Library: libName, Error: "no agent available"}
				return
			}
			text, err := a.runAgentLoop(a.chatCtx, target, task)
			if err != nil {
				results[idx] = domainResult{Library: libName, Agent: target.Name, Error: err.Error()}
			} else {
				results[idx] = domainResult{Library: libName, Agent: target.Name, Text: text}
			}
		}(i, name)
	}
	wg.Wait()
	return tools.OkResult(results)
}


// hAgentSynthesizeTool dynamically creates a tool from LLM-generated code and
// registers it as an external tool for the current session.
func hAgentSynthesizeTool(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	code := tools.GetString(p, "code")
	desc := tools.GetString(p, "description")
	if name == "" || code == "" {
		return tools.ErrResult(fmt.Errorf("name and code are required"))
	}
	synthName := "synth__" + name
	toolDef := &tools.ToolDef{
		Name:        synthName,
		Description: desc,
		Category:    "synthesized",
		Parameters: &tools.ToolParams{
			Type:       "object",
			Properties: map[string]tools.ToolProp{
				"args": {Type: "object", Description: "Arguments to pass to the tool"},
			},
		},
	}
	tools.RegisterExternal(toolDef, "synthesized")
	return tools.OkResult(map[string]any{
		"name":    synthName,
		"message": "Tool registered. Python code:\n" + code,
	})
}

func hAgentSetLibrary(a *App, p map[string]any) tools.ToolResult {
	if a.agentManager == nil {
		return tools.ErrMsg("agent 管理器未初始化")
	}
	agentID := tools.GetString(p, "agentId")
	libraryID := tools.GetString(p, "libraryId")
	if agentID == "" {
		return tools.ErrMsg("缺少必填参数: agentId")
	}
	if err := a.agentManager.SetLibrary(agentID, libraryID); err != nil {
		return tools.ErrResult(err)
	}
	a.emitChanged("agents:changed", "update", agentID)
	return tools.OkResult(map[string]string{"status": "ok"})
}

// ─── Paradigm tools ──────────────────────────────────────────────────────

func hParadigmList(a *App, p map[string]any) tools.ToolResult {
	libID := tools.GetString(p, "libraryId")
	list, err := a.ParadigmList(libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(list)
}

func hParadigmSelect(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	methodology, err := a.ParadigmSelect(id)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"methodology": methodology})
}

func hParadigmFeedback(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	matchQ := floatParam(p, "match", 0.5)
	execQ := floatParam(p, "exec", 0.5)
	outcomeQ := floatParam(p, "outcome", 0.5)
	reason := tools.GetString(p, "reason")
	composite, err := a.ParadigmFeedback(id, matchQ, execQ, outcomeQ, reason)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"composite": composite,
		"hint":      "match=匹配度 exec=执行度 outcome=结果度，低match可能是任务不匹配而非范式本身问题",
	})
}

func hParadigmRefine(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	result, err := a.ParadigmRefine(id)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hParadigmDistill(a *App, p map[string]any) tools.ToolResult {
	text := tools.GetString(p, "text")
	wsID := tools.GetString(p, "libraryId")
	result, err := a.ParadigmDistill(text, wsID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hParadigmMatch(a *App, p map[string]any) tools.ToolResult {
	task := tools.GetString(p, "task")
	list, err := a.ParadigmMatch(task)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(list)
}

func floatParam(p map[string]any, key string, def float64) float64 {
	if v, ok := p[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case json.Number:
			f, _ := n.Float64()
			return f
		case string:
			// LLMs may serialize numbers as strings in tool calls.
			f, err := strconv.ParseFloat(n, 64)
			if err == nil {
				return f
			}
		}
	}
	return def
}

// hAgentRunAsync launches a sub-agent as a background task (non-blocking).
// Returns a task ID immediately; the agent result arrives in a future turn
// via the notification queue.
func hAgentRunAsync(a *App, p map[string]any) tools.ToolResult {
	if a.agentManager == nil {
		return tools.ErrMsg("agent 管理器未初始化")
	}
	agentID := tools.GetString(p, "agentId")
	name := tools.GetString(p, "name")
	task := tools.GetString(p, "task")
	if task == "" {
		return tools.ErrMsg("缺少必填参数: task")
	}
	if agentID == "" && name == "" {
		return tools.ErrMsg("需要指定 agentId 或 name")
	}
	// Resilient lookup: try agentId first as ID then as name, and vice versa.
	// LLMs sometimes put agent names into the agentId field, or IDs into name.
	var agent *agents.Agent
	var err error
	if agentID != "" {
		agent, err = a.agentManager.Get(agentID)
		if err != nil {
			agent, err = a.agentManager.FindByName(agentID)
		}
	}
	if agent == nil && name != "" {
		agent, err = a.agentManager.FindByName(name)
		if err != nil {
			agent, err = a.agentManager.Get(name)
		}
	}
	if err != nil || agent == nil {
		if err == nil {
			err = fmt.Errorf("agent 未找到")
		}
		return tools.ErrResult(err)
	}
	taskID, err := agentPlugin.RunAgentLoopAsync(agent, task, "")
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"status":    "async_launched",
		"taskId":    taskID,
		"agentName": agent.Name,
		"hint":      "Agent " + agent.Name + " 已启动后台执行。结果将在后续轮次自动注入。",
	})
}

func hCodebaseImport(a *App, p map[string]any) tools.ToolResult {
	libID := tools.GetString(p, "libraryId")
	result, err := a.CodebaseImport(libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hDomainSearch(a *App, p map[string]any) tools.ToolResult {
	query := tools.GetString(p, "query")
	libID := tools.GetString(p, "libraryId")
	if query == "" || libID == "" {
		return tools.ErrMsg("缺少必填参数: query 和 libraryId")
	}
	minSim := float64(tools.GetInt(p, "minSimilarity")) / 100.0
	result, err := a.DomainSearch(query, libID, minSim)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}
