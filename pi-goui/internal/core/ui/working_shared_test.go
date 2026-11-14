package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pi-goui/internal/core/config"
	rpc "pi-goui/internal/runtime"
)

func TestWorkingSharedLateJoinMatchesOriginalWithCapabilityUsage(t *testing.T) {
	m := testModel(t, "{}")
	original := m.current()
	var executor rpc.WorkingState
	// The same fixed application adapter is used by helper initialization.
	executor.NewExtensions = original.working.NewExtensions
	start := time.Unix(1700000000, 0)
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start","turnIndex":0}`,
		`{"type":"message_end","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"one"}],"usage":{"input":10000,"output":200}}}`,
		`{"type":"turn_start","turnIndex":1}`,
		`{"type":"message_end","message":{"role":"assistant","timestamp":2,"content":[{"type":"text","text":"two"}],"usage":{"input":20000,"output":300}}}`,
		`{"type":"turn_start","turnIndex":2}`,
		`{"type":"message_update","message":{"role":"assistant","timestamp":3,"content":[{"type":"text","text":"live"}],"usage":{"input":50000,"output":150}}}`,
		`{"type":"tool_execution_update","toolCallId":"worker-1","partialResult":{"details":{"kind":"worker-ui","batchId":"batch-1","tasks":[{"index":0,"usage":{"input":900,"cacheRead":100,"output":15}}]}}}`,
	}
	for _, event := range events {
		executor.Apply(json.RawMessage(event), start)
		original.working.apply(json.RawMessage(event), start)
	}
	projection := executor.View()
	projection.Revision = uint64(len(events))
	frame := func(v rpc.WorkingSnapshot) json.RawMessage {
		raw, err := json.Marshal(map[string]any{"type": "goui_working_snapshot", "gouiWorking": v})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	late := m.addTab("")
	late.editor.SetValue("independent draft")
	if !late.working.applyShared(frame(projection)) {
		t.Fatal("working bootstrap not recognized")
	}
	clock := start.Add(27 * time.Second)
	want := "3 turns · ↑51k ↓665 · 24.6 TPS · 27s"
	if got := late.working.line(clock, config.Theme{}); got != want || got != original.working.line(clock, config.Theme{}) {
		t.Fatalf("late=%q original=%q want=%q", got, original.working.line(clock, config.Theme{}), want)
	}
	// An absolute update already contains extension usage; replaying native
	// detail or final events must not count it again in the new UI.
	late.working.apply(json.RawMessage(events[len(events)-1]), clock)
	if late.working.line(clock, config.Theme{}) != want || late.editor.Value() != "independent draft" {
		t.Fatal("shared usage was replayed or local draft changed")
	}
	executor.Apply(json.RawMessage(`{"type":"agent_settled"}`), clock)
	final := executor.View()
	final.Revision = projection.Revision + 1
	late.working.applyShared(frame(final))
	frozen := late.working.line(clock, config.Theme{})
	late.working.applyShared(frame(projection))
	if !strings.HasPrefix(frozen, "✓ ") || late.working.line(clock.Add(time.Hour), config.Theme{}) != frozen {
		t.Fatal("settled duration/TPS moved or stale bootstrap reopened the run")
	}
}

func TestWorkingEventsRejectReplacedBackend(t *testing.T) {
	m := testModel(t, "{}")
	tab := m.current()
	tab.editor.SetValue("keep draft")
	stale := &rpc.Client{}
	revision := tab.revision
	for _, message := range []eventMsg{
		{id: tab.id, client: stale, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_start"}`)}},
		{id: tab.id, client: stale, closed: true},
	} {
		m.Update(message)
	}
	if tab.working.Active || tab.dead || tab.revision != revision || tab.editor.Value() != "keep draft" {
		t.Fatal("replaced backend changed current tab")
	}
}
