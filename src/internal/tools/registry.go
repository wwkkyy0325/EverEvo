// Deprecated: This is the legacy tool registration system. Tool schemas should
// be registered by implementing core.ToolPlugin (internal/core/plugin.go) and
// calling core.GlobalTools.Register() during init(). See
// internal/plugins/tools/ for examples.
package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"sync"
)

// registry holds all registered internal tool definitions.
var registry = map[string]*ToolDef{}

// externalRegistry holds tools from external MCP servers.
// key = tool name (mcp.<server_id>.<tool_name>), value = source server ID.
var externalRegistry = map[string]*ToolDef{}
var externalSource = map[string]string{} // tool name → server ID

var regMu sync.RWMutex // protects registry, externalRegistry, externalSource

// Register adds an internal tool definition to the global registry.
func Register(t *ToolDef) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[t.Name] = t
}

// RegisterExternal adds a tool from an external MCP server.
func RegisterExternal(t *ToolDef, source string) {
	regMu.Lock()
	defer regMu.Unlock()
	externalRegistry[t.Name] = t
	externalSource[t.Name] = source
}

// UnregisterExternal removes all tools from a given source server.
func UnregisterExternal(source string) {
	regMu.Lock()
	defer regMu.Unlock()
	for name, src := range externalSource {
		if src == source {
			delete(externalRegistry, name)
			delete(externalSource, name)
		}
	}
}

// Lookup finds a tool by name, checking internal first then external.
func Lookup(name string) *ToolDef {
	regMu.RLock()
	defer regMu.RUnlock()
	if t, ok := registry[name]; ok {
		return t
	}
	return externalRegistry[name]
}

// IsExternal reports whether a tool name belongs to an external MCP server.
func IsExternal(name string) bool {
	regMu.RLock()
	defer regMu.RUnlock()
	_, ok := externalRegistry[name]
	return ok
}

// List returns all registered tools (internal + external) sorted by name.
func List() []*ToolDef {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(registry)+len(externalRegistry))
	for n := range registry {
		names = append(names, n)
	}
	for n := range externalRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*ToolDef, len(names))
	for i, n := range names {
		if t, ok := registry[n]; ok {
			out[i] = t
		} else {
			out[i] = externalRegistry[n]
		}
	}
	return out
}

// ListMCP returns all registered tools with MCP-compatible metadata.
// Alias for List() — kept for clarity in MCP context.
func ListMCP() []*ToolDef { return List() }

// RegisterAll registers every tool from every subsystem.
func RegisterAll() {
	registerModelTools()
	registerPluginTools()
	registerKBTools()
	registerCatalogTools()
	registerDownloadTools()
	registerSystemTools()
	registerGuideTools()
	registerWorkflowTools()
	registerMCPTools()
	registerProviderTools()
	registerA2ATools()
	registerAgentTools()
		registerCodebaseTools()
		registerDomainTools()
	registerAppControlTools()
	registerZoneTools()
	registerCollabTools()
	registerPlanTools()
	registerTaskBoardTools()
	registerContextTools()
	registerParadigmTools()
}

// registerContextTools registers context-management meta-tools.
func registerContextTools() {
	Register(&ToolDef{
		Name:        "tool_search",
		Title:       "搜索工具",
		Description: "搜索可用的工具并返回匹配的完整 Schema。使用此工具发现和获取工具的详细参数定义，而不是依赖预先加载的工具列表。query 为空或 '*' 时返回所有工具，category 可限定工具类别",
		Category:    "system",
		Annotations: &ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Parameters: &ToolParams{
			Type: "object",
			Properties: map[string]ToolProp{
				"query":    {Type: "string", Description: "搜索关键词（中英文均可），匹配工具名称、标题和描述。空字符串或 '*' 返回全部"},
				"category": {Type: "string", Description: "限定工具类别：model/plugin/kb/catalog/download/system/toolbox/guide/workflow/mcp/provider/a2a/agent/external"},
			},
		},
	})
}

// ─── helpers ────────────────────────────────────────────────────

// OkResult wraps a successful value into a ToolResult.
// Nil slices are coerced to empty arrays so JSON output is [] not null.
func OkResult(v any) ToolResult {
	if v != nil {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			v = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	data, _ := json.Marshal(v)
	return ToolResult{Success: true, Data: data}
}

// ErrResult wraps an error into a ToolResult.
func ErrResult(err error) ToolResult {
	return ToolResult{Success: false, Error: err.Error()}
}

// ErrMsg wraps an error string into a ToolResult.
func ErrMsg(msg string) ToolResult {
	return ToolResult{Success: false, Error: msg}
}

// GetString extracts a string parameter by key, or returns "".
func GetString(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// GetInt extracts an int parameter by key, or returns 0.
func GetInt(params map[string]any, key string) int {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// GetStringSlice extracts a []string parameter by key.
func GetStringSlice(params map[string]any, key string) []string {
	v, ok := params[key]
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

// GetMap extracts a map[string]string parameter by key.
func GetMap(params map[string]any, key string) map[string]string {
	v, ok := params[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}
