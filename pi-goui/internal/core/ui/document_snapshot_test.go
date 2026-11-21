package ui

import (
	"fmt"
	"strings"
	"testing"

	"pi-goui/internal/core/render"
)

// Keep the number of records constant while varying the stable native JSON.
// Setup, event projection and actual Markdown/terminal layout are not timed in prepare.
func BenchmarkNativeLayoutSnapshot(b *testing.B) {
	for _, n := range []int{128, 32768} {
		for _, phase := range []string{"prepare", "cold", "warm"} {
			b.Run(fmt.Sprintf("%d/%s", n, phase), func(b *testing.B) {
				m, _ := streamingModel(b, 128)
				t := m.current()
				var rows []string
				payload := strings.Repeat("x", n)
				for i := 0; i < 32; i++ {
					rows = append(rows, fmt.Sprintf(`{"role":"user","timestamp":%d,"content":"next"},{"role":"assistant","timestamp":%d,"content":[{"type":"toolCall","name":"example","id":"t%d","arguments":{"data":"%s"}}]},{"role":"toolResult","toolCallId":"t%d","toolName":"example","content":"ok","details":{"data":"%s"}}`, 3*i+1, 3*i+2, i, payload, i, payload))
				}
				if err := t.state.LoadMessages([]byte(`{"messages":[` + strings.Join(rows, ",") + `]}`)); err != nil {
					b.Fatal(err)
				}
				if len(t.state.Items) < 64 {
					b.Fatal(len(t.state.Items))
				}
				t.documentJob = nil
				t.invalidateStream()
				m.scheduleDocument(t)
				j := t.documentJob
				if j == nil {
					b.Fatal("missing layout")
				}
				j.opts.Tools = nil
				if phase == "warm" {
					j.cache.Transcript(j.items, j.theme, j.opts)
				}
				b.ReportAllocs()
				for b.Loop() {
					switch phase {
					case "prepare":
						t.documentJob = nil
						m.scheduleDocument(t)
					case "cold":
						var cache render.TranscriptCache
						cache.Transcript(j.items, j.theme, j.opts)
					case "warm":
						j.cache.Transcript(j.items, j.theme, j.opts)
					}
				}
			})
		}
	}
}
