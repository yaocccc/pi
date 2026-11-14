package web

import (
	"bytes"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
)

func historyReplay(t *testing.T, term *terminal) []frame {
	t.Helper()
	p := newPeer(nil)
	term.attach(p)
	defer term.detach(p)
	var frames []frame
	for len(p.output) > 0 {
		frames = append(frames, <-p.output)
	}
	select {
	case <-p.stopped:
		t.Fatal("bounded history exceeded replay frame queue")
	default:
	}
	return frames
}

func replayBytes(frames []frame) []byte {
	var data []byte
	for _, f := range frames {
		if f.kind == websocket.MessageBinary {
			data = append(data, f.data...)
		}
	}
	return data
}

func TestHistoryWindowContinuousReplay(t *testing.T) {
	term := &terminal{cols: 120, rows: 40, historySizes: []historySize{{0, 120, 40}}}
	var stream []byte
	absoluteSizes := []historySize{{0, 120, 40}}
	var snapshots [][]frame
	var snapshotBytes [][]byte
	const chunkSize = 16 << 10
	for i := 0; i < 512; i++ { // Eight MiB, crossing the logical bound repeatedly.
		if i > 0 && i%16 == 0 {
			term.cols, term.rows = 40+i%120, 25+i%30
			term.historySizes = append(term.historySizes, historySize{len(term.history), term.cols, term.rows})
			absoluteSizes = append(absoluteSizes, historySize{len(stream), term.cols, term.rows})
			if len(term.historySizes) > 16 {
				term.trimHistory(term.historySizes[1].at)
			}
		}
		chunk := bytes.Repeat([]byte{byte('a' + i%26)}, chunkSize)
		stream = append(stream, chunk...)
		term.appendHistory(chunk)
		if len(term.history) > historyLimit || cap(term.history) > 2*historyLimit {
			t.Fatalf("unbounded history: len=%d cap=%d", len(term.history), cap(term.history))
		}
		if i%64 != 63 {
			continue
		}
		start := len(stream) - len(term.history)
		want := stream[start:]
		if !bytes.Equal(term.history, want) {
			t.Fatal("history lost or duplicated bytes at a window boundary")
		}
		var sizes []historySize
		for _, size := range absoluteSizes {
			size.at -= start
			if size.at <= 0 {
				size.at = 0
				sizes = []historySize{size}
			} else {
				sizes = append(sizes, size)
			}
		}
		if len(term.historySizes) > 16 || !slices.Equal(term.historySizes, sizes) {
			t.Fatalf("geometry changed: got=%v want=%v", term.historySizes, sizes)
		}
		frames := historyReplay(t, term)
		replayed := replayBytes(frames)
		if len(replayed) < len(want) || !bytes.Equal(replayed[:len(want)], want) {
			t.Fatal("replay did not preserve the exact bounded byte suffix")
		}
		// Every binary segment must follow the geometry active at that byte.
		var cols, rows, at, sizeIndex int
		for _, f := range frames {
			if f.kind == websocket.MessageText {
				var size struct{ Cols, Rows int }
				if err := json.Unmarshal(f.data, &size); err != nil {
					t.Fatal(err)
				}
				cols, rows = size.Cols, size.Rows
				continue
			}
			for sizeIndex+1 < len(sizes) && sizes[sizeIndex+1].at <= at {
				sizeIndex++
			}
			if cols != sizes[sizeIndex].cols || rows != sizes[sizeIndex].rows {
				t.Fatal("replayed bytes used the wrong historical geometry")
			}
			at += len(f.data)
		}
		snapshots = append(snapshots, frames)
		snapshotBytes = append(snapshotBytes, replayed)
	}
	// Keep actual queued slices alive: later window reuse must not mutate them.
	for i, frames := range snapshots {
		if !bytes.Equal(replayBytes(frames), snapshotBytes[i]) {
			t.Fatal("later output mutated an earlier replay snapshot")
		}
	}
	if !term.truncated {
		t.Fatal("overflow did not mark replay truncated")
	}
}

