//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/agents"
	"everevo/internal/config"
	"everevo/internal/memory"
	"everevo/internal/rag"
)

// ─── Deep Ingestion Pipeline ───────────────────────────────────────
//
// Unlike the basic pipeline which shows LLM only 100-char previews,
// this pipeline lets the LLM deeply read every file (one at a time),
// extract structured understanding, then synthesize everything into
// a rich knowledge architecture.
//
// Flow:
//   Phase 1 (前置): LLM reads each file fully → structured metadata
//   Phase 2 (前置): LLM synthesizes all metadata → domain design
//   Phase 3 (执行): Create KBs, embed, build agents
//   Phase 4 (后置): LLM reviews, cross-links, generates FAQ/README
//
// Context safety: files are processed ONE AT A TIME. The LLM sees
// full file content but never more than one file per call. Metadata
// from all files is accumulated and synthesized in a second pass.

// ─── Rich metadata per file ───────────────────────────────────────

type FileMeta struct {
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	Ext        string   `json:"ext"`
	Size       int64    `json:"size"`
	Hash       string   `json:"hash"`

	// LLM-extracted understanding
	Summary      string   `json:"summary"`      // 一句话概括
	Topics       []string `json:"topics"`        // 主题关键词
	Entities     []string `json:"entities"`      // 函数名/类名/关键概念
	DomainHint   string   `json:"domainHint"`    // LLM 建议归属的领域
	Importance   string   `json:"importance"`    // high | medium | low
	Dependencies []string `json:"dependencies"`  // 依赖的其他文件或概念
	FileType     string   `json:"fileType"`      // code | doc | config | data | other
	Quality      string   `json:"quality"`       // good | ok | noise
}

// ─── Domain design from synthesis ──────────────────────────────────

type DeepDomain struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Purpose     string     `json:"purpose"`     // 这个领域解决什么问题
	AgentPrompt string     `json:"agentPrompt"` // LLM 为 Agent 生成的专属 system prompt
	Files       []FileMeta `json:"files"`
	KeyTopics   []string   `json:"keyTopics"`   // 该领域的核心主题
	CrossRefs   []string   `json:"crossRefs"`   // 与其他领域的关联
}

type DeepAnalysis struct {
	FileMetas   []FileMeta  `json:"fileMetas"`
	Domains     []DeepDomain `json:"domains"`
	Skipped     []string    `json:"skipped"`
	KnowledgeGaps []string  `json:"knowledgeGaps"` // LLM 发现的缺失内容
}

// ─── Phase 1: Deep per-file analysis ──────────────────────────────

// IngestDeepAnalyze reads every file and has the LLM deeply analyze
// each one, extracting structured metadata. Files are processed one
// at a time — the LLM sees full content but never context-overflows.
func (a *App) IngestDeepAnalyze(dirPath, libraryID string) (*DeepAnalysis, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("目录不存在: %s", dirPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("不是目录: %s", dirPath)
	}

	rawFiles, skipped := scanIngestFiles(dirPath)
	if len(rawFiles) == 0 {
		return nil, fmt.Errorf("无可用文件 (跳过 %d)", len(skipped))
	}

	provider, err := a.resolveActiveProvider()
	if err != nil {
		return nil, fmt.Errorf("LLM provider: %w", err)
	}

	// Phase 1: Analyze each file individually with LLM.
	var metas []FileMeta
	total := len(rawFiles)

	for i, rf := range rawFiles {
		a.emitIngestDeep("file_analyze", map[string]any{
			"index": i + 1, "total": total, "name": rf.Name,
		})

		meta, err := a.deepAnalyzeFile(provider, rf)
		if err != nil {
			log.Printf("[deep] %s 分析失败: %v", rf.Name, err)
			skipped = append(skipped, rf.Name+": "+err.Error())
			continue
		}
		metas = append(metas, *meta)

		a.emitIngestDeep("file_done", map[string]any{
			"index": i + 1, "total": total, "name": rf.Name,
			"summary": meta.Summary, "importance": meta.Importance,
			"domainHint": meta.DomainHint,
		})
	}

	if len(metas) == 0 {
		return nil, fmt.Errorf("所有文件分析失败")
	}
	log.Printf("[deep] Phase 1 完成: %d 文件分析完毕", len(metas))

	// Phase 2: Synthesize metadata into domain design.
	a.emitIngestDeep("synthesize", map[string]any{"files": len(metas)})
	domains, gaps, err := a.synthesizeDomains(provider, metas)
	if err != nil {
		log.Printf("[deep] 综合失败: %v", err)
		// Fallback: basic domain grouping.
		domains = fallbackDomains(metas)
	}
	log.Printf("[deep] Phase 2 完成: %d 领域", len(domains))

	return &DeepAnalysis{
		FileMetas:     metas,
		Domains:       domains,
		Skipped:       skipped,
		KnowledgeGaps: gaps,
	}, nil
}

