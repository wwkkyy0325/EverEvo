//go:build windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"everevo/internal/agents"
	"everevo/internal/ingest"
	"everevo/internal/rag"
)

// ─── Upgraded Folder Ingestion Pipeline ────────────────────────────
//
// Key improvements over v1:
//   1. Per-domain KB — each domain gets its own isolated knowledge base
//   2. Smart chunking — different strategies for code/markdown/plain
//   3. Two-phase flow — analyze (dry-run) → user reviews → commit
//   4. Incremental — hash files, skip unchanged on re-ingestion
//   5. Auto-split large files — split code at function/class boundaries

// ─── Types ─────────────────────────────────────────────────────────

type IngestFileInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Ext     string `json:"ext"`
	Size    int64  `json:"size"`
	Preview string `json:"preview"` // first 200 chars
	Hash    string `json:"hash"`    // content hash for incremental
	IsNew   bool   `json:"isNew"`   // not previously ingested
}

type IngestDomain struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Files       []IngestFileInfo `json:"files"`
	FileCount   int              `json:"fileCount"`
	TotalSize   int64            `json:"totalSize"`
}

type IngestAnalysis struct {
	TotalFiles   int            `json:"totalFiles"`
	TotalSize    int64          `json:"totalSize"`
	SkippedFiles []string       `json:"skippedFiles"`
	Domains      []IngestDomain `json:"domains"`
	Suggested    string         `json:"suggested"` // human-readable suggestion
}

type IngestResult struct {
	TotalFiles   int      `json:"totalFiles"`
	TotalChunks  int      `json:"totalChunks"`
	NewFiles     int      `json:"newFiles"`
	Domains      int      `json:"domains"`
	Agents       []string `json:"agents"`
	WikiPages    int      `json:"wikiPages"`
	GraphNodes   int      `json:"graphNodes"`
	GraphEdges   int      `json:"graphEdges"`
	Duration     string   `json:"duration"`
}

var ingestCtx context.Context
var ingestCancel context.CancelFunc

// ─── Phase 1: Analyze (dry-run, no embedding) ──────────────────────

// IngestAnalyze scans a directory and returns a categorization plan
// WITHOUT embedding or storing anything. The user/AI reviews the plan,
// then calls IngestCommit to execute.
func (a *App) IngestAnalyze(dirPath string) (*IngestAnalysis, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("目录不存在: %s", dirPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("不是目录: %s", dirPath)
	}

	// Scan all files, compute hashes, check which are new.
	files, skipped := scanIngestFiles(dirPath)
	if len(files) == 0 {
		return nil, fmt.Errorf("没有可处理的文件 (跳过 %d 个)", len(skipped))
	}

	// Build file summaries with content preview and hash.
	summaries := make([]IngestFileInfo, 0, len(files))
	var totalSize int64
	for _, f := range files {
		hash, preview := fileHashAndPreview(f.Path)
		isNew := !a.isFileIngested(f.Path, hash)
		summaries = append(summaries, IngestFileInfo{
			Path:    f.Path,
			Name:    f.Name,
			Ext:     f.Ext,
			Size:    f.Size,
			Preview: preview,
			Hash:    hash,
			IsNew:   isNew,
		})
		totalSize += f.Size
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})

	// LLM categorization using only summaries.
	domains, err := a.categorizeSummaries(summaries)
	if err != nil {
		return nil, err
	}

	newCount := 0
	for _, s := range summaries {
		if s.IsNew {
			newCount++
		}
	}

	var domainNames []string
	for _, d := range domains {
		domainNames = append(domainNames, d.Name)
	}

	return &IngestAnalysis{
		TotalFiles:   len(summaries),
		TotalSize:    totalSize,
		SkippedFiles: skipped,
		Domains:      domains,
		Suggested: fmt.Sprintf(
			"共 %d 个文件 (%s)，其中 %d 个新文件。建议分为 %d 个领域：%s。确认后将创建对应的知识库和专家 Agent。",
			len(summaries), formatSize(totalSize), newCount,
			len(domains), strings.Join(domainNames, "、")),
	}, nil
}

