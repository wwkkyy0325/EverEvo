// Package models provides tools for local model management, model catalog browsing,
// file downloads, toolbox embedding, and LLM provider configuration.
package models

import (
	"context"
	"fmt"

	"everevo/internal/config"
	"everevo/internal/core"
	"everevo/internal/downloader"
	"everevo/internal/model"
)

const pluginID = "models"

// ── Backend interface ───────────────────────────────────────────────────

// Backend provides App-level infrastructure the catalog/download logic needs.
type Backend interface {
	// Config returns the current user configuration (never nil).
	Config() *config.Config
	// SaveConfig persists the configuration to disk.
	SaveConfig() error
	// EmitEvent sends a Wails event to the frontend.
	EmitEvent(event string, data any)
	// DownloadManager returns the download manager.
	DownloadManager() *downloader.Manager
}

// ── ModelsService interface ────────────────────────────────────────────

// ModelsService abstracts App-level operations the plugin needs for model
// management, toolbox, and LLM provider configuration.
// Catalog and download logic now lives in this package directly and no longer
// goes through ModelsService.
type ModelsService interface {
	// ── Local model management ──
	ListModels() []model.ModelInfo
	LoadModelFile(id, name, modelPath string) (model.ModelInfo, error)
	UnloadModel(id string) error
	RunModel(id, input string) (string, error)
	ListDownloadedModels() any
	ListToolModels() any
	// ── Toolbox ──
	EmbedTexts(modelDir string, texts []string) ([][]float32, error)
	// ── LLM Providers ──
	ListProviders() []config.LLMProvider
	GetActiveProvider() any
	SetActiveProvider(id string) error
	TestProviderConnection(id string) (string, error)
	ListPresets() []config.Preset
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin implements core.ToolPlugin for model/catalog/download/toolbox/provider tools.
type Plugin struct {
	service ModelsService
	backend Backend
}

var _ core.ToolPlugin = (*Plugin)(nil)

// SetModelsService wires the App-level delegate for model/toolbox/provider ops.
// Called once at startup.
func SetModelsService(svc ModelsService) {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return
	}
	if plug, ok := p.(*Plugin); ok {
		plug.service = svc
	}
}

// SetBackend wires the App-level infrastructure for catalog/download logic.
// Called once at startup.
func SetBackend(b Backend) {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return
	}
	if plug, ok := p.(*Plugin); ok {
		plug.backend = b
	}
}

func init() {
	core.GlobalTools.Register(pluginID, &Plugin{}, core.PluginManifest{
		ID: pluginID, Name: "模型与下载管理", Version: "1.0",
		Description: "model_*, catalog_*, download_*, toolbox_*, provider_* — 模型管理、市场、下载、工具箱、供应商配置",
		Author: "EverEvo", Type: "toolset",
	})
}

// Init satisfies core.LifecyclePlugin; no-op — wiring is done via SetModelsService / SetBackend.
func (p *Plugin) Init(_ context.Context) error { return nil }

// Stop satisfies core.LifecyclePlugin.
func (p *Plugin) Stop() error { return nil }

func (p *Plugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID: pluginID, Name: "模型与下载管理", Version: "1.0",
		Description: "model/catalog/download/toolbox/provider tools",
		Author: "EverEvo", Type: "toolset",
	}
}