// ─── Single file deep analysis ────────────────────────────────────

func (a *App) deepAnalyzeFile(provider *config.LLMProvider, rf ingestRawFile) (*FileMeta, error) {
	data, err := os.ReadFile(rf.Path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	text := string(data)
	if isIngestBinary(data) {
		txt, err := rag.ExtractTextFromPDF(data)
		if err != nil {
			return nil, fmt.Errorf("binary: %w", err)
		}
		text = txt
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty")
	}

	// Truncate if insanely long (50K chars max for analysis).
	if len(text) > 50000 {
		text = text[:50000] + "\n...(truncated)"
	}

	hash, _ := fileHashAndPreview(rf.Path)

	prompt := fmt.Sprintf(
		`深度分析以下文件，提取结构化信息。

文件名: %s
类型: %s
大小: %s

内容:
---
%s
---

以 JSON 格式返回（不要 markdown 标记）：
{
  "summary": "一句话概括文件作用",
  "topics": ["主题1", "主题2", ...],
  "entities": ["关键函数名/类名/变量名/概念", ...],
  "domainHint": "建议归属的领域（中文，2-6字）",
  "importance": "high|medium|low",
  "dependencies": ["此文件依赖的其他文件或概念", ...],
  "fileType": "code|doc|config|data|other",
  "quality": "good|ok|noise"
}`,
		rf.Name, rf.Ext, formatSize(rf.Size), text)

	msgs := []map[string]any{{"role": "user", "content": prompt}}
	msgsJSON, _ := json.Marshal(msgs)
	temp := 0.1
	resp, err := a.chatCompletion(provider, msgsJSON, nil, chatOpts{Temperature: &temp})
	if err != nil {
		return nil, err
	}

	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, fmt.Errorf("LLM no response")
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	content, _ := msg["content"].(string)

	type rawMeta struct {
		Summary      string   `json:"summary"`
		Topics       []string `json:"topics"`
		Entities     []string `json:"entities"`
		DomainHint   string   `json:"domainHint"`
		Importance   string   `json:"importance"`
		Dependencies []string `json:"dependencies"`
		FileType     string   `json:"fileType"`
		Quality      string   `json:"quality"`
	}
	var rm rawMeta
	if err := json.Unmarshal([]byte(cleanJSON(content)), &rm); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	if rm.Importance == "" {
		rm.Importance = "medium"
	}
	if rm.FileType == "" {
		rm.FileType = fileTypeFromExt(rf.Ext)
	}

	return &FileMeta{
		Path: rf.Path, Name: rf.Name, Ext: rf.Ext, Size: rf.Size, Hash: hash,
		Summary: rm.Summary, Topics: rm.Topics, Entities: rm.Entities,
		DomainHint: rm.DomainHint, Importance: rm.Importance,
		Dependencies: rm.Dependencies, FileType: rm.FileType, Quality: rm.Quality,
	}, nil
}

// ─── Phase 2: Synthesize domain design ─────────────────────────────

func (a *App) synthesizeDomains(provider *config.LLMProvider, metas []FileMeta) ([]DeepDomain, []string, error) {
	// Build compact metadata for LLM consumption.
	var lines []string
	for i, m := range metas {
		lines = append(lines, fmt.Sprintf("%d. [%s|%s] %s — %s (topics: %s)",
			i+1, m.Importance, m.FileType, m.Name,
			m.Summary, strings.Join(m.Topics, ", ")))
	}

	prompt := fmt.Sprintf(
		`你是一个知识架构师。以下是对 %d 个文件的深度分析结果。请将它们组织成 2-6 个领域（domain），每个领域是独立的知识库，可以支撑一个专用 AI Agent。

设计原则：
- 相关性优先：内容相关的文件归入同一领域
- 大小适中：每个领域 3-30 个文件，避免过大或过小
- Agent 友好：每个领域的知识应足够支撑一个有用的专家 Agent
- 关注噪音：quality="noise" 的文件可以忽略
- 标注缺口：如果发现缺少某些关键内容，列在 knowledgeGaps 中

文件元数据：
%s

返回 JSON（无 markdown）：
{
  "domains": [
    {
      "name": "领域名(中文2-6字)",
      "description": "一句话描述",
      "purpose": "这个领域解决什么问题",
      "agentPrompt": "为这个领域的专家 Agent 写一段 system prompt（中文，100-200字），告诉 Agent 它负责什么、知识库包含什么、如何回答",
      "fileIndices": [1,3,5],
      "keyTopics": ["核心主题1", "核心主题2"],
      "crossRefs": ["关联的领域名"]
    }
  ],
  "knowledgeGaps": ["缺失的内容1", "缺失的内容2"]
}`,
		len(metas), strings.Join(lines, "\n"))

	msgs := []map[string]any{{"role": "user", "content": prompt}}
	msgsJSON, _ := json.Marshal(msgs)
	temp := 0.4
	resp, err := a.chatCompletion(provider, msgsJSON, nil, chatOpts{Temperature: &temp})
	if err != nil {
		return nil, nil, err
	}

	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, nil, fmt.Errorf("LLM no response")
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	content, _ := msg["content"].(string)

	type rawDomain struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Purpose     string   `json:"purpose"`
		AgentPrompt string   `json:"agentPrompt"`
		FileIndices []int    `json:"fileIndices"`
		KeyTopics   []string `json:"keyTopics"`
		CrossRefs   []string `json:"crossRefs"`
	}
	type rawResp struct {
		Domains       []rawDomain `json:"domains"`
		KnowledgeGaps []string    `json:"knowledgeGaps"`
	}
	var rr rawResp
	if err := json.Unmarshal([]byte(cleanJSON(content)), &rr); err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}

	var domains []DeepDomain
	seen := map[int]bool{}
	for _, rd := range rr.Domains {
		var d DeepDomain
		d.Name = rd.Name
		d.Description = rd.Description
		d.Purpose = rd.Purpose
		d.AgentPrompt = rd.AgentPrompt
		d.KeyTopics = rd.KeyTopics
		d.CrossRefs = rd.CrossRefs
		for _, idx := range rd.FileIndices {
			if idx >= 1 && idx <= len(metas) {
				d.Files = append(d.Files, metas[idx-1])
				seen[idx-1] = true
			}
		}
		if len(d.Files) > 0 {
			domains = append(domains, d)
		}
	}

	// Unclassified files → "其他"
	var orphan []FileMeta
	for i, m := range metas {
		if !seen[i] && m.Quality != "noise" {
			orphan = append(orphan, m)
		}
	}
	if len(orphan) > 0 {
		domains = append(domains, DeepDomain{
			Name: "其他", Description: "未归类的文件",
			Purpose: "存放未被明确分类的内容", Files: orphan,
		})
	}

	return domains, rr.KnowledgeGaps, nil
}