// ─── Phase 2: Commit (embed + store + create agents) ───────────────

// IngestCommit executes the ingestion plan produced by IngestAnalyze.
// It creates per-domain KBs, chunks and embeds each file into its
// domain's KB, then creates domain agents.
func (a *App) IngestCommit(analysis *IngestAnalysis) (*IngestResult, error) {
	if analysis == nil || len(analysis.Domains) == 0 {
		return nil, fmt.Errorf("没有可提交的领域")
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

		// Create per-domain KB.
		a.emitIngest("domain_start", map[string]any{
			"domain": domain.Name, "index": di + 1, "total": len(analysis.Domains),
		})

		kb, err := a.CreateKnowledgeBase(domain.Name, modelDir, "")
		if err != nil {
			log.Printf("[ingest] 创建 KB %s 失败: %v", domain.Name, err)
			continue
		}

		domainChunks := 0
		for fi, f := range domain.Files {
			if !f.IsNew {
				continue // incremental skip
			}
			a.emitIngest("file_start", map[string]any{
				"domain": domain.Name, "index": fi + 1, "total": len(domain.Files),
				"name": f.Name,
			})

			chunks, err := a.chunkAndStore(kb.ID, f)
			if err != nil {
				log.Printf("[ingest] %s/%s 失败: %v", domain.Name, f.Name, err)
				a.emitIngest("file_error", map[string]any{"domain": domain.Name, "name": f.Name, "error": err.Error()})
				continue
			}

			domainChunks += chunks
			result.TotalChunks += chunks
			result.NewFiles++
			a.markFileIngested(f.Path, f.Hash)

			a.emitIngest("file_done", map[string]any{
				"domain": domain.Name, "name": f.Name, "chunks": chunks,
			})
		}

		log.Printf("[ingest] 域 %s: %d chunks", domain.Name, domainChunks)
		a.emitIngest("domain_done", map[string]any{"domain": domain.Name, "chunks": domainChunks})

		// Create domain agent.
		agentName, err := a.createDomainAgentV2(&domain, kb.ID)
		if err != nil {
			log.Printf("[ingest] 创建 agent(%s) 失败: %v", domain.Name, err)
		} else {
			result.Agents = append(result.Agents, agentName)
		}
	}

	result.TotalFiles = analysis.TotalFiles
	result.Duration = time.Since(start).Round(time.Second).String()
	a.emitIngest("done", map[string]any{"result": result})
	log.Printf("[ingest] 完成: %d 文件(新:%d), %d chunks, %d 域, %d agent, %s",
		result.TotalFiles, result.NewFiles, result.TotalChunks, result.Domains, len(result.Agents), result.Duration)

	return result, nil
}

// ─── Convenience: one-shot ingest (analyze + commit) ───────────────

func (a *App) IngestFolder(dirPath string) (*IngestResult, error) {
	analysis, err := a.IngestAnalyze(dirPath)
	if err != nil {
		return nil, err
	}
	return a.IngestCommit(analysis)
}

// ─── Smart chunking + store ────────────────────────────────────────

func (a *App) chunkAndStore(kbID string, f IngestFileInfo) (int, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return 0, err
	}

	text := string(data)
	if isIngestBinary(data) {
		text, err = rag.ExtractTextFromPDF(data)
		if err != nil {
			return 0, fmt.Errorf("非文本且 PDF 解析失败")
		}
	}
	if strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("空文件")
	}

	// Smart chunking: choose strategy based on file type.
	chunks := ingest.SmartChunk(text, f.Ext)

	// Pass each chunk individually — AddTexts will only further split
	// chunks > 720 runes, so our logical boundaries are preserved.
	meta := map[string]string{
		"source": f.Path, "filename": f.Name, "domain": kbID, "ext": f.Ext,
	}
	return a.AddTexts(kbID, chunks, meta)
}

// ─── Incremental tracking ──────────────────────────────────────────

type ingestManifest struct {
	Files map[string]string `json:"files"` // path → hash
}

