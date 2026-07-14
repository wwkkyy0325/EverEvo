//go:build windows

package app

// enrichAgentPrompt adds per-turn context (memory/domain/wiki) to the system
// prompt. ThinkLang and paradigm hint are now inline in buildOrchestratorPrompt,
// so this only handles external context that varies per query.
//
// Future: inject per-turn memory recall, wiki recall, RAG KB results here.
// Currently these are handled by the frontend and passed via API messages.
func (a *App) enrichAgentPrompt(base string, userQuery string) string {
	_ = userQuery
	return base
}
