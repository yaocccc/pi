package runtime

import (
	"testing"
)

func TestEventBacklogFIFOAndBudgetRelease(t *testing.T) {
	var q eventBacklog[int]
	next := 0
	for round := 0; round < 30; round++ {
		for i := 0; i < 300; i++ {
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