func (p *Plugin) ToolDefs() []core.ToolDef {
	return []core.ToolDef{
		// ── model ──
		{
			Name: "model_list", Description: "列出当前已加载的所有模型。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
		{
			Name: "model_load", Description: "通过本地路径加载模型文件（自动检测 ONNX/GGUF 等格式）。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":        map[string]any{"type": "string", "description": "模型唯一标识"},
					"name":      map[string]any{"type": "string", "description": "模型显示名称"},
					"modelPath": map[string]any{"type": "string", "description": "模型文件路径"},
				},
				"required": []string{"id", "name", "modelPath"},
			},
		},
		{
			Name: "model_unload", Description: "卸载已加载的模型。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "模型 ID"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name: "model_run", Description: "在指定模型上运行推理。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string", "description": "模型 ID"},
					"input": map[string]any{"type": "string", "description": "输入文本"},
				},
				"required": []string{"id", "input"},
			},
		},
		{
			Name: "model_list_downloaded", Description: "列出已下载到本地的模型文件。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
		{
			Name: "model_list_tool", Description: "列出可用的工具箱模型（句向量/图像分类等）。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
		// ── catalog ──
		{
			Name: "catalog_search", Description: "在模型市场搜索模型。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "搜索关键词"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "catalog_get_detail", Description: "获取指定模型的详细信息（含文件列表）。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "来源平台，如 huggingface / modelscope"},
					"repoId": map[string]any{"type": "string", "description": "仓库 ID"},
				},
				"required": []string{"source", "repoId"},
			},
		},
		{
			Name: "catalog_list_files", Description: "列出仓库的所有文件。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "来源平台"},
					"repoId": map[string]any{"type": "string", "description": "仓库 ID"},
				},
			},
		},
		// ── download ──
		{
			Name: "download_file", Description: "下载模型仓库的单个文件。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source":   map[string]any{"type": "string", "description": "来源平台"},
					"repoId":   map[string]any{"type": "string", "description": "仓库 ID"},
					"filename": map[string]any{"type": "string", "description": "文件路径"},
				},
				"required": []string{"source", "repoId", "filename"},
			},
		},
		{
			Name: "download_package", Description: "一键下载模型仓库的全部文件。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "来源平台"},
					"repoId": map[string]any{"type": "string", "description": "仓库 ID"},
				},
				"required": []string{"source", "repoId"},
			},
		},
		{
			Name: "download_selected", Description: "下载用户选中的模型文件。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "来源平台"},
					"repoId": map[string]any{"type": "string", "description": "仓库 ID"},
					"files":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "文件路径列表"},
				},
				"required": []string{"source", "repoId", "files"},
			},
		},
		{
			Name: "download_engine", Description: "下载执行引擎文件（ONNX Runtime / llama.cpp 等）。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":     map[string]any{"type": "string", "description": "引擎标识: onnx, llama, cuda"},
					"mirror":  map[string]any{"type": "string", "description": "镜像地址（可选）"},
					"variant": map[string]any{"type": "string", "description": "变体: cpu / cuda"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name: "model_embed_texts", Description: "使用句向量模型将文本编码为嵌入向量。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"modelDir": map[string]any{"type": "string", "description": "模型目录路径"},
					"texts":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "待编码的文本列表"},
				},
				"required": []string{"modelDir", "texts"},
			},
		},
		// ── provider ──
		{
			Name: "provider_list", Description: "列出所有已配置的 LLM 供应商。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
		{
			Name: "provider_get_active", Description: "获取当前激活的 LLM 供应商。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
		{
			Name: "provider_set_active", Description: "设置当前激活的 LLM 供应商。",
			ReadOnly: false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "供应商 ID"},
				},
			},
		},
		{
			Name: "provider_test", Description: "测试指定供应商的连接是否正常。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "供应商 ID"},
				},
			},
		},
		{
			Name: "provider_list_presets", Description: "列出内置的供应商预设配置。",
			ReadOnly: true,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{},
			},
		},
	}
}

// ── CallTool ────────────────────────────────────────────────────────────