// ─── Phase 3: Commit (create KBs + agents with LLM-designed prompts) ──

func (a *App) IngestDeepCommit(analysis *DeepAnalysis, libraryID string) (*IngestResult, error) {
	if analysis == nil || len(analysis.Domains) == 0 {
		return nil, fmt.Errorf("无领域可提交")
	}

	start := time.Now()
	ingestCtx, ingestCancel = context.WithCancel(context.Background())
	defer func() { ingestCancel = nil }()

	result := &IngestResult{Domains: len(analysis.Domains)}
	modelDir := detectEmbeddingModelDir()
	if modelDir == "" {
		return nil, fmt.Errorf("未找到嵌入模型")
	}

	for di, domain := range analysis.Domains {
		select {
		case <-ingestCtx.Done():
			return nil, fmt.Errorf("已取消")
		default:
		}

		a.emitIngestDeep("domain_start", map[string]any{
			"domain": domain.Name, "index": di + 1, "total": len(analysis.Domains),
			"purpose": domain.Purpose,
		})

		kb, err := a.CreateKnowledgeBase(domain.Name, modelDir, libraryID)
		if err != nil {
			log.Printf("[deep] KB %s 失败: %v", domain.Name, err)
			continue
		}

		domainChunks := 0
		for fi, f := range domain.Files {
			if !f.isNew() {
				continue
			}
			a.emitIngestDeep("file_start", map[string]any{
				"domain": domain.Name, "index": fi + 1, "total": len(domain.Files), "name": f.Name,
			})

			fi2 := IngestFileInfo{Path: f.Path, Name: f.Name, Ext: f.Ext, Size: f.Size, Hash: f.Hash}
			chunks, err := a.chunkAndStore(kb.ID, fi2)
			if err != nil {
				log.Printf("[deep] %s/%s: %v", domain.Name, f.Name, err)
				continue
			}
			domainChunks += chunks
			result.TotalChunks += chunks
			result.NewFiles++
			a.markFileIngested(f.Path, f.Hash)

			// Per-file wiki page (deep analysis metadata in Markdown).
			if a.memoryStore != nil {
				pageID := sanitizeWikiPageID(f.Name)
				pageContent := buildFileWikiPage(f)
				if saveErr := a.WikiSavePage(libraryID, pageID, f.Name, pageContent); saveErr != nil {
					log.Printf("[deep] wiki %s: %v", f.Name, saveErr)
				} else {
					result.WikiPages++
				}
			}
		}

		log.Printf("[deep] 域 %s: %d chunks, %d wiki", domain.Name, domainChunks, result.WikiPages)
		a.emitIngestDeep("domain_done", map[string]any{"domain": domain.Name, "chunks": domainChunks})

		// Use LLM-designed agent prompt if available, otherwise generate.
		sysPrompt := domain.AgentPrompt
		if sysPrompt == "" {
			sysPrompt = defaultAgentPrompt(domain)
		}
		// Append KB reference.
		sysPrompt += fmt.Sprintf("\n\n知识库 ID: %s。使用 knowledge_search 检索。回答时注明信息来源。", kb.ID)

		now := time.Now().UnixMilli()
		agentName := sanitizeAgentName(domain.Name) + " 专家"
		agent := agents.Agent{
			Name: agentName,
			Description: fmt.Sprintf("%s — %s", domain.Description, domain.Purpose),
			Icon: "📚", SystemPrompt: sysPrompt,
			Tools:    []string{"knowledge_search", "knowledge_list", "knowledge_context"},
			LibraryID: libraryID,
			CreatedAt: now, UpdatedAt: now,
		}
		created, err := a.agentManager.Create(agent)
		if err != nil {
			log.Printf("[deep] agent(%s): %v", domain.Name, err)
		} else {
			result.Agents = append(result.Agents, created.Name)
		}

		// ── Build knowledge graph (after KB + Agent are created) ──
		if a.memoryStore != nil {
			nodeCount, edgeCount := a.buildDeepGraph(domain, kb.Name, agentName, libraryID, modelDir)
			result.GraphNodes += nodeCount
			result.GraphEdges += edgeCount
			log.Printf("[deep] 域 %s: %d 图谱节点, %d 关系 (含 KB/Agent)", domain.Name, nodeCount, edgeCount)
		}
	}

	result.TotalFiles = len(analysis.FileMetas)
	result.Duration = time.Since(start).Round(time.Second).String()
	a.emitIngestDeep("done", map[string]any{"result": result})
	log.Printf("[deep] 完成: %d 文件, %d chunks, %d 域, %d agent, %s, gaps: %v",
		result.TotalFiles, result.TotalChunks, result.Domains,
		len(result.Agents), result.Duration, analysis.KnowledgeGaps)

	return result, nil
}

