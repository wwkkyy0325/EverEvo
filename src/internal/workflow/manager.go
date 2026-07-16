package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Manager persists workflow definitions to disk and loads them back.
// Follows the same pattern as skills.Manager and guides.Manager.
type Manager struct {
	mu          sync.RWMutex
	workflows   map[string]*WorkflowDef // in-memory cache keyed by ID
	persistDir  string
	executions  map[string]*ExecutionState // active executions
	cancels     map[string]context.CancelFunc
}

// NewManager creates a new workflow Manager, loading persisted workflows from disk.
func NewManager() *Manager {
	dataDir, err := storage.AppDataDir()
	if err != nil {
		dataDir = "data"
	}
	dir := filepath.Join(dataDir, "workflows")
	os.MkdirAll(dir, 0755)

	m := &Manager{
		workflows:  make(map[string]*WorkflowDef),
		persistDir: dir,
		executions: make(map[string]*ExecutionState),
		cancels:    make(map[string]context.CancelFunc),
	}
	m.loadAll()
	return m
}

// ─── Persistence ─────────────────────────────────────────────────

func (m *Manager) loadAll() {
	entries, err := os.ReadDir(m.persistDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || e.Name() == "_index.json" {
			continue
		}
		path := filepath.Join(m.persistDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[workflow] read %s: %v", e.Name(), err)
			continue
		}
		var wf WorkflowDef
		if err := json.Unmarshal(data, &wf); err != nil {
			log.Printf("[workflow] parse %s: %v", e.Name(), err)
			continue
		}
		m.workflows[wf.ID] = &wf
	}
}

func (m *Manager) saveIndex() error {
	var summaries []WorkflowSummary
	for _, wf := range m.workflows {
		summaries = append(summaries, wf.Summary())
	}
	if summaries == nil {
		summaries = []WorkflowSummary{}
	}
	data, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return err
	}
	return atomic.WriteFile(filepath.Join(m.persistDir, "_index.json"), data, 0644)
}

func (m *Manager) saveOne(wf *WorkflowDef) error {
	data, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.persistDir, wf.ID+".json")
	return atomic.WriteFile(path, data, 0644)
}

func (m *Manager) deleteFile(id string) {
	path := filepath.Join(m.persistDir, id+".json")
	os.Remove(path)
}

// ─── CRUD ────────────────────────────────────────────────────────

// List returns summaries of all saved workflows.
func (m *Manager) List() []WorkflowSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []WorkflowSummary
	for _, wf := range m.workflows {
		out = append(out, wf.Summary())
	}
	if out == nil {
		out = []WorkflowSummary{}
	}
	return out
}

// Get returns the full workflow definition by ID.
func (m *Manager) Get(id string) (*WorkflowDef, error) {
	m.mu.RLock()
	wf, ok := m.workflows[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return wf, nil
}

// normalizeEdges assigns a deterministic id to every edge that lacks one.
// Edges authored externally (LLM tool calls, imports) often omit ids; without
// them the frontend (Vue Flow) drops connections because it keys edges by id.
func normalizeEdges(edges []WorkflowEdge) {
	for i := range edges {
		if edges[i].ID == "" {
			edges[i].ID = EdgeID(edges[i].Source, edges[i].Target, edges[i].SourceHandle)
		}
	}
}

// Create saves a new workflow and returns the assigned ID.
func (m *Manager) Create(wf WorkflowDef) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if wf.ID == "" {
		wf.ID = "wf_" + nowString()
	}
	now := nowMillis()
	if wf.CreatedAt == 0 {
		wf.CreatedAt = now
	}
	wf.UpdatedAt = now
	normalizeEdges(wf.Edges)
	if _, exists := m.workflows[wf.ID]; exists {
		return "", fmt.Errorf("workflow %q already exists", wf.ID)
	}
	clone := wf // copy
	m.workflows[wf.ID] = &clone
	if err := m.saveOne(&clone); err != nil {
		delete(m.workflows, wf.ID)
		return "", err
	}
	return wf.ID, m.saveIndex()
}

