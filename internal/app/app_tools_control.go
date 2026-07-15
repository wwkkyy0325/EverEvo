//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/memory"
	"everevo/internal/async"
	"everevo/internal/taskboard"
	"everevo/internal/tools"
)

func init() {
	// Register all app-control handlers in the global map.
	// These give the LLM full control over every app feature.

	toolHandlers["memory_list"] = hMemoryList
	toolHandlers["memory_search"] = hMemorySearch
	toolHandlers["memory_delete"] = hMemoryDelete
	toolHandlers["memory_add_fact"] = hMemoryAddFact
	toolHandlers["memory_clear"] = hMemoryClear
	toolHandlers["core_list"] = hCoreList
	toolHandlers["core_add"] = hCoreAdd
	toolHandlers["core_delete"] = hCoreDelete
	toolHandlers["session_list"] = hSessionList
	toolHandlers["session_delete"] = hSessionDelete
	toolHandlers["graph_migrate"] = hGraphMigrate
	toolHandlers["graph_rebuild_from_domain"] = hGraphRebuildFromDomain
	toolHandlers["graph_list"] = hGraphList
	toolHandlers["graph_add_edge"] = hGraphAddEdge
	toolHandlers["graph_delete_node"] = hGraphDeleteNode
	toolHandlers["graph_rename_node"] = hGraphRenameNode
	toolHandlers["wiki_list"] = hWikiList
	toolHandlers["wiki_read"] = hWikiRead
	toolHandlers["wiki_save"] = hWikiSave
	toolHandlers["wiki_delete"] = hWikiDelete
	toolHandlers["wiki_search"] = hWikiSearch
	toolHandlers["wiki_move"] = hWikiMove
	toolHandlers["wiki_reindex"] = hWikiReindex
	toolHandlers["experience_list"] = hExperienceList
	toolHandlers["experience_delete"] = hExperienceDelete
	toolHandlers["library_list"] = hLibraryList2
	toolHandlers["library_create"] = hLibraryCreate2
	toolHandlers["library_delete"] = hLibraryDelete2
	toolHandlers["write_file"] = hWriteFile
	toolHandlers["list_directory"] = hListDirectory

	toolHandlers["zone_list"] = hZoneList
	toolHandlers["zone_create_experiment"] = hZoneCreateExperiment
	toolHandlers["zone_launch"] = hZoneLaunch
	toolHandlers["zone_stop"] = hZoneStop
	toolHandlers["zone_merge"] = hZoneMerge
	toolHandlers["zone_discard"] = hZoneDiscard
	toolHandlers["backup_list"] = hBackupList
	toolHandlers["backup_create"] = hBackupCreate
	toolHandlers["backup_restore"] = hBackupRestore
	toolHandlers["evolve_capability"] = hEvolveCapability
	toolHandlers["evolve_build"] = hEvolveBuild
	toolHandlers["evolve_swap"] = hEvolveSwap
	toolHandlers["evolve_tasks"] = hEvolveTasks
	toolHandlers["ingest_folder"] = hIngestFolder
	toolHandlers["ingest_analyze"] = hIngestAnalyze
	toolHandlers["ingest_commit"] = hIngestCommit
	toolHandlers["ingest_cancel"] = hIngestCancel
	toolHandlers["ingest_deep"] = hIngestDeep
	toolHandlers["ingest_deep_analyze"] = hIngestDeepAnalyze
	toolHandlers["ingest_deep_commit"] = hIngestDeepCommit
	toolHandlers["ingest_deep_review"] = hIngestDeepReview

	toolHandlers["collab_create"] = hCollabCreate
	toolHandlers["collab_dispatch"] = hCollabDispatch
	toolHandlers["collab_dispatch_async"] = hCollabDispatchAsync
	toolHandlers["collab_wait"] = hCollabWait
	toolHandlers["blackboard_set"] = hBlackboardSet
	toolHandlers["blackboard_get"] = hBlackboardGet
	toolHandlers["blackboard_list"] = hBlackboardList
	toolHandlers["agent_message"] = hAgentMessage
	toolHandlers["collab_list_sessions"] = hCollabListSessions
	toolHandlers["collab_complete"] = hCollabComplete

	toolHandlers["plan_create"] = hPlanCreate
	toolHandlers["plan_step_update"] = hPlanStepUpdate
	toolHandlers["plan_list"] = hPlanList

	toolHandlers["taskboard_list"] = hTaskBoardList
	toolHandlers["taskboard_add"] = hTaskBoardAdd
	toolHandlers["taskboard_update"] = hTaskBoardUpdate
	toolHandlers["taskboard_steps"] = hTaskBoardSteps
}

