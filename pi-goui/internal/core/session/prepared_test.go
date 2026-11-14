package session

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/extensions/sessionview"
)

const preparedHistory = `{"entries":[{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"question"}},{"type":"message","id":"a","parentId":"u","message":{"role":"assistant","content":[{"type":"text","text":"answer"}],"stopReason":"stop"}}],"leafId":"a"}`

type ownerProjection struct {
	*sessionview.State
	forbid bool
	calls  int
}

func (p *ownerProjection) NewSnapshot(entries []extensionapi.SessionEntry) extensionapi.SessionSnapshot {
	if p.forbid {
		panic("preparation called the UI-owned extension")
	}
	p.calls++
	return p.State.NewSnapshot(entries)
}

func TestPreparedSnapshotIsDetachedAndReplaysOnOwner(t *testing.T) {
	var state State
	projection := &ownerProjection{State: &sessionview.State{}}
	state.SetExtensionProjection(projection)
	if err := state.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	preparation := state.SnapshotPreparation()
	projection.forbid = true
	var prepared *PreparedSnapshot
	var prepareErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prepared, prepareErr = preparation.Prepare(nil, json.RawMessage(preparedHistory), nil)
	}()
	// Exercise the original maps, slices and projections concurrently. A
	// caller would reject this result by revision rather than commit it.
	for i := 0; i < 20; i++ {
		state.Apply(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"later"}}`))
	}
	wg.Wait()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	if projection.calls != 1 {
		t.Fatal("background preparation reached extension capability")
	}
	projection.forbid = false
	var target State
	targetProjection := &ownerProjection{State: &sessionview.State{}}
	target.SetExtensionProjection(targetProjection)
	target.ApplySnapshot(prepared)
	if targetProjection.calls != 1 || projection.calls != 1 || len(target.Items) != 2 || target.Items[1].Text != "answer" {
		t.Fatalf("prepared state contaminated by later live updates: %+v", target.Items)
	}
	// Adoption must not make the reusable immutable result alias target maps.
	target.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"next"}}`))
	var other State
	other.ApplySnapshot(prepared)
	if len(other.Items) != 2 {
		t.Fatal("adopted state mutated the prepared result")
	}
}

func TestPreparedSnapshotFailureIsAtomic(t *testing.T) {
	var state State
	state.SetExtensionProjection(&sessionview.State{})
	if err := state.LoadState(json.RawMessage(`{"sessionId":"original","sessionFile":"original.jsonl"}`)); err != nil {
		t.Fatal(err)
	}
	if err := state.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	before := state
	beforeItems := append([]Item(nil), state.Items...)
	bad := json.RawMessage(strings.Replace(preparedHistory, `"parentId":"u"`, `"parentId":"missing"`, 1))
	prepared, err := state.SnapshotPreparation().Prepare(json.RawMessage(`{"sessionId":"replacement","sessionFile":"replacement.jsonl"}`), bad, nil)
	if err == nil || prepared != nil {
		t.Fatal("invalid history produced a commit candidate")
	}
	state.ApplySnapshot(prepared)
	if state.ID != before.ID || state.File != before.File || !reflect.DeepEqual(state.Items, beforeItems) || state.leaf != before.leaf {
		t.Fatal("failed preparation partially replaced existing history")
	}
}

func TestPreparedSnapshotRetainsHistoryOnOptionalStatsError(t *testing.T) {
	var state State
	prepared, err := state.SnapshotPreparation().Prepare(nil, json.RawMessage(preparedHistory), json.RawMessage(`{"tokens":"invalid"}`))
	if err == nil || prepared == nil {
		t.Fatal("stats failure discarded valid history")
	}
	state.ApplySnapshot(prepared)
	if len(state.Items) != 2 || state.Items[1].Text != "answer" {
		t.Fatal("valid history was not adopted")
	}
}

func TestPreparedSnapshotPreservesMetadataOmittedByEndpoints(t *testing.T) {
	var state State
	if err := state.LoadState(json.RawMessage(`{"sessionId":"one"}`)); err != nil {
		t.Fatal(err)
	}
	state.Steering = []string{"queued"}
	state.Stats.Input = 3
	prepared, err := state.SnapshotPreparation().Prepare(json.RawMessage(`{"sessionId":"one"}`), json.RawMessage(preparedHistory), nil)
	if err != nil {
		t.Fatal(err)
	}
	// These acknowledgements do not change the branch revision. A detached
	// history candidate must not resurrect their older captured metadata.
	state.Steering = nil
	state.Stats.Input = 9
	state.ApplySnapshot(prepared)
	if state.Steering != nil || state.Stats.Input != 9 || len(state.Items) != 2 {
		t.Fatal("prepared history overwrote newer metadata omitted by endpoints")
	}
}

func TestDetachedStateReplaysWithoutOwnerCapabilitiesOrContainers(t *testing.T) {
	var original State
	projection := &sessionview.State{}
	original.SetExtensionProjection(projection)
	if err := original.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	original.Steering = []string{"original"}
	next := original.Detached()
	if next.projection != nil || next.Plan != nil {
		t.Fatal("detached state retained an owner capability")
	}
	next.Steering[0] = "background"
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			next.Apply(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"background"}}`))
			next.Apply(json.RawMessage(`{"type":"tool_execution_start","toolCallId":"background","toolName":"read","args":{"path":"background"}}`))
		}
	}()
	for i := 0; i < 30; i++ {
		original.Apply(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"foreground"}}`))
		original.Apply(json.RawMessage(`{"type":"tool_execution_start","toolCallId":"foreground","toolName":"read","args":{"path":"foreground"}}`))
	}
	wg.Wait()
	if original.Steering[0] != "original" || original.tools["background"].ToolID != "" || next.tools["foreground"].ToolID != "" {
		t.Fatal("detached replay shared mutable owner containers")
	}
	for _, item := range original.Items {
		if strings.Contains(item.Text, "background") {
			t.Fatal("background replay changed the owner message")
		}
	}
}

func BenchmarkPreparedSnapshotCommit(b *testing.B) {
	raw := json.RawMessage(strings.Replace(preparedHistory, "answer", strings.Repeat("x", 1<<20), 1))
	var state State
	prepared, err := state.SnapshotPreparation().Prepare(nil, raw, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.ApplySnapshot(prepared)
	}
}
