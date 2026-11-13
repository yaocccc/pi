package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/session"
)

func uiForkFixture(t *testing.T) (*Model, *tab, string, json.RawMessage) {
	t.Helper()
	m := testModel(t, "")
	tab := m.current()
	c, wire := fakeClient(t)
	tab.client = c
	path := filepath.Join(t.TempDir(), "source.jsonl")
	header := `{"type":"session","version":3,"id":"original","cwd":"/tmp"}`
	entries := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"old","parentId":null,"message":{"role":"user","content":"first"}}`),
		json.RawMessage(`{"type":"message","id":"answer","parentId":"old","message":{"role":"assistant","content":[{"type":"text","text":"earlier answer"}],"stopReason":"stop"}}`),
		json.RawMessage(`{"type":"message","id":"new","parentId":"answer","message":{"role":"user","content":"last"}}`),
		json.RawMessage(`{"type":"message","id":"error","parentId":"new","message":{"role":"assistant","content":[{"type":"text","text":"This operation was aborted"}],"stopReason":"error"}}`),
	}
	data := header + "\n"
	for _, e := range entries {
		data += string(e) + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(map[string]any{"entries": entries, "leafId": "error"})
	tab.state.ID, tab.state.File, tab.state.Name, tab.state.Cwd = "original", path, "Original", "/tmp"
	if err := tab.state.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	tab.setValue("original draft")
	return m, tab, wire, snapshot
}

func TestForkNewestFirstAndIndependentTab(t *testing.T) {
	m, source, wire, snapshot := uiForkFixture(t)
	originalBytes, _ := os.ReadFile(source.state.File)
	originalItems := append([]session.Item(nil), source.state.Items...)
	originalClient := source.client
	// Add a source-only live suffix to exercise the stale overlay risk as well.
	source.state.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","timestamp":123,"content":[{"type":"text","text":"LIVE ERROR SUFFIX"}],"stopReason":"error"}}`))
	originalItems = append(originalItems[:0], source.state.Items...)
	m.reply(source, replyMsg{op: "get_fork_messages", data: json.RawMessage(`{"messages":[{"entryId":"old","text":"first"},{"entryId":"new","text":"last"}]}`)})
	d := source.dialogs[0]
	if d.options[0].Value != "new" || d.options[1].Value != "old" || d.selected != 0 {
		t.Fatal("fork choices not newest first")
	}
	cmd := m.confirmDialog(source, d)
	if cmd == nil || !source.forkPending {
		t.Fatal("fork not requested")
	}
	if m.requestFork(source, "new") != nil {
		t.Fatal("duplicate fork accepted")
	}
	v := cmd().(forkSnapshotMsg)
	if v.err != nil {
		t.Fatal(v.err)
	}
	v.entries = snapshot // fake RPC returns an empty tree; supply its fixture tree.
	write := m.forkSnapshot(v)
	if write == nil || len(source.dialogs) != 0 {
		t.Fatal("selection must directly schedule creation without a preview")
	}
	result := write().(forkCreatedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if cmd := m.forkCreated(result); cmd == nil {
		t.Fatal("new tab not started")
	}
	fork := m.current()
	if len(m.tabs) != 2 || m.tabs[0] != source || fork == source || source.client != originalClient {
		t.Fatal("original Active tab replaced")
	}
	if source.state.ID != "original" || source.state.Name != "Original" || source.editor.Value() != "original draft" || !reflect.DeepEqual(source.state.Items, originalItems) {
		t.Fatal("source state changed")
	}
	after, _ := os.ReadFile(source.state.File)
	if string(originalBytes) != string(after) {
		t.Fatal("source file mutated")
	}
	if fork.state.ID == source.state.ID || fork.state.File == source.state.File || fork.state.Name != "Original" || fork.editor.Value() != "" {
		t.Fatal("fork identity/name/draft wrong")
	}
	var texts []string
	for _, item := range fork.state.Items {
		texts = append(texts, item.Text)
	}
	text := strings.Join(texts, "\n")
	if !strings.Contains(text, "aborted") || strings.Contains(text, "SUFFIX") || !strings.Contains(text, "last") || !strings.Contains(text, "earlier answer") {
		t.Fatal("incorrect fork boundary:", text)
	}
	if fork.state.Busy || fork.working.present || fork.assistantStats != nil || len(fork.dialogs) != 0 {
		t.Fatal("inherited source UI state")
	}
	records := wireRecords(t, wire, 1)
	if len(records) != 1 || records[0]["type"] != "get_entries" {
		t.Fatalf("source received mutation: %#v", records)
	}
}

func TestForkInheritsDisplayName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Original", "Original"},
		{"项目重构", "项目重构"},
		{"", "first"},
		{"fork from Original", "fork from Original"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			m, source, _, _ := uiForkFixture(t)
			source.state.Name = tc.name
			cmd := m.requestFork(source, "new")
			if cmd == nil {
				t.Fatal("fork request rejected")
			}
			snapshot := cmd().(forkSnapshotMsg)
			if snapshot.err != nil || snapshot.name != tc.want {
				t.Fatalf("fork name = %q, error = %v; want %q", snapshot.name, snapshot.err, tc.want)
			}
			if source.state.Name != tc.name {
				t.Fatal("fork naming changed the source")
			}
		})
	}
}

func TestForkCancelBusyAndStale(t *testing.T) {
	m, tab, _, snapshot := uiForkFixture(t)
	m.reply(tab, replyMsg{op: "get_fork_messages", data: json.RawMessage(`{"messages":[{"entryId":"new","text":"last"}]}`)})
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
	if tab.forkPending || len(m.tabs) != 1 || tab.editor.Value() != "original draft" {
		t.Fatal("cancel changed source")
	}
	tab.state.Busy = true
	if m.requestFork(tab, "new") != nil || tab.forkPending {
		t.Fatal("busy fork allowed")
	}
	tab.state.Busy = false
	cmd := m.requestFork(tab, "new")
	v := cmd().(forkSnapshotMsg)
	v.entries = snapshot
	tab.revision++
	if m.forkSnapshot(v) != nil || tab.forkPending {
		t.Fatal("stale fork created")
	}
	cmd = m.requestFork(tab, "new")
	v = cmd().(forkSnapshotMsg)
	v.err = errors.New("read failed")
	if m.forkSnapshot(v) != nil || tab.forkPending {
		t.Fatal("failed snapshot created")
	}
	cmd = m.requestFork(tab, "new")
	v = cmd().(forkSnapshotMsg)
	v.entries = json.RawMessage(`{"entries":[],"leafId":null}`)
	if m.forkSnapshot(v) != nil || tab.forkPending {
		t.Fatal("invalid selection accepted")
	}
	if len(m.tabs) != 1 || tab.forkPending || tab.state.Name != "Original" {
		t.Fatal("failure changed source")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(tab.state.File), "*.jsonl"))
	if len(files) != 1 {
		t.Fatal("failed fork left files", files)
	}
}
