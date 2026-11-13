package sessionview

import (
	"encoding/json"
	"testing"
)

func planMessage(id, status string) Message {
	return Message{Role: "toolResult", ToolName: "update_plan", ToolCallID: id,
		Details: json.RawMessage(`{"plan":[{"id":"a","goal":"Inspect","status":"` + status + `"}]}`)}
}

func planEntry(id, status string) Entry {
	return Entry{ID: id, Type: "custom", CustomType: "sol-pi-online-context-state-v1",
		Data: json.RawMessage(`{"version":1,"plan":[{"id":"a","goal":"Inspect","status":"` + status + `"}]}`)}
}

func status(t *testing.T, s *State, want string) {
	t.Helper()
	if len(s.Plan) != 1 || s.Plan[0].Status != want {
		t.Fatalf("want %s, got %#v", want, s.Plan)
	}
}

func TestLiveToolSurvivesStaleSnapshotUntilPersistence(t *testing.T) {
	var s State
	s.ObserveMessage(planMessage("live", "completed"), true)
	revision := s.PlanRevision
	stale := s.NewSnapshot(nil)
	stale.ObserveMessage(planMessage("old", "pending"))
	s.Commit(stale, false)
	status(t, &s, "completed")
	if s.PlanRevision != revision {
		t.Fatal("stale snapshot changed revision")
	}
	s.ObserveFallback([]Message{planMessage("fallback", "pending")}, false)
	status(t, &s, "completed")

	current := s.NewSnapshot(nil)
	current.ObserveMessage(planMessage("live", "completed"))
	s.Commit(current, false)
	if s.liveEntry != "" || s.liveTool != "" || s.PlanRevision != revision {
		t.Fatal("persisted result did not release the cursor without revising identical data")
	}
	next := s.NewSnapshot(nil)
	next.ObserveMessage(planMessage("next", "pending"))
	s.Commit(next, false)
	status(t, &s, "pending")
}

func TestLiveEntryAndExplicitBranchSwitch(t *testing.T) {
	var s State
	entry := planEntry("new-entry", "completed")
	raw, err := json.Marshal(map[string]any{"type": "entry_appended", "entry": entry})
	if err != nil {
		t.Fatal(err)
	}
	s.ObserveEvent(raw)
	revision := s.PlanRevision
	p := s.NewSnapshot(nil)
	p.ObserveEntry(planEntry("old-entry", "pending"))
	s.Commit(p, false)
	status(t, &s, "completed")
	// The same plan arriving in a nested execution must not disturb its entry cursor.
	s.ObserveEvent(json.RawMessage(`{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"nested","parentToolCallId":"parent","result":{"details":{"plan":[{"id":"a","goal":"Inspect","status":"completed"}]}}}`))
	if s.liveEntry != "new-entry" || s.liveTool != "" || s.PlanRevision != revision {
		t.Fatal("nested result changed the authoritative entry cursor")
	}
	caughtUp := s.NewSnapshot(nil)
	caughtUp.ObserveEntry(entry)
	s.Commit(caughtUp, false)
	if s.liveEntry != "" || s.PlanRevision != revision {
		t.Fatal("entry persistence did not catch up")
	}
	// An intentional branch change is authoritative even with another live cursor.
	s.ObserveMessage(planMessage("unpersisted", "pending"), true)
	s.Commit(s.NewSnapshot(nil), true)
	if len(s.Plan) != 0 || s.liveEntry != "" || s.liveTool != "" {
		t.Fatal("branch leaked extension state")
	}
}

func TestFallbackResetAndIgnoredMessages(t *testing.T) {
	var s State
	for _, finished := range []bool{false, true} {
		m := planMessage("a", "pending")
		bad := true
		m.IsError = &bad
		s.ObserveMessage(m, finished)
	}
	partial := planMessage("partial", "pending")
	s.ObserveMessage(partial, false)
	other := partial
	other.ToolName = "native-or-unknown"
	s.ObserveMessage(other, true)
	for _, raw := range []string{`{`, `{"type":"unknown"}`, `{"type":"entry_appended","entry":{"type":"custom","customType":"other","data":{"version":1,"plan":[]}}}`} {
		s.ObserveEvent(json.RawMessage(raw))
	}
	if len(s.Plan) != 0 || s.PlanRevision != 0 {
		t.Fatal("irrelevant data installed a projection")
	}
	messages := []Message{planMessage("a", "pending"), planMessage("b", "completed")}
	s.ObserveFallback(messages, true)
	if len(s.Plan) != 0 {
		t.Fatal("fallback replaced full history")
	}
	s.ObserveFallback(messages, false)
	status(t, &s, "completed")
	s.ObserveMessage(planMessage("live", "pending"), true)
	s.Reset()
	if len(s.Plan) != 0 || s.liveEntry != "" || s.liveTool != "" {
		t.Fatal("reset retained private state")
	}
	s.ObserveFallback(messages, false)
	status(t, &s, "completed")
	revision := s.PlanRevision
	s.ObserveFallback(messages, false)
	if s.PlanRevision != revision {
		t.Fatal("duplicate fallback changed revision")
	}
}

func TestSnapshotMetricsPreferNewestActiveAncestor(t *testing.T) {
	metric := func(id string, before int) Entry {
		raw, err := json.Marshal(map[string]any{"version": 1, "compactionId": "c", "estimated": true, "beforeTokens": before, "afterTokens": 2})
		if err != nil {
			t.Fatal(err)
		}
		return Entry{ID: id, Type: "custom", CustomType: "sol-pi-online-context-metrics-v1", Data: raw}
	}
	var s State
	p := s.NewSnapshot([]Entry{metric("new", 40), metric("old", 20)})
	if got := p.Compaction("c"); got == nil || got.BeforeTokens != 40 {
		t.Fatalf("newest metric lost: %#v", got)
	}
	if p.Compaction("unrelated") != nil || s.NewSnapshot(nil).Compaction("c") != nil {
		t.Fatal("metrics escaped their active branch")
	}
	if !HideMessage("custom", "web-search-content-ready") || HideMessage("custom", "other") {
		t.Fatal("Web composition is missing or too broad")
	}
}
