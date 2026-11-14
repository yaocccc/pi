package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWorkingProjectionLateJoinAndStaleFrames(t *testing.T) {
	start := time.Unix(1700000000, 0)
	var executor WorkingState
	apply := func(raw string, offset time.Duration) { executor.Apply(json.RawMessage(raw), start.Add(offset)) }
	apply(`{"type":"agent_start"}`, 0)
	apply(`{"type":"turn_start","turnIndex":0}`, 0)
	apply(`{"type":"message_end","message":{"role":"assistant","id":"a","timestamp":1,"content":[{"type":"text","text":"one"}],"usage":{"input":100,"output":200}}}`, time.Second)
	apply(`{"type":"turn_start","turnIndex":1}`, 2*time.Second)
	apply(`{"type":"message_end","message":{"role":"assistant","id":"b","timestamp":2,"content":[{"type":"text","text":"two"}],"usage":{"input":200,"output":300}}}`, 3*time.Second)
	apply(`{"type":"turn_start","turnIndex":2}`, 4*time.Second)
	apply(`{"type":"message_update","message":{"role":"assistant","id":"c","timestamp":3,"content":[{"type":"thinking","thinking":"in progress"}],"usage":{"input":51000,"output":165}}}`, 5*time.Second)
	view := executor.View()
	view.Revision = 7
	if !view.Active || view.Turn != 3 || view.Input != 51000 || view.Output != 665 || !view.Started.Equal(start) {
		t.Fatalf("run projection: %+v", view)
	}
	bootstrap := withWorkingSnapshot(json.RawMessage(`{"type":"goui_working_snapshot"}`), view)
	var late WorkingState
	if !late.ApplyShared(bootstrap) || late.View() != view {
		t.Fatalf("late join did not adopt original run: %+v", late.View())
	}
	// Absolute projections must not cause either native or extension replays.
	late.Apply(json.RawMessage(`{"type":"agent_start"}`), start.Add(27*time.Second))
	late.Apply(json.RawMessage(`{"type":"turn_start"}`), start.Add(27*time.Second))
	if late.View() != view {
		t.Fatal("late subscriber re-counted native events")
	}
	apply(`{"type":"message_end","message":{"role":"assistant","id":"a","timestamp":1,"content":[{"type":"text","text":"one"}],"usage":{"input":100,"output":200}}}`, 6*time.Second)
	if executor.View().Output != 665 || !executor.Current || executor.View().Turn != 3 {
		t.Fatal("duplicate final cleared or counted against current turn")
	}
	apply(`{"type":"agent_end"}`, 7*time.Second)
	if !executor.View().Active {
		t.Fatal("agent_end settled the run")
	}
	apply(`{"type":"agent_settled"}`, 27*time.Second)
	settled := executor.View()
	settled.Revision = 10
	late.ApplyShared(withWorkingSnapshot(json.RawMessage(`{"type":"agent_settled"}`), settled))
	late.ApplyShared(bootstrap)
	if late.View() != settled || !late.View().Ended.Equal(start.Add(27*time.Second)) {
		t.Fatalf("stale bootstrap rewound settled run: %+v", late.View())
	}
	late.Dismiss()
	settled.Revision++
	late.ApplyShared(withWorkingSnapshot(json.RawMessage(`{"type":"extension_ui_request"}`), settled))
	if late.View().Present {
		t.Fatal("unrelated shared status resurrected dismissed run")
	}
	apply(`{"type":"agent_start"}`, time.Minute)
	next := executor.View()
	next.Revision = 12
	late.ApplyShared(withWorkingSnapshot(json.RawMessage(`{"type":"agent_start"}`), next))
	if !late.View().Active || !late.View().Present || late.View().Input != 0 || late.View().Output != 0 || !late.View().Started.Equal(start.Add(time.Minute)) {
		t.Fatalf("new run retained old totals/dismissal: %+v", late.View())
	}
	var independent WorkingState
	if independent.View().Present {
		t.Fatal("state leaked across executors")
	}
}

func TestWorkingSnapshotBoundedAndPreservesNativeObject(t *testing.T) {
	var state WorkingState
	now := time.Unix(1, 0)
	state.Apply(json.RawMessage(`{"type":"agent_start"}`), now)
	for i := 0; i < workingDedupLimit+5; i++ {
		state.Apply(json.RawMessage(fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","id":"%d","content":[{"type":"text","text":"secret body"}],"usage":{"input":2,"output":1}}}`, i)), now)
	}
	if len(state.Finished) != workingDedupLimit || len(state.Order) != workingDedupLimit || state.View().Output != workingDedupLimit+5 {
		t.Fatal("completion ledger is no longer bounded/cumulative")
	}
	view := state.View()
	raw := json.RawMessage("  {\"type\":\"message_update\",\"message\":{\"role\":\"assistant\",\"unknown\":[1,2]}}\n")
	wire := withWorkingSnapshot(raw, view)
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["message"]) != `{"role":"assistant","unknown":[1,2]}` {
		t.Fatal("annotation changed native payload")
	}
	if len(wire)-len(bytes.TrimSpace(raw)) >= workingFrameAllowance {
		t.Fatal("progress frame exceeds shared-reader allowance")
	}
	if len(decoded["gouiWorking"]) > 300 || bytes.Contains(decoded["gouiWorking"], []byte("secret")) {
		t.Fatal("projection retained body/ledger instead of constant-size metrics")
	}
	for _, empty := range []string{"{}", " { }\n"} {
		if !json.Valid(withWorkingSnapshot(json.RawMessage(empty), view)) {
			t.Fatal("empty native object produced invalid JSON")
		}
	}
}

func BenchmarkWorkingSnapshotAnnotation(b *testing.B) {
	var w WorkingState
	w.Apply(json.RawMessage(`{"type":"agent_start"}`), time.Now())
	raw := json.RawMessage(`{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"` + strings.Repeat("x", 1<<20) + `"}]}}`)
	v := w.View()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		withWorkingSnapshot(raw, v)
	}
}
