package runtime

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestWorkingLocalRunAndBoundedLedger(t *testing.T) {
	start := time.Unix(1700000000, 0)
	var state WorkingState
	apply := func(raw string, offset time.Duration) { state.Apply(json.RawMessage(raw), start.Add(offset)) }
	apply(`{"type":"agent_start"}`, 0)
	apply(`{"type":"turn_start","turnIndex":0}`, 0)
	apply(`{"type":"message_end","message":{"role":"assistant","id":"a","timestamp":1,"content":[{"type":"text","text":"one"}],"usage":{"input":100,"output":200}}}`, time.Second)
	apply(`{"type":"turn_start","turnIndex":1}`, 2*time.Second)
	apply(`{"type":"message_update","message":{"role":"assistant","id":"b","timestamp":2,"content":[{"type":"thinking","thinking":"in progress"}],"usage":{"input":51000,"output":165}}}`, 3*time.Second)
	if v := state.View(); !v.Active || v.Turn != 2 || v.Input != 51000 || v.Output != 365 || !v.Started.Equal(start) {
		t.Fatalf("run: %+v", v)
	}
	apply(`{"type":"message_end","message":{"role":"assistant","id":"a","timestamp":1,"content":[{"type":"text","text":"one"}],"usage":{"input":100,"output":200}}}`, 4*time.Second)
	if state.View().Output != 365 || !state.Current {
		t.Fatal("duplicate final counted against current turn")
	}
	apply(`{"type":"agent_end"}`, 5*time.Second)
	if !state.View().Active {
		t.Fatal("agent_end settled the run")
	}
	apply(`{"type":"agent_settled"}`, 27*time.Second)
	if v := state.View(); v.Active || !v.Ended.Equal(start.Add(27*time.Second)) {
		t.Fatalf("settled: %+v", v)
	}
	state.Dismiss()
	if state.View().Present {
		t.Fatal("dismiss retained local presentation")
	}
	apply(`{"type":"agent_start"}`, time.Minute)
	if v := state.View(); !v.Active || !v.Present || v.Input != 0 || v.Output != 0 {
		t.Fatalf("new run retained totals: %+v", v)
	}
	for i := 0; i < workingDedupLimit+5; i++ {
		apply(fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","id":"%d","content":[{"type":"text","text":"body"}],"usage":{"input":2,"output":1}}}`, i), time.Minute)
	}
	if len(state.Finished) != workingDedupLimit || len(state.Order) != workingDedupLimit || state.View().Output != workingDedupLimit+5 {
		t.Fatal("completion ledger no longer bounded/cumulative")
	}
}
