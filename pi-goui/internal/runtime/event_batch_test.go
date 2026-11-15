package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func eventLoopFixture(t *testing.T, batches bool) *Client {
	t.Helper()
	c := &Client{events: make(chan Event), incoming: make(chan Event), stop: make(chan struct{}), readDone: make(chan struct{})}
	if batches {
		c.eventBatches = make(chan []Event)
		c.EventBatches() // Activate before populating the queue for boundary tests.
	}
	done := make(chan struct{})
	go func() {
		c.eventLoop()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-c.stop:
		default:
			close(c.stop)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("event loop did not stop")
		}
	})
	return c
}

func readEventBatch(t *testing.T, c *Client) ([]Event, bool) {
	t.Helper()
	select {
	case batch, ok := <-c.EventBatches():
		return batch, ok
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event batch")
		return nil, false
	}
}

func TestEventBatchesBoundedFIFO(t *testing.T) {
	c := eventLoopFixture(t, true)
	var want []Event
	for i := 0; i < 3*eventBatchLimit+1; i++ {
		e := Event{Data: json.RawMessage(fmt.Sprintf(`{"type":"future_event","n":%d}`, i))}
		if i == 97 {
			e.Err = errors.New("retained warning")
		}
		want = append(want, e)
		// The unbuffered incoming channel is a barrier: the previous event is
		// already in the real event loop's queue before the next is accepted.
		c.incoming <- e
	}
	terminal := errors.New("terminal fixture failure")
	c.setFailure(terminal)
	close(c.readDone)
	close(c.incoming)
	var got []Event
	largest := 0
	for {
		batch, ok := readEventBatch(t, c)
		if !ok {
			break
		}
		if len(batch) == 0 || len(batch) > eventBatchLimit {
			t.Fatalf("invalid batch length: %d", len(batch))
		}
		largest = max(largest, len(batch))
		got = append(got, batch...)
	}
	if largest != eventBatchLimit || !reflect.DeepEqual(got, want) {
		t.Fatalf("largest batch=%d, delivered=%d, want %d ordered events", largest, len(got), len(want))
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("batch delivery duplicated an event in Events")
	}
	if !errors.Is(c.Err(), terminal) {
		t.Fatalf("lost final failure: %v", c.Err())
	}
}

func TestEventBatchesByteLimitAndOversizedSingleton(t *testing.T) {
	c := eventLoopFixture(t, true)
	warmup := Event{Data: json.RawMessage(`{"type":"warmup"}`)}
	want := []Event{
		{Data: json.RawMessage(strings.Repeat("a", eventBatchBytes/2))},
		{Err: errors.New(strings.Repeat("e", eventBatchBytes/2))},
		{Data: json.RawMessage("b")},
		{Data: json.RawMessage(strings.Repeat("c", eventBatchBytes+1))},
		{Data: json.RawMessage("tail")},
	}
	// The first cached prefix is a singleton. Everything following it is
	// queued before a consumer asks for a batch, so byte boundaries are exact.
	c.incoming <- warmup
	for _, e := range want {
		c.incoming <- e
	}
	close(c.incoming)
	if first, ok := readEventBatch(t, c); !ok || !reflect.DeepEqual(first, []Event{warmup}) {
		t.Fatal("warmup event changed")
	}
	var saved [][]Event
	at := 0
	for _, count := range []int{2, 1, 1, 1} {
		batch, ok := readEventBatch(t, c)
		if !ok || len(batch) != count || !reflect.DeepEqual(batch, want[at:at+count]) {
			t.Fatalf("byte boundary at event %d: length=%d, want %d", at, len(batch), count)
		}
		if len(batch[0].Data) > 0 && &batch[0].Data[0] != &want[at].Data[0] {
			t.Fatal("batch copied its payload instead of only its headers")
		}
		saved = append(saved, batch)
		at += count
	}
	if _, ok := readEventBatch(t, c); ok {
		t.Fatal("stream did not close after the tail")
	}
	var retained []Event
	for _, batch := range saved {
		retained = append(retained, batch...)
	}
	if !reflect.DeepEqual(retained, want) {
		t.Fatal("subsequent queue removals mutated a previously delivered batch")
	}
}

func TestEventBatchesDoesNotWaitToFill(t *testing.T) {
	c := eventLoopFixture(t, true)
	for i := 0; i < 2; i++ {
		e := Event{Data: json.RawMessage(fmt.Sprint(i))}
		c.incoming <- e
		batch, ok := readEventBatch(t, c)
		if !ok || !reflect.DeepEqual(batch, []Event{e}) {
			t.Fatal("partial batch was not delivered while the input stayed open")
		}
		select {
		case <-c.EventBatches():
			t.Fatal("empty queue delivered a batch or closed early")
		default:
		}
	}
}

func TestEventBatchesAndEventsUseSameQueue(t *testing.T) {
	c := eventLoopFixture(t, true)
	for i := 0; i < 4; i++ {
		c.incoming <- Event{Data: json.RawMessage(fmt.Sprint(i))}
	}
	close(c.incoming)
	// Consumers should choose one API. Alternating here specifically verifies
	// that a single-event read invalidates the cached batch's queue prefix.
	if e := <-c.Events(); string(e.Data) != "0" {
		t.Fatalf("first event = %s", e.Data)
	}
	batch, ok := readEventBatch(t, c)
	if !ok || len(batch) != 3 {
		t.Fatalf("remaining batch length=%d", len(batch))
	}
	for i, e := range batch {
		if string(e.Data) != fmt.Sprint(i+1) {
			t.Fatalf("batch repeated or skipped event %d: %s", i+1, e.Data)
		}
	}
	if _, ok := readEventBatch(t, c); ok {
		t.Fatal("batch stream did not close")
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("single-event stream did not close")
	}
}

func TestEventLoopLegacyClientWithoutBatchChannel(t *testing.T) {
	c := eventLoopFixture(t, false)
	c.incoming <- Event{Data: json.RawMessage("legacy")}
	close(c.incoming)
	if e := <-c.Events(); string(e.Data) != "legacy" {
		t.Fatal("legacy event delivery changed")
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("legacy event channel did not close")
	}
}

func TestEventBatchesStopClosesBothOutputs(t *testing.T) {
	c := eventLoopFixture(t, true)
	c.incoming <- Event{Data: json.RawMessage("queued")}
	failure := errors.New("stopped with queued events")
	c.setFailure(failure)
	close(c.stop)
	// A ready receiver can race stop for the already accepted event, just as
	// with Events. Either way closure and the retained terminal error follow.
	for {
		if _, ok := readEventBatch(t, c); !ok {
			break
		}
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("stop left the single-event output open")
	}
	if !errors.Is(c.Err(), failure) {
		t.Fatal("stop discarded the terminal diagnosis")
	}
}
