//go:build windows

package app

import (
	"testing"

	"everevo/internal/tools"
)

// TestIsOrchestrationToolBlocksReentry locks the recursion guard: a sub-agent
// (runAgentLoop via the workflow agent node) must not be able to re-enter the
// agent/workflow orchestration layer. agent_run would recurse synchronously;
// workflow_execute spawns a detached engine that can itself run agent nodes —
// an unbounded goroutine tree. Read-only workflow tools stay available.
func TestIsOrchestrationToolBlocksReentry(t *testing.T) {
	blocked := []string{
		"agent_run", "agent_create", "agent_list",
		"workflow_execute", "workflow_create", "workflow_update", "workflow_delete",
	}
	for _, name := range blocked {
		if !tools.IsOrchestrationTool(name) {
			t.Errorf("tools.IsOrchestrationTool(%q) = false, want true (must block re-entry)", name)
		}
	}
	allowed := []string{
		"workflow_list", "workflow_get", "workflow_status", "workflow_validate",
		"model_list", "kb_search", "system_info", "plugin_status",
	}
	for _, name := range allowed {
		if tools.IsOrchestrationTool(name) {
			t.Errorf("tools.IsOrchestrationTool(%q) = true, want false (read-only tools stay)", name)
		}
	}
}