// ─── Phase 4: Post-cleanup ────────────────────────────────────────

// IngestDeepReview runs post-ingestion quality checks: samples random
// chunks for relevance, checks domain coherence, generates FAQ entries.
func (a *App) IngestDeepReview(analysis *DeepAnalysis, libraryID string) (map[string]any, error) {
	provider, err := a.resolveActiveProvider()
	if err != nil {
		return nil, err
	}

	log.Printf("[deep] Phase 4: 后置审查...")

	// Collect noise files and low-quality items.
	var noiseFiles, lowQuality []string
	for _, m := range analysis.FileMetas {
		if m.Quality == "noise" {
			noiseFiles = append(noiseFiles, m.Name)
		}
		if m.Importance == "low" && m.Quality == "ok" {
			lowQuality = append(lowQuality, m.Name)
		}
	}

	// Generate domain FAQ if there are few domains and enough context.
	var faqs []string
	for _, d := range analysis.Domains {
		if len(d.Files) > 3 && len(d.Files) < 30 {
			faq, err := a.generateDomainFAQ(provider, &d)
			if err != nil {
				log.Printf("[deep] FAQ(%s): %v", d.Name, err)
			} else {
				faqs = append(faqs, faq...)
			}
		}
	}

	// Summary report.
	report := map[string]any{
		"noiseFiles":     noiseFiles,
		"lowQuality":     lowQuality,
		"suggestCleanup": len(noiseFiles) > 0 || len(lowQuality) > 10,
		"faqs":           faqs,
		"gaps":           analysis.KnowledgeGaps,
	}

	a.emitIngestDeep("review_done", report)
	log.Printf("[deep] Phase 4 完成: noise=%d, lowQ=%d, faq=%d, gaps=%d",
		len(noiseFiles), len(lowQuality), len(faqs), len(analysis.KnowledgeGaps))

	return report, nil
}

