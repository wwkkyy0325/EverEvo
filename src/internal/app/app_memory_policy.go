//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"everevo/internal/memory"
	"everevo/internal/sysinfo"
)

// MemoryPolicy returns the current hardware-adaptive memory policy.
func (a *App) MemoryPolicy() (memory.MemoryPolicy, error) {
	if a.memoryStore == nil {
		return memory.DefaultMemoryPolicy(), nil
	}
	return a.memoryStore.Policy(), nil
}

// applyMemoryPolicy computes a hardware-adaptive policy from host RAM/disk and
// persists it to the memory store. Called once at startup.
func (a *App) applyMemoryPolicy() {
	if a.memoryStore == nil {
		return
	}
	dyn := sysinfo.CollectDynamic()
	availRAM := dyn.MemoryTotalGB - dyn.MemoryUsedGB
	if availRAM < 0 {
		availRAM = 0
	}
	var diskFree float64
	for _, d := range dyn.Disks {
		if d.FreeGB > diskFree {
			diskFree = d.FreeGB
		}
	}
	p := memoryPolicyFor(availRAM, diskFree)
	if raw, err := json.Marshal(p); err == nil {
		_ = a.memoryStore.SetPolicyJSON(string(raw))
		log.Printf("[memory] 策略: tier=%s 半衰期=%dd TTL=%dd (availRAM=%.1fGB diskFree=%.1fGB)", p.Tier, p.HalfLifeDays, p.TTLDays, availRAM, diskFree)
	}
}

// memoryPolicyFor maps available RAM (GB) + disk free (GB) to a MemoryPolicy tier.
// RAM-driven; a disk-constrained host (<20GB free) drops one tier (more aggressive).
func memoryPolicyFor(availRAM, diskFreeGB float64) memory.MemoryPolicy {
	std := memory.MemoryPolicy{Tier: "standard", HalfLifeDays: 14, TTLDays: 90, RecallK: 3, ItemCap: 2000, CoreCap: 200, Alpha: 0.7}
	var p memory.MemoryPolicy
	switch {
	case availRAM < 6:
		p = memory.MemoryPolicy{Tier: "low", HalfLifeDays: 7, TTLDays: 30, RecallK: 2, ItemCap: 500, CoreCap: 50, Alpha: 0.7}
	case availRAM <= 16:
		p = std
	default:
		p = memory.MemoryPolicy{Tier: "high", HalfLifeDays: 30, TTLDays: 180, RecallK: 5, ItemCap: 10000, CoreCap: 1000, Alpha: 0.7}
	}
	if diskFreeGB < 20 {
		switch p.Tier {
		case "high":
			p = std
		case "standard":
			p = memory.MemoryPolicy{Tier: "low", HalfLifeDays: 7, TTLDays: 30, RecallK: 2, ItemCap: 500, CoreCap: 50, Alpha: 0.7}
		}
	}
	return p
}

// MemoryCoreList returns permanent core-memory facts (global view, all libraries).
// Kept no-arg for Wails binding compatibility. Use MemoryCoreListByLibrary for
// per-library scoping.
func (a *App) MemoryCoreList() ([]memory.UserFact, error) {
	return a.MemoryCoreListByLibrary("")
}

// MemoryCoreListByLibrary returns core-memory facts scoped to a domain library.
// libraryID "" → all (global); otherwise scoped to that library + legacy 'default'.
func (a *App) MemoryCoreListByLibrary(libraryID string) ([]memory.UserFact, error) {
	if a.memoryStore == nil {
		return []memory.UserFact{}, nil
	}
	facts, err := a.memoryStore.ListUserFacts(libraryID)
	if err != nil {
		return nil, err
	}
	if facts == nil {
		facts = []memory.UserFact{}
	}
	return facts, nil
}

// MemoryCoreAdd inserts a core-memory fact (manual).
func (a *App) MemoryCoreAdd(key, value, category string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.AddUserFact(uuid.NewString(), key, value, category, "high", "manual", "", nil); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "core", "")
	return nil
}

// MemoryCoreLock sets/clears the locked flag on a core-memory fact.
func (a *App) MemoryCoreLock(id string, locked bool) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LockUserFact(id, locked)
}

// ─── Workspaces (P7) — multi-project isolation ───────────────────

// ─── Domain Libraries (P7) — AI-managed knowledge domains ──────

// LibraryList returns all domain libraries.
func (a *App) LibraryList() ([]map[string]any, error) {
	if a.memoryStore == nil {
		return []map[string]any{}, nil
	}
	list, err := a.memoryStore.LibraryList()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(list))
	for i, lib := range list {
		out[i] = map[string]any{"id": lib.ID, "name": lib.Name, "description": lib.Description, "tags": lib.Tags, "autoCreated": lib.AutoCreated, "useCount": lib.UseCount, "createdAt": lib.CreatedAt}
	}
	return out, nil
}

