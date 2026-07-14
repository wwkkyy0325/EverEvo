package tools

import (
	"encoding/json"
	"fmt"
)

// OutputPolicy controls how a tool's result is truncated before entering context.
type OutputPolicy struct {
	MaxBytes     int  // hard cap; 0 = no limit
	KeepHead     int  // preserve first N bytes (before truncation marker)
	KeepTail     int  // preserve last N bytes (after truncation marker)
	Summarizable bool // whether the result can be summarized by an LLM
}

// DefaultOutputPolicy is applied to tools without an explicit policy.
// 8KB cap, 3KB head, 1KB tail.
var DefaultOutputPolicy = OutputPolicy{
	MaxBytes:     8192,
	KeepHead:     3072,
	KeepTail:     1024,
	Summarizable: true,
}

// toolOutputPolicies maps tool names to their output policies.
// Tools not listed here use DefaultOutputPolicy.
var toolOutputPolicies = map[string]OutputPolicy{
	// Model tools — naturally bounded
	"model_list":            {MaxBytes: 0}, // lists are bounded
	"model_list_downloaded": {MaxBytes: 0},
	"model_list_tool":       {MaxBytes: 0},

	// Plugin tools — naturally bounded
	"plugin_list":   {MaxBytes: 0},
	"plugin_status": {MaxBytes: 0},
	"plugin_logs":   {MaxBytes: 16384, KeepHead: 4096, KeepTail: 2048},

	// KB tools — search can be large
	"kb_search": {MaxBytes: 8192, KeepHead: 4096, KeepTail: 1024},
	"kb_list":   {MaxBytes: 0},
	"kb_list_docs": {MaxBytes: 0},

	// File reads — largest outputs
	"read_file":       {MaxBytes: 12288, KeepHead: 3072, KeepTail: 2048, Summarizable: true},
	"read_media_file": {MaxBytes: 4096, KeepHead: 1024, KeepTail: 512},

	// Catalog — can return many results
	"catalog_search":     {MaxBytes: 10240, KeepHead: 4096, KeepTail: 1024},
	"catalog_list_files": {MaxBytes: 8192, KeepHead: 3072, KeepTail: 1024},

	// System — naturally bounded
	"system_info":    {MaxBytes: 0},
	"system_dynamic": {MaxBytes: 0},
	"system_backends": {MaxBytes: 0},

	// Shell — can be huge
	"shell_exec": {MaxBytes: 8192, KeepHead: 2048, KeepTail: 2048, Summarizable: true},

	// Guide — markdown documents
	"guide_read":   {MaxBytes: 10240, KeepHead: 3072, KeepTail: 1024},
	"guide_search": {MaxBytes: 8192, KeepHead: 3072, KeepTail: 1024},

	// Workflow — naturally bounded
	"workflow_list":     {MaxBytes: 0},
	"workflow_get":      {MaxBytes: 0},
	"workflow_status":   {MaxBytes: 0},
	"workflow_validate": {MaxBytes: 0},

	// Agent — can be verbose
	"agent_list":  {MaxBytes: 0},
	"agent_run":   {MaxBytes: 16384, KeepHead: 4096, KeepTail: 2048, Summarizable: true},

	// Download — status is bounded
	"download_engine": {MaxBytes: 0},

	// Web tools — fetch can be very large
	"web_search": {MaxBytes: 10240, KeepHead: 4096, KeepTail: 1024},
	"web_fetch":  {MaxBytes: 16384, KeepHead: 4096, KeepTail: 2048, Summarizable: true},
}

// GetOutputPolicy returns the output policy for a tool, falling back to DefaultOutputPolicy.
func GetOutputPolicy(name string) OutputPolicy {
	if p, ok := toolOutputPolicies[name]; ok {
		return p
	}
	return DefaultOutputPolicy
}

// Truncate applies the output policy to raw data. Returns the (possibly truncated)
// data and a boolean indicating whether truncation occurred.
func Truncate(data []byte, policy OutputPolicy) ([]byte, bool) {
	if policy.MaxBytes <= 0 || len(data) <= policy.MaxBytes {
		return data, false
	}

	head := policy.KeepHead
	tail := policy.KeepTail
	if head+tail > policy.MaxBytes {
		// Ensure head+tail don't exceed max
		head = policy.MaxBytes * 2 / 3
		tail = policy.MaxBytes - head
	}
	if head > len(data) {
		head = len(data)
		tail = 0
	}
	if head+tail > len(data) {
		tail = len(data) - head
	}

	totalLen := len(data)
	truncated := make([]byte, 0, head+tail+200)

	// Head portion
	truncated = append(truncated, data[:head]...)

	// Truncation marker
	marker := fmt.Sprintf("\n\n─── [输出已截断: %d bytes 总计, 显示前 %d + 后 %d bytes] ───\n", totalLen, head, tail)
	if policy.Summarizable {
		marker = fmt.Sprintf("\n\n─── [输出已截断: %d bytes 总计. 使用 tool_search 重新获取或指定 offset 读取特定部分] ───\n", totalLen)
	}
	truncated = append(truncated, []byte(marker)...)

	// Tail portion
	if tail > 0 {
		truncated = append(truncated, data[len(data)-tail:]...)
	}

	return truncated, true
}

// CompactResult applies output truncation to a ToolResult's Data field.
// Returns the (possibly modified) result and whether truncation was applied.
func CompactResult(name string, result ToolResult) (ToolResult, bool) {
	if !result.Success || len(result.Data) == 0 {
		return result, false
	}
	policy := GetOutputPolicy(name)
	if policy.MaxBytes <= 0 || len(result.Data) <= policy.MaxBytes {
		return result, false
	}

	truncated, did := Truncate(result.Data, policy)
	if !did {
		return result, false
	}

	result.Data = json.RawMessage(truncated)
	return result, true
}

// ConsumedMarker returns a compact marker replacing a consumed tool result.
// Used after the assistant has processed a tool output, to shrink context.
func ConsumedMarker(name string, resultBytes int) string {
	size := "small"
	switch {
	case resultBytes > 10000:
		size = fmt.Sprintf("%.1fKB", float64(resultBytes)/1024)
	case resultBytes > 1000:
		size = fmt.Sprintf("%d bytes", resultBytes)
	default:
		size = fmt.Sprintf("%d bytes", resultBytes)
	}
	return fmt.Sprintf("[tool_result consumed: %s → %s]", name, size)
}

// IsCoreTool reports whether a tool name belongs to the always-loaded core set.
func IsCoreTool(name string) bool {
	switch name {
	case "tool_search", "read_file", "shell_exec", "web_search", "web_fetch", "agent_run",
		"paradigm_match", "paradigm_select", "paradigm_list", "paradigm_feedback":
		return true
	default:
		return false
	}
}

// CoreToolNames returns the list of always-loaded core tool names.
func CoreToolNames() []string {
	return []string{
		"tool_search", "read_file", "shell_exec", "web_search", "web_fetch", "agent_run",
		"paradigm_match", "paradigm_select", "paradigm_list", "paradigm_feedback",
	}
}
