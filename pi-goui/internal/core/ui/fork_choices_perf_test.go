package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

// Measure the actual get_fork_messages reply and the default chooser view,
// keeping setup/serialization and filesystem or RPC latency out of the result.
func BenchmarkLongForkChooser(b *testing.B) {
	root := b.TempDir()
	b.Setenv("PI_CODING_AGENT_DIR", root)
	cfg, err := config.Load(root)
	if err != nil {
		b.Fatal(err)
	}
	m := New(Options{Cwd: root}, cfg)
	b.Cleanup(func() { _ = m.Close() })
	tab := m.current()
	const turns = 4000
	var response struct {
		Messages []struct{ EntryID, Text string }
	}
	for i := range turns {
		id := fmt.Sprintf("u%d", i)
		response.Messages = append(response.Messages, struct{ EntryID, Text string }{id, "question"})
		tab.state.Items = append(tab.state.Items, session.Item{ID: id + "/0", Role: "user", Text: "question"}, session.Item{Role: "assistant", Text: "reply"})
	}
	data, err := json.Marshal(response)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.reply(tab, replyMsg{op: "get_fork_messages", data: data})
		rows := m.filtered(tab.dialogs[0])
		if len(rows) != turns || rows[0].Value != "u3999" || !strings.Contains(rows[0].Detail, "reply") {
			b.Fatal("incorrect branch list")
		}
		tab.dialogs = nil
	}
}
