package worker

import (
	"encoding/json"
	"fmt"

	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/sessionview"
)

// Replay owns the recording protocol and its session projection. Its zero
// value is ready to read; callers only schedule reads and render View().
type Replay struct {
	initialized       bool
	projection        *sessionview.State
	state             session.State
	inheritedMessages int
	// Status and Stopped are read-only results for presentation and polling.
	Status  string
	Stopped bool
	// Err is sticky until a validated reset. A later end record cannot hide it.
	Err error
}

// ReplayRead contains facts from a read and the caller's polling policy.
// TaskEnded means the caller's grace period for a terminal record has elapsed.
type ReplayRead struct {
	Batch      Batch
	Descriptor Transcript
	Err        error
	Partial    bool
	TaskEnded  bool
}

// Detached is captured on the owning goroutine. Both the native state and its
// optional projection get independent mutable containers; replaying this copy
// never changes the currently displayed recording.
func (r *Replay) Detached() Replay {
	next := *r
	next.state = r.state.Detached()
	next.projection = r.projection.Clone()
	next.initialized = false
	return next
}

// Apply validates a read transactionally. A malformed replacement keeps the
// previous readable recording and cannot be hidden by a subsequent end record.
// Hosts run nonempty reads on a detached background owner, not the UI loop.
func (r *Replay) Apply(read ReplayRead) bool {
	if r.Err != nil && !read.Batch.Reset {
		return false
	}
	if read.Err != nil {
		r.Err = read.Err
		r.Status = "会话记录不可用: " + read.Err.Error()
		r.Stopped, r.state.Busy = true, false
		return true
	}
	wasBusy := r.state.Busy
	if !read.Batch.Reset && len(read.Batch.Records) == 0 {
		r.bindProjection()
		r.finish(read)
		return wasBusy != r.state.Busy
	}
	next := r.Detached()
	if read.Batch.Reset {
		next = Replay{Status: "实时 · 只读"}
	}
	if err := next.replay(read.Batch); err != nil {
		r.Err = err
		r.Status = "Incomplete recording · " + err.Error()
		r.Stopped, r.state.Busy = true, false
		return true
	}
	next.finish(read)
	*r = next
	return true
}

func (r *Replay) bindProjection() {
	if !r.initialized {
		if r.projection == nil {
			r.projection = &sessionview.State{}
		}
		r.state.SetExtensionProjection(r.projection)
		r.initialized = true
	}
}

func (r *Replay) replay(batch Batch) error {
	r.bindProjection()
	for _, raw := range eventBatch(batch.Records) {
		var event struct {
			Type, Status, Error, Cwd string
			Complete                 bool
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return fmt.Errorf("invalid Worker record: %w", err)
		}
		switch event.Type {
		case "worker_transcript_header":
			if err := r.state.LoadEntries(raw); err != nil {
				return fmt.Errorf("invalid Worker history: %w", err)
			}
			// The fork is projection context, not part of this task's conversation.
			r.inheritedMessages = r.state.MessageCount
			r.state.Cwd = event.Cwd
		case "worker_transcript_end":
			r.Status = event.Status
			r.Stopped = true
			r.state.Busy = false
			if !event.Complete || event.Error != "" {
				r.Status = "Incomplete recording · " + event.Status + " " + event.Error
			}
		default:
			r.state.Apply(raw)
		}
	}
	return nil
}

func (r *Replay) finish(read ReplayRead) {
	if read.Descriptor.Incomplete {
		r.Status = "Incomplete recording · " + read.Descriptor.Error
		if !read.Batch.More {
			r.Stopped = true
			r.state.Busy = false
		}
	}
	if !r.Stopped && read.TaskEnded && !read.Batch.More {
		r.Status = "Incomplete recording · task ended without a terminal record"
		r.Stopped = true
		r.state.Busy = false
	}
	if r.Stopped && read.Partial {
		r.Status += " · incomplete trailing record"
	}
}

// View is a read-only projection of the task's own conversation. The inherited
// branch remains in the replay state for message/tool attribution, but is never
// exposed as task output. As with session.State, callers must not mutate its items.
func (r *Replay) View() session.State {
	view := r.state
	view.Items = nil
	for _, item := range r.state.Items {
		// Live tools acquire their ordinal when the assistant snapshot arrives;
		// persisted tools in the inherited branch already have an ordinal.
		unattributed := item.MessageIndex == 0 && item.Role == "tool"
		if item.MessageIndex > r.inheritedMessages || unattributed {
			view.Items = append(view.Items, item)
		}
	}
	return view
}
