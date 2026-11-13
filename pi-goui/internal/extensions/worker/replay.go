package worker

import (
	"encoding/json"

	"pi-goui/internal/session"
)

// Replay owns the recording protocol and its session projection. Its zero
// value is ready to read; callers only schedule reads and render View().
type Replay struct {
	state             session.State
	inheritedMessages int
	// Status and Stopped are read-only results for presentation and polling.
	Status  string
	Stopped bool
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

// Apply replays one read in order. It reports whether the conversation view
// may have changed, including activity changes caused by an empty/error read.
func (r *Replay) Apply(read ReplayRead) bool {
	wasBusy := r.state.Busy
	if read.Batch.Reset {
		*r = Replay{Status: "实时 · 只读"}
	}
	for _, raw := range eventBatch(read.Batch.Records) {
		var event struct {
			Type, Status, Error, Cwd string
			Complete                 bool
		}
		if json.Unmarshal(raw, &event) != nil {
			continue
		}
		switch event.Type {
		case "worker_transcript_header":
			r.state.LoadEntries(raw)
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
	if read.Descriptor.Incomplete {
		r.Status = "Incomplete recording · " + read.Descriptor.Error
		if !read.Batch.More {
			r.Stopped = true
			r.state.Busy = false
		}
	}
	if read.Err != nil {
		r.Status = "会话记录不可用: " + read.Err.Error()
		r.Stopped = true
		r.state.Busy = false
	}
	if !r.Stopped && read.TaskEnded && !read.Batch.More {
		r.Status = "Incomplete recording · task ended without a terminal record"
		r.Stopped = true
		r.state.Busy = false
	}
	if r.Stopped && read.Partial {
		r.Status += " · incomplete trailing record"
	}
	return read.Batch.Reset || len(read.Batch.Records) > 0 || wasBusy != r.state.Busy
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
