package ui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"pi-goui/internal/core/config"
)

const workingDedupLimit = 4096

type workingUsage struct{ input, output float64 }
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

// workingState belongs to one tab and is mutated only by the UI event loop.
// It has no timer or IO. line uses the caller's clock and freezes at settled.
type workingState struct {
	active, present, current, waitingTurn bool
	started, ended                        time.Time
	turn                                  int
	currentTimestamp                      string
	parts                                 map[int]workingPart
	live                                  workingUsage
	total                                 workingUsage
	finished                              map[[32]byte]workingFinished
	order                                 [][32]byte
	extensions                            extensionUsage
	newExtensions                         func() extensionUsage
}

func (w *workingState) reset() {
	factory := w.newExtensions
	*w = workingState{newExtensions: factory}
	if factory != nil {
		w.extensions = factory()
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
func (w *workingState) begin() {
	if w.current {
		return
	}
	if !w.waitingTurn {
		w.turn++
	}
	if w.turn < 1 {
		w.turn = 1
	}
	w.waitingTurn = false
	w.current = true
	w.currentTimestamp = ""
	w.parts = make(map[int]workingPart)
	w.live = workingUsage{}
}
func (w *workingState) estimate() float64 {
	chars := 0
	for _, p := range w.parts {
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
func (w *workingState) snapshot(m workingMessage) {
	if w.wasFinished(m) {
		return
	}
	w.begin()
	w.currentTimestamp = string(m.Timestamp)
	var content []struct {
		Type, Text, Thinking, Name string
		Arguments                  json.RawMessage
	}
	if json.Unmarshal(m.Content, &content) == nil {
		w.parts = make(map[int]workingPart, len(content))
		for i, c := range content {
			if i >= 4096 {
				break
			}
			p := workingPart{kind: c.Type, text: workingChars(c.Text), thinking: workingChars(c.Thinking), name: workingChars(c.Name)}
			if c.Type == "toolCall" {
				p.arguments = workingJSONChars(c.Arguments)
			}
			w.parts[i] = p
		}
	}
	if m.Usage != nil {
		w.live.input = workingNonnegative(m.Usage.Input) + workingNonnegative(m.Usage.CacheRead) + workingNonnegative(m.Usage.CacheWrite)
		w.live.output = workingNonnegative(m.Usage.Output)
	}
}
func workingIdentity(m workingMessage) [32]byte {
	data, _ := json.Marshal(struct {
		ID        string
		Timestamp json.RawMessage
	}{m.ID, m.Timestamp})
	return sha256.Sum256(data)
}

func (w *workingState) wasFinished(m workingMessage) bool {
	if len(w.finished) == 0 {
		return false
	}
	// The existing bounded completion ledger can reject a different message
	// identity without parsing and hashing its entire growing body. Messages
	// with reused/absent timestamps still require the full content check.
	identity := workingIdentity(m)
	for _, previous := range w.finished {
		if previous.identity == identity {
			_, done := w.finished[workingFingerprint(m)]
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
func (w *workingState) finish(m workingMessage) {
	key := workingFingerprint(m)
	previous, seen := w.finished[key]
	old := previous.usage
	usage := old
	if m.Usage != nil {
		usage = workingUsage{workingNonnegative(m.Usage.Input) + workingNonnegative(m.Usage.CacheRead) + workingNonnegative(m.Usage.CacheWrite), workingNonnegative(m.Usage.Output)}
	} else if !seen {
		// A missing usage object is not an authoritative zero-token result.
		temp := workingState{}
		temp.snapshot(m)
		usage = workingUsage{w.live.input, math.Max(temp.estimate(), w.live.output)}
	}
	if !seen {
		if !w.current {
			w.begin()
		}
		if w.finished == nil {
			w.finished = make(map[[32]byte]workingFinished)
		}
		if len(w.order) == workingDedupLimit {
			delete(w.finished, w.order[0])
			copy(w.order, w.order[1:])
			w.order = w.order[:len(w.order)-1]
		}
		w.order = append(w.order, key)
	}
	// Main input is the latest turn's context, not cumulative token usage.
	// Repeated finals for earlier turns must not replace the latest context.
	if !seen || (len(w.order) > 0 && w.order[len(w.order)-1] == key) {
		w.total.input = usage.input
	}
	w.total.output += usage.output - old.output
	w.finished[key] = workingFinished{usage: usage, identity: workingIdentity(m)}
	// A duplicate final for an earlier turn must not clear the next live turn.
	if !seen && (w.currentTimestamp == "" || w.currentTimestamp == string(m.Timestamp)) {
		w.current = false
		w.currentTimestamp = ""
		w.parts = nil
		w.live = workingUsage{}
	}
}

func (w *workingState) apply(raw json.RawMessage, now time.Time) {
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
		if !w.active {
			w.reset()
			w.active, w.present, w.started = true, true, now
		}
		return
	}
	if !w.active {
		return
	}
	w.extensions.Observe(raw)
	switch event.Type {
	case "agent_settled":
		w.active = false
		w.ended = now
	case "turn_start":
		if event.TurnIndex != nil && *event.TurnIndex >= 0 {
			if *event.TurnIndex+1 <= w.turn {
				return
			}
			w.turn = *event.TurnIndex + 1
		} else {
			w.turn++
		}
		w.current = false
		w.parts = nil
		w.live = workingUsage{}
		w.waitingTurn = true
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
		p := w.parts[u.ContentIndex]
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
		w.parts[u.ContentIndex] = p
	}
}
func workingTokens(value float64) string {
	value = workingNonnegative(value)
	if value >= 1e6 {
		return fmt.Sprintf("%.1fM", value/1e6)
	}
	if value >= 1000 {
		return fmt.Sprintf("%.0fk", value/1000)
	}
	return fmt.Sprintf("%.0f", value)
}
func (w *workingState) line(now time.Time, theme config.Theme) string {
	return w.formatLine(now, theme, true)
}

func (w *workingState) formatLine(now time.Time, theme config.Theme, detailed bool) string {
	if !w.present {
		return ""
	}
	end := now
	if !w.active {
		end = w.ended
	}
	elapsed := end.Sub(w.started)
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := int(elapsed / time.Second)
	duration := fmt.Sprintf("%ds", seconds)
	if seconds >= 60 {
		duration = fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
	}
	input := w.total.input
	// Keep the last known context while the next turn has no usage yet.
	if w.current && w.live.input > 0 {
		input = w.live.input
	}
	output := w.total.output
	extraInput, extraOutput := w.extensions.Totals()
	input += extraInput
	output += extraOutput
	if w.current {
		output += math.Max(w.live.output, w.estimate())
	}
	var parts []string
	if detailed {
		parts = append(parts, fmt.Sprintf("%d turns", max(1, w.turn)))
	}
	var usage []string
	if input > 0 {
		usage = append(usage, "↑"+workingTokens(input))
	}
	if output > 0 {
		usage = append(usage, "↓"+workingTokens(output))
	}
	if len(usage) > 0 {
		parts = append(parts, strings.Join(usage, " "))
	}
	if detailed && elapsed > 0 && output > 0 {
		parts = append(parts, fmt.Sprintf("%.1f TPS", output/elapsed.Seconds()))
	}
	parts = append(parts, duration)
	paint := func(s, c string) string {
		if c == "" {
			return s
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(s)
	}
	text := strings.Join(parts, " · ")
	if w.active {
		return paint(text, theme.Muted)
	}
	return paint("✓", theme.Success) + " " + paint(text, theme.Muted)
}
