package collab

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Step statuses.
const (
	StepPending    = "pending"
	StepInProgress = "in_progress"
	StepDone       = "done"
	StepSkipped    = "skipped"
)

// Plan statuses.
const (
	PlanActive    = "active"
	PlanCompleted = "completed"
	PlanAbandoned = "abandoned"
)

// PlanStep is one executable step in a plan.
type PlanStep struct {
	Index     int       `json:"index"`
	Title     string    `json:"title"`
	Status    string    `json:"status"` // pending | in_progress | done | skipped
	Note      string    `json:"note,omitempty"`
	AgentID   string    `json:"agentId,omitempty"` // agent responsible (optional)
	UpdatedAt time.Time `json:"updatedAt"`
}

// Plan is an AI-authored task breakdown: a goal decomposed into ordered steps
// that are checked off as work proceeds. Lives on the bus so the UI reflects
// progress in real time. Distinct from a workflow (which is a fixed DAG) — a
// plan is a lightweight checklist an agent updates as it works.
type Plan struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId,omitempty"`
	Goal       string     `json:"goal"`
	Steps      []PlanStep `json:"steps"`
	Status     string     `json:"status"`
	Author     string     `json:"author"` // the agent that created it
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// PlanManager holds active plans in-process. Plans are ephemeral work artifacts
// (persisted optionally by the app layer via callbacks).
type PlanManager struct {
	mu    sync.RWMutex
	plans map[string]*Plan
	bus   *EventBus
}

// NewPlanManager creates a plan manager wired to publish change events.
func NewPlanManager(bus *EventBus) *PlanManager {
	return &PlanManager{plans: map[string]*Plan{}, bus: bus}
}

// Create makes a new plan from a goal + ordered step titles.
func (pm *PlanManager) Create(id, goal, sessionID, author string, stepTitles []string) *Plan {
	now := time.Now()
	steps := make([]PlanStep, len(stepTitles))
	for i, t := range stepTitles {
		steps[i] = PlanStep{Index: i, Title: t, Status: StepPending, UpdatedAt: now}
	}
	p := &Plan{
		ID: id, SessionID: sessionID, Goal: goal, Steps: steps,
		Status: PlanActive, Author: author, CreatedAt: now, UpdatedAt: now,
	}
	pm.mu.Lock()
	pm.plans[id] = p
	pm.mu.Unlock()
	pm.bus.Publish("plan."+id+".created", Event{Source: author, Type: "created", Payload: pm.snapshot(p)})
	return p
}

// Get returns a plan by ID.
func (pm *PlanManager) Get(id string) *Plan {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.plans[id]
}

// List returns all active plans.
func (pm *PlanManager) List() []*Plan {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]*Plan, 0, len(pm.plans))
	for _, p := range pm.plans {
		out = append(out, p)
	}
	return out
}

// UpdateStep changes a step's status/note and publishes the change.
// Returns false if the plan or step index doesn't exist.
func (pm *PlanManager) UpdateStep(planID string, stepIndex int, status, note, agentID string) bool {
	pm.mu.Lock()
	p, ok := pm.plans[planID]
	if !ok || stepIndex < 0 || stepIndex >= len(p.Steps) {
		pm.mu.Unlock()
		return false
	}
	s := &p.Steps[stepIndex]
	s.Status = status
	if note != "" {
		s.Note = note
	}
	if agentID != "" {
		s.AgentID = agentID
	}
	s.UpdatedAt = time.Now()
	p.UpdatedAt = time.Now()
	// Auto-complete the plan when all steps are done/skipped.
	if pm.allStepsDoneLocked(p) {
		p.Status = PlanCompleted
	}
	pm.mu.Unlock()

	pm.bus.Publish("plan."+planID+".step", Event{
		Source: agentID, Type: "step",
		Payload: map[string]any{"planId": planID, "index": stepIndex, "status": status, "note": note},
	})
	return true
}

// Complete marks a plan finished regardless of step state.
func (pm *PlanManager) Complete(planID string) bool {
	pm.mu.Lock()
	p, ok := pm.plans[planID]
	if !ok {
		pm.mu.Unlock()
		return false
	}
	p.Status = PlanCompleted
	p.UpdatedAt = time.Now()
	pm.mu.Unlock()
	pm.bus.Publish("plan."+planID+".completed", Event{Type: "completed"})
	return true
}

// PersistTo saves all active plans as JSON to the given path (best-effort).
func (pm *PlanManager) PersistTo(path string) error {
	pm.mu.RLock()
	snapshot := make([]Plan, 0, len(pm.plans))
	for _, p := range pm.plans {
		if p.Status == PlanActive {
			snapshot = append(snapshot, *p)
		}
	}
	pm.mu.RUnlock()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RestoreFrom loads plans from a JSON file written by PersistTo.
func (pm *PlanManager) RestoreFrom(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err // missing file is fine (first run)
	}
	var plans []Plan
	if err := json.Unmarshal(data, &plans); err != nil {
		return 0, err
	}
	pm.mu.Lock()
	for i := range plans {
		pm.plans[plans[i].ID] = &plans[i]
	}
	pm.mu.Unlock()
	return len(plans), nil
}

// Drop removes a plan.
func (pm *PlanManager) Drop(planID string) {
	pm.mu.Lock()
	delete(pm.plans, planID)
	pm.mu.Unlock()
}

func (pm *PlanManager) allStepsDoneLocked(p *Plan) bool {
	if len(p.Steps) == 0 {
		return false
	}
	for _, s := range p.Steps {
		if s.Status != StepDone && s.Status != StepSkipped {
			return false
		}
	}
	return true
}

// snapshot returns a value copy safe to publish.
func (pm *PlanManager) snapshot(p *Plan) Plan {
	return *p
}