// LibraryCreate adds a domain library and returns its id.
func (a *App) LibraryCreate(name, description, icon string, autoCreated bool) (string, error) {
	if a.memoryStore == nil {
		return "", fmt.Errorf("记忆库未就绪")
	}
	libID, err := a.memoryStore.LibraryCreate(name, description, icon, autoCreated)
	if err != nil {
		return "", err
	}
	// 三位一体: auto-create a default RAG KB for this domain library.
	dir := detectEmbeddingModelDir()
	if dir != "" {
		if _, kbErr := a.CreateKnowledgeBase(name+"-知识库", dir, libID); kbErr != nil {
			log.Printf("[library] 自动创建 KB 失败: %v", kbErr)
		}
	}
	return libID, nil
}

// LibraryUpdate updates a domain library's mutable fields.
func (a *App) LibraryUpdate(id, name, description, icon string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryUpdate(id, name, description, icon)
}

// LibraryDelete removes a domain library. Caller should cascade data first.
func (a *App) LibraryDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryDelete(id)
}

// LibraryBumpUse increments the usage counter for a domain library. Called by
// the frontend whenever the user switches to that domain.
func (a *App) LibraryBumpUse(id string) {
	if a.memoryStore != nil {
		a.memoryStore.BumpLibraryUse(id)
	}
}

// LibraryMerge repoints all knowledge from dropID to keepID and deletes dropID.
func (a *App) LibraryMerge(keepID, dropID string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.LibraryMerge(keepID, dropID)
}

// WorkspaceList returns all workspaces.
func (a *App) WorkspaceList() ([]map[string]any, error) {
	if a.memoryStore == nil {
		return []map[string]any{}, nil
	}
	list, err := a.memoryStore.WorkspaceList()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(list))
	for i, ws := range list {
		out[i] = map[string]any{"id": ws.ID, "name": ws.Name, "createdAt": ws.CreatedAt}
	}
	return out, nil
}

// WorkspaceCreate adds a workspace and returns its id.
func (a *App) WorkspaceCreate(name string) (string, error) {
	if a.memoryStore == nil {
		return "", fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.WorkspaceCreate(name)
}

// WorkspaceDelete removes a workspace row.
func (a *App) WorkspaceDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	return a.memoryStore.WorkspaceDelete(id)
}

// MemoryCoreDelete removes a core-memory fact.
func (a *App) MemoryCoreDelete(id string) error {
	if a.memoryStore == nil {
		return fmt.Errorf("记忆库未就绪")
	}
	if err := a.memoryStore.DeleteUserFact(id); err != nil {
		return err
	}
	a.emitChanged("memory:changed", "core", "")
	return nil
}

// runMemorySweep runs a one-shot expired-episodic sweep at boot, then daily. It
// stops when memSweepDone is closed (shutdown). Best-effort; logs only.
// user_facts (core) is never swept.
func (a *App) runMemorySweep() {
	if a.memoryStore == nil {
		return
	}
	sweep := func() {
		changed := false
		// TTL expiry
		if ids, err := a.memoryStore.SweepExpiredPolicy(); err == nil && len(ids) > 0 {
			log.Printf("[memory] TTL 清理 %d 条过期情节记忆", len(ids))
			changed = true
		}
		// P8: soft cap — compress low-importance items via LLM summary before trimming.
		a.maybeCompress()
		// P8: hard capacity cap — prevent unbounded memory growth.
		policy := a.memoryStore.Policy()
		if n, err := a.memoryStore.TrimMemoryCapacity(policy.ItemCap); err == nil && n > 0 {
			log.Printf("[memory] 容量裁剪 %d 条低优先级记忆 (上限 %d)", n, policy.ItemCap)
			changed = true
		}
		// Notify frontend so counts don't silently drift after background cleanup.
		if changed {
			a.emitChanged("memory:changed", "sweep", "")
		}
	}
	sweep() // boot
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sweep()
		case <-a.memSweepDone:
			return
		}
	}
}

// resolveOrCreateLibrary returns the id of the library with the given name,
// creating it (auto_created) if it doesn't exist (P7 domain auto-discovery).
func (a *App) resolveOrCreateLibrary(name string) string {
	if a.memoryStore == nil {
		return "default"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "default"
	}
	list, _ := a.memoryStore.LibraryList()
	for _, lib := range list {
		if strings.EqualFold(lib.Name, name) {
			return lib.ID
		}
	}
	id, err := a.memoryStore.LibraryCreate(name, "由 AI 自动发现", "🤖", true)
	if err != nil {
		log.Printf("[memory] 自动创建领域库失败: %v", err)
		return "default"
	}
	log.Printf("[memory] 自动发现新领域库: %s", name)
	return id
}
