//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"everevo/internal/memory"
	"everevo/internal/rag"
)

// MemoryGraphList returns the current knowledge graph (nodes + valid edges) for
// the UI viewer.
func (a *App) MemoryGraphList(history bool, libraryID string) (map[string]any, error) {
	if a.memoryStore == nil {
		return map[string]any{"nodes": []any{}, "edges": []any{}}, nil
	}
	nodes, err := a.memoryStore.ListNodesByLibrary(libraryID)
	if err != nil {
		return nil, err
	}
	var edges []memory.GraphEdge
	if history {
		edges, err = a.memoryStore.ListAllEdgesIncludeHistoryByLibrary(libraryID)
	} else {
		edges, err = a.memoryStore.ListAllEdgesByLibrary(libraryID)
	}
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []memory.GraphNode{}
	}
	if edges == nil {
		edges = []memory.GraphEdge{}
	}
	return map[string]any{"nodes": nodes, "edges": edges}, nil
}

// MemoryNodeDelete removes a graph node (and its edges).
func (a *App) MemoryNodeDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteNode(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

// MemoryEdgeDelete removes a single graph edge.
func (a *App) MemoryEdgeDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteEdge(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "delete", "")
	return nil
}

// MemoryNodeRename renames a graph entity.
func (a *App) MemoryNodeRename(id, name string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameNode(id, name); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryNodesMerge folds dropID into keepID.
func (a *App) MemoryNodesMerge(keepID, dropID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.MergeNodes(keepID, dropID); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryGraphMigrate reassigns graph nodes from "default" workspace to their
// proper domain libraries using content-based entity matching. Strategy:
//  1. Exact/suffix match: domain name, wiki page titles, KB names, agent names
//  2. Content match: search wiki page bodies and KB document previews for
//     each entity name (case-insensitive substring)
//  3. Only reassigns nodes currently in "default" — never steals from an
//     already-assigned domain.
//
// Returns the number of nodes reassigned.
func (a *App) MemoryGraphMigrate() (int, error) {
	if a.memoryStore == nil {
		return 0, fmt.Errorf("记忆库未就绪")
	}

	// ── Step 1: Collect per-domain signature terms ──
	// domainTerms[libID] = set of lowercase terms that strongly signal that domain.
	domainTerms := map[string]map[string]bool{} // libID → set of terms
	libs, err := a.memoryStore.LibraryList()
	if err != nil {
		return 0, err
	}

	for _, lib := range libs {
		if lib.ID == "" {
			continue
		}
		terms := map[string]bool{}

		// Domain name itself and its components.
		dn := strings.ToLower(strings.TrimSpace(lib.Name))
		terms[dn] = true
		for _, part := range strings.Fields(dn) {
			if len(part) >= 2 {
				terms[part] = true
			}
		}

		// Wiki page titles + content under this domain.
		if pages, pErr := a.WikiListPages(lib.ID); pErr == nil {
			for _, p := range pages {
				terms[strings.ToLower(strings.TrimSpace(p.Title))] = true
				// Read wiki page body for content-level matching.
				if page, rErr := a.WikiReadPage(lib.ID, p.ID); rErr == nil {
					if content, ok := page["content"].(string); ok && content != "" {
						collectContentTerms(terms, content)
					}
				}
			}
		}

		// Agent names in this domain.
		if a.agentManager != nil {
			for _, ag := range a.agentManager.ListByLibrary(lib.ID) {
				terms[strings.ToLower(strings.TrimSpace(ag.Name))] = true
			}
		}

		if len(terms) > 0 {
			domainTerms[lib.ID] = terms
		}
	}

	// KB names and document previews (associated by libraryID).
	if ragStore, rErr := a.getRagStore(); rErr == nil {
		for _, kb := range ragStore.ListKBs("") {
			if kb.LibraryID == "" {
				continue
			}
			if domainTerms[kb.LibraryID] == nil {
				domainTerms[kb.LibraryID] = map[string]bool{}
			}
			domainTerms[kb.LibraryID][strings.ToLower(strings.TrimSpace(kb.Name))] = true
			// KB document previews.
			if docs, dErr := ragStore.ListDocuments(kb.ID); dErr == nil {
				for _, d := range docs {
					collectContentTerms(domainTerms[kb.LibraryID], d.Preview)
					if d.Metadata != nil {
						if src, ok := d.Metadata["source"]; ok {
							domainTerms[kb.LibraryID][strings.ToLower(strings.TrimSpace(src))] = true
						}
					}
				}
			}
		}
	}

	if len(domainTerms) == 0 {
		return 0, nil
	}

	// ── Step 2: For each default-workspace node, find best matching domain ──
	nodes, nErr := a.memoryStore.ListNodesByLibrary("")
	if nErr != nil {
		return 0, nErr
	}

	type assignment struct{ name, libID string }
	var reassign []assignment
	for _, node := range nodes {
		nn := strings.ToLower(strings.TrimSpace(node.Name))
		if nn == "" {
			continue
		}
		// Only reassign nodes currently in the default workspace pool.
		ws := a.memoryStore.NodeWorkspace(node.ID)
		if !a.memoryStore.IsDefaultWS(ws) && ws != "" {
			continue
		}
		// Score each domain: +1 per matching term.
		bestLib, bestScore := "", 0
		for libID, terms := range domainTerms {
			score := 0
			if terms[nn] {
				score += 3 // exact match
			}
			// Substring: entity name appears in domain content.
			for t := range terms {
				if len(t) >= 3 && strings.Contains(t, nn) {
					score++
				}
				if len(nn) >= 3 && strings.Contains(nn, t) && len(t) >= 3 {
					score++
				}
			}
			if score > bestScore {
				bestScore = score
				bestLib = libID
			}
		}
		if bestScore >= 3 { // require at least one strong signal
			reassign = append(reassign, assignment{name: nn, libID: bestLib})
		}
	}

	if len(reassign) == 0 && len(domainTerms) == 0 {
		return 0, nil
	}

	// ── Step 3: Apply seed hints, then propagate along edges ──
	// Collect direct-hit seeds from the scoring pass above.
	seedHints := make(map[string]string, len(reassign))
	for _, a := range reassign {
		seedHints[a.name] = a.libID
	}
	// Also add direct domain-name matches as seeds (cheap, high-precision).
	for _, lib := range libs {
		if lib.ID == "" {
			continue
		}
		dn := strings.ToLower(strings.TrimSpace(lib.Name))
		if _, already := seedHints[dn]; !already && dn != "" {
			seedHints[dn] = lib.ID
		}
	}

	n, err := a.memoryStore.PropagateGraphWorkspace(seedHints)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		log.Printf("[memory] 图谱迁移: %d 节点从默认领域分配到各领域 (种子+拓扑传播)", n)
		a.emitChanged("memory:changed", "update", "")
	}
	return n, nil
}

// GraphRebuildFromDomain reconstructs the knowledge graph for a given domain
// library from existing KB documents and Wiki pages — no re-ingest needed.
// Each KB document and Wiki page becomes an entity node (with vector embedding
// for semantic recall); topics extracted from content become sub-nodes linked
// via "讨论"/"包含" edges. Returns (nodes, edges) counts.
func (a *App) GraphRebuildFromDomain(libraryID string) (int, int, error) {
	if a.memoryStore == nil {
		return 0, 0, fmt.Errorf("记忆库未就绪")
	}

	modelDir := detectEmbeddingModelDir()
	if modelDir == "" {
		return 0, 0, fmt.Errorf("未找到嵌入模型")
	}
	embedFn := func(text string) ([]float32, error) {
		return rag.EmbedQuery(modelDir, text)
	}

	var entities []memory.ExtractedEntity
	var relations []memory.ExtractedRelation
	seen := map[string]bool{}

	addEntity := func(name, etype string) {
		norm := strings.ToLower(strings.TrimSpace(name))
		if norm == "" || seen[norm] {
			return
		}
		seen[norm] = true
		entities = append(entities, memory.ExtractedEntity{Name: name, Type: etype})
	}

		// ── Clear old graph nodes for this domain before rebuild ──
	log.Printf("[memory] 图谱重建: 清除 %s 领域的旧节点", libraryID)
	if err := a.memoryStore.DeleteNodesByLibrary(libraryID); err != nil {
		log.Printf("[memory] 清除旧节点失败 (可能没有): %v", err)
	} else {
		log.Printf("[memory] 图谱重建: 已清除 %s 的旧节点", libraryID)
	}

	// ── Wiki pages as entities ──
	if pages, pErr := a.WikiListPages(libraryID); pErr == nil {
		for _, p := range pages {
			addEntity(p.Title, "wiki_page")
			relations = append(relations, memory.ExtractedRelation{
				Subject: "Wiki", Predicate: "包含页面", Object: p.Title,
			})
			// Read page content for topic extraction.
			if page, rErr := a.WikiReadPage(libraryID, p.ID); rErr == nil {
				if content, ok := page["content"].(string); ok && content != "" {
					terms := extractTopicTerms(content)
					for _, t := range terms {
						addEntity(t, "topic")
						relations = append(relations, memory.ExtractedRelation{
							Subject: p.Title, Predicate: "讨论", Object: t,
						})
					}
				}
			}
		}
	}

	// ── KB documents as entities ──
	if ragStore, rErr := a.getRagStore(); rErr == nil {
		for _, kb := range ragStore.ListKBs(libraryID) {
			addEntity(kb.Name, "knowledge_base")
			relations = append(relations, memory.ExtractedRelation{
				Subject: "知识库", Predicate: "包含", Object: kb.Name,
			})
			if docs, dErr := ragStore.ListDocuments(kb.ID); dErr == nil {
				for i, d := range docs {
					// Prefer metadata source, then preview text, then a readable label.
					name := ""
					if d.Metadata != nil {
						if src, ok := d.Metadata["source"]; ok && src != "" {
							name = src
						}
					}
					if name == "" {
						name = truncateLabel(d.Preview, 60)
					}
					if name == "" {
						// Never use raw UUID — generate a readable fallback.
						name = fmt.Sprintf("%s-doc-%d", kb.Name, i+1)
					}
					addEntity(name, "document")
					relations = append(relations, memory.ExtractedRelation{
						Subject: kb.Name, Predicate: "包含文档", Object: name,
					})
					// Extract topics from document preview.
					terms := extractTopicTerms(d.Preview)
					for _, t := range terms {
						addEntity(t, "topic")
						relations = append(relations, memory.ExtractedRelation{
							Subject: name, Predicate: "讨论", Object: t,
						})
					}
				}
			}
		}
	}

	// ── Domain agents as entities ──
	if a.agentManager != nil {
		for _, ag := range a.agentManager.ListByLibrary(libraryID) {
			addEntity(ag.Name, "agent")
			relations = append(relations, memory.ExtractedRelation{
				Subject: "Agent", Predicate: "包含", Object: ag.Name,
			})
		}
	}

	// ── Phase 1: Write deterministic edges NOW (no LLM dependency) ──
	if len(entities) > 0 {
		// Snapshot current relations for the first ingest (deterministic only).
		snapRelations := make([]memory.ExtractedRelation, len(relations))
		copy(snapRelations, relations)
		// Phase 1 uses nil embedFn: skip coreference embeddings for speed (200+ entities).
		// Nodes are created without vectors here; Phase 2 LLM pass adds topic entities with vectors.
		if err := a.memoryStore.IngestGraph(entities, snapRelations, "graph_rebuild", libraryID, nil); err != nil {
			return 0, 0, err
		}
		log.Printf("[memory] 图谱重建 (%s) Phase 1: %d 实体, %d 关系",
			libraryID, len(entities), len(relations))
	}

	// ── Phase 2: LLM synthesis async (don't block the response) ──
	go func() {
		keyTopics, crossRefs := a.synthesizeDomainTopics(libraryID)
		if len(keyTopics)+len(crossRefs) == 0 {
			return
		}
		var llmEntities []memory.ExtractedEntity
		var llmRelations []memory.ExtractedRelation
		for _, t := range keyTopics {
			llmEntities = append(llmEntities, memory.ExtractedEntity{Name: t, Type: "domain_topic"})
			llmRelations = append(llmRelations, memory.ExtractedRelation{
				Subject: "领域", Predicate: "涵盖", Object: t,
			})
		}
		for _, x := range crossRefs {
			llmEntities = append(llmEntities, memory.ExtractedEntity{Name: x, Type: "domain"})
			llmRelations = append(llmRelations, memory.ExtractedRelation{
				Subject: "领域", Predicate: "关联", Object: x,
			})
		}
		if err := a.memoryStore.IngestGraph(llmEntities, llmRelations, "graph_rebuild_llm", libraryID, embedFn); err != nil {
			log.Printf("[memory] 图谱重建 LLM 阶段失败: %v", err)
			return
		}
		log.Printf("[memory] 图谱重建 Phase 2 LLM: %d 主题 + %d 关联",
			len(keyTopics), len(crossRefs))
		a.emitChanged("memory:changed", "update", "")
	}()

	return len(entities), len(relations), nil
}

// synthesizeDomainTopics makes one LLM call to derive cross-cutting domain-level
// themes from the Wiki pages in a domain. Returns (keyTopics, crossRefs).
// Unlike synthesizeDomains (which groups files into domains), this operates on an
// already-established domain — it reads existing Wiki page summaries and asks the
// LLM to extract overarching themes and cross-domain connections.
func (a *App) synthesizeDomainTopics(libraryID string) ([]string, []string) {
	pages, err := a.WikiListPages(libraryID)
	if err != nil || len(pages) == 0 {
		return nil, nil
	}

	// Build compact page summaries (title + topics + entities from markdown).
	var lines []string
	type pageSummary struct {
		Title   string
		Topics  []string
		Entities []string
	}
	var summaries []pageSummary
	for i, p := range pages {
		page, rErr := a.WikiReadPage(libraryID, p.ID)
		if rErr != nil {
			continue
		}
		content, _ := page["content"].(string)
		if content == "" {
			continue
		}
		ps := pageSummary{Title: p.Title}
		ps.Topics = parseWikiSection(content, "主题")
		ps.Entities = parseWikiSection(content, "实体")
		summaries = append(summaries, ps)
		lines = append(lines, fmt.Sprintf("%d. %s | 主题: %s | 实体: %s",
			i+1, ps.Title, strings.Join(ps.Topics, "、"), strings.Join(ps.Entities, "、")))
	}

	if len(summaries) == 0 {
		return nil, nil
	}

	// Resolve LLM provider.
	provider, pErr := a.resolveActiveProvider()
	if pErr != nil {
		log.Printf("[memory] synthesizeDomainTopics: no provider (%v)", pErr)
		return nil, nil
	}

	// Build prompt with size limit (max ~6K chars → ~1500 tokens, safe for small models).
	joined := strings.Join(lines, "\n")
	if len(joined) > 6000 {
		// Truncate: keep first 20 and last 10 summaries.
		maxIdx := len(summaries)
		if maxIdx > 30 {
			keep := lines[:20]
			keep = append(keep, fmt.Sprintf("... (省略 %d 个文档) ...", maxIdx-30))
			keep = append(keep, lines[maxIdx-10:]...)
			joined = strings.Join(keep, "\n")
		}
	}
	prompt := fmt.Sprintf(
		`你是一个知识架构师。以下是对一个知识领域中 %d 个文档的摘要信息。
请提炼出 3-8 个跨文档的领域级核心主题（keyTopics）和 0-5 个与其他领域的关联（crossRefs）。

文档摘要：
%s

返回 JSON（无 markdown 标记）：
{
  "keyTopics": ["核心主题1", "核心主题2", ...],
  "crossRefs": ["关联领域1", ...]
}`,
		len(summaries), joined)

	msgs := []map[string]any{{"role": "user", "content": prompt}}
	msgsJSON, _ := json.Marshal(msgs)
	temp := 0.3
	resp, err := a.chatCompletion(provider, msgsJSON, nil, chatOpts{Temperature: &temp})
	if err != nil {
		log.Printf("[memory] synthesizeDomainTopics: LLM error (%v)", err)
		return nil, nil
	}

	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, nil
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	text, _ := msg["content"].(string)

	var result struct {
		KeyTopics []string `json:"keyTopics"`
		CrossRefs []string `json:"crossRefs"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		log.Printf("[memory] synthesizeDomainTopics: parse error (%v)", err)
		return nil, nil
	}

	return result.KeyTopics, result.CrossRefs
}

// parseWikiSection extracts comma/ideographic-comma separated terms from a
// named markdown section (e.g., "## 主题\n\nterm1、term2"). Returns the list.
func parseWikiSection(md, section string) []string {
	// Find "## section" header.
	idx := strings.Index(md, "## "+section)
	if idx < 0 {
		return nil
	}
	// Extract line after header.
	rest := md[idx+len("## "+section):]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	// Stop at next header or horizontal rule.
	if end := strings.IndexAny(rest, "#-"); end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	// Split on Chinese/English separators.
	parts := strings.FieldsFunc(rest, func(r rune) bool {
		return r == '、' || r == ',' || r == '，' || r == ' ' || r == '\n' || r == '|'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// extractTopicTerms pulls significant topical terms from text content.
// Returns up to 15 unique terms (min 3 chars each), excluding noise words.
func extractTopicTerms(text string) []string {
	noise := map[string]bool{
		"the": true, "and": true, "for": true, "that": true, "this": true,
		"with": true, "from": true, "are": true, "has": true, "its": true,
		"not": true, "but": true, "was": true, "all": true, "can": true,
		"；": true, "。": true, "？": true, "！": true,
	}
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' ||
			r == ';' || r == ':' || r == '!' || r == '?' || r == '(' ||
			r == ')' || r == '[' || r == ']' || r == '"' || r == '\'' ||
			r == '#' || r == '*' || r == '-' || r == '_' || r == '|'
	})
	seen := map[string]bool{}
	var terms []string
	for _, f := range fields {
		if len(f) < 3 || noise[f] || seen[f] {
			continue
		}
		seen[f] = true
		terms = append(terms, f)
		if len(terms) >= 15 {
			break
		}
	}
	return terms
}

// truncateLabel returns up to maxRunes runes from text, appending "..." if truncated.
func truncateLabel(text string, maxRunes int) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

// collectContentTerms extracts significant lowercase terms from text content
// (splits on whitespace/punctuation, keeps tokens >= 2 chars).
func collectContentTerms(out map[string]bool, text string) {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' ||
			r == ';' || r == ':' || r == '!' || r == '?' || r == '(' ||
			r == ')' || r == '[' || r == ']' || r == '{' || r == '}' ||
			r == '"' || r == '\'' || r == '#' || r == '*' || r == '-' || r == '_'
	})
	for _, f := range fields {
		if len(f) >= 2 {
			out[f] = true
		}
	}
}

// MemoryEdgeRename renames the relation type of an edge.
func (a *App) MemoryEdgeRename(id, newType string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.RenameEdge(id, newType); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryEdgeAdd manually adds a relation (replaces=false coexists).
func (a *App) MemoryEdgeAdd(srcID, dstID, relType string, replaces bool) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.AddEdge(srcID, dstID, relType, "{}", "", "[]", replaces); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryEdgeAddByNames upserts source and destination entities by name (scoped to
// workspace_id), then adds an edge between them. This is the LLM-facing graph
// builder — accepts entity names rather than internal node IDs.
//
// If workspaceID is empty, it infers the domain from connected nodes: if either
// entity already exists in a non-default domain, the other entity inherits that
// domain. This ensures that new entities discovered via edges (e.g. "DomainPanel.vue
// → 对话面板" added alongside an EverEvo node) automatically land in the right
// domain instead of falling into "default".
func (a *App) MemoryEdgeAddByNames(srcName, dstName, relType, workspaceID string, replaces bool) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}

	// ── Domain inference: if no explicit workspace, inherit from existing nodes ──
	if workspaceID == "" {
		_, srcWS := a.memoryStore.FindNodeByName(srcName)
		_, dstWS := a.memoryStore.FindNodeByName(dstName)
		if srcWS != "" && !a.memoryStore.IsDefaultWS(srcWS) {
			workspaceID = srcWS
		} else if dstWS != "" && !a.memoryStore.IsDefaultWS(dstWS) {
			workspaceID = dstWS
		} else {
			workspaceID = a.resolveLibraryID("")
		}
	}

	srcID, err := a.memoryStore.UpsertNode("", srcName, workspaceID, nil)
	if err != nil {
		return fmt.Errorf("源实体 %q: %w", srcName, err)
	}
	dstID, err := a.memoryStore.UpsertNode("", dstName, workspaceID, nil)
	if err != nil {
		return fmt.Errorf("目标实体 %q: %w", dstName, err)
	}

	// ── Back-propagate: ensure both nodes share the same domain ──
	if workspaceID != "" && workspaceID != "default" {
		a.memoryStore.SetNodeWorkspace(srcID, workspaceID)
		a.memoryStore.SetNodeWorkspace(dstID, workspaceID)
	}

	if err := a.memoryStore.AddEdge(srcID, dstID, relType, "{}", "", "[]", replaces); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "update", "")
	return nil
}

// MemoryRecallGraphContext does a lightweight keyword search of graph nodes
// matching the query, returning formatted "entity → relation → entity" lines,
// entity property snapshots, and relevant events.
// Unlike the full graph retrieval in MemoryRecall, this needs no embedding model.
func (a *App) MemoryRecallGraphContext(query, libraryID string) string {
	if a.memoryStore == nil || query == "" {
		return ""
	}
	nodes, _ := a.memoryStore.SearchNodesByKeyword(query, 8)
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	seen := map[string]bool{}

	for _, n := range nodes {
		// Graph edges (entity → relation → entity)
		edges, _ := a.memoryStore.ListEdgesForNode(n.ID, 5)
		for _, e := range edges {
			key := e.SrcName + e.Type + e.DstName
			if seen[key] {
				continue
			}
			seen[key] = true
			sb.WriteString(e.SrcName + " → " + e.Type + " → " + e.DstName)
			if e.Weight > 1 {
				sb.WriteString(fmt.Sprintf(" (×%d)", e.Weight))
			}
			sb.WriteString("\n")
		}

		// Entity properties (temporal layer)
		props, _ := a.memoryStore.GetEntityProperties(n.ID)
		for _, p := range props {
			key := n.Name + "::" + p.Property
			if seen[key] {
				continue
			}
			seen[key] = true
			timeInfo := ""
			if p.ValidFrom > 0 || p.ValidTo > 0 {
				timeInfo = formatTimeRange(p.ValidFrom, p.ValidTo)
			}
			sb.WriteString(n.Name + " — " + p.Property + " = " + p.Value + timeInfo + "\n")
		}

		// Related events
		events, _ := a.memoryStore.GetEventsForEntity(n.ID, 3)
		for _, ev := range events {
			key := "event:" + ev.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			sb.WriteString("[事件] " + ev.Title)
			if ev.TimeExpression != "" {
				sb.WriteString(" (" + ev.TimeExpression + ")")
			}
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func formatTimeRange(from, to int64) string {
	f := ""
	t := ""
	if from > 0 {
		f = time.UnixMilli(from).Format("2006-01")
	}
	if to > 0 {
		t = time.UnixMilli(to).Format("2006-01")
	}
	if f != "" && t != "" {
		return " (" + f + " ~ " + t + ")"
	} else if f != "" {
		return " (自 " + f + ")"
	} else if t != "" {
		return " (至 " + t + ")"
	}
	return ""
}

// MemoryGraphStats returns edge-counts-per-type + top hub nodes.
func (a *App) MemoryGraphStats() (*memory.GraphStats, error) {
	if a.memoryStore == nil {
		return &memory.GraphStats{EdgesPerType: map[string]int{}}, nil
	}
	return a.memoryStore.Stats()
}
