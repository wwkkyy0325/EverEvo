package workflow

import "testing"

func TestMergePositions(t *testing.T) {
	old := []WorkflowNode{
		{ID: "n1", Position: &NodePosition{X: 10, Y: 20}},
		{ID: "n2", Position: &NodePosition{X: 30, Y: 40}},
	}
	// n1: LLM omits position → inherit. n2: LLM sends explicit position → keep.
	// n3: brand-new node → nil (frontend lays it out).
	incoming := []WorkflowNode{
		{ID: "n1"},
		{ID: "n2", Position: &NodePosition{X: 99, Y: 99}},
		{ID: "n3"},
	}
	got := MergePositions(old, incoming)

	if got[0].Position == nil || *got[0].Position != (NodePosition{X: 10, Y: 20}) {
		t.Errorf("n1: expected inherited {10,20}, got %v", got[0].Position)
	}
	if got[1].Position == nil || *got[1].Position != (NodePosition{X: 99, Y: 99}) {
		t.Errorf("n2: expected explicit {99,99}, got %v", got[1].Position)
	}
	if got[2].Position != nil {
		t.Errorf("n3: expected nil for new node, got %v", got[2].Position)
	}

	// Merged positions must not alias the old slice's pointers.
	got[0].Position.X = 777
	if old[0].Position.X != 10 {
		t.Errorf("old n1 mutated through alias: expected 10, got %v", old[0].Position.X)
	}
}
