package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/codex"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func TestQuotaViewGatedByCurrentIdentityAndRejectsLateReply(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.state.Model = session.ModelInfo{Provider: "openai-codex", BaseURL: "https://chatgpt.com/backend-api/codex", ID: "codex"}
	// No auth file exists in this isolated agent directory: no real HTTP can run.
	cmd := m.pollQuota(time.Now())
	if cmd == nil {
		t.Fatal("missing eligibility check")
	}
	msg := cmd().(quotaMsg)
	m.Update(msg)
	if q := m.sidebarContext().Quota; q == nil || q.RemainingPercent != nil {
		t.Fatal("missing default quota placeholder")
	}
	key, _ := m.quotaIdentity()
	remaining, credits := 80.2, 82334.75
	snapshot := codex.Snapshot{LoggedIn: true, RemainingPercent: &remaining, Credits: &credits, ResetAt: time.Now().Add(49 * time.Hour)}
	m.Update(quotaMsg{key: key, generation: m.quotaGeneration, snapshot: snapshot})
	if m.sidebarContext().Quota == nil {
		t.Fatal("matching logged-in quota missing")
	}
	tab.state.Model.Provider = ""
	m.pollQuota(time.Now())
	if q := m.sidebarContext().Quota; q == nil || q.RemainingPercent != snapshot.RemainingPercent {
		t.Fatal("session initialization cleared cached quota")
	}
	tab.state.Model.Provider = "other"
	if q := m.sidebarContext().Quota; q == nil || q.RemainingPercent != nil {
		t.Fatal("other provider inherited Codex quota")
	}
	m.pollQuota(time.Now())
	m.Update(quotaMsg{key: key, generation: m.quotaGeneration - 1, snapshot: snapshot})
	if q := m.sidebarContext().Quota; q == nil || q.RemainingPercent != nil {
		t.Fatal("late response resurrected previous account display")
	}
}
func TestWorkingCompletionPersistsUntilNewMainInput(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.status = "Ready"
	if ansi.Strip(m.workingLine(tab)) != "" {
		t.Fatal("idle Ready must be hidden")
	}
	now := time.Now().Add(-2 * time.Second)
	tab.working.apply([]byte(`{"type":"agent_start"}`), now)
	tab.working.apply([]byte(`{"type":"turn_start"}`), now)
	tab.working.apply([]byte(`{"type":"agent_settled"}`), now.Add(time.Second))
	before := ansi.Strip(m.workingLine(tab))
	if !strings.HasPrefix(before, "✓ 1 turns") {
		t.Fatal(before)
	}
	m.Update(tickMsg(time.Now()))
	if ansi.Strip(m.workingLine(tab)) != before {
		t.Fatal("finished metrics changed during idle tick")
	}
	press(m, 'n', 0, "n")
	if ansi.Strip(m.workingLine(tab)) != "" {
		t.Fatal("new input did not clear completed working line")
	}
	tab.working.apply([]byte(`{"type":"agent_start"}`), time.Now())
	tab.state.Busy = true
	press(m, 'x', 0, "x")
	if !tab.working.active {
		t.Fatal("typing steering draft interrupted active metrics")
	}
}
func TestScrollMessageProgressAndLatestHitbox(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	tab.doc = render.Document{Text: strings.Repeat("row\n", 200), MessageCount: 345, Blocks: []render.Block{{Start: 0, End: 3, MessageIndex: 121}, {Start: 3, End: 6, MessageIndex: 122}, {Start: 6, End: 80, MessageIndex: 123}, {Start: 80, End: 200, MessageIndex: 124}}}
	tab.dirty = false
	tab.follow = false
	tab.offset = 0
	if got := m.scrollProgress(tab); got != "123/345" {
		t.Fatal(got)
	}
	label, rect, ok := m.latestButton(tab)
	if !ok {
		t.Fatal("button hidden while scrolling")
	}
	line := ansi.Strip(m.statusLine(tab, ""))
	if !strings.Contains(line, "123/345 "+label) {
		t.Fatal(line)
	}
	if ansi.Cut(line, rect.x, rect.x+rect.width) != label {
		t.Fatal("counter moved button hitbox")
	}
	tab.offset = 81
	if m.scrollProgress(tab) != "124/345" {
		t.Fatal("message ordinal did not advance")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y})
	if !tab.follow {
		t.Fatal("latest click no longer follows")
	}
}
