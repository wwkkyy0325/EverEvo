package memory

import "testing"

// TestLogAndListActivity exercises the unified activity log: insert rows,
// verify newest-first ordering, filtering by session/kind, and that an empty
// result is a non-nil slice (so JSON is [] not null).
func TestLogAndListActivity(t *testing.T) {
	s := newTestStore(t)
	rows := []ActivityRow{
		{Ts: 1000, Kind: "agent_start", Source: "ag1", SourceName: "Evo", Topic: "agent.ag1.start", Summary: "开始：hello", SessionID: "s1"},
		{Ts: 2000, Kind: "tool_call", Source: "ag1", Topic: "tool.ag1.call", Summary: "调用 model_list", SessionID: "s1"},
		{Ts: 3000, Kind: "workflow_start", Source: "exec_1", SourceName: "翻译流", Topic: "wf-exec-start", Summary: "运行工作流：翻译流"},
	}
	for _, r := range rows {
		if err := s.LogActivity(r); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListActivity(ActivityFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 rows, got %d", len(all))
	}
	// newest-first by ts DESC
	if all[0].Kind != "workflow_start" {
		t.Errorf("newest-first: want workflow_start, got %s", all[0].Kind)
	}

	s1, _ := s.ListActivity(ActivityFilter{SessionID: "s1", Limit: 10})
	if len(s1) != 2 {
		t.Errorf("session s1 filter: want 2, got %d", len(s1))
	}

	tc, _ := s.ListActivity(ActivityFilter{Kind: "tool_call", Limit: 10})
	if len(tc) != 1 {
		t.Errorf("kind=tool_call filter: want 1, got %d", len(tc))
	}

	// empty result must be non-nil so JSON serializes to [] not null
	none, _ := s.ListActivity(ActivityFilter{Kind: "nonexistent", Limit: 10})
	if none == nil {
		t.Error("empty result should be non-nil [] (JSON [] not null)")
	}
	if len(none) != 0 {
		t.Errorf("empty filter: want 0, got %d", len(none))
	}
}
