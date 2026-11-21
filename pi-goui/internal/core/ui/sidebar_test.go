package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pi-goui/internal/core/session"
)

func TestSidebarHistoryRefreshesSavedSessionName(t *testing.T) {
	m := testModel(t, "")
	cwd := m.tabDirectory(m.current())
	dir := filepath.Join(m.cfg.AgentDir, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(session.Header{Type: "session", ID: "saved", Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "saved.jsonl")
	initial := string(header) + "\n" + `{"type":"message","message":{"role":"user","content":"first"}}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	check := func(seconds int, name string) {
		t.Helper()
		cmd := m.requestSidebarHistory(now.Add(time.Duration(seconds) * time.Second))
		if cmd == nil {
			t.Fatal("expected background scan")
		}
		msg := cmd().(sidebarHistoryMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		m.Update(msg)
		if got := m.sidebarContext().History; len(got) != 1 || got[0] != name {
			t.Fatalf("RECENTS = %q, want %q", got, name)
		}
	}
	check(0, "first")
	check(6, "first")
	if err := os.WriteFile(path, []byte(initial+`{"type":"session_info","name":"renamed"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check(12, "renamed")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cmd := m.requestSidebarHistory(now.Add(18 * time.Second))
	if cmd == nil {
		t.Fatal("expected background scan after deletion")
	}
	m.Update(cmd())
	if got := m.sidebarContext().History; len(got) != 0 {
		t.Fatalf("deleted session retained in RECENTS: %q", got)
	}
}
