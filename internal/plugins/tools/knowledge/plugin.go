// Package knowledge provides knowledge base (RAG) operations as a self-registering
// ToolPlugin. The App delegates all KB logic through the Service interface so that
// app_knowledge.go stays thin (Wails-exposed methods only).
package knowledge

import (
	"context"

	"everevo/internal/core"
	"everevo/internal/rag"
)

const pluginID = "knowledge"

// ── Service interface ────────────────────────────────────────────────────

// Service exposes knowledge base operations. The App delegates to this
// interface for all KB logic; it only adds emitChanged and rerank-LLM.
type Service interface {
	CreateKB(name, modelDir, libraryID string) (rag.KnowledgeBase, error)
	AddTexts(kbID string, texts []string, metadata map[string]string) (int, error)
	SearchKnowledge(kbID, query string, k int, filter map[string]string) ([]rag.SearchResult, error)
	ListKnowledgeBases(libraryID string) ([]rag.KnowledgeBase, error)
	DeleteKB(kbID string) error
	ClearKB(kbID string) error
	DeleteKBChunks(kbID string, ids []string) (int, error)
	ListKBDocuments(kbID string) ([]rag.DocEntry, error)
	SetKBLibrary(kbID, libraryID string) error
	UpdateKBModelDir(kbID, newDir string) error
	MigrateKBModel(kbID, newDir string) error
	SearchAllKBs(query, libraryID string, k, perKB int) ([]RagContextResult, error)
	ParseFileForKB(filePath string) (string, error)
	ParseFileBytes(b64Data string, filename string) (string, error)
	SaveChatFile(b64Data string, filename string) (ChatFileInfo, error)
	ReadChatFile(path string) (content string, isScanned bool, err error)
	ReadMediaFile(path string) (map[string]any, error)
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Plugin implements core.ToolPlugin and holds the Service singleton.
type Plugin struct {
	svc Service
}

var _ core.ToolPlugin = (*Plugin)(nil)

var pluginInstance = &Plugin{svc: newServiceImpl()}

func init() {
	core.GlobalTools.Register(pluginID, pluginInstance, core.PluginManifest{
		ID:          pluginID,
		Name:        "知识库",
		Version:     "1.0",
		Description: "RAG knowledge base operations (CreateKB, AddTexts, SearchKnowledge, etc.)",
		Author:      "EverEvo",
		Type:        "toolset",
	})
}

// Get returns the singleton Service for delegation from App.
func Get() Service {
	return pluginInstance.svc
}

func (p *Plugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID:          pluginID,
		Name:        "知识库",
		Version:     "1.0",
		Description: "RAG knowledge base operations",
		Author:      "EverEvo",
		Type:        "toolset",
	}
}

func (p *Plugin) ToolDefs() []core.ToolDef {
	// Knowledge operations are Wails-exposed, not LLM tools.
	return nil
}

func (p *Plugin) CallTool(_ context.Context, name string, args map[string]any) (core.ToolResult, error) {
	return core.ToolResult{Success: false, Error: "knowledge: no LLM tools defined"}, nil
}
