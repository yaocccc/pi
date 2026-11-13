package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
	"pi-goui/internal/updates"
)

func TestStartupUpdateNoticeBelowWelcome(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.dead = true
	tab.setValue("keep draft")
	m.Update(updateCheckMsg{report: updates.Report{LatestVersion: "1.2.4", Packages: []updates.Package{{}}}})
	view := ansi.Strip(m.View().Content)
	welcome, notice := strings.Index(view, "A quiet place to work."), strings.Index(view, "Updates available:")
	if welcome < 0 || notice <= welcome || !strings.Contains(view, "Pi 1.2.4") {
		t.Fatal("notice should follow default chat content", view)
	}
	if len(tab.dialogs) != 0 || tab.editor.Value() != "keep draft" {
		t.Fatal("background check interrupted interaction")
	}
	for _, line := range m.extraLines(tab) {
		if strings.Contains(line, "Updates available") {
			t.Fatal("notice leaked into editor/footer area")
		}
	}
}

func TestStartupCheckFailuresStaySilent(t *testing.T) {
	for _, result := range []updateCheckMsg{
		{err: errors.New("SECRET registry credentials")},
		{report: updates.Report{Offline: true}},
		{report: updates.Report{Unsupported: "unsupported"}},
		{report: updates.Report{CurrentVersion: "1.2.3"}},
	} {
		m := testModel(t, "")
		m.Update(result)
		if m.updates.notice != "" || len(m.current().dialogs) != 0 {
			t.Fatal("unexpected notification")
		}
	}
}

func TestUpdateConfigUsesSelectedPiAndOfflineFlag(t *testing.T) {
	m := testModel(t, "")
	t.Setenv("PI_BIN", "/selected/pi")
	m.opts.Bin = ""
	m.opts.PiArgs = []string{"--offline"}
	cfg := m.updateConfig()
	if cfg.PiBin != "/selected/pi" || cfg.AgentDir != m.cfg.AgentDir || !cfg.Offline {
		t.Fatalf("wrong check target: %+v", cfg)
	}
	m.opts.Bin = "/explicit/pi"
	if m.updateConfig().PiBin != "/explicit/pi" {
		t.Fatal("PI_BIN overrode explicit executable")
	}
}

func TestUpdateReaderCloseCancelsAndPreventsLateStarts(t *testing.T) {
	r := newUpdateReader()
	defer r.close()
	started, done := make(chan struct{}), make(chan struct{})
	r.check = func(ctx context.Context, _ updates.Config) (updates.Report, error) {
		close(started)
		<-ctx.Done()
		return updates.Report{}, ctx.Err()
	}
	go func() { _ = r.command(updates.Config{})(); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("check not started")
	}
	r.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("check not reaped")
	}
	late := r.command(updates.Config{})().(updateCheckMsg)
	if !errors.Is(late.err, context.Canceled) {
		t.Fatal("late command started after shutdown")
	}
}

func TestInitSchedulesBackgroundCheck(t *testing.T) {
	m := testModel(t, "")
	m.updates.reader.check = func(context.Context, updates.Config) (updates.Report, error) {
		return updates.Report{LatestVersion: "1.2.4"}, nil
	}
	batch := m.Init()().(tea.BatchMsg)
	result, ok := batch[len(batch)-1]().(updateCheckMsg)
	if !ok || result.report.LatestVersion != "1.2.4" {
		t.Fatal("startup check missing")
	}
}

func TestPiReleaseNotesBelowWelcomeAreScrollableAndNotHistory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.dead = true
	tab.setValue("keep draft")
	notes := updates.ReleaseNotes{Version: "1.2.4", Markdown: strings.Repeat("- Pi change.\n", 50) + "\nLast release note."}
	m.receiveReleaseNotes(releaseNotesMsg{notes})
	m.document(tab)
	text := ansi.Strip(tab.doc.Text)
	welcome, title := strings.Index(text, "A quiet place to work."), strings.Index(text, "Pi 1.2.4")
	if welcome < 0 || title <= welcome || !strings.Contains(text, "Last release note.") || tab.offset != 0 {
		t.Fatal("release notes missing or not shown from the top", text)
	}
	if len(tab.state.Items) != 0 || tab.state.MessageCount != 0 || len(tab.dialogs) != 0 || tab.editor.Value() != "keep draft" {
		t.Fatal("release notes changed conversation or interaction state")
	}
	cmd := m.markReleaseNotesDisplayed()
	if cmd == nil || m.markReleaseNotesDisplayed() != nil {
		t.Fatal("display receipt must be scheduled once")
	}
	_ = cmd()
	m.scroll(tab, 1000)
	if !strings.Contains(ansi.Strip(m.View().Content), "Last release note.") {
		t.Fatal("long release notes are not scrollable")
	}
	tab.state.Items = []session.Item{{ID: "u", Role: "user", Text: "Real chat"}}
	tab.follow = true
	m.document(tab)
	if strings.Contains(ansi.Strip(tab.doc.Text), "What's new") || !strings.Contains(ansi.Strip(tab.doc.Text), "Real chat") {
		t.Fatal("startup release notes leaked into actual transcript")
	}
}

func TestPiNotesNotAcknowledgedInResumedConversation(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.newSession = false
	// Even before the historical entries arrive, this is not a blank new tab.
	m.receiveReleaseNotes(releaseNotesMsg{updates.ReleaseNotes{Version: "1.2.4", Markdown: "Pi change."}})
	m.document(tab)
	if m.markReleaseNotesDisplayed() != nil || m.updates.notesAck || strings.Contains(tab.doc.Text, "Pi change") {
		t.Fatal("unshown release notes were presented or acknowledged during resume")
	}
	tab.state.Items = []session.Item{{ID: "u", Role: "user", Text: "Existing history"}}
	m.document(tab)
	if m.markReleaseNotesDisplayed() != nil || m.updates.notesAck {
		t.Fatal("unshown release notes were acknowledged")
	}
}