func (a *App) ingestManifestPath() string {
	dir, _ := os.Getwd()
	return filepath.Join(os.TempDir(), "everevo_ingest_"+filepath.Base(dir)+".json")
}

func (a *App) loadIngestManifest() ingestManifest {
	m := ingestManifest{Files: map[string]string{}}
	data, err := os.ReadFile(a.ingestManifestPath())
	if err != nil {
		return m
	}
	json.Unmarshal(data, &m)
	return m
}

func (a *App) saveIngestManifest(m ingestManifest) {
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(a.ingestManifestPath(), data, 0644)
}

func (a *App) isFileIngested(path, hash string) bool {
	m := a.loadIngestManifest()
	prev, ok := m.Files[path]
	return ok && prev == hash
}

func (a *App) markFileIngested(path, hash string) {
	m := a.loadIngestManifest()
	m.Files[path] = hash
	a.saveIngestManifest(m)
}

// ─── File scanning ─────────────────────────────────────────────────

type ingestRawFile struct {
	Path string
	Name string
	Ext  string
	Size int64
}

func scanIngestFiles(dir string) ([]ingestRawFile, []string) {
	var files []ingestRawFile
	var skipped []string

	extBlock := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".bin": true, ".dat": true,
		".db": true, ".sqlite": true, ".zip": true, ".tar": true, ".gz": true,
		".7z": true, ".rar": true, ".png": true, ".jpg": true, ".jpeg": true,
		".gif": true, ".ico": true, ".bmp": true, ".mp3": true, ".mp4": true,
		".avi": true, ".mov": true, ".wav": true, ".ttf": true, ".otf": true,
		".lock": true, ".sum": true, ".woff": true, ".woff2": true,
	}
	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, "__pycache__": true,
		"vendor": true, "venv": true, ".venv": true,
		"dist": true, "build": true, "target": true,
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			skipped = append(skipped, path)
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if extBlock[ext] {
			return nil
		}
		if info.Size() > 20*1024*1024 {
			skipped = append(skipped, info.Name()+" (过大)")
			return nil
		}
		if info.Size() == 0 {
			return nil
		}
		files = append(files, ingestRawFile{
			Path: path, Name: info.Name(), Ext: ext, Size: info.Size(),
		})
		return nil
	})
	return files, skipped
}

// ─── File hashing ──────────────────────────────────────────────────