// ── Memory ──

func hMemoryList(a *App, p map[string]any) tools.ToolResult {
	limit := tools.GetInt(p, "limit")
	if limit <= 0 { limit = 20 }
	libID := tools.GetString(p, "libraryId")
	list, err := a.MemoryList(limit, libID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(list)
}

func hMemorySearch(a *App, p map[string]any) tools.ToolResult {
	query := tools.GetString(p, "query")
	k := tools.GetInt(p, "k")
	libID := tools.GetString(p, "libraryId")
	if k <= 0 { k = 5 }
	result, err := a.MemoryRecallScoped(query, k, libID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(result)
}

func hMemoryDelete(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.MemoryItemDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

func hMemoryAddFact(a *App, p map[string]any) tools.ToolResult {
	content := tools.GetString(p, "content")
	category := tools.GetString(p, "category")
	if content == "" || category == "" { return tools.ErrMsg("content and category required") }
	if err := a.MemoryCoreAdd(category, content, category); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("fact added")
}

func hMemoryClear(a *App, p map[string]any) tools.ToolResult {
	if err := a.MemoryClear(); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("cleared")
}

// ── Core Facts ──

func hCoreList(a *App, p map[string]any) tools.ToolResult {
	libID := tools.GetString(p, "libraryId")
	facts, err := a.MemoryCoreListByLibrary(libID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(facts)
}

func hCoreAdd(a *App, p map[string]any) tools.ToolResult {
	key := tools.GetString(p, "key")
	value := tools.GetString(p, "value")
	category := tools.GetString(p, "category")
	if key == "" { key = category }
	if value == "" { return tools.ErrMsg("value required") }
	if err := a.MemoryCoreAdd(key, value, category); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("core fact added")
}

func hCoreDelete(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.MemoryCoreDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

// ── Sessions ──

func hSessionList(a *App, p map[string]any) tools.ToolResult {
	list, err := a.MemorySessionList()
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(list)
}

func hSessionDelete(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.MemorySessionDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

// ── Knowledge Graph ──

func hGraphList(a *App, p map[string]any) tools.ToolResult {
	search := tools.GetString(p, "search")
	// Honor caller-provided libraryId (e.g. from an agent's bound domain);
	// fall back to the default library for standalone calls.
	libID := tools.GetString(p, "libraryId")
	if libID == "" {
		libID, _ = a.memoryStore.DefaultLibrary()
	}
	result, err := a.MemoryGraphList(false, libID)
	if err != nil { return tools.ErrResult(err) }
	// MemoryGraphList returns nodes as []memory.GraphNode, not []any — asserting
	// to []any always fails (Go forbids the conversion), yielding nil → JSON null.
	nodes, _ := result["nodes"].([]memory.GraphNode)
	if search != "" {
		filtered := make([]memory.GraphNode, 0, len(nodes))
		for _, n := range nodes {
			if contains(n.Name, search) {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	return tools.OkResult(map[string]any{"nodes": nodes, "edges": result["edges"]})
}

func hGraphAddEdge(a *App, p map[string]any) tools.ToolResult {
	srcName := tools.GetString(p, "srcName")
	dstName := tools.GetString(p, "dstName")
	relType := tools.GetString(p, "type")
	if srcName == "" || dstName == "" || relType == "" { return tools.ErrMsg("srcName, dstName, type required") }
	replaces := false
	if v, ok := p["replaces"].(bool); ok { replaces = v }
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	if err := a.MemoryEdgeAddByNames(srcName, dstName, relType, libID, replaces); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult("edge added")
}

func hGraphDeleteNode(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.MemoryNodeDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

func hGraphRenameNode(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	name := tools.GetString(p, "name")
	if id == "" || name == "" { return tools.ErrMsg("id and name required") }
	if err := a.MemoryNodeRename(id, name); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("renamed")
}

func hGraphMigrate(a *App, p map[string]any) tools.ToolResult {
	n, err := a.MemoryGraphMigrate()
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"migrated": n, "status": "ok"})
}

func hGraphRebuildFromDomain(a *App, p map[string]any) tools.ToolResult {
	libID := tools.GetString(p, "libraryId")
	if libID == "" {
		return tools.ErrMsg("libraryId is required")
	}
	nodes, edges, err := a.GraphRebuildFromDomain(libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{"nodes": nodes, "edges": edges, "status": "ok"})
}

// ── Wiki ──

func hWikiList(a *App, p map[string]any) tools.ToolResult {
	libID := tools.GetString(p, "libraryId")
	if libID == "" {
		libID, _ = a.memoryStore.DefaultLibrary()
	}
	pages, err := a.WikiListPages(libID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(pages)
}

func hWikiRead(a *App, p map[string]any) tools.ToolResult {
	pageID := tools.GetString(p, "pageId")
	if pageID == "" { return tools.ErrMsg("pageId required") }
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	content, err := a.WikiReadPage(libID, pageID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(content)
}

func hWikiSave(a *App, p map[string]any) tools.ToolResult {
	pageID := tools.GetString(p, "pageId")
	title := tools.GetString(p, "title")
	content := tools.GetString(p, "content")
	if title == "" || content == "" { return tools.ErrMsg("title and content required") }
	if pageID == "" { pageID = title }
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	if err := a.WikiSavePage(libID, pageID, title, content); err != nil { return tools.ErrResult(err) }
	return tools.OkResult(map[string]string{"pageId": pageID, "status": "saved"})
}

func hWikiDelete(a *App, p map[string]any) tools.ToolResult {
	pageID := tools.GetString(p, "pageId")
	if pageID == "" { return tools.ErrMsg("pageId required") }
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	if err := a.WikiDeletePage(libID, pageID); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

func hWikiSearch(a *App, p map[string]any) tools.ToolResult {
	query := tools.GetString(p, "query")
	if query == "" { return tools.ErrMsg("query required") }
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	hits, err := a.WikiSearch(libID, query)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(hits)
}

func hWikiMove(a *App, p map[string]any) tools.ToolResult {
	fromLibID := tools.GetString(p, "fromLibraryId")
	toLibID := tools.GetString(p, "toLibraryId")
	pageID := tools.GetString(p, "pageId")
	if fromLibID == "" || toLibID == "" || pageID == "" {
		return tools.ErrMsg("fromLibraryId, toLibraryId, pageId are required")
	}
	deleteSource := false
	if v, ok := p["deleteSource"].(bool); ok { deleteSource = v }
	if err := a.WikiMovePage(fromLibID, toLibID, pageID, deleteSource); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]string{"status": "moved", "pageId": pageID})
}

func hWikiReindex(a *App, p map[string]any) tools.ToolResult {
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	result, err := a.WikiReindex(libID)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(result)
}

// ── Experience ──

func hExperienceList(a *App, p map[string]any) tools.ToolResult {
	limit := tools.GetInt(p, "limit")
	if limit <= 0 { limit = 10 }
	libID := tools.GetString(p, "libraryId")
	if libID == "" {
		libID, _ = a.memoryStore.DefaultLibrary()
	}
	items, err := a.MemoryRecallExperience(libID, limit)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(items)
}

func hExperienceDelete(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.MemoryExperienceDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

// ── Domain Libraries ──

func hLibraryList2(a *App, p map[string]any) tools.ToolResult {
	list, err := a.LibraryList()
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(list)
}

func hLibraryCreate2(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	desc := tools.GetString(p, "description")
	if name == "" { return tools.ErrMsg("name required") }
	id, err := a.LibraryCreate(name, desc, "", false)
	if err != nil { return tools.ErrResult(err) }
	return tools.OkResult(map[string]string{"id": id, "name": name})
}

func hLibraryDelete2(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" { return tools.ErrMsg("id required") }
	if err := a.LibraryDelete(id); err != nil { return tools.ErrResult(err) }
	return tools.OkResult("deleted")
}

// ── File write ──

func hWriteFile(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	content := tools.GetString(p, "content")
	if path == "" || content == "" { return tools.ErrMsg("path and content required") }
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) { return tools.ErrMsg("absolute path not allowed") }
	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) > 0 && parts[0] == ".." { return tools.ErrMsg(".. path escape not allowed") }
	if a.fileCtl.Mode() == ModeReadOnly { return tools.ErrMsg("read-only mode: write_file blocked") }
	if a.fileCtl.Mode() == ModeAudit {
		firstPart := strings.SplitN(clean, string(filepath.Separator), 2)[0]
		if firstPart != "workspace" && firstPart != "data" {
			pv := content
			if len(pv) > 500 { pv = pv[:500] }
			if err := a.fileCtl.CheckWrite("write_file", clean, pv, a.emitAudit); err != nil { return tools.ErrResult(err) }
		}
	}
	dir := filepath.Dir(clean)
	if err := os.MkdirAll(dir, 0755); err != nil { return tools.ErrResult(err) }
	if err := os.WriteFile(clean, []byte(content), 0644); err != nil { return tools.ErrResult(err) }
	return tools.OkResult(map[string]any{"path": clean, "size": len(content)})
}

func (a *App) emitAudit(event string, data any) {
	wailsRuntime.EventsEmit(a.ctx, event, data)
}

func hListDirectory(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" { return tools.ErrMsg("path required") }
	entries, err := os.ReadDir(path)
	if err != nil { return tools.ErrResult(err) }
	var out []map[string]any
	for _, e := range entries {
		out = append(out, map[string]any{"name": e.Name(), "isDir": e.IsDir()})
	}
	return tools.OkResult(map[string]any{"entries": out})
}

// ─── Zone management handlers ──────────────────────────────────────

func hZoneList(a *App, _ map[string]any) tools.ToolResult {
	zones, err := a.ListZones()
	if err != nil {
		return tools.ErrResult(err)
	}
	type zoneInfo struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		Parent    string `json:"parent"`
		PID       int    `json:"pid"`
		MCPPort   int    `json:"mcpPort"`
		A2APort   int    `json:"a2aPort"`
		CreatedAt string `json:"createdAt"`
	}
	var out []zoneInfo
	for _, z := range zones {
		out = append(out, zoneInfo{
			Name:      z.Name,
			Type:      string(z.Type),
			Parent:    z.Parent,
			PID:       z.PID,
			MCPPort:   z.MCPPort,
			A2APort:   z.A2APort,
			CreatedAt: z.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return tools.OkResult(out)
}

func hZoneCreateExperiment(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("实验区名称不能为空")
	}
	z, err := a.CreateExperiment(name)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"name":    z.Name,
		"type":    string(z.Type),
		"dir":     z.Dir,
		"message": "实验区已创建，使用 zone_launch(\"" + z.Name + "\") 启动",
	})
}

func hZoneLaunch(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("运行区名称不能为空")
	}
	if err := a.LaunchZone(name); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "运行区 " + name + " 已启动（新窗口）",
	})
}

func hZoneStop(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("运行区名称不能为空")
	}
	if err := a.StopZone(name); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "运行区 " + name + " 已停止",
	})
}

func hZoneMerge(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("实验区名称不能为空")
	}
	if err := a.MergeZone(name); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "实验区 " + name + " 已合并到生产区（备份已自动创建）。建议手动重启生产实例使配置生效。",
	})
}

