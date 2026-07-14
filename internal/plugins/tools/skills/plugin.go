// Package skills provides skill management tools as a self-registering ToolPlugin.
//
// Tools: skill_list, skill_enable, skill_disable, skill_export, skill_import,
// skill_market_list, skill_market_install.
package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"everevo/internal/core"
	"everevo/internal/marketplace"
)

const pluginID = "skills"

// ── Delegate interface ──────────────────────────────────────────────────

// SkillsDelegate is implemented by the App to provide skill management operations.
type SkillsDelegate interface {
	// SkillList returns all skills with their enabled state, optionally filtered by libraryID.
	SkillList(libraryID string) ([]SkillInfo, error)
	// SkillEnable enables or disables a skill by name.
	SkillEnable(name string, enabled bool) error
	// SkillExport returns all skills as JSON for export.
	SkillExport() (json.RawMessage, error)
	// SkillImport merges incoming JSON skills into the manager.
	SkillImport(data json.RawMessage) error
	// SkillMarketList returns the marketplace with install status.
	SkillMarketList() ([]marketplace.SkillPackage, error)
	// SkillMarketInstall installs a skill from the marketplace.
	SkillMarketInstall(name string) (marketplace.InstallResult, error)
}

// SkillInfo is a lightweight skill descriptor returned by SkillList.
type SkillInfo struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Package     string   `json:"package"`
	Icon        string   `json:"icon,omitempty"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools"`
	LibraryID   string   `json:"libraryId,omitempty"`
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin implements core.ToolPlugin for skill CRUD and marketplace operations.
type Plugin struct {
	delegate SkillsDelegate
}

var _ core.ToolPlugin = (*Plugin)(nil)

// SetSkillsDelegate wires the App-level delegate. Called once at startup.
func SetSkillsDelegate(d SkillsDelegate) {
	p, ok := core.GlobalTools.Get(pluginID)
	if !ok {
		return
	}
	if plug, ok := p.(*Plugin); ok {
		plug.delegate = d
	}
}

func init() {
	core.GlobalTools.Register(pluginID, &Plugin{}, core.PluginManifest{
		ID: pluginID, Name: "技能管理", Version: "1.0",
		Description: "skill_list, skill_enable, skill_disable, skill_export, skill_import, skill_market_list, skill_market_install",
		Author: "EverEvo", Type: "toolset",
	})
}

func (p *Plugin) Manifest() core.PluginManifest {
	return core.PluginManifest{
		ID: pluginID, Name: "技能管理", Version: "1.0",
		Description: "Skill CRUD and marketplace operations",
		Author: "EverEvo", Type: "toolset",
	}
}

func (p *Plugin) ToolDefs() []core.ToolDef {
	return []core.ToolDef{
		{
			Name:        "skill_list",
			Description: "列出所有技能（Skill）及其启用状态、工具列表、分类等详细信息",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"libraryId": map[string]any{"type": "string", "description": "领域库 ID（可选，不填返回全部）"},
				},
			},
		},
		{
			Name:        "skill_enable",
			Description: "启用一个指定的技能。启用的技能会在对话中生效",
			ReadOnly:    false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "技能名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "skill_disable",
			Description: "禁用一个指定的技能。禁用的技能不再出现在对话中",
			ReadOnly:    false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "技能名称"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "skill_export",
			Description: "导出所有技能为 JSON 格式，用于备份或分享",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "skill_import",
			Description: "从 JSON 数据导入技能。已存在的同名技能会被更新，新的会被添加",
			ReadOnly:    false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"data": map[string]any{"type": "string", "description": "JSON 格式的技能数据字符串"},
				},
				"required": []string{"data"},
			},
		},
		{
			Name:        "skill_market_list",
			Description: "列出技能市场中可安装的技能包及其安装状态",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "skill_market_install",
			Description: "从技能市场安装一个技能包（自动添加依赖的 MCP 服务器）",
			ReadOnly:    false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "技能包名称"},
				},
				"required": []string{"name"},
			},
		},
	}
}

// CallTool dispatches a skill tool call to the delegate.
func (p *Plugin) CallTool(_ context.Context, name string, args map[string]any) (core.ToolResult, error) {
	if p.delegate == nil {
		return core.ToolResult{Success: false, Error: fmt.Sprintf("skills/%s: delegate not wired", name)}, nil
	}

	switch name {
	case "skill_list":
		return p.callSkillList(args)
	case "skill_enable":
		return p.callSkillEnable(args)
	case "skill_disable":
		return p.callSkillDisable(args)
	case "skill_export":
		return p.callSkillExport()
	case "skill_import":
		return p.callSkillImport(args)
	case "skill_market_list":
		return p.callSkillMarketList()
	case "skill_market_install":
		return p.callSkillMarketInstall(args)
	default:
		return core.ToolResult{Success: false, Error: fmt.Sprintf("skills: unknown tool %q", name)}, nil
	}
}

// ── Tool implementations ────────────────────────────────────────────────

func (p *Plugin) callSkillList(args map[string]any) (core.ToolResult, error) {
	libraryID := getStr(args, "libraryId")
	list, err := p.delegate.SkillList(libraryID)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: list}, nil
}

func (p *Plugin) callSkillEnable(args map[string]any) (core.ToolResult, error) {
	name := getStr(args, "name")
	if name == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: name"}, nil
	}
	if err := p.delegate.SkillEnable(name, true); err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: map[string]string{"name": name, "status": "enabled"}}, nil
}

func (p *Plugin) callSkillDisable(args map[string]any) (core.ToolResult, error) {
	name := getStr(args, "name")
	if name == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: name"}, nil
	}
	if err := p.delegate.SkillEnable(name, false); err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: map[string]string{"name": name, "status": "disabled"}}, nil
}

func (p *Plugin) callSkillExport() (core.ToolResult, error) {
	data, err := p.delegate.SkillExport()
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: json.RawMessage(data)}, nil
}

func (p *Plugin) callSkillImport(args map[string]any) (core.ToolResult, error) {
	dataStr := getStr(args, "data")
	if dataStr == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: data"}, nil
	}
	if err := p.delegate.SkillImport(json.RawMessage(dataStr)); err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: "技能导入成功"}, nil
}

func (p *Plugin) callSkillMarketList() (core.ToolResult, error) {
	pkgs, err := p.delegate.SkillMarketList()
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: pkgs}, nil
}

func (p *Plugin) callSkillMarketInstall(args map[string]any) (core.ToolResult, error) {
	name := getStr(args, "name")
	if name == "" {
		return core.ToolResult{Success: false, Error: "缺少必填参数: name"}, nil
	}
	result, err := p.delegate.SkillMarketInstall(name)
	if err != nil {
		return core.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return core.ToolResult{Success: true, Data: result}, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func getStr(args map[string]any, key string) string {
	v, _ := args[key]
	s, _ := v.(string)
	return s
}
