package tools

// IsMemoryScopedTool reports whether a tool only operates within the agent's
// memory scope (read-only or add-only memory/wiki/graph operations).
func IsMemoryScopedTool(name string) bool {
	switch name {
	case "memory_search", "memory_list", "memory_add_fact",
		"core_list", "core_add",
		"graph_list", "graph_add_edge",
		"wiki_list", "wiki_read", "wiki_save", "wiki_search",
		"experience_list":
		return true
	}
	return false
}

// IsBlackboardTool reports whether a tool touches the collaboration blackboard.
func IsBlackboardTool(name string) bool {
	switch name {
	case "blackboard_set", "blackboard_get", "blackboard_list":
		return true
	}
	return false
}

// IsOrchestrationTool reports whether a tool is an orchestration or destructive
// system tool that must be blocked for sub-agents (recursion guard).
func IsOrchestrationTool(name string) bool {
	switch name {
	// orchestration (recursion / re-entry)
	case "agent_list", "agent_create", "agent_set_library", "agent_run", "agent_run_async",
		"library_list", "agent_delegate_to_domain", "agent_delegate_multi_domain",
		"workflow_execute", "workflow_create", "workflow_update", "workflow_delete",
		"agent_synthesize_tool",
		"collab_create", "collab_dispatch", "collab_dispatch_async", "collab_wait",
		"collab_complete", "agent_message",
		"plan_create", "plan_step_update":
		return true
	// destructive system tools — sub-agents must not escalate
	case "shell_exec", "evolve_swap", "evolve_build",
		"zone_merge", "zone_discard",
		"backup_restore", "backup_delete",
		"plugin_create", "plugin_delete":
		return true
	}
	return false
}

// IsReadOnlyCollabTool reports whether a tool is a read-only collaboration view.
// These are safe for ALL agents including sub-agents.
func IsReadOnlyCollabTool(name string) bool {
	switch name {
	case "plan_list", "blackboard_list", "blackboard_get", "collab_list_sessions":
		return true
	}
	return false
}

// IsCoreCollabTool reports whether a tool is a core collaboration primitive
// that the orchestrator should always be able to invoke.
func IsCoreCollabTool(name string) bool {
	switch name {
	case "plan_create", "plan_step_update", "plan_list",
		"collab_create", "collab_dispatch", "collab_dispatch_async", "collab_wait",
		"blackboard_set", "blackboard_get", "blackboard_list",
		"agent_message":
		return true
	}
	return false
}
