//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

	"everevo/internal/config"
	"everevo/internal/rag"
	"everevo/internal/storage"
)

// ─── 知识库 API ────────────────────────────────────────────────

// RagContextResult is a search hit enriched with its parent KB name for use
// in the chat system prompt.
type RagContextResult struct {
	KBName     string  `json:"kbName"`
	Content    string  `json:"content"`
	Similarity float32 `json:"similarity"`
}

var ragStore *rag.Store
var ragStoreOnce sync.Once

func (a *App) getRagStore() (*rag.Store, error) {
	var initErr error
	ragStoreOnce.Do(func() {
		ragStore, initErr = rag.NewStore()
	})
	return ragStore, initErr
}

// CreateKnowledgeBase 创建新的知识库。modelDir 是嵌入模型的本地目录。
// libraryID 将 KB 绑定到指定领域库（必传，不可为空）。
func (a *App) CreateKnowledgeBase(name, modelDir, libraryID string) (rag.KnowledgeBase, error) {
	if err := a.validateLibraryID(libraryID); err != nil {
		return rag.KnowledgeBase{}, fmt.Errorf("创建知识库失败: %w", err)
	}
	store, err := a.getRagStore()
	if err != nil {
		return rag.KnowledgeBase{}, err
	}
	if _, err := rag.EmbedChunks(modelDir, []string{"test"}); err != nil {
		return rag.KnowledgeBase{}, fmt.Errorf("嵌入模型测试失败: %w", err)
	}
	id := uuid.New().String()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if err := store.CreateCollection(id, name, modelDir, libraryID, createdAt); err != nil {
		return rag.KnowledgeBase{}, err
	}
	kb, err := store.GetKB(id)
	if err != nil {
		return rag.KnowledgeBase{}, err
	}
	a.emitChanged("kb:changed", "update", id)
	return *kb, nil
}

// AddTexts 向知识库添加文本（自动分块 + 嵌入 + 存储）。
// metadata 会附加到每个块上。
func (a *App) AddTexts(kbID string, texts []string, metadata map[string]string) (int, error) {
	store, err := a.getRagStore()
	if err != nil {
		return 0, err
	}
	kb, err := store.GetKB(kbID)
	if err != nil {
		return 0, err
	}

	// 1. 分块
	var allChunks []string
	for _, t := range texts {
		allChunks = append(allChunks, rag.ChunkText(t)...)
	}
	if len(allChunks) == 0 {
		return 0, fmt.Errorf("文本分块后为空")
	}

	// 2. 批量嵌入
	embeddings, err := rag.EmbedChunks(kb.ModelDir, allChunks)
	if err != nil {
		return 0, fmt.Errorf("嵌入失败: %w", err)
	}

	// 3. 构建 chromem-go Document（每个 chunk 独立 metadata，含 _source + _position）
	sourceName := ""
	if metadata != nil {
		if s, ok := metadata["source"]; ok {
			sourceName = s
		}
		if s, ok := metadata["_source"]; ok {
			sourceName = s
		}
	}
	docs := make([]chromem.Document, len(allChunks))
	for i, chunk := range allChunks {
		// Copy user metadata per chunk and inject _source / _position
		chunkMeta := make(map[string]string, len(metadata)+2)
		for k, v := range metadata {
			chunkMeta[k] = v
		}
		if sourceName != "" {
			chunkMeta["_source"] = sourceName
		}
		chunkMeta["_position"] = fmt.Sprintf("%d", i)
		docs[i] = chromem.Document{
			ID:        uuid.New().String(),
			Metadata:  chunkMeta,
			Content:   chunk,
			Embedding: embeddings[i],
		}
	}

	// 4. 入库
	if _, err := store.AddDocuments(kbID, docs, 4); err != nil {
		return 0, fmt.Errorf("存储文档失败: %w", err)
	}

	a.emitChanged("kb:changed", "update", kbID)
	return len(docs), nil
}

