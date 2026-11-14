package runtime

import (
	"strings"
	"testing"
)

func TestEventBacklogFIFOAndBudgetRelease(t *testing.T) {
	var q eventBacklog[int]
	next := 0
	for round := 0; round < 30; round++ {
		for i := 0; i < 300; i++ {
			if !q.accepts(i) {
				t.Fatal("small burst exceeded budget")
			}
			q.append(next, i)
			next++
		}
		// Leave a changing prefix to exercise wraparound and growth.
		for i := 0; i < 200; i++ {
			want := round*200 + i
			if got := q.first(); got != want {
				t.Fatalf("FIFO: got %d, want %d", got, want)
			}
			q.remove()
		}
	}
	for want := 6000; want < next; want++ {
		if got := q.first(); got != want {
			t.Fatalf("tail: got %d, want %d", got, want)
		}
		q.remove()
	}
	if q.count != 0 || q.bytes != 0 || q.items != nil {
		t.Fatalf("drained queue retained memory: %+v", q)
	}
}

func TestEventBacklogByteAndFrameBoundaries(t *testing.T) {
	var q eventBacklog[int]
	exact := sharedBacklogBytes - backlogEventOverhead
	if !q.accepts(exact) {
		t.Fatal("exact budget rejected")
	}
	q.append(1, exact)
	if q.bytes != sharedBacklogBytes || q.accepts(0) {
		t.Fatal("full byte budget accepted another event")
	}
	q.remove()
	for _, size := range []int{-1, sharedFrameLimit, sharedFrameLimit + 1} {
		if q.accepts(size) {
			t.Fatalf("invalid frame size %d accepted", size)
		}
	}
	large := sharedFrameLimit - 1
	if !q.accepts(large) {
		t.Fatal("legal single large frame rejected")
	}
	q.append(2, large)
	if q.accepts(0) {
		t.Fatal("large singleton accepted a follower before draining")
	}
	q.remove()
	if !q.accepts(1) {
		t.Fatal("large frame did not release its budget")
	}
}

func TestEventBacklogRetainedSlotsStayCharged(t *testing.T) {
	var q eventBacklog[int]
	for i := 0; i < 256; i++ {
		q.append(i, 0)
	}
	for i := 0; i < 255; i++ {
		q.remove()
	}
	reserved := 256 * backlogEventOverhead
	if q.bytes != reserved || q.count != 1 {
		t.Fatalf("retained slots lost their memory reservation: count=%d bytes=%d", q.count, q.bytes)
	}
	if !q.accepts(sharedBacklogBytes-reserved) || q.accepts(sharedBacklogBytes-reserved+1) {
		t.Fatal("payload admission ignored retained slot capacity")
	}
	q.append(256, sharedBacklogBytes-reserved)
	if q.bytes != sharedBacklogBytes {
		t.Fatal("payload plus reserved slots exceeded budget")
	}
}

func TestEventBacklogMetadataIsBounded(t *testing.T) {
	var q eventBacklog[int]
	for i := 0; i < sharedBacklogBytes/backlogEventOverhead; i++ {
		if !q.accepts(0) {
			t.Fatal("metadata budget exhausted early")
		}
		q.append(i, 0)
	}
	if q.accepts(0) || q.bytes != sharedBacklogBytes {
		t.Fatal("empty frames bypassed byte budget")
	}
	for _, source := range []string{"client", "broker"} {
		err := q.overflow(source, 1).Error()
		for _, part := range []string{"shared event backlog exceeded", source, "pending=65536", "bytes=16777216", "budget=16777216", "incoming=1", "frame_limit="} {
			if !strings.Contains(err, part) {
				t.Fatalf("diagnostic %q missing %q", err, part)
			}
		}
	}
}
