package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pi-goui/internal/core/session"
)

func TestForkPublishesRecentsBeforeRPCAndRejectsOldScan(t *testing.T) {
	m, source, _, snapshot := uiForkFixture(t)
	dir := filepath.Join(m.cfg.AgentDir, "sessions", "forks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "original.jsonl")
	if err := os.Rename(source.state.File, path); err != nil {
		t.Fatal(err)
	}
	source.state.File = path
	initial := m.requestSidebarHistory(time.Now())().(sidebarHistoryMsg)
	m.Update(initial)
	// This scan completed before creation, but its reply arrives afterwards.
	stale := m.requestSidebarHistory(time.Now().Add(6 * time.Second))().(sidebarHistoryMsg)
	fork, err := session.CreateNumberedFork(m.cfg.AgentDir, path, source.state.ID, source.state.Cwd, "Original", "new", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.forkCreated(prepareForkMessage(t, source.id, fork)) // Never execute RPC startup.
	assertVisible := func() {
		t.Helper()
		for _, row := range m.sidebarSummaries() {
			if row.ID == fork.ID && row.Name == "[1]Original" && row.ParentSession == path {
				return
			}
		}
		t.Fatalf("fork missing from Recents: %+v", m.sidebarSummaries())
	}
	assertVisible()
	generation := m.sidebarHistory.generation
	m.Update(stale)
	assertVisible()
	if !m.sidebarHistory.pending || m.sidebarHistory.generation != generation {
		t.Fatal("stale scan cleared the new pending request")
	}
	stale.err = errors.New("old scan failed")
	m.Update(stale)
	assertVisible()
	list, err := session.List(m.cfg.AgentDir, source.state.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(sidebarHistoryMsg{generation: generation, cwd: source.state.Cwd, sessions: list})
	assertVisible()
	if m.sidebarHistory.pending {
		t.Fatal("fresh scan did not complete")
	}
}
