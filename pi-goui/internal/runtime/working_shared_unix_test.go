//go:build unix

package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func sharedWorkingView(t *testing.T, raw json.RawMessage) WorkingSnapshot {
	t.Helper()
	var e struct {
		Working *WorkingSnapshot `json:"gouiWorking"`
	}
	if err := json.Unmarshal(raw, &e); err != nil || e.Working == nil {
		t.Fatalf("missing executor projection: %s (%v)", raw, err)
	}
	return *e.Working
}

func TestSharedWorkingLateJoinOrderedWithNativeEvents(t *testing.T) {
	cfg, root := sharedFixture(t)
	a, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	sharedEvent(t, a, "goui_session_snapshot")
	emit := func(raw string) WorkingSnapshot {
		t.Helper()
		var event struct{ Type string }
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		sharedCall(t, a, map[string]any{"type": "get_fixture_event", "event": json.RawMessage(raw)})
		return sharedWorkingView(t, sharedEvent(t, a, event.Type))
	}
	emit(`{"type":"agent_start"}`)
	emit(`{"type":"message_end","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"first turn"}],"usage":{"input":50,"output":10}}}`)
	before := emit(`{"type":"message_update","message":{"role":"assistant","timestamp":2,"content":[{"type":"text","text":"live"}],"usage":{"input":100,"output":12}}}`)
	b, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	late := sharedWorkingView(t, sharedEvent(t, b, "goui_working_snapshot"))
	if a.PID() != b.PID() || !late.Active || late.Turn != 2 || late.Input != 100 || late.Output != 22 || !late.Started.Equal(before.Started) || late.Revision != before.Revision {
		t.Fatalf("late join=%+v original=%+v pids=%d/%d", late, before, a.PID(), b.PID())
	}
	next := emit(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"more live output"}}`)
	live := sharedWorkingView(t, sharedEvent(t, b, "message_update"))
	if live.Revision <= late.Revision || live != next {
		t.Fatalf("bootstrap/live ordering: before=%+v next=%+v late=%+v", late, next, live)
	}
	end := emit(`{"type":"agent_end"}`)
	if !end.Active || !sharedWorkingView(t, sharedEvent(t, b, "agent_end")).Active {
		t.Fatal("agent_end prematurely froze shared working")
	}
	settled := emit(`{"type":"agent_settled"}`)
	if settled.Active || settled.Ended.IsZero() || sharedWorkingView(t, sharedEvent(t, b, "agent_settled")) != settled {
		t.Fatal("clients did not share settled totals/end time")
	}
	fresh := emit(`{"type":"agent_start"}`)
	if !fresh.Active || fresh.Output != 0 || fresh.Input != 0 || fresh.Turn != 0 || !fresh.Started.After(late.Started) || sharedWorkingView(t, sharedEvent(t, b, "agent_start")) != fresh {
		t.Fatal("new run was not reset consistently")
	}
	wire, err := os.ReadFile(filepath.Join(root, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(wire, []byte(`"kind":"start"`)) != 1 || bytes.Contains(wire, []byte(`"type":"prompt"`)) {
		t.Fatal("progress bootstrap started another backend or replayed a prompt")
	}
}
