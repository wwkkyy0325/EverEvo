package mcp

import (
	"encoding/json"

	"everevo/internal/tools"
)

// HandleToolsList returns all registered tools in MCP format.
func HandleToolsList() (*ListToolsResult, error) {
	all := tools.ListMCP()
	mcpTools := make([]ToolDef, 0, len(all))
	for _, t := range all {
		def := ToolDef{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: &ToolParams{
				Type:       t.Parameters.Type,
				Properties: convertProps(t.Parameters.Properties),
				Required:   t.Parameters.Required,
			},
		}
		if t.OutputSchema != nil {
			def.OutputSchema = map[string]any{
				"type":       t.OutputSchema.Type,
				"properties": t.OutputSchema.Properties,
			}
		}
		if t.Annotations != nil {
			def.Annotations = &ToolAnnot{
				ReadOnlyHint:    t.Annotations.ReadOnlyHint,
				DestructiveHint: t.Annotations.DestructiveHint,
				IdempotentHint:  t.Annotations.IdempotentHint,
			}
		}
		if def.Title == "" {
			def.Title = t.Name // fallback
		}
		mcpTools = append(mcpTools, def)
	}
	return &ListToolsResult{Tools: mcpTools}, nil
}

func convertProps(src map[string]tools.ToolProp) map[string]ToolProp {
	if src == nil {
		return nil
	}
	out := make(map[string]ToolProp, len(src))
	for k, v := range src {
		p := ToolProp{
			Type:        v.Type,
			Description: v.Description,
			Enum:        v.Enum,
			Default:     v.Default,
		}
		if v.Items != nil {
			p.Items = &ToolProp{
				Type:        v.Items.Type,
				Description: v.Items.Description,
			}
		}
		out[k] = p
	}
	return out
}

// HandleToolsCall dispatches a tool call through the app's CallTool method.
func HandleToolsCall(app ToolCaller, name string, args map[string]any) (*CallToolResult, error) {
	result := app.CallTool(name, args)

	text := ""
	if result.Error != "" {
		text = "Error: " + result.Error
		if result.Data != nil {
			text += "\n\nData: " + string(result.Data)
		}
	} else if result.Data != nil {
		var prettied any
		if json.Unmarshal(result.Data, &prettied) == nil {
			pretty, _ := json.MarshalIndent(prettied, "", "  ")
			text = string(pretty)
		} else {
			text = string(result.Data)
		}
	} else {
		text = "OK"
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(text)},
		IsError: !result.Success,
	}, nil
}

// ToolCaller is the interface the MCP server needs from the app.
type ToolCaller interface {
	CallTool(name string, params map[string]any) tools.ToolResult
}
