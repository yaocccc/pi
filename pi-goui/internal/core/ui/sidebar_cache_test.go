package ui

import (
	"errors"
	"testing"
	"time"

	"pi-goui/internal/core/session"
)

func TestSidebarHistoryCachesDirectories(t *testing.T) {
	m := testModel(t, "")
	now := time.Now()
	first := m.tabDirectory(m.current())
	m.sidebarHistory = sidebarHistoryState{cwd: first, sessions: []session.Summary{{Name: "first"}}, offset: 2, checked: now}
	m.current().state.Cwd = t.TempDir()
	cmd := m.requestSidebarHistory(now)
	if cmd == nil {
		t.Fatal("new directory must load")
	}
	stale := cmd().(sidebarHistoryMsg)
	m.Update(sidebarHistoryMsg{generation: stale.generation, cwd: stale.cwd, sessions: []session.Summary{{Name: "second"}}})
	m.current().state.Cwd = first
	if m.requestSidebarHistory(now) != nil {
		t.Fatal("cached directory unnecessarily scanned")
	}
	if m.sidebarHistory.offset != 2 || len(m.sidebarHistory.sessions) != 1 || m.sidebarHistory.sessions[0].Name != "first" {
		t.Fatal("cache not restored")
	}
	m.Update(stale)
	if m.sidebarHistory.cwd != first || m.sidebarHistory.sessions[0].Name != "first" {
		t.Fatal("stale scan replaced restored cache")
	}
	refresh := m.requestSidebarHistory(now.Add(6 * time.Second))
	if refresh == nil || m.sidebarHistory.sessions[0].Name != "first" {
		t.Fatal("background refresh cleared cached history")
	}
	msg := refresh().(sidebarHistoryMsg)
	msg.err = errors.New("temporary read failure")
	m.Update(msg)
	if len(m.sidebarHistory.sessions) != 1 {
		t.Fatal("failed refresh cleared cache")
	}
	m.current().state.Cwd = stale.cwd
	if m.requestSidebarHistory(now) != nil || m.sidebarHistory.sessions[0].Name != "second" {
		t.Fatal("second directory not cached")
	}
}

func TestSidebarHistorySwitchWhilePending(t *testing.T) {
	m := testModel(t, "")
	now := time.Now()
	cmd := m.requestSidebarHistory(now)
	stale := cmd().(sidebarHistoryMsg)
	m.current().state.Cwd = t.TempDir()
	next := m.requestSidebarHistory(now)
	if next == nil {
		t.Fatal("previous directory scan blocked current directory")
	}
	m.Update(stale)
	if !m.sidebarHistory.pending || m.sidebarHistory.cwd == stale.cwd {
		t.Fatal("stale result affected current scan")
	}
	m.Update(next().(sidebarHistoryMsg))
	if m.sidebarHistory.pending {
		t.Fatal("current scan did not complete")
	}
}
