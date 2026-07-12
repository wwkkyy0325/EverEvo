package collab

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestE2ECollaborationFlow exercises the full multi-agent collaboration path:
// create session → fan-out async dispatch to N agents → blackboard sharing →
// wait/gather. Uses a stub runner that simulates agent work.
func TestE2ECollaborationFlow(t *testing.T) {
	k := NewKernel(nil)
	// Stub runner: 3 distinct agents, each writes to the blackboard and
	// returns a short result.
	runner := &e2eRunner{kernel: k}
	d := NewDispatcher(k, runner, nil)
	k.SetDispatcher(d)
	for _, id := range []string{"ag-law", "ag-arch", "ag-sec"} {
		d.RegisterLocal(id)
	}

	// 1. Create a collaboration session.
	session := k.Sessions.Create("collab-e2e", "analyze code", "orch", []Member{
		{AgentID: "ag-law", Role: "compliance"},
		{AgentID: "ag-arch", Role: "design"},
		{AgentID: "ag-sec", Role: "security"},
	})

	// 2. Fan out 3 async dispatches.
	runIDs := make([]string, 3)
	for i, id := range []string{"ag-law", "ag-arch", "ag-sec"} {
		rid, err := dispatchAsyncForTest(k, d, session.ID, id, "review")
		if err != nil {
			t.Fatalf("dispatch %s failed: %v", id, err)
		}
		runIDs[i] = rid
	}

	// 3. Wait for all to complete via Done channels.
	for _, rid := range runIDs {
		r := k.Run(rid)
		if r == nil {
			t.Fatalf("run %s not registered", rid)
		}
		select {
		case <-r.Done:
		case <-time.After(5 * time.Second):
			t.Fatalf("run %s timed out", rid)
		}
	}

	// 4. Each stub agent wrote to the blackboard — verify 3 entries.
	bb := k.Blackboard(session.BlackboardID)
	if bb.Size() != 3 {
		t.Fatalf("expected 3 blackboard entries, got %d", bb.Size())
	}

	// 5. Session still active.
	if s := k.Sessions.Get(session.ID); s == nil || s.Status != SessionActive {
		t.Fatalf("session should be active")
	}

	// 6. Complete the session → blackboard dropped.
	k.Sessions.Complete(session.ID)
	if k.Blackboard(session.BlackboardID) != nil {
		t.Fatal("blackboard should be dropped after session complete")
	}
}

// e2eRunner simulates agents that write findings to the blackboard.
type e2eRunner struct {
	mu     sync.Mutex
	kernel *Kernel
}

func (r *e2eRunner) RunAgent(ctx context.Context, agentID, task, collabSessionID string) (string, error) {
	// Simulate work + a blackboard write scoped to the session.
	if collabSessionID != "" {
		if s := r.kernel.Sessions.Get(collabSessionID); s != nil {
			bb := r.kernel.Blackboard(s.BlackboardID)
			if bb != nil {
				bb.Set("finding:"+agentID, fmt.Sprintf("%s analyzed %s", agentID, task), agentID, KindArtifact)
			}
		}
	}
	return agentID + ": done", nil
}

// dispatchAsyncForTest mirrors App.CollabDispatchAsync without needing the App.
func dispatchAsyncForTest(k *Kernel, d *Dispatcher, sessionID, agentID, task string) (string, error) {
	rid := "run_" + agentID
	r := &AgentRun{
		ID: rid, AgentID: agentID, CollabSessionID: sessionID, Task: task,
		Status: StatusRunning, StartedAt: time.Now(), Done: make(chan struct{}),
	}
	if !k.RegisterRun(r) {
		return "", fmt.Errorf("run cap reached")
	}
	go func() {
		text, err := d.Send(context.Background(), agentID, task, sessionID)
		k.CompleteRun(rid, text, err)
	}()
	return rid, nil
}
