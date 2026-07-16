package knowledge

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

	"everevo/internal/rag"
	"everevo/internal/storage"
)

// ── Types ────────────────────────────────────────────────────────────────

// RagContextResult is a search hit enriched with its parent KB name.
type RagContextResult struct {
	KBName     string  `json:"kbName"`
	Content    string  `json:"content"`
	Similarity float32 `json:"similarity"`
}

// ChatFileInfo is returned by SaveChatFile to describe a saved upload.
type ChatFileInfo struct {
	Path      string `json:"path"`
	Preview   string `json:"preview"`
	IsScanned bool   `json:"isScanned"`
	SizeBytes int64  `json:"sizeBytes"`
}

// ── Service implementation ───────────────────────────────────────────────

type serviceImpl struct {
	ragStore     *rag.Store
	ragStoreOnce sync.Once
	ragStoreErr  error
}

func newServiceImpl() *serviceImpl {
	return &serviceImpl{}
}

func (s *serviceImpl) getRagStore() (*rag.Store, error) {
	s.ragStoreOnce.Do(func() {
		s.ragStore, s.ragStoreErr = rag.NewStore()
	})
	return s.ragStore, s.ragStoreErr
}

// ── KB CRUD ──────────────────────────────────────────────────────────────

func (s *serviceImpl) CreateKB(name, modelDir, libraryID string) (rag.KnowledgeBase, error) {
	store, err := s.getRagStore()
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
	return *kb, nil
}

func (s *serviceImpl) AddTexts(kbID string, texts []string, metadata map[string]string) (int, error) {
	store, err := s.getRagStore()
	if err != nil {
		return 0, err
	}
	kb, err := store.GetKB(kbID)
	if err != nil {
		return 0, err
	}

	var allChunks []string
	for _, t := range texts {
		allChunks = append(allChunks, rag.ChunkText(t)...)
	}
	if len(allChunks) == 0 {
		return 0, fmt.Errorf("文本分块后为空")
	}

	embeddings, err := rag.EmbedChunks(kb.ModelDir, allChunks)
	if err != nil {
		return 0, fmt.Errorf("嵌入失败: %w", err)
	}

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

	if _, err := store.AddDocuments(kbID, docs, 4); err != nil {
		return 0, fmt.Errorf("存储文档失败: %w", err)
	}

	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	_ = rag.SaveChunksToSource(kbID, allChunks, ids, metadata)

	return len(docs), nil
}