func fileHashAndPreview(path string) (hash string, preview string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	h := sha256.Sum256(data)
	hash = hex.EncodeToString(h[:])[:16]

	text := string(data)
	if isIngestBinary(data) {
		preview = "(binary/PDF)"
	} else {
		preview = text
		if len(preview) > 200 {
			preview = preview[:200]
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
	}
	return
}

// ─── LLM categorization ────────────────────────────────────────────

func (a *App) categorizeSummaries(summaries []IngestFileInfo) ([]IngestDomain, error) {
	var lines []string
	for i, s := range summaries {
		p := s.Preview
		if len(p) > 100 {
			p = p[:100]
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s (%s, %s)",
			i+1, s.Ext, s.Name, formatSize(s.Size), p))
	}

	prompt := fmt.Sprintf(
		`将 %d 个文件分入 2-6 个领域（domain）。每个领域对应一个独立的知识库和专家 Agent。

规则：
- 领域名用中文，2-6 个字，准确概括内容
- 相关文件归入同一领域（如所有 Go 后端文件归入"后端服务"）
- 文件类型是重要信号（.go→后端, .vue→前端, .md→文档, .sql→数据库）
- 如果一个文件可以归入多个领域，选最主要的一个
- 无法归类的放入"其他"

文件：
%s

返回 JSON 数组（不含 markdown 标记）：
[{"name":"领域名","description":"该领域的内容和用途","fileIndices":[1,3,5]}]`,
		len(summaries), strings.Join(lines, "\n"))

	provider, err := a.resolveActiveProvider()
	if err != nil {
		return nil, err
	}
	msgs := []map[string]any{{"role": "user", "content": prompt}}
	msgsJSON, _ := json.Marshal(msgs)
	temp := 0.2
	data, err := a.chatCompletion(provider, msgsJSON, nil, chatOpts{Temperature: &temp})
	if err != nil {
		return nil, err
	}
	choices, _ := data["choices"].([]any)
	if len(choices) == 0 {
		return nil, fmt.Errorf("LLM 空返回")
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	content, _ := msg["content"].(string)
	content = cleanJSON(content)

	type rawD struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		FileIndices []int  `json:"fileIndices"`
	}
	var raw []rawD
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("解析分类失败: %w\n%s", err, content[:minInt(300, len(content))])
	}

	var domains []IngestDomain
	seen := map[int]bool{}
	for _, rd := range raw {
		var d IngestDomain
		d.Name = rd.Name
		d.Description = rd.Description
		for _, idx := range rd.FileIndices {
			if idx >= 1 && idx <= len(summaries) {
				d.Files = append(d.Files, summaries[idx-1])
				seen[idx-1] = true
				d.TotalSize += summaries[idx-1].Size
			}
		}
		d.FileCount = len(d.Files)
		if d.FileCount > 0 {
			domains = append(domains, d)
		}
	}
	var orphan []IngestFileInfo
	for i, s := range summaries {
		if !seen[i] {
			orphan = append(orphan, s)
		}
	}
	if len(orphan) > 0 {
		var sz int64
		for _, o := range orphan {
			sz += o.Size
		}
		domains = append(domains, IngestDomain{
			Name: "其他", Description: "未归类的文件",
			Files: orphan, FileCount: len(orphan), TotalSize: sz,
		})
	}
	return domains, nil
}

// ─── Domain agent (v2 — receives per-domain KB ID) ─────────────────

func (a *App) createDomainAgentV2(d *IngestDomain, kbID string) (string, error) {
	var fileList []string
	for _, f := range d.Files {
		tag := ""
		if f.IsNew {
			tag = " [新]"
		}
		fileList = append(fileList, fmt.Sprintf("- %s (%s, %s)%s", f.Name, f.Ext, formatSize(f.Size), tag))
	}

	// Store KB ID in agent metadata so it knows which KB to search.
	sysPrompt := fmt.Sprintf(
		`你是「%s」领域专家 Agent。

领域说明：%s

知识库 ID: %s（包含 %d 个文件）。使用 knowledge_search 工具检索此知识库获取准确信息。

文件清单：
%s

规则：
- 用中文回答
- 所有答案基于知识库检索结果，不凭记忆编造
- 检索不到时诚实告知，并说明知识库中缺少什么
- 涉及多个文件时，注明信息来源`,
		d.Name, d.Description, kbID, d.FileCount, strings.Join(fileList, "\n"))

	now := time.Now().UnixMilli()
	agent := agents.Agent{
		Name:         sanitizeAgentName(d.Name) + " 专家",
		Description:  fmt.Sprintf("%s（%d 个文件）", d.Description, d.FileCount),
		Icon:         "📚",
		SystemPrompt: sysPrompt,
		Tools:        []string{"knowledge_search", "knowledge_list", "knowledge_context"},
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	created, err := a.agentManager.Create(agent)
	if err != nil {
		return "", err
	}
	log.Printf("[ingest] agent: %s (kb=%s, %d files)", created.Name, kbID, d.FileCount)
	return created.Name, nil
}

// ─── Cancel ────────────────────────────────────────────────────────

func (a *App) CancelIngest() {
	if ingestCancel != nil {
		ingestCancel()
	}
}

// ─── Helpers ───────────────────────────────────────────────────────

func (a *App) emitIngest(event string, data any) {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "ingest:"+event, data)
	}
}

func isIngestBinary(data []byte) bool {
	for i := 0; i < len(data) && i < 1024; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func sanitizeAgentName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' ||
			(r >= 0x4e00 && r <= 0x9fff) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 20 {
		s = s[:20]
	}
	if s == "" {
		s = "agent"
	}
	return s
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%dKB", n/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
