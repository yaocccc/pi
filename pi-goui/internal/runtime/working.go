package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"strings"
	"time"
)

const workingDedupLimit = 4096

type workingUsage struct{ Input, Output float64 }
type workingFinished struct {
	usage    workingUsage
	identity [32]byte
}
type workingPart struct {
	kind                            string
	text, thinking, arguments, name int
}
type workingMessage struct {
	Role      string
	ID        string
	Timestamp json.RawMessage
	Content   json.RawMessage
	Usage     *struct{ Input, Output, CacheRead, CacheWrite float64 }
}

// WorkingUsageObserver allows the application composition layer to supply
// optional capability usage without introducing runtime-to-extension imports.
type WorkingUsageObserver interface {
	Observe(json.RawMessage)
	Totals() (float64, float64)
}

// WorkingState is an event-derived running-usage accumulator with a bounded
// main-message ledger. Its owner serializes Apply/Reset/View; it has no timers,
// IO or presentation dependencies. Shared executors own the authoritative
// instance; standalone clients use the same algorithm locally.
type WorkingState struct {
	shared                                *WorkingSnapshot
	sharedRevision                        uint64
	dismissedStart                        time.Time
	Active, Present, Current, WaitingTurn bool
	Started, Ended                        time.Time
	Turn                                  int
	CurrentTimestamp                      string
	Parts                                 map[int]workingPart
	Live                                  workingUsage
	Total                                 workingUsage
	Finished                              map[[32]byte]workingFinished
	Order                                 [][32]byte
	Extensions                            WorkingUsageObserver
	NewExtensions                         func() WorkingUsageObserver
}

func (w *WorkingState) Reset() {
	factory := w.NewExtensions
	*w = WorkingState{NewExtensions: factory}
	if factory != nil {
		w.Extensions = factory()
	}
}

func workingNonnegative(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}
func workingChars(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func workingJSONChars(raw json.RawMessage) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return workingChars(string(raw))
	}
	// Encoding without HTML escapes closely follows JSON.stringify's size.
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return workingChars(strings.TrimSuffix(b.String(), "\n"))
}
func workingDecode(raw json.RawMessage) (workingMessage, bool) {
	var m workingMessage
	err := json.Unmarshal(raw, &m)
	return m, err == nil && m.Role == "assistant"
}
func (w *WorkingState) begin() {
	if w.Current {
		return
	}
	if !w.WaitingTurn {
		w.Turn++
	}
	if w.Turn < 1 {
		w.Turn = 1
	}
	w.WaitingTurn = false
	w.Current = true
	w.CurrentTimestamp = ""
	w.Parts = make(map[int]workingPart)
	w.Live = workingUsage{}
}
func (w *WorkingState) estimate() float64 {
	chars := 0
	for _, p := range w.Parts {
		switch p.kind {
		case "text":
			chars += p.text
		case "thinking":
			chars += p.thinking
		case "toolCall":
			chars += p.name + p.arguments
		}
	}
	return math.Ceil(float64(chars) / 4)
}
func (w *WorkingState) snapshot(m workingMessage) {
	if w.wasFinished(m) {
		return
	}
	w.begin()
	w.CurrentTimestamp = string(m.Timestamp)
	var content []struct {
		Type, Text, Thinking, Name string
		Arguments                  json.RawMessage
	}
	if json.Unmarshal(m.Content, &content) == nil {
		w.Parts = make(map[int]workingPart, len(content))
		for i, c := range content {
			if i >= 4096 {
				break
			}
			p := workingPart{kind: c.Type, text: workingChars(c.Text), thinking: workingChars(c.Thinking), name: workingChars(c.Name)}
			if c.Type == "toolCall" {
				p.arguments = workingJSONChars(c.Arguments)
			}
			w.Parts[i] = p
		}
	}
	if m.Usage != nil {
		w.Live.Input = workingNonnegative(m.Usage.Input) + workingNonnegative(m.Usage.CacheRead) + workingNonnegative(m.Usage.CacheWrite)
		w.Live.Output = workingNonnegative(m.Usage.Output)
	}
}
func workingIdentity(m workingMessage) [32]byte {
	data, _ := json.Marshal(struct {
		ID        string
		Timestamp json.RawMessage
	}{m.ID, m.Timestamp})
	return sha256.Sum256(data)
}

func (w *WorkingState) wasFinished(m workingMessage) bool {
	if len(w.Finished) == 0 {
		return false
	}
	// The existing bounded completion ledger can reject a different message
	// identity without parsing and hashing its entire growing body. Messages
	// with reused/absent timestamps still require the full content check.
	identity := workingIdentity(m)
	for _, previous := range w.Finished {
		if previous.identity == identity {
			_, done := w.Finished[workingFingerprint(m)]
			return done
		}
	}
	return false
}