func (a *App) generateDomainFAQ(provider *config.LLMProvider, d *DeepDomain) ([]string, error) {
	var fileList []string
	for _, f := range d.Files {
		fileList = append(fileList, fmt.Sprintf("- %s: %s", f.Name, f.Summary))
	}

	prompt := fmt.Sprintf(
		`基于以下领域知识库，生成 3-5 个常见问题及解答（FAQ）。

领域: %s
用途: %s
包含文件:
%s

返回 JSON 数组:
[{"q":"问题","a":"简短回答"}]`,
		d.Name, d.Purpose, strings.Join(fileList, "\n"))

	msgs := []map[string]any{{"role": "user", "content": prompt}}
	msgsJSON, _ := json.Marshal(msgs)
	temp := 0.3
	resp, err := a.chatCompletion(provider, msgsJSON, nil, chatOpts{Temperature: &temp})
	if err != nil {
		return nil, err
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return nil, fmt.Errorf("no response")
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	content, _ := msg["content"].(string)

	type faq struct {
		Q string `json:"q"`
		A string `json:"a"`
	}
	var items []faq
	json.Unmarshal([]byte(cleanJSON(content)), &items)

	var out []string
	for _, item := range items {
		out = append(out, fmt.Sprintf("Q: %s\nA: %s", item.Q, item.A))
	}
	return out, nil
}

// ─── Convenience: one-shot deep ingest ────────────────────────────

func (a *App) IngestDeep(dirPath, libraryID string) (*DeepAnalysis, error) {
	analysis, err := a.IngestDeepAnalyze(dirPath, libraryID)
	if err != nil {
		return nil, err
	}
	if _, err := a.IngestDeepCommit(analysis, libraryID); err != nil {
		return nil, err
	}
	// Phase 4 runs async — don't block the user.
	go func() {
		if _, err := a.IngestDeepReview(analysis, libraryID); err != nil {
			log.Printf("[deep] review: %v", err)
		}
	}()
	return analysis, nil
}

// ─── Helpers ───────────────────────────────────────────────────────

// sanitizeWikiPageID converts a file name into a safe wiki page ID.
func sanitizeWikiPageID(name string) string {
	// Remove extension, replace spaces/special chars with underscore.
	base := name
	if idx := strings.LastIndex(name, "."); idx > 0 {
		base = name[:idx]
	}
	base = strings.Map(func(r rune) rune {
		if r == ' ' || r == '(' || r == ')' || r == '[' || r == ']' || r == '/' || r == '\\' || r == ':' || r == ',' {
			return '_'
		}
		return r
	}, base)
	return strings.ReplaceAll(strings.ReplaceAll(base, "__", "_"), "__", "_")
}

// buildFileWikiPage generates a Markdown page from FileMeta for per-file wikis.
func buildFileWikiPage(f FileMeta) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", f.Name))
	sb.WriteString(fmt.Sprintf("**类型**: %s  |  **重要性**: %s  |  **质量**: %s\n\n", f.FileType, f.Importance, f.Quality))
	if f.Summary != "" {
		sb.WriteString(fmt.Sprintf("## 摘要\n\n%s\n\n", f.Summary))
	}
	if len(f.Topics) > 0 {
		sb.WriteString(fmt.Sprintf("## 主题\n\n%s\n\n", strings.Join(f.Topics, "、 ")))
	}
	if len(f.Entities) > 0 {
		sb.WriteString(fmt.Sprintf("## 实体\n\n%s\n\n", strings.Join(f.Entities, "、 ")))
	}
	if len(f.Dependencies) > 0 {
		sb.WriteString(fmt.Sprintf("## 依赖\n\n%s\n\n", strings.Join(f.Dependencies, "、 ")))
	}
	if f.DomainHint != "" {
		sb.WriteString(fmt.Sprintf("## 领域\n\n%s\n\n", f.DomainHint))
	}
	sb.WriteString(fmt.Sprintf("---\n*文件: %s  |  大小: %d bytes*\n", f.Path, f.Size))
	return sb.String()
}

