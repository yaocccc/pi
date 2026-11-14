package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"pi-goui/internal/extensions/sessionview"
	rpc "pi-goui/internal/runtime"
)

// This helper is only started with the current test binary, never a real Pi.
func TestUIEventBatchProcess(t *testing.T) {
	if os.Getenv("PIGOUI_EVENT_BATCH_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		switch request["type"] {
		case "burst":
			for i := 0; i < 4096; i++ {
				fmt.Fprintf(os.Stdout, "{\"type\":\"message_update\",\"assistantMessageEvent\":{\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"%04d \"}}\n", i)
			}
			_ = encoder.Encode(map[string]any{"type": "response", "id": request["id"], "success": true, "data": map[string]any{}})
		case "finish":
			fmt.Fprintln(os.Stderr, "event batch fixture stopped")
			os.Exit(7)
		}
	}
	os.Exit(0)
}

func TestNextEventBatchesRealClientBurst(t *testing.T) {
	// One scheduler thread makes a nonblocking drain of the old unbuffered
	// Events channel reliably return singletons, even with a full backlog.
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PIGOUI_EVENT_BATCH_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	client, err := rpc.Start(rpc.Config{Bin: executable, Args: []string{"-test.run=^TestUIEventBatchProcess$", "--"}, Cwd: dir, AgentDir: dir, NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The response is a barrier: all preceding deltas have entered the real
	// runtime queue (apart from its bounded incoming staging channel).
	if _, err := client.Call(ctx, map[string]any{"type": "burst"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Send(ctx, map[string]any{"type": "finish"}); err != nil {
		t.Fatal(err)
	}
	m, projection := batchFixture(t)
	tab := m.current()
	tab.client = client
	multi, closed := false, false
	batches, largest := 0, 0
	for i := 0; i < 4100; i++ {
		batch, ok := nextEvent(tab)().(eventBatchMsg)
		if !ok {
			t.Fatal("nextEvent did not return a batch")
		}
		multi = multi || len(batch.events) > 1
		largest = max(largest, len(batch.events))
		if len(batch.events) > 0 {
			batches++
		}
		m.Update(batch)
		if batch.closed {
			closed = true
			break
		}
	}
	if !closed || !tab.dead {
		t.Fatal("terminal closure not delivered")
	}
	if !multi || projection.visits >= 4096/4 {
		t.Fatalf("runtime did not batch its backlog: multi=%v projection visits=%d", multi, projection.visits)
	}
	if len(projection.seen) != 4096 {
		t.Fatalf("observed %d deltas, want 4096", len(projection.seen))
	}
	var expected strings.Builder
	for i := 0; i < 4096; i++ {
		fmt.Fprintf(&expected, "%04d ", i)
	}
	items := tab.state.Items
	if len(items) != 3 || items[2].Text != expected.String() {
		t.Fatal("real client burst lost or reordered tail data")
	}
	if !strings.Contains(tab.status, "exit status 7") || !strings.Contains(tab.status, "event batch fixture stopped") {
		t.Fatalf("terminal diagnosis lost: %q", tab.status)
	}
	t.Logf("4096 ordered deltas: %d batches, largest=%d, projection visits=%d (individual=12288)", batches, largest, projection.visits)
}

type batchProjection struct {
	*sessionview.State
	visits  int
	seen    []string
	onEvent func(string)
}

func (p *batchProjection) HideMessage(role, customType string) bool {
	p.visits++
	return p.State.HideMessage(role, customType)
}
func (p *batchProjection) ObserveEvent(raw json.RawMessage) {
	var event struct{ Type string }
	_ = json.Unmarshal(raw, &event)
	p.seen = append(p.seen, event.Type)
	if p.onEvent != nil {
		p.onEvent(event.Type)
	}
	p.State.ObserveEvent(raw)
}

func batchFixture(t *testing.T) (*Model, *batchProjection) {
	t.Helper()
	m := testModel(t, "{}")
	p := &batchProjection{State: &sessionview.State{}}
	m.current().state.SetExtensionProjection(p)
	if err := m.current().state.LoadMessages(json.RawMessage(`{"messages":[{"role":"user","content":"older user","timestamp":1},{"role":"assistant","content":"older response","timestamp":2}]}`)); err != nil {
		t.Fatal(err)
	}
	m.current().state.Apply(json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":3}}`))
	p.visits, p.seen = 0, nil
	return m, p
}

func batchEvent(raw string) rpc.Event { return rpc.Event{Data: json.RawMessage(raw)} }

func TestEventBatchProjectsOnceAndObservesEveryUpdate(t *testing.T) {
	const eventBatchLimit = 64
	t.Setenv("HOME", t.TempDir())
	m, p := batchFixture(t)
	tab := m.current()
	var events []rpc.Event
	for i := 0; i < eventBatchLimit; i++ {
		events = append(events, batchEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"x"}}`))
	}
	m.Update(eventBatchMsg{id: tab.id, events: events})
	if p.visits != 3 || len(p.seen) != eventBatchLimit {
		t.Fatalf("projection visits=%d (want 3), observed=%d (want %d)", p.visits, len(p.seen), eventBatchLimit)
	}
	items := tab.state.Items
	if items[len(items)-1].Text != strings.Repeat("x", eventBatchLimit) {
		t.Fatalf("lost stream deltas: %+v", items)
	}
	if tab.revision != eventBatchLimit {
		t.Fatalf("revision increments lost: %d", tab.revision)
	}
}

func TestEventBatchFlushesControlBoundaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, p := batchFixture(t)
	tab := m.current()
	observed := map[string]string{}
	p.onEvent = func(kind string) {
		if kind == "future_event" || kind == "compaction_start" || kind == "agent_start" {
			items := tab.state.Items
			observed[kind] = items[len(items)-1].Text
		}
	}
	update := batchEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"x"}}`)
	events := []rpc.Event{
		update, update,
		batchEvent(`{"type":"future_event","unknown":"must be observed"}`),
		update,
		batchEvent(`{"type":"compaction_start"}`),
		update,
		batchEvent(`{"type":"goui_session_snapshot","initial":true}`),
		update,
		batchEvent(`{"type":"extension_ui_request","method":"setStatus","statusKey":"batch","statusText":"kept"}`),
		update,
		batchEvent(`{"type":"agent_start"}`),
	}
	m.Update(eventBatchMsg{id: tab.id, events: events})
	want := map[string]string{"future_event": "xx", "compaction_start": "xxx", "agent_start": "xxxxxx"}
	// There are five independently flushed runs, totaling six deltas.
	if !reflect.DeepEqual(observed, want) || p.visits != 5*3 {
		t.Fatalf("boundary observations=%v visits=%d, want %v visits=15", observed, p.visits, want)
	}
	if tab.workingStart != len(tab.state.Items) || !tab.state.Busy {
		t.Fatal("agent_start did not see fully projected preceding history")
	}
}

func TestEventBatchMatchesSingleEvents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	single, singleProjection := batchFixture(t)
	batched, batchProjection := batchFixture(t)
	events := []rpc.Event{
		batchEvent(`{"type":"agent_start"}`),
		batchEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello "}}`),
		batchEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"世界"}}`),
		batchEvent(`{"type":"tool_execution_start","toolCallId":"tool","toolName":"read","args":{"path":"a.go"}}`),
		batchEvent(`{"type":"tool_execution_update","toolCallId":"tool","partialResult":{"content":[{"type":"text","text":"partial"}]}}`),
		batchEvent(`{"type":"tool_execution_update","toolCallId":"tool","partialResult":{"content":[{"type":"text","text":"more"}]}}`),
		batchEvent(`{"type":"tool_execution_end","toolCallId":"tool","result":{"content":[{"type":"text","text":"final"}]},"isError":false}`),
		batchEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello 世界"}],"timestamp":3,"usage":{"input":12,"output":7}}}`),
		batchEvent(`{"type":"tool_execution_end","toolCallId":"plan","toolName":"update_plan","result":{"details":{"plan":[{"id":"one","goal":"keep plan","status":"pending"}]}}}`),
		batchEvent(`{"type":"queue_update","steering":["keep queue"],"followUp":[]}`),
		batchEvent(`{"type":"agent_settled"}`),
	}
	for _, event := range events {
		single.Update(eventMsg{id: single.current().id, event: event})
	}
	batched.Update(eventBatchMsg{id: batched.current().id, events: events})
	a, b := single.current(), batched.current()
	if !reflect.DeepEqual(a.state.Items, b.state.Items) || !reflect.DeepEqual(a.state.Plan, b.state.Plan) || !reflect.DeepEqual(a.state.Steering, b.state.Steering) || !reflect.DeepEqual(singleProjection.seen, batchProjection.seen) {
		t.Fatal("batched state or extension observations differ from individual events")
	}
	if a.status != b.status || a.state.Busy != b.state.Busy || a.revision != b.revision || a.workingStart != b.workingStart {
		t.Fatal("batched lifecycle state differs from individual events")
	}
	av, bv := a.working.view(), b.working.view()
	av.Started, av.Ended = time.Time{}, time.Time{}
	bv.Started, bv.Ended = time.Time{}, time.Time{}
	if !reflect.DeepEqual(av, bv) {
		t.Fatalf("working state differs: %+v != %+v", av, bv)
	}
}

func TestEventBatchClosesAfterTailAndRejectsOldClient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _ := batchFixture(t)
	tab := m.current()
	tab.client = &rpc.Client{}
	tab.editor.SetValue("keep draft")
	tail := batchEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"tail"}}`)
	terminal := errors.New("client backlog exhausted")
	old := &rpc.Client{}
	_, cmd := m.Update(eventBatchMsg{id: tab.id, client: old, events: []rpc.Event{tail}, closed: true, err: terminal})
	if cmd != nil || tab.dead || tab.lastEventErr != nil || len(tab.state.Items) != 2 {
		t.Fatal("old-client batch changed current tab or scheduled another read")
	}
	m.Update(eventBatchMsg{id: tab.id, client: tab.client, events: []rpc.Event{tail}, closed: true, err: terminal})
	if !tab.dead || !strings.Contains(tab.status, terminal.Error()) || tab.editor.Value() != "keep draft" {
		t.Fatal("terminal batch lost failure or draft")
	}
	if items := tab.state.Items; items[len(items)-1].Text != "tail" {
		t.Fatal("close discarded the last stream update")
	}
}