func (p *Plugin) CallTool(_ context.Context, name string, args map[string]any) (core.ToolResult, error) {
	switch name {
	// ── model ──
	case "model_list":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		return ok(p.service.ListModels()), nil
	case "model_load":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		id, n, mp := getStr(args, "id"), getStr(args, "name"), getStr(args, "modelPath")
		if id == "" || n == "" || mp == "" {
			return errMsg("缺少必填参数: id, name, modelPath"), nil
		}
		info, e := p.service.LoadModelFile(id, n, mp)
		if e != nil {
			return err(e), nil
		}
		return ok(info), nil
	case "model_unload":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		id := getStr(args, "id")
		if id == "" {
			return errMsg("缺少必填参数: id"), nil
		}
		if e := p.service.UnloadModel(id); e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"status": "unloaded", "id": id}), nil
	case "model_run":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		id, input := getStr(args, "id"), getStr(args, "input")
		if id == "" || input == "" {
			return errMsg("缺少必填参数: id, input"), nil
		}
		out, e := p.service.RunModel(id, input)
		if e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"output": out}), nil
	case "model_list_downloaded":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		return ok(p.service.ListDownloadedModels()), nil
	case "model_list_tool":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		return ok(p.service.ListToolModels()), nil

	// ── catalog (plugin-internal logic, no service needed) ──
	case "catalog_search":
		return ok(SearchAllCatalog(getStr(args, "query"), nil)), nil
	case "catalog_get_detail":
		src, repo := getStr(args, "source"), getStr(args, "repoId")
		if src == "" || repo == "" {
			return errMsg("缺少必填参数: source, repoId"), nil
		}
		return ok(GetModelDetail(src, repo)), nil
	case "catalog_list_files":
		return ok(ListModelFiles(getStr(args, "source"), getStr(args, "repoId"))), nil
	// ── download (plugin-internal logic, uses backend for dlManager) ──
	case "download_file":
		src, repo, file := getStr(args, "source"), getStr(args, "repoId"), getStr(args, "filename")
		if src == "" || repo == "" || file == "" {
			return errMsg("缺少必填参数: source, repoId, filename"), nil
		}
		dlID, e := DownloadModelFile(p.backend, src, repo, file)
		if e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"downloadId": dlID}), nil
	case "download_package":
		src, repo := getStr(args, "source"), getStr(args, "repoId")
		if src == "" || repo == "" {
			return errMsg("缺少必填参数: source, repoId"), nil
		}
		dlID, e := DownloadModelPackage(p.backend, src, repo)
		if e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"downloadId": dlID}), nil
	case "download_selected":
		src, repo := getStr(args, "source"), getStr(args, "repoId")
		files := getStrSlice(args, "files")
		if src == "" || repo == "" || len(files) == 0 {
			return errMsg("缺少必填参数: source, repoId, files"), nil
		}
		dlID, e := DownloadSelectedFiles(p.backend, src, repo, files)
		if e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"downloadId": dlID}), nil
	case "download_engine":
		key, mirror, variant := getStr(args, "key"), getStr(args, "mirror"), getStr(args, "variant")
		if key == "" {
			return errMsg("缺少必填参数: key"), nil
		}
		dlID, e := DownloadEngineFile(p.backend, key, mirror, variant)
		if e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"downloadId": dlID}), nil

	case "model_embed_texts":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		modelDir, texts := getStr(args, "modelDir"), getStrSlice(args, "texts")
		if modelDir == "" || len(texts) == 0 {
			return errMsg("缺少必填参数: modelDir, texts"), nil
		}
		embs, e := p.service.EmbedTexts(modelDir, texts)
		if e != nil {
			return err(e), nil
		}
		return ok(embs), nil

	// ── provider ──
	case "provider_list":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		return ok(p.service.ListProviders()), nil
	case "provider_get_active":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		p := p.service.GetActiveProvider()
		return ok(p), nil
	case "provider_set_active":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		if e := p.service.SetActiveProvider(getStr(args, "id")); e != nil {
			return err(e), nil
		}
		return ok(map[string]string{"status": "ok"}), nil
	case "provider_test":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		msg, e := p.service.TestProviderConnection(getStr(args, "id"))
		if e != nil {
			return err(e), nil
		}
		return ok(msg), nil
	case "provider_list_presets":
		if p.service == nil {
			return fail("models plugin: service not configured"), nil
		}
		return ok(p.service.ListPresets()), nil

	default:
		return core.ToolResult{Success: false, Error: fmt.Sprintf("models: unknown tool %q", name)}, nil
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func getStr(args map[string]any, key string) string {
	v, _ := args[key]
	s, _ := v.(string)
	return s
}

func getStrSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ok(v any) core.ToolResult {
	return core.ToolResult{Success: true, Data: v}
}

func err(e error) core.ToolResult {
	return core.ToolResult{Success: false, Error: e.Error()}
}

func errMsg(msg string) core.ToolResult {
	return core.ToolResult{Success: false, Error: msg}
}

func fail(msg string) core.ToolResult {
	return core.ToolResult{Success: false, Error: msg}
}
