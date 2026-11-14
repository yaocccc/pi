package runtime

import (
	"bytes"
	"encoding/json"
	"math"
	"sync"
	"time"
)

// Shared readers reserve only this bounded framing allowance above the native
// JSONL limit, so annotating a valid near-limit native event cannot truncate it.
const workingFrameAllowance = 1024

// WorkingSnapshot is a constant-size projection, not a native event replay or
// persisted session state. Revision is monotonic within one executor connection.
// Started/Ended come from the executor clock, never a subscribing UI's clock.
type WorkingSnapshot struct {
	Revision uint64    `json:"revision"`
	Active   bool      `json:"active"`
	Present  bool      `json:"present"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`
	Turn     int       `json:"turn"`
	Input    float64   `json:"input"`
	Output   float64   `json:"output"`
}

// View includes the latest main-turn context, cumulative output, current live
// estimate and optional capability usage. It does not expose replay ledgers.
func (w *WorkingState) View() WorkingSnapshot {
	if w.shared != nil {
		v := *w.shared
		v.Active, v.Present = w.Active, w.Present
		return v
	}
	input, output := w.Total.Input, w.Total.Output
	if w.Current {
		if w.Live.Input > 0 {
			input = w.Live.Input
		}
		output += math.Max(w.Live.Output, w.estimate())
	}
	if w.Extensions != nil {
		i, o := w.Extensions.Totals()
		input += i
		output += o
	}
	return WorkingSnapshot{Active: w.Active, Present: w.Present, Started: w.Started, Ended: w.Ended, Turn: w.Turn, Input: input, Output: output}
}

// ApplyShared consumes an absolute executor projection. Once shared, native
// events are not accumulated again. Older/duplicate snapshots cannot rewind
// progress, and local dismissal stays dismissed until a genuinely new run.
func (w *WorkingState) ApplyShared(raw json.RawMessage) bool {
	var envelope struct {
		Working *WorkingSnapshot `json:"gouiWorking"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Working == nil {
		return false
	}
	v := envelope.Working
	if (w.shared != nil && v.Revision <= w.sharedRevision) || v.Turn < 0 || v.Input < 0 || v.Output < 0 || (v.Present && v.Started.IsZero()) {
		return true
	}
	w.sharedRevision = v.Revision
	if !w.dismissedStart.IsZero() && v.Started.Equal(w.dismissedStart) {
		v.Present = false
	} else {
		w.dismissedStart = time.Time{}
	}
	w.shared = v
	w.Active, w.Present = v.Active, v.Present
	w.Started, w.Ended, w.Turn = v.Started, v.Ended, v.Turn
	return true
}

// Dismiss clears a completed presentation without resurrecting it on the next
// shared status event. It is distinct from Reset, which begins a fresh run.
func (w *WorkingState) Dismiss() {
	if w.shared == nil {
		w.Reset()
		return
	}
	w.dismissedStart = w.shared.Started
	v := *w.shared
	v.Present, w.Present = false, false
	w.shared = &v
}

var sharedWorkingUsage struct {
	sync.RWMutex
	factory func() WorkingUsageObserver
}

// ConfigureSharedWorkingUsage installs the application's fixed capability
// adapter during startup, before shared helpers run. Runtime itself knows no
// third-party protocols. Each executor/run receives a fresh observer instance.
func ConfigureSharedWorkingUsage(factory func() WorkingUsageObserver) {
	sharedWorkingUsage.Lock()
	sharedWorkingUsage.factory = factory
	sharedWorkingUsage.Unlock()
}

func newSharedWorkingState() WorkingState {
	sharedWorkingUsage.RLock()
	factory := sharedWorkingUsage.factory
	sharedWorkingUsage.RUnlock()
	return WorkingState{NewExtensions: factory}
}

// withWorkingSnapshot adds a compact projection to an already validated native
// JSON object, without decoding/re-encoding its potentially large message body.
func withWorkingSnapshot(raw json.RawMessage, snapshot WorkingSnapshot) json.RawMessage {
	view, err := json.Marshal(snapshot)
	if err != nil {
		return raw
	}
	raw = bytes.TrimSpace(raw)
	end := len(raw)
	if end < 2 || raw[0] != '{' || raw[end-1] != '}' {
		return raw
	}
	out := make([]byte, 0, end+len(view)+16)
	out = append(out, raw[:end-1]...)
	if len(bytes.TrimSpace(raw[1:end-1])) != 0 {
		out = append(out, ',')
	}
	out = append(out, `"gouiWorking":`...)
	out = append(out, view...)
	return append(out, '}')
}