func hZoneDiscard(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("运行区名称不能为空")
	}
	if err := a.DiscardZone(name); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "运行区 " + name + " 已删除",
	})
}

func hBackupList(a *App, _ map[string]any) tools.ToolResult {
	backups, err := a.ListBackups()
	if err != nil {
		return tools.ErrResult(err)
	}
	type backupInfo struct {
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
	}
	var out []backupInfo
	for _, b := range backups {
		out = append(out, backupInfo{
			Name:      b.Name,
			CreatedAt: b.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return tools.OkResult(out)
}

func hBackupCreate(a *App, _ map[string]any) tools.ToolResult {
	b, err := a.CreateBackup()
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"name":    b.Name,
		"message": "备份已创建: " + b.Name,
	})
}

func hBackupRestore(a *App, p map[string]any) tools.ToolResult {
	name := tools.GetString(p, "name")
	if name == "" {
		return tools.ErrMsg("备份名称不能为空")
	}
	if err := a.RestoreBackup(name); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "已从备份 " + name + " 恢复。建议重启生产实例。",
	})
}

// ─── Self-evolution handlers ───────────────────────────────────────

func hEvolveCapability(a *App, _ map[string]any) tools.ToolResult {
	c := a.GetEvolveCapability()
	return tools.OkResult(map[string]any{
		"sourceAvailable": c.SourceAvailable,
		"sourceDir":       c.SourceDir,
		"buildOutput":     c.BuildOutput,
		"currentExe":      c.CurrentExe,
		"goAvailable":     c.GoAvailable,
		"nodeAvailable":   c.NodeAvailable,
		"wailsAvailable":  c.WailsAvailable,
	})
}

