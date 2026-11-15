package runtime

import (
	"math"
	"time"
)

// WorkingSnapshot is the local run's presentation value.
type WorkingSnapshot struct {
	Active  bool      `json:"active"`
	Present bool      `json:"present"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	Turn    int       `json:"turn"`
	Input   float64   `json:"input"`
	Output  float64   `json:"output"`
}

// View includes the latest main-turn context, cumulative output, current live
// estimate and optional capability usage. It does not expose replay ledgers.
func (w *WorkingState) View() WorkingSnapshot {
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

// Dismiss clears completed local statistics.
func (w *WorkingState) Dismiss() { w.Reset() }
