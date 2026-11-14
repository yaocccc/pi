package runtime

import "fmt"

const (
	sharedBacklogBytes = 16 * 1024 * 1024
	eventBatchLimit    = 64
	eventBatchBytes    = 256 * 1024
	// Covers queue slots, retained slice/string headers and growth overhead even
	// for empty events; tiny frames cannot bypass the memory bound by count.
	backlogEventOverhead = 256
	nativeFrameLimit     = 64 * 1024 * 1024
	sharedFrameLimit     = nativeFrameLimit + workingFrameAllowance
)

type backlogItem[T any] struct {
	value T
	bytes int
}

// eventBacklog is an owner-synchronized FIFO. bytes includes payloads and a
// reservation for every allocated slot, even when a burst has partially drained.
// One legal large frame may occupy an otherwise empty backlog; until it drains,
// no other event is accepted. Transport buffers/in-flight writes are separate.
// append is deliberately unchecked for the existing direct-backend queue policy.
type eventBacklog[T any] struct {
	items              []backlogItem[T]
	head, count, bytes int
}

func (q *eventBacklog[T]) accepts(payloadBytes int) bool {
	if payloadBytes < 0 || payloadBytes >= sharedFrameLimit {
		return false
	}
	cost := payloadBytes + q.growth()*backlogEventOverhead
	return q.count == 0 || cost <= sharedBacklogBytes-q.bytes
}

func (q *eventBacklog[T]) growth() int {
	if q.count < len(q.items) {
		return 0
	}
	if len(q.items) == 0 {
		return 1
	}
	return len(q.items)
}

func (q *eventBacklog[T]) append(value T, payloadBytes int) {
	growth := q.growth()
	if growth != 0 {
		capacity := len(q.items) + growth
		items := make([]backlogItem[T], capacity)
		for i := 0; i < q.count; i++ {
			items[i] = q.items[(q.head+i)%len(q.items)]
		}
		q.items, q.head = items, 0
	}
	q.items[(q.head+q.count)%len(q.items)] = backlogItem[T]{value: value, bytes: payloadBytes}
	q.count++
	q.bytes += payloadBytes + growth*backlogEventOverhead
}

func (q *eventBacklog[T]) first() T { return q.items[q.head].value }

// batch snapshots only headers, leaving the payloads in the charged queue until
// delivery. The caller may cache this prefix across appends, but must discard it
// after removing any item. The returned slice does not alias the queue's slots.
func (q *eventBacklog[T]) batch(maxEvents, maxBytes int) []T {
	var bytes int
	batch := make([]T, 0, min(q.count, maxEvents))
	for i := 0; i < q.count && i < maxEvents; i++ {
		item := q.items[(q.head+i)%len(q.items)]
		if i > 0 && item.bytes > maxBytes-bytes {
			break
		}
		batch = append(batch, item.value)
		bytes += item.bytes
	}
	return batch
}

func (q *eventBacklog[T]) remove() {
	q.bytes -= q.items[q.head].bytes
	q.items[q.head] = backlogItem[T]{}
	q.count--
	if q.count == 0 {
		q.items, q.head, q.bytes = nil, 0, 0
	} else {
		q.head = (q.head + 1) % len(q.items)
	}
}

func (q *eventBacklog[T]) overflow(source string, payloadBytes int) error {
	return fmt.Errorf("shared event backlog exceeded (%s: pending=%d bytes=%d budget=%d incoming=%d frame_limit=%d); reconnect and rebuild session state", source, q.count, q.bytes, sharedBacklogBytes, payloadBytes, sharedFrameLimit-1)
}