func (s *serviceImpl) SearchKnowledge(kbID, query string, k int, filter map[string]string) ([]rag.SearchResult, error) {
	store, err := s.getRagStore()
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

	queryEmb, err := rag.EmbedQuery(kb.ModelDir, query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	results, err := store.HybridSearch(kbID, queryEmb, query, k, filter)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

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

func (s *serviceImpl) ListKnowledgeBases(libraryID string) ([]rag.KnowledgeBase, error) {
	store, err := s.getRagStore()
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

func (s *serviceImpl) DeleteKB(kbID string) error {
	store, err := s.getRagStore()
	if err != nil {
		return err
	}
	return store.DeleteCollection(kbID)
}

func (s *serviceImpl) ClearKB(kbID string) error {
	store, err := s.getRagStore()
	if err != nil {
		return err
	}
	if err := store.ClearCollection(kbID); err != nil {
		return err
	}
	rag.ClearSource(kbID)
	return nil
}

func (s *serviceImpl) DeleteKBChunks(kbID string, ids []string) (int, error) {
	store, err := s.getRagStore()
	if err != nil {
		return 0, err
	}
	n, err := store.DeleteDocuments(kbID, ids)
	if err != nil {
		return 0, err
	}
	rag.DeleteChunksFromSource(kbID, ids)
	return n, nil
}

func (s *serviceImpl) ListKBDocuments(kbID string) ([]rag.DocEntry, error) {
	store, err := s.getRagStore()
	if err != nil {
		return nil, err
	}
	return store.ListDocuments(kbID)
}

func (s *serviceImpl) SetKBLibrary(kbID, libraryID string) error {
	store, err := s.getRagStore()
	if err != nil {
		return err
	}
	return store.SetKBLibrary(kbID, libraryID)
}

func (s *serviceImpl) UpdateKBModelDir(kbID, newDir string) error {
	store, err := s.getRagStore()
	if err != nil {
		return err
	}
	if store.Count(kbID) > 0 {
		return fmt.Errorf("知识库非空，请用「迁移模型」重新嵌入现有文档")
	}
	if _, err := rag.EmbedQuery(newDir, "test"); err != nil {
		return fmt.Errorf("模型不可用: %w", err)
	}
	return store.UpdateKBModelDir(kbID, newDir)
}

func (s *serviceImpl) MigrateKBModel(kbID, newDir string) error {
	store, err := s.getRagStore()
	if err != nil {
		return err
	}
	if _, err := rag.EmbedQuery(newDir, "test"); err != nil {
		return fmt.Errorf("模型不可用: %w", err)
	}
	embedBatch := func(texts []string) ([][]float32, error) {
		return rag.EmbedChunks(newDir, texts)
	}
	return store.MigrateKBModel(kbID, newDir, embedBatch)
}

func (s *serviceImpl) SearchAllKBs(query, libraryID string, k, perKB int) ([]RagContextResult, error) {
	if query == "" || k <= 0 {
		return nil, nil
	}
	if perKB <= 0 {
		perKB = 3
	}

	store, err := s.getRagStore()
	if err != nil {
		return nil, err
	}

	kbs := store.ListKBs(libraryID)
	if len(kbs) == 0 {
		return nil, nil
	}

	var all []RagContextResult
	for _, kb := range kbs {
		results, err := s.SearchKnowledge(kb.ID, query, perKB, nil)
		if err != nil {
			continue
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

	return all, nil
}

// ── File parsing ─────────────────────────────────────────────────────────

func (s *serviceImpl) ParseFileForKB(filePath string) (string, error) {
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
		text := string(data)
		if isBinaryContent(data) {
			return "", fmt.Errorf("不支持的文件格式: %s（二进制文件）", ext)
		}
		return text, nil
	}
}

func (s *serviceImpl) ParseFileBytes(b64Data string, filename string) (string, error) {
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

func (s *serviceImpl) SaveChatFile(b64Data string, filename string) (ChatFileInfo, error) {
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return ChatFileInfo{}, fmt.Errorf("解码文件数据失败: %w", err)
	}
	if len(data) == 0 {
		return ChatFileInfo{}, fmt.Errorf("文件为空")
	}

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

func (s *serviceImpl) ReadChatFile(path string) (content string, isScanned bool, err error) {
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
		return "", false, fmt.Errorf("图片文件无法提取文字，请使用 read_media_file 工具以视觉模式查看: %s", absPath)
	default:
		if isBinaryContent(data) {
			return "", false, fmt.Errorf("无法读取二进制文件: %s", ext)
		}
		return string(data), false, nil
	}
}

func (s *serviceImpl) ReadMediaFile(path string) (map[string]any, error) {
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
		mime = detectMimeByContent(data)
		if mime == "" {
			return nil, fmt.Errorf("不支持的文件类型: %s（不是图片文件）", ext)
		}
	}

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

// ── Helpers ──────────────────────────────────────────────────────────────

func isBinaryContent(data []byte) bool {
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

func detectMimeByContent(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' && data[3] == '8' {
		return "image/gif"
	}
	if data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	return ""
}

func isGarbageText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 20 {
		return true
	}
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
	if float64(letters)/float64(total) < 0.20 {
		return true
	}
	words := 0
	run := 0
	for _, r := range trimmed {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			run++
		} else {
			if run >= 3 {
				words++
			}
			run = 0
		}
	}
	if run >= 3 {
		words++
	}
	if float64(words)/float64(total)*500.0 < 5.0 {
		return true
	}
	return false
}

func chatUploadDir() (string, error) {
	dir := filepath.Join(storage.DataDir(), "data", "uploads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建上传目录失败: %w", err)
	}
	return dir, nil
}
