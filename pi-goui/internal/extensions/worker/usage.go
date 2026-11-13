package worker

import (
	"encoding/json"
	"fmt"
	"math"
)

// UsageTracker projects cumulative Worker snapshots for the host status line.
// It never modifies execution accounting or persisted task results.
type Usage struct{ Input, Output float64 }
type UsageTracker map[string]Usage

func nonnegative(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}

// Worker usage snapshots are cumulative. Key by batch and task, not the
// invocation ID: continuation calls and toolResult messages repeat snapshots.
func (w *UsageTracker) Apply(raw json.RawMessage, toolCallID string) {
	var result struct {
		ToolCallID string
		Details    struct {
			Kind, BatchID, OriginToolCallID string
			Tasks                           []struct {
				Index int
				Usage struct{ Input, Output, CacheRead, CacheWrite float64 }
			}
		}
	}
	if json.Unmarshal(raw, &result) != nil || result.Details.Kind != "worker-ui" {
		return
	}
	d := result.Details
	id := d.BatchID
	if id == "" {
		id = d.OriginToolCallID
	}
	if id == "" {
		id = result.ToolCallID
	}
	if id == "" {
		id = toolCallID
	}
	if id == "" {
		return
	}
	if (*w) == nil {
		(*w) = make(map[string]Usage)
	}
	for _, task := range d.Tasks {
		key := fmt.Sprintf("%s:%d", id, task.Index)
		old := (*w)[key]
		u := task.Usage
		// Ignore duplicate or stale snapshots, including late progress updates.
		(*w)[key] = Usage{
			Input:  math.Max(old.Input, nonnegative(u.Input)),
			Output: math.Max(old.Output, nonnegative(u.Output)),
		}
	}
}