// buildDeepGraph converts extracted domain metadata into knowledge graph entities
// and relations, then writes them via IngestGraph. kbName and agentName are the
// KB and Agent created for this domain — they are added as graph nodes and linked.
// modelDir is the embedding model path for vectorizing entities so they are
// semantically searchable via EntitySearch (QueryMemory graph retrieval).
// Returns (nodes, edges) counts.
func (a *App) buildDeepGraph(domain DeepDomain, kbName, agentName, workspaceID, modelDir string) (int, int) {
	seen := map[string]bool{}
	var entities []memory.ExtractedEntity
	var relations []memory.ExtractedRelation

	addEntity := func(name, etype string) {
		norm := strings.ToLower(strings.TrimSpace(name))
		if norm == "" || seen[norm] {
			return
		}
		seen[norm] = true
		entities = append(entities, memory.ExtractedEntity{Name: name, Type: etype})
	}

	// Domain itself as a topic area entity.
	addEntity(domain.Name, "domain")

	// Key topics from domain synthesis.
	for _, t := range domain.KeyTopics {
		addEntity(t, "topic")
		relations = append(relations, memory.ExtractedRelation{
			Subject: domain.Name, Predicate: "涵盖", Object: t,
		})
	}

	// Entities and dependencies from per-file analysis.
	for _, f := range domain.Files {
		for _, e := range f.Entities {
			addEntity(e, "concept")
			relations = append(relations, memory.ExtractedRelation{
				Subject: f.Name, Predicate: "定义", Object: e,
			})
		}
		for _, dep := range f.Dependencies {
			addEntity(dep, "concept")
			relations = append(relations, memory.ExtractedRelation{
				Subject: f.Name, Predicate: "依赖", Object: dep,
			})
		}
		// File belongs to domain.
		relations = append(relations, memory.ExtractedRelation{
			Subject: domain.Name, Predicate: "包含文件", Object: f.Name,
		})
	}

	// Cross-references to other domains.
	for _, xref := range domain.CrossRefs {
		addEntity(xref, "domain")
		relations = append(relations, memory.ExtractedRelation{
			Subject: domain.Name, Predicate: "关联", Object: xref,
		})
	}

	// KB and Agent as graph nodes, linked to the domain.
	if kbName != "" {
		addEntity(kbName, "knowledge_base")
		relations = append(relations, memory.ExtractedRelation{
			Subject: domain.Name, Predicate: "拥有知识库", Object: kbName,
		})
	}
	if agentName != "" {
		addEntity(agentName, "agent")
		relations = append(relations, memory.ExtractedRelation{
			Subject: domain.Name, Predicate: "拥有Agent", Object: agentName,
		})
		if kbName != "" {
			relations = append(relations, memory.ExtractedRelation{
				Subject: agentName, Predicate: "检索", Object: kbName,
			})
		}
		relations = append(relations, memory.ExtractedRelation{
			Subject: agentName, Predicate: "服务", Object: domain.Name,
		})
	}

	if len(entities) == 0 && len(relations) == 0 {
		return 0, 0
	}

	// Build graph with vector embeddings so entities are semantically searchable
	// (EntitySearch in QueryMemory can find seed nodes by name similarity).
	embedFn := func(text string) ([]float32, error) {
		return rag.EmbedQuery(modelDir, text)
	}
	if err := a.memoryStore.IngestGraph(entities, relations, "ingest_deep", workspaceID, embedFn); err != nil {
		log.Printf("[deep] 图谱构建失败 (%s): %v", domain.Name, err)
		return 0, 0
	}

	return len(entities), len(relations)
}