// Update saves an existing workflow (upsert).
func (m *Manager) Update(wf WorkflowDef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalizeEdges(wf.Edges)
	clone := wf
	m.workflows[wf.ID] = &clone
	if err := m.saveOne(&clone); err != nil {
		return err
	}
	return m.saveIndex()
}

// Delete removes a workflow by ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[id]; !ok {
		return fmt.Errorf("workflow %q not found", id)
	}
	delete(m.workflows, id)
	m.deleteFile(id)
	return m.saveIndex()
}

// Export returns the JSON of a workflow for file download.
func (m *Manager) Export(id string) (json.RawMessage, error) {
	m.mu.RLock()
	wf, ok := m.workflows[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return json.Marshal(wf)
}

// Import parses a JSON workflow definition and saves it.
func (m *Manager) Import(data json.RawMessage) error {
	var wf WorkflowDef
	if err := json.Unmarshal(data, &wf); err != nil {
		return fmt.Errorf("parse workflow JSON: %w", err)
	}
	if wf.ID == "" {
		wf = NewWorkflow(wf.Name)
		// Re-parse to get nodes/edges while using new ID
		_ = json.Unmarshal(data, &wf)
		wf.ID = NewWorkflow(wf.Name).ID
		wf.CreatedAt = nowMillis()
	}
	wf.UpdatedAt = nowMillis()
	normalizeEdges(wf.Edges)
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := wf
	m.workflows[wf.ID] = &clone
	if err := m.saveOne(&clone); err != nil {
		return err
	}
	return m.saveIndex()
}

// Duplicate creates a copy of an existing workflow with a new ID.
func (m *Manager) Duplicate(id string) (*WorkflowDef, error) {
	m.mu.RLock()
	orig, ok := m.workflows[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	dup := *orig
	dup.ID = NewWorkflow(orig.Name + " (副本)").ID
	dup.Name = orig.Name + " (副本)"
	dup.CreatedAt = nowMillis()
	dup.UpdatedAt = nowMillis()

	m.mu.Lock()
	m.workflows[dup.ID] = &dup
	m.mu.Unlock()
	if err := m.saveOne(&dup); err != nil {
		return nil, err
	}
	m.saveIndex()
	return &dup, nil
}

// ─── Execution Tracking ──────────────────────────────────────────

// TrackExecution registers an active execution.
func (m *Manager) TrackExecution(state *ExecutionState) {
	m.mu.Lock()
	m.executions[state.ExecID] = state
	m.mu.Unlock()
}

// RegisterCancel stores the cancel func for an execution and spawns a goroutine
// that evicts the entry once the execution finishes (Done closes), so cancel
// funcs and completed executions don't leak across a long-lived process.
func (m *Manager) RegisterCancel(state *ExecutionState, cancel context.CancelFunc) {
	m.mu.Lock()
	m.cancels[state.ExecID] = cancel
	m.mu.Unlock()
	go func() {
		<-state.Done
		m.mu.Lock()
		delete(m.cancels, state.ExecID)
		delete(m.executions, state.ExecID)
		m.mu.Unlock()
	}()
}

// GetExecution returns the current state of an execution.
func (m *Manager) GetExecution(execID string) *ExecutionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.executions[execID]
}

// CancelExecution cancels a running execution by its ID. It invokes the engine's
// real cancel func (interrupting the in-flight node and emitting wf-exec-cancel),
// rather than just flipping a status flag.
func (m *Manager) CancelExecution(execID string) {
	m.mu.RLock()
	cancel := m.cancels[execID]
	state := m.executions[execID]
	m.mu.RUnlock()
	if cancel != nil {
		cancel()
		return
	}
	// No cancel func registered (already finished): best-effort flag.
	if state != nil {
		state.Status = ExecCancelled
	}
}

// CancelAll cancels every active execution (used at shutdown). It calls each
// engine's real cancel func so in-flight nodes stop promptly.
func (m *Manager) CancelAll() {
	m.mu.RLock()
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, c := range m.cancels {
		cancels = append(cancels, c)
	}
	m.mu.RUnlock()
	for _, c := range cancels {
		c()
	}
}