func workingFingerprint(m workingMessage) [32]byte {
	var content any
	_ = json.Unmarshal(m.Content, &content)
	data, _ := json.Marshal(struct {
		ID        string
		Timestamp json.RawMessage
		Content   any
	}{m.ID, m.Timestamp, content})
	return sha256.Sum256(data)
}
func (w *WorkingState) finish(m workingMessage) {
	key := workingFingerprint(m)
	previous, seen := w.Finished[key]
	old := previous.usage
	usage := old
	if m.Usage != nil {
		usage = workingUsage{workingNonnegative(m.Usage.Input) + workingNonnegative(m.Usage.CacheRead) + workingNonnegative(m.Usage.CacheWrite), workingNonnegative(m.Usage.Output)}
	} else if !seen {
		// A missing usage object is not an authoritative zero-token result.
		temp := WorkingState{}
		temp.snapshot(m)
		usage = workingUsage{w.Live.Input, math.Max(temp.estimate(), w.Live.Output)}
	}
	if !seen {
		if !w.Current {
			w.begin()
		}
		if w.Finished == nil {
			w.Finished = make(map[[32]byte]workingFinished)
		}
		if len(w.Order) == workingDedupLimit {
			delete(w.Finished, w.Order[0])
			copy(w.Order, w.Order[1:])
			w.Order = w.Order[:len(w.Order)-1]
		}
		w.Order = append(w.Order, key)
	}
	// Main input is the latest turn's context, not cumulative token usage.
	// Repeated finals for earlier turns must not replace the latest context.
	if !seen || (len(w.Order) > 0 && w.Order[len(w.Order)-1] == key) {
		w.Total.Input = usage.Input
	}
	w.Total.Output += usage.Output - old.Output
	w.Finished[key] = workingFinished{usage: usage, identity: workingIdentity(m)}
	// A duplicate final for an earlier turn must not clear the next live turn.
	if !seen && (w.CurrentTimestamp == "" || w.CurrentTimestamp == string(m.Timestamp)) {
		w.Current = false
		w.CurrentTimestamp = ""
		w.Parts = nil
		w.Live = workingUsage{}
	}
}

func (w *WorkingState) Apply(raw json.RawMessage, now time.Time) {
	if w.shared != nil {
		return
	}
	var event struct {
		Type                  string
		TurnIndex             *int
		ToolCallID            string
		PartialResult, Result json.RawMessage
		Message               json.RawMessage
		AssistantMessageEvent struct {
			Type                     string
			ContentIndex             int
			Delta, Content           string
			Message, Error, ToolCall json.RawMessage
		}
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	if event.Type == "agent_start" {
		if !w.Active {
			w.Reset()
			w.Active, w.Present, w.Started = true, true, now
		}
		return
	}
	if !w.Active {
		return
	}
	if w.Extensions != nil {
		w.Extensions.Observe(raw)
	}
	switch event.Type {
	case "agent_settled":
		w.Active = false
		w.Ended = now
	case "turn_start":
		if event.TurnIndex != nil && *event.TurnIndex >= 0 {
			if *event.TurnIndex+1 <= w.Turn {
				return
			}
			w.Turn = *event.TurnIndex + 1
		} else {
			w.Turn++
		}
		w.Current = false
		w.Parts = nil
		w.Live = workingUsage{}
		w.WaitingTurn = true
	case "message_start":
		if m, ok := workingDecode(event.Message); ok {
			w.snapshot(m)
		}
	case "message_end":
		if m, ok := workingDecode(event.Message); ok {
			w.finish(m)
		}
	case "message_update":
		if m, ok := workingDecode(event.Message); ok {
			w.snapshot(m)
			return
		}
		u := event.AssistantMessageEvent
		if u.Type == "done" || u.Type == "error" {
			data := u.Message
			if u.Type == "error" {
				data = u.Error
			}
			if m, ok := workingDecode(data); ok {
				w.finish(m)
			}
			return
		}
		if u.ContentIndex < 0 || u.ContentIndex >= 4096 {
			return
		}
		switch u.Type {
		case "text_start", "text_delta", "text_end", "thinking_start", "thinking_delta", "thinking_end", "toolcall_start", "toolcall_delta", "toolcall_end":
		default:
			return
		}
		w.begin()
		p := w.Parts[u.ContentIndex]
		switch u.Type {
		case "text_start":
			p = workingPart{kind: "text"}
		case "text_delta":
			p.kind = "text"
			p.text += workingChars(u.Delta)
		case "text_end":
			p.kind = "text"
			p.text = workingChars(u.Content)
		case "thinking_start":
			p = workingPart{kind: "thinking"}
		case "thinking_delta":
			p.kind = "thinking"
			p.thinking += workingChars(u.Delta)
		case "thinking_end":
			p.kind = "thinking"
			p.thinking = workingChars(u.Content)
		case "toolcall_start":
			p = workingPart{kind: "toolCall"}
		case "toolcall_delta":
			p.kind = "toolCall"
			p.arguments += workingChars(u.Delta)
		case "toolcall_end":
			var call struct {
				Name      string
				Arguments json.RawMessage
			}
			if json.Unmarshal(u.ToolCall, &call) == nil {
				p = workingPart{kind: "toolCall", name: workingChars(call.Name), arguments: workingJSONChars(call.Arguments)}
			}
		}
		w.Parts[u.ContentIndex] = p
	}
}
