package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Both wire forms produce the same text. Measure decode, apply and projection
// only: event generation, cloning the fixture and UI layout are not timed.
func BenchmarkStreamingWireBatch(b *testing.B) {
	prefix := strings.Repeat("**Markdown** · 中文 🧪 and `code`\n\n", 1600)
	const count = 16
	for _, snapshot := range []bool{false, true} {
		name := "delta"
		if snapshot {
			name = "snapshot+delta"
		}
		b.Run(name, func(b *testing.B) {
			message := func(text string) string {
				return fmt.Sprintf(`{"role":"assistant","timestamp":101,"content":[{"type":"text","text":%s}]}`, encodeStreamText(b, text))
			}
			history := make([]string, 100)
			for i := range history {
				history[i] = fmt.Sprintf(`{"role":"user","content":"history %d","timestamp":%d}`, i, i)
			}
			var base State
			if err := base.LoadMessages(json.RawMessage(`{"messages":[` + strings.Join(history, ",") + `]}`)); err != nil {
				b.Fatal(err)
			}
			base.Apply(json.RawMessage(`{"type":"message_start","message":` + message(prefix) + `}`))
			events := make([]json.RawMessage, count)
			want := prefix
			total := 0
			for i := range events {
				delta := fmt.Sprintf("part-%02d ", i)
				want += delta
				full := ""
				if snapshot {
					full = `,"message":` + message(want)
				}
				events[i] = json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":` + encodeStreamText(b, delta) + `}` + full + `}`)
				total += len(events[i])
			}
			check := base.Detached()
			check.ApplyBatch(events)
			if check.Items[len(check.Items)-1].Text != want {
				b.Fatal("wire form did not produce the expected final text")
			}
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				state := base.Detached()
				b.StartTimer()
				state.ApplyBatch(events)
			}
			b.ReportMetric(float64(total), "wire-B/op")
		})
	}
}

func encodeStreamText(b *testing.B, s string) string {
	b.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}