func hEvolveBuild(a *App, _ map[string]any) tools.ToolResult {
	result, err := a.BuildSelf()
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hEvolveSwap(a *App, _ map[string]any) tools.ToolResult {
	if err := a.SwapAndRestart(); err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(map[string]any{
		"message": "正在重启...程序即将退出并以新版本重新启动",
	})
}

func hEvolveTasks(a *App, _ map[string]any) tools.ToolResult {
	return tools.OkResult(a.ListEvolveTasks())
}

func hIngestFolder(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("path 不能为空")
	}
	result, err := a.IngestFolder(path)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestAnalyze(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("path 不能为空")
	}
	result, err := a.IngestAnalyze(path)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestCommit(a *App, p map[string]any) tools.ToolResult {
	// The "analysis" param comes in as a raw JSON object from the LLM.
	raw, ok := p["analysis"]
	if !ok {
		return tools.ErrMsg("analysis 参数不能为空（请传入 ingest_analyze 返回的完整结果）")
	}
	// Re-serialize to JSON then parse into IngestAnalysis.
	data, err := json.Marshal(raw)
	if err != nil {
		return tools.ErrResult(fmt.Errorf("序列化 analysis 失败: %w", err))
	}
	var analysis IngestAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return tools.ErrResult(fmt.Errorf("解析 analysis 失败: %w", err))
	}
	result, err := a.IngestCommit(&analysis)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestDeep(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("path 不能为空")
	}
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	// Fire-and-forget: deep ingest can take minutes. Return task ID immediately,
	// result arrives via auto-continue notification injection.
	if a.asyncManager != nil {
		task, err := a.asyncManager.Create("", "深度导入: "+path, "ingest_deep", path, `{"path":"`+path+`","libraryId":"`+libID+`"}`)
		if err == nil {
			a.asyncManager.Start(task.ID)
			go func() {
				result, runErr := a.IngestDeep(path, libID)
				if runErr != nil {
					a.asyncManager.Fail(task.ID, runErr.Error())
				} else {
					b, _ := json.Marshal(result)
					a.asyncManager.Complete(task.ID, string(b))
				}
				// Enqueue notification for auto-continue.
				if a.commandQueue != nil {
					entry := &async.QueueEntry{Priority: async.PriorityNow, TaskID: task.ID, Kind: "agent_done"}
					if runErr != nil {
						entry.Content = runErr.Error()
					} else {
						entry.Content = fmt.Sprintf("深度导入完成: %s (%d 文件)", path, len(result.FileMetas))
					}
					a.commandQueue.Enqueue(entry)
				}
			}()
			return tools.OkResult(map[string]any{"status": "async_launched", "taskId": task.ID, "hint": "深度导入已启动，完成后自动通知"})
		}
	}
	// Fallback: sync execution
	result, err := a.IngestDeep(path, libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestDeepAnalyze(a *App, p map[string]any) tools.ToolResult {
	path := tools.GetString(p, "path")
	if path == "" {
		return tools.ErrMsg("path 不能为空")
	}
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	result, err := a.IngestDeepAnalyze(path, libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestDeepCommit(a *App, p map[string]any) tools.ToolResult {
	raw, ok := p["analysis"]
	if !ok {
		return tools.ErrMsg("analysis 参数不能为空")
	}
	data, _ := json.Marshal(raw)
	var analysis DeepAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return tools.ErrResult(fmt.Errorf("解析 analysis: %w", err))
	}
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	result, err := a.IngestDeepCommit(&analysis, libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestDeepReview(a *App, p map[string]any) tools.ToolResult {
	raw, ok := p["analysis"]
	if !ok {
		return tools.ErrMsg("analysis 参数不能为空")
	}
	data, _ := json.Marshal(raw)
	var analysis DeepAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return tools.ErrResult(fmt.Errorf("解析 analysis: %w", err))
	}
	libID := a.resolveLibraryID(tools.GetString(p, "libraryId"))
	result, err := a.IngestDeepReview(&analysis, libID)
	if err != nil {
		return tools.ErrResult(err)
	}
	return tools.OkResult(result)
}

func hIngestCancel(a *App, _ map[string]any) tools.ToolResult {
	a.CancelIngest()
	return tools.OkResult(map[string]any{"message": "已取消"})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) > 0 && findSub(s, sub))
}
func findSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub { return true }
	}
	return false
}