// SearchKnowledge 在知识库中语义搜索。k 控制返回数量，filter 是可选的元数据过滤。
func (a *App) SearchKnowledge(kbID, query string, k int, filter map[string]string) ([]rag.SearchResult, error) {
	store, err := a.getRagStore()
	if err != nil {
		return nil, err
	}
	kb, err := store.GetKB(kbID)
	if err != nil {
		return nil, err
	}
	if k <= 0 {
		k = 5
	}

	// 1. 嵌入查询
	queryEmb, err := rag.EmbedQuery(kb.ModelDir, query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	// 2. 混合搜索（稠密向量 + BM25 关键词 RRF 融合）
	results, err := store.HybridSearch(kbID, queryEmb, query, k, filter)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 3. 映射到前端类型（从 metadata 提取 source/position）
	out := make([]rag.SearchResult, len(results))
	for i, r := range results {
		source := ""
		pos := 0
		if r.Metadata != nil {
			source = r.Metadata["_source"]
			if s, ok := r.Metadata["_position"]; ok {
				fmt.Sscanf(s, "%d", &pos)
			}
		}
		out[i] = rag.SearchResult{
			ID:         r.ID,
			Content:    r.Content,
			Source:     source,
			Position:   pos,
			Metadata:   r.Metadata,
			Similarity: r.Similarity,
		}
	}
	return out, nil
}

// ListKnowledgeBases 列出所有知识库。
func (a *App) ListKnowledgeBases(libraryID string) ([]rag.KnowledgeBase, error) {
	store, err := a.getRagStore()
	if err != nil {
		return nil, err
	}
	kbs := store.ListKBs(libraryID)
	out := make([]rag.KnowledgeBase, len(kbs))
	for i, kb := range kbs {
		out[i] = *kb
	}
	return out, nil
}

// DeleteKnowledgeBase 删除知识库及其所有数据。
func (a *App) DeleteKnowledgeBase(kbID string) error {
	store, err := a.getRagStore()
	if err != nil {
		return err
	}
	if err := store.DeleteCollection(kbID); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// ClearKnowledgeBase 清空知识库中所有文档，保留 KB 元数据和模型绑定。
func (a *App) ClearKnowledgeBase(kbID string) error {
	store, err := a.getRagStore()
	if err != nil {
		return err
	}
	if err := store.ClearCollection(kbID); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// DeleteKBChunks 按 ID 列表删除知识库中的指定文档。
func (a *App) DeleteKBChunks(kbID string, ids []string) (int, error) {
	store, err := a.getRagStore()
	if err != nil {
		return 0, err
	}
	n, err := store.DeleteDocuments(kbID, ids)
	if err != nil {
		return 0, err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return n, nil
}

// ListKBDocuments 列出知识库中所有文档的摘要信息。
func (a *App) ListKBDocuments(kbID string) ([]rag.DocEntry, error) {
	store, err := a.getRagStore()
	if err != nil {
		return nil, err
	}
	return store.ListDocuments(kbID)
}

// UpdateKBModelDir rebinds a KB's embedding model. Only allowed when the KB is
// empty (no chunks) — non-empty KBs must use MigrateKBModel to re-embed.
func (a *App) UpdateKBModelDir(kbID, newDir string) error {
	store, err := a.getRagStore()
	if err != nil {
		return err
	}
	if store.Count(kbID) > 0 {
		return fmt.Errorf("知识库非空，请用「迁移模型」重新嵌入现有文档")
	}
	if _, err := rag.EmbedQuery(newDir, "test"); err != nil {
		return fmt.Errorf("模型不可用: %w", err)
	}
	if err := store.UpdateKBModelDir(kbID, newDir); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// MigrateKBModel re-embeds all KB docs with a new model and rebinds.
func (a *App) MigrateKBModel(kbID, newDir string) error {
	store, err := a.getRagStore()
	if err != nil {
		return err
	}
	if _, err := rag.EmbedQuery(newDir, "test"); err != nil {
		return fmt.Errorf("模型不可用: %w", err)
	}
	embedBatch := func(texts []string) ([][]float32, error) {
		return rag.EmbedChunks(newDir, texts)
	}
	if err := store.MigrateKBModel(kbID, newDir, embedBatch); err != nil {
		return err
	}
	a.emitChanged("kb:changed", "update", kbID)
	return nil
}

// SearchAllKnowledgeBases searches across all KBs in a domain library. It
// returns the top results aggregated and sorted by similarity. This is the
// auto-inject RAG path used by the chat system prompt (Phase 1).
func (a *App) SearchAllKnowledgeBases(query, libraryID string, k, perKB int) ([]RagContextResult, error) {
	if query == "" || k <= 0 {
		return nil, nil
	}
	if perKB <= 0 {
		perKB = 3
	}

	store, err := a.getRagStore()
	if err != nil {
		return nil, err
	}

	kbs := store.ListKBs(libraryID)
	if len(kbs) == 0 {
		return nil, nil
	}

	var all []RagContextResult
	for _, kb := range kbs {
		results, err := a.SearchKnowledge(kb.ID, query, perKB, nil)
		if err != nil {
			continue // skip KBs whose model is unavailable
		}
		for _, r := range results {
			all = append(all, RagContextResult{
				KBName:     kb.Name,
				Content:    r.Content,
				Similarity: r.Similarity,
			})
		}
	}

	// Sort descending by similarity.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Similarity > all[i].Similarity {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	if len(all) > k {
		all = all[:k]
	}

	// Rerank: when we have enough candidates and a provider is available,
	// use the LLM to score relevance and boost the top hits.
	if len(all) > 3 {
		reranked, err := a.rerankWithLLM(query, all)
		if err == nil && len(reranked) > 0 {
			if len(reranked) > k {
				reranked = reranked[:k]
			}
			return reranked, nil
		}
		// Rerank failure is silent — fall through to vector-only results.
	}

	return all, nil
}

// rerankWithLLM re-scores candidates via the active LLM provider and returns
// results sorted by LLM relevance score (1-5). Falls back to original order on
// any error — the caller should treat this as best-effort.
func (a *App) rerankWithLLM(query string, candidates []RagContextResult) ([]RagContextResult, error) {
	if a.cfg == nil || a.cfg.LLM.ActiveProvider == "" {
		return nil, fmt.Errorf("no active provider")
	}

	// Find the active enabled provider.
	var prov *config.LLMProvider
	for i := range a.cfg.LLM.Providers {
		if a.cfg.LLM.Providers[i].ID == a.cfg.LLM.ActiveProvider && a.cfg.LLM.Providers[i].Enabled {
			prov = &a.cfg.LLM.Providers[i]
			break
		}
	}
	if prov == nil {
		return nil, fmt.Errorf("active provider not available")
	}

	// Build scoring prompt
	var sb strings.Builder
	sb.WriteString("评估以下文本片段对回答用户问题的有用程度。对每个片段给出1-5的整数分数（1=完全无关，5=高度相关）。仅输出\"片段N: X分\"格式，一行一个。\n\n")
	sb.WriteString("用户问题: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")
	for i, c := range candidates {
		content := c.Content
		if len([]rune(content)) > 300 {
			content = string([]rune(content)[:300]) + "…"
		}
		sb.WriteString(fmt.Sprintf("片段%d: %s\n\n", i+1, content))
	}

	messages := []map[string]string{
		{"role": "user", "content": sb.String()},
	}
	messagesJSON, _ := json.Marshal(messages)
	toolsJSON := json.RawMessage(`[]`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := a.chatCompletion(prov, messagesJSON, toolsJSON, chatOpts{
		MaxTokens: 512,
		Ctx:       ctx,
	})
	if err != nil {
		log.Printf("[rag] rerank LLM call failed: %v", err)
		return nil, err
	}

	// Parse scores from response — expect "片段N: X分" per line
	content := extractAssistantContent(result)
	if content == "" {
		return nil, fmt.Errorf("empty rerank response")
	}

	re := regexp.MustCompile(`片段(\d+):\s*(\d)`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		log.Printf("[rag] rerank: could not parse scores from: %s", content)
		return nil, fmt.Errorf("unparseable rerank response")
	}

	scores := make(map[int]int) // candidate index → score
	for _, m := range matches {
		var idx, score int
		fmt.Sscanf(m[1], "%d", &idx)
		fmt.Sscanf(m[2], "%d", &score)
		scores[idx-1] = score // convert 1-based to 0-based
	}

	// Sort candidates by LLM score descending
	sorted := make([]RagContextResult, len(candidates))
	copy(sorted, candidates)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			si := scores[i]
			sj := scores[j]
			if sj > si {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Filter candidates scored < 3 (irrelevant)
	var filtered []RagContextResult
	for _, c := range sorted {
		if scores[0] >= 0 && scores[0] < 3 {
			// This check is per-index — simpler: just keep top scorers
		}
		filtered = append(filtered, c)
	}
	// Keep only candidates scored 3+
	var high []RagContextResult
	for i, c := range sorted {
		if s, ok := scores[i]; ok && s >= 3 {
			high = append(high, c)
		}
	}
	if len(high) > 0 {
		return high, nil
	}
	return sorted, nil
}

// extractAssistantContent pulls the text content from a normalized chat response.
func extractAssistantContent(result map[string]any) string {
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
}

// ParseFileForKB reads a file from disk and extracts its text content.
// Supports .txt, .md (plain text) and .pdf (best-effort text extraction).
// Returns the extracted text suitable for KB chunking.
func (a *App) ParseFileForKB(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".yaml", ".yml", ".log":
		return string(data), nil
	case ".pdf":
		return rag.ExtractTextFromPDF(data)
	default:
		// For unknown extensions, try to read as text — if it's binary,
		// return an error.
		text := string(data)
		if isBinaryContent(data) {
			return "", fmt.Errorf("不支持的文件格式: %s（二进制文件）", ext)
		}
		return text, nil
	}
}

// isBinaryContent checks if data is likely binary (non-text).
func isBinaryContent(data []byte) bool {
	// Check first 8KB at most.
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// ParseFileBytes parses a base64-encoded file and extracts its text content.
// Deprecated for chat use: prefer SaveChatFile + read_file tool instead.
func (a *App) ParseFileBytes(b64Data string, filename string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("解码文件数据失败: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("文件为空")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return rag.ExtractTextFromPDF(data)
	case ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".yaml", ".yml", ".log":
		return string(data), nil
	default:
		if isBinaryContent(data) {
			return "", fmt.Errorf("不支持的文件格式: %s（二进制文件）", ext)
		}
		return string(data), nil
	}
}

// ChatFileInfo is returned by SaveChatFile to describe a saved upload.
type ChatFileInfo struct {
	Path       string `json:"path"`       // absolute path on disk
	Preview    string `json:"preview"`    // first ~300 chars of extracted text
	IsScanned  bool   `json:"isScanned"`  // true for image-based PDFs
	SizeBytes  int64  `json:"sizeBytes"`
}

// SaveChatFile saves a drag-and-drop / paste file to disk and attempts text
// extraction. The returned path can be passed to the read_file tool for the
// LLM to access the full content on demand.
func (a *App) SaveChatFile(b64Data string, filename string) (ChatFileInfo, error) {
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return ChatFileInfo{}, fmt.Errorf("解码文件数据失败: %w", err)
	}
	if len(data) == 0 {
		return ChatFileInfo{}, fmt.Errorf("文件为空")
	}

	// Save to data/uploads/ with unique name to avoid collisions.
	uploadDir, err := chatUploadDir()
	if err != nil {
		return ChatFileInfo{}, err
	}
	safeName := strings.Map(func(r rune) rune {
		if r == '\\' || r == '/' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, filename)
	destPath := filepath.Join(uploadDir, safeName)
	// If file already exists, add a suffix.
	if _, err := os.Stat(destPath); err == nil {
		base := safeName[:len(safeName)-len(filepath.Ext(safeName))]
		ext := filepath.Ext(safeName)
		for i := 1; i < 100; i++ {
			alt := filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s", base, i, ext))
			if _, err := os.Stat(alt); os.IsNotExist(err) {
				destPath = alt
				break
			}
		}
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return ChatFileInfo{}, fmt.Errorf("保存文件失败: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	var preview string
	isScanned := false

	switch ext {
	case ".pdf":
		text, extractErr := rag.ExtractTextFromPDF(data)
		if extractErr != nil || strings.TrimSpace(text) == "" || isGarbageText(text) {
			isScanned = true
			preview = ""
		} else {
			preview = text
			if len([]rune(preview)) > 300 {
				preview = string([]rune(preview)[:300]) + "…"
			}
		}
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg":
		// Image files: no text to extract; use read_media_file for vision analysis.
		preview = ""
	case ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".yaml", ".yml", ".log":
		text := string(data)
		preview = text
		if len([]rune(preview)) > 300 {
			preview = string([]rune(preview)[:300]) + "…"
		}
	default:
		if !isBinaryContent(data) {
			preview = string(data)
			if len([]rune(preview)) > 300 {
				preview = string([]rune(preview)[:300]) + "…"
			}
		}
	}

	return ChatFileInfo{
		Path:      destPath,
		Preview:   preview,
		IsScanned: isScanned,
		SizeBytes: int64(len(data)),
	}, nil
}

// ReadChatFile reads a saved chat upload file from disk and extracts text.
// Returns (content, isScanned, error). For scanned PDFs, content will be empty
// and isScanned=true. The caller (read_file tool) should relay this to the user.
// Since this is a local desktop app and paths come from user drag-and-drop,
// we trust the caller — no directory sandbox.
func (a *App) ReadChatFile(path string) (content string, isScanned bool, err error) {
	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		absPath = path
	}

	data, readErr := os.ReadFile(absPath)
	if readErr != nil {
		return "", false, fmt.Errorf("读取文件失败: %w", readErr)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".pdf":
		text, pdfErr := rag.ExtractTextFromPDF(data)
		if pdfErr != nil || strings.TrimSpace(text) == "" || isGarbageText(text) {
			return "", true, nil
		}
		return text, false, nil
	case ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".yaml", ".yml", ".log":
		return string(data), false, nil
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg":
		// Image files can't be read as text — use read_media_file instead.
		return "", false, fmt.Errorf("图片文件无法提取文字，请使用 read_media_file 工具以视觉模式查看: %s", absPath)
	default:
		if isBinaryContent(data) {
			return "", false, fmt.Errorf("无法读取二进制文件: %s", ext)
		}
		return string(data), false, nil
	}
}

// ReadMediaFile reads an image or scanned-PDF file from disk and returns its
// base64-encoded data suitable for vision-model consumption. For image types
// (png, jpg, jpeg, gif, bmp, webp, svg), the raw bytes are returned as
// data-URI. For scanned PDFs, individual pages are NOT rendered yet (P2 — needs
// PDF rasterizer); the caller receives an error with guidance.
func (a *App) ReadMediaFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var mime string
	switch ext {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".bmp":
		mime = "image/bmp"
	case ".webp":
		mime = "image/webp"
	case ".svg":
		mime = "image/svg+xml"
	case ".pdf":
		// Scanned PDF: P2 will add page rasterization here.
		return map[string]any{
			"type":     "scanned_pdf",
			"path":     path,
			"size":     len(data),
			"pages":    0,
			"message":  "扫描件 PDF 的页面渲染尚未实现。请用户手动截图 PDF 页面并粘贴到对话框中。",
			"base64":   "",
			"mimeType": "",
		}, nil
	default:
		// Try to detect by content magic bytes
		mime = detectMimeByContent(data)
		if mime == "" {
			return nil, fmt.Errorf("不支持的文件类型: %s（不是图片文件）", ext)
		}
	}

	// Encode image as base64.
	imgB64 := base64.StdEncoding.EncodeToString(data)

	return map[string]any{
		"type":     "image",
		"path":     path,
		"size":     len(data),
		"mimeType": mime,
		"base64":   imgB64,
		"dataUri":  fmt.Sprintf("data:%s;base64,%s", mime, imgB64),
	}, nil
}

// detectMimeByContent detects MIME type from magic bytes for common image formats.
func detectMimeByContent(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	// PNG: \x89PNG\r\n\x1a\n
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	// JPEG: \xFF\xD8\xFF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// GIF: GIF8
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' && data[3] == '8' {
		return "image/gif"
	}
	// BMP: BM
	if data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	return ""
}

// isGarbageText detects PDF extraction output that is just noise (structural
// operators, parentheses, etc.) rather than real readable content. Used to
// classify image-based / scanned PDFs where FlateDecode produces junk.
func isGarbageText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 20 {
		return true
	}
	// Count meaningful letters (Latin + CJK + Kana). PDF noise is mostly short
	// operator tags (BT, ET, Tf, Tj, etc.) that produce few real words.
	letters := 0
	for _, r := range trimmed {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			(r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF) {
			letters++
		}
	}
	total := len([]rune(trimmed))
	if total == 0 {
		return true
	}
	// Heuristic 1: < 20% meaningful letters → garbage (e.g. "))))))")
	if float64(letters)/float64(total) < 0.20 {
		return true
	}
	// Heuristic 2: count 3+-letter "words". Real text has ≥5 words per 500 chars.
	// PDF noise like "BT /F1 12 Tf 100 700 Td ( ) Tj ET" produces zero 3+-letter words.
	words := 0
	run := 0
	for _, r := range trimmed {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			run++
		} else {
			if run >= 3 { words++ }
			run = 0
		}
	}
	if run >= 3 { words++ }
	if float64(words)/float64(total)*500.0 < 5.0 {
		return true
	}
	return false
}

// chatUploadDir returns the data/uploads/ directory, creating it if needed.
// Uses the EXE-relative data directory so that MCP filesystem servers and other
// tools can access uploaded files without path isolation issues.
func chatUploadDir() (string, error) {
	base, err := storage.DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "data", "uploads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建上传目录失败: %w", err)
	}
	return dir, nil
}