func (a *App) emitIngestDeep(event string, data any) {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "ingest-deep:"+event, data)
	}
}

func fallbackDomains(metas []FileMeta) []DeepDomain {
	groups := map[string][]FileMeta{}
	for _, m := range metas {
		hint := m.DomainHint
		if hint == "" {
			hint = "其他"
		}
		groups[hint] = append(groups[hint], m)
	}
	var domains []DeepDomain
	for name, files := range groups {
		domains = append(domains, DeepDomain{
			Name: name, Description: "自动分组",
			Files: files,
		})
	}
	return domains
}

func defaultAgentPrompt(d DeepDomain) string {
	var fileList []string
	for _, f := range d.Files {
		fileList = append(fileList, fmt.Sprintf("- %s: %s", f.Name, f.Summary))
	}
	return fmt.Sprintf(
		"你是「%s」领域专家。%s\n\n知识库文件:\n%s\n\n用中文回答，基于知识库检索。",
		d.Name, d.Purpose, strings.Join(fileList, "\n"))
}

func fileTypeFromExt(ext string) string {
	switch ext {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".c", ".cpp", ".h":
		return "code"
	case ".md", ".rst", ".txt", ".adoc":
		return "doc"
	case ".json", ".yaml", ".yml", ".toml", ".xml", ".ini", ".cfg", ".env":
		return "config"
	case ".csv", ".tsv", ".sql", ".parquet":
		return "data"
	default:
		return "other"
	}
}

// isNew checks if a FileMeta's file hasn't been ingested yet.
func (m *FileMeta) isNew() bool {
	// Reuse the ingest manifest from app_ingest.go.
	return !fileWasIngested(m.Path, m.Hash)
}

func fileWasIngested(path, hash string) bool {
	// Direct inline check (avoid dependency on a. method).
	m := loadIngestManifestGlobal()
	prev, ok := m.Files[path]
	return ok && prev == hash
}

func loadIngestManifestGlobal() struct{ Files map[string]string } {
	m := struct{ Files map[string]string }{Files: map[string]string{}}
	dir, _ := os.Getwd()
	path := filepath.Join(os.TempDir(), "everevo_ingest_"+filepath.Base(dir)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	json.Unmarshal(data, &m)
	return m
}