// ─── Task Board handlers ───────────────────────────────────────────

func hTaskBoardList(a *App, _ map[string]any) tools.ToolResult {
	tasks := a.ListTasks()
	return tools.OkResult(tasks)
}

func hTaskBoardAdd(a *App, p map[string]any) tools.ToolResult {
	title := tools.GetString(p, "title")
	priority := tools.GetString(p, "priority")
	if title == "" || priority == "" {
		return tools.ErrMsg("title and priority required")
	}
	desc := tools.GetString(p, "description")

	// Parse steps — each step is either a string or {text, done}
	stepStrs := tools.GetStringSlice(p, "steps")
	var steps []taskboard.Step
	if rawSteps, ok := p["steps"].([]any); ok {
		for _, s := range rawSteps {
			switch v := s.(type) {
			case string:
				steps = append(steps, taskboard.Step{Text: v})
			case map[string]any:
				txt, _ := v["text"].(string)
				done, _ := v["done"].(bool)
				if txt != "" {
					steps = append(steps, taskboard.Step{Text: txt, Done: done})
				}
			}
		}
	}
	_ = stepStrs // consumed by raw path above

	deps := tools.GetStringSlice(p, "dependsOn")

	result := a.AddTask(title, desc, priority, steps, deps)
	return tools.OkResult(result)
}

func hTaskBoardUpdate(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	status := tools.GetString(p, "status")
	if id == "" || status == "" {
		return tools.ErrMsg("id and status required")
	}
	progress := tools.GetInt(p, "progress")
	notes := tools.GetString(p, "notes")

	result := a.UpdateTaskStatus(id, status, progress, notes)
	return tools.OkResult(result)
}

func hTaskBoardSteps(a *App, p map[string]any) tools.ToolResult {
	id := tools.GetString(p, "id")
	if id == "" {
		return tools.ErrMsg("id required")
	}
	var steps []taskboard.Step
	if raw, ok := p["steps"].([]any); ok {
		for _, s := range raw {
			if m, ok := s.(map[string]any); ok {
				txt, _ := m["text"].(string)
				done, _ := m["done"].(bool)
				steps = append(steps, taskboard.Step{Text: txt, Done: done})
			}
		}
	}
	result := a.UpdateTaskSteps(id, steps)
	return tools.OkResult(result)
}