func TestHistoryWindowAmortizesCopies(t *testing.T) {
	term := &terminal{}
	chunk := bytes.Repeat([]byte("x"), 16<<10)
	term.appendHistory(bytes.Repeat([]byte("x"), historyLimit))
	compactions := 0
	var previous, allocation *byte
	for i := 0; i < 1024; i++ { // Sixteen MiB after reaching the four-MiB limit.
		before := &term.history[0]
		capacity := cap(term.history)
		term.appendHistory(chunk)
		// A capacity exhaustion compacts the window, at most once/window.
		if capacity < historyLimit+len(chunk) {
			compactions++
		}
		store := &term.historyStore[:1][0]
		if allocation != nil && store != allocation {
			t.Fatal("steady-state output allocated another history buffer")
		}
		allocation = store
		if previous == before {
			t.Fatal("full history was shifted in place instead of advancing")
		}
		previous = before
		if len(term.history) != historyLimit || cap(term.historyStore) > 2*historyLimit {
			t.Fatal("history storage bound changed")
		}
	}
	if compactions > 5 {
		t.Fatalf("compacted full history %d times for only four windows of output", compactions)
	}
	// An oversized read retains only its suffix and drops any spare window.
	large := bytes.Repeat([]byte("z"), historyLimit+32)
	term.appendHistory(large)
	if !bytes.Equal(term.history, large[32:]) || cap(term.history) > 2*historyLimit {
		t.Fatal("oversized read violated suffix or storage bounds")
	}
	large[32] = 'y'
	if term.history[0] != 'z' {
		t.Fatal("history retained the mutable input buffer")
	}
}

func TestHistoryWindowReplayPrecedesLiveOutput(t *testing.T) {
	term := &terminal{cols: 80, rows: 24, parser: ansi.NewParser()}
	term.appendHistory(bytes.Repeat([]byte("x"), historyLimit+3))
	p := newPeer(nil)
	term.attach(p)
	defer term.detach(p)
	term.record([]byte("live-one"))
	term.record([]byte("live-two"))
	var binary [][]byte
	for len(p.output) > 0 {
		f := <-p.output
		if f.kind == websocket.MessageBinary {
			binary = append(binary, f.data)
		}
	}
	if len(binary) != 3 || len(binary[0]) < historyLimit ||
		!bytes.Equal(binary[0][:historyLimit], bytes.Repeat([]byte("x"), historyLimit)) ||
		string(binary[1]) != "live-one" || string(binary[2]) != "live-two" {
		t.Fatal("live output interleaved with, overwrote or skipped replay bytes")
	}
}

func TestHistoryWindowConcurrentSnapshots(t *testing.T) {
	term := &terminal{cols: 80, rows: 24, historySizes: []historySize{{0, 80, 24}}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		chunk := bytes.Repeat([]byte("x"), 16<<10)
		for i := 0; i < 1024; i++ {
			term.mu.Lock()
			term.appendHistory(chunk)
			term.mu.Unlock()
		}
	}()
	var snapshots [][]frame
	var expected [][]byte
	for i := 0; i < 24; i++ {
		frames := historyReplay(t, term)
		snapshots = append(snapshots, frames)
		expected = append(expected, replayBytes(frames))
	}
	wg.Wait()
	for i := range snapshots {
		if !bytes.Equal(replayBytes(snapshots[i]), expected[i]) {
			t.Fatal("concurrent output changed a queued replay snapshot")
		}
	}
}

func BenchmarkTerminalHistoryAppend(b *testing.B) {
	const outputBytes = 32 << 20
	chunk := bytes.Repeat([]byte("x"), 16<<10)
	for _, window := range []bool{false, true} {
		name := "legacy_full_copy"
		if window {
			name = "bounded_window"
		}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(outputBytes)
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				store := make([]byte, historyLimit, 2*historyLimit)
				term := &terminal{history: store, historyStore: store[:0]}
				for written := 0; written < outputBytes; written += len(chunk) {
					if window {
						term.appendHistory(chunk)
					} else {
						// Previous implementation; omit geometry bookkeeping in
						// both cases to isolate the repeated whole-window copy.
						overflow := len(term.history) + len(chunk) - historyLimit
						copy(term.history, term.history[overflow:])
						term.history = term.history[:len(term.history)-overflow]
						term.history = append(term.history, chunk...)
					}
				}
				if len(term.history) != historyLimit {
					b.Fatal("history bound changed")
				}
			}
		})
	}
}
