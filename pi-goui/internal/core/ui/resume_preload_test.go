package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func warmResume(t *testing.T, m *Model, file string) *resumePreloadEntry {
	t.Helper()
	cmd := m.requestResumePreload(file)
	if cmd == nil {
		t.Fatal("no preload job")
	}
	if next := m.receiveResumePreload(cmd().(resumePreloadMsg)); next != nil {
		t.Fatal("unchanged candidate scheduled another read")
	}
	if m.resumePreload.cached == nil || m.resumePreload.cached.err != nil {
		t.Fatalf("preload failed: %+v", m.resumePreload.cached)
	}
	return m.resumePreload.cached
}

func resumeCommandMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, resumeCommandMessages(child)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func TestResumePreloadHitIsPreparedAndIndependentAcrossTabs(t *testing.T) {
	m := testModel(t, "")
	file := previewHistory(t)
	cached := warmResume(t, m, file)
	first := m.addTab(file)
	second := m.addTab(file)
	first.setValue("first draft")
	second.setValue("second draft")
	first.images = []map[string]any{{"data": "first image"}}
	for _, tab := range []*tab{first, second} {
		result := m.previewSession(tab)().(resumePreviewMsg)
		if result.prepared != cached.prepared {
			t.Fatal("warm hit reparsed history")
		}
		cmd := m.receiveResumePreview(result)
		if cmd == nil || tab.documentJob == nil {
			t.Fatal("warm preview did not render asynchronously")
		}
		m.receiveDocument(cmd().(documentResult))
		if !strings.Contains(tab.doc.Text, "SAVED-LOCAL-HISTORY") {
			t.Fatal("warm transcript missing")
		}
	}
	first.state.Items[0].Text = "changed first tab"
	if second.state.Items[0].Text != "SAVED-LOCAL-HISTORY" {
		t.Fatal("cached projection shared mutable item containers")
	}
	assertValue(t, first, "first draft")
	assertValue(t, second, "second draft")
	if len(first.images) != 1 || len(second.images) != 0 {
		t.Fatal("cache changed local attachments")
	}
	// Applying the cached immutable records again must not inherit local edits.
	third := m.addTab(file)
	m.receiveResumePreview(m.previewSession(third)().(resumePreviewMsg))
	if third.state.Items[0].Text != "SAVED-LOCAL-HISTORY" {
		t.Fatal("local mutation corrupted cached records")
	}
}

func TestResumePreloadOneJobAndLatestHoverWins(t *testing.T) {
	m := testModel(t, "")
	first, middle, last := previewHistory(t), previewHistory(t), previewHistory(t)
	cmd := m.requestResumePreload(first)
	job := m.resumePreload.job
	for i := 0; i < 100; i++ {
		if m.requestResumePreload(middle) != nil || m.requestResumePreload(last) != nil {
			t.Fatal("hover spawned concurrent parses")
		}
	}
	if m.resumePreload.job != job || m.resumePreload.wanted != last || m.resumePreload.cached != nil {
		t.Fatal("hover queue was not coalesced")
	}
	next := m.receiveResumePreload(cmd().(resumePreloadMsg))
	if m.resumePreload.cached != nil || m.resumePreload.job == nil || m.resumePreload.job.file != last {
		t.Fatal("obsolete result displaced newest candidate")
	}
	messages := resumeCommandMessages(next)
	if len(messages) != 1 {
		t.Fatalf("queued %d parses, expected only latest", len(messages))
	}
	m.receiveResumePreload(messages[0].(resumePreloadMsg))
	if m.resumePreload.cached == nil || m.resumePreload.cached.file != last || m.resumePreload.job != nil {
		t.Fatal("latest hover did not settle")
	}
	for i := 0; i < 100; i++ {
		if m.requestResumePreload(last) != nil {
			t.Fatal("warm mouse movement started another read")
		}
	}
	// Duplicate delivery from an old command cannot replace a newer cache.
	m.receiveResumePreload(resumePreloadMsg{job: job})
	if m.resumePreload.cached.file != last {
		t.Fatal("stale job replaced cache")
	}
}

func TestResumePreloadClickJoinsInFlightAndRejectsStaleWaiters(t *testing.T) {
	m := testModel(t, "")
	file := previewHistory(t)
	cmd := m.requestResumePreload(file)
	current := m.addTab(file)
	stale := m.addTab(file)
	for _, tab := range []*tab{current, stale, current} {
		if m.previewSession(tab) != nil {
			t.Fatal("click duplicated in-flight parse")
		}
	}
	if len(m.resumePreload.waiters) != 2 {
		t.Fatal("waiters were not deduplicated")
	}
	stale.revision++
	results := resumeCommandMessages(m.receiveResumePreload(cmd().(resumePreloadMsg)))
	if len(results) != 1 {
		t.Fatalf("got %d results; stale tab should be rejected", len(results))
	}
	result := results[0].(resumePreviewMsg)
	if result.owner != current {
		t.Fatal("wrong tab received preview")
	}
	m.receiveResumePreview(result)
	if !current.resume.preview || stale.resume.preview {
		t.Fatal("preview applied across owners")
	}
}

func TestResumePreloadInvalidatesFileIdentityAndMetadata(t *testing.T) {
	for _, kind := range []string{"append", "replacement", "mtime", "symlink", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			file := previewHistory(t)
			if kind == "parent-symlink" {
				link := filepath.Join(t.TempDir(), "parent")
				if err := os.Symlink(filepath.Dir(file), link); err != nil {
					t.Skip(err)
				}
				file = filepath.Join(link, filepath.Base(file))
			}
			cached := warmResume(t, m, file)
			tab := m.addTab(file)
			pending := m.previewSession(tab)().(resumePreviewMsg)
			switch kind {
			case "append":
				f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("\n")
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "replacement":
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				replacement := file + ".new"
				if err = os.WriteFile(replacement, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(replacement, file); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				changed := cached.stamp.info.ModTime().Add(time.Second)
				if err := os.Chtimes(file, changed, changed); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(file, file+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(file+".old", file); err != nil {
					t.Skip(err)
				}
			case "parent-symlink":
				other := t.TempDir()
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(other, filepath.Base(file)), raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(filepath.Dir(file)); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(other, filepath.Dir(file)); err != nil {
					t.Fatal(err)
				}
			}
			retry := m.receiveResumePreview(pending)
			if tab.resume.preview || len(tab.state.Items) != 0 || retry == nil {
				t.Fatal("changed file was published from delayed warm hit")
			}
			fresh := retry().(resumePreviewMsg)
			if fresh.prepared == cached.prepared {
				t.Fatal("invalid cache was reused")
			}
			if kind == "symlink" && fresh.err == nil {
				t.Fatal("symlink replacement was followed")
			}
			if kind != "symlink" && fresh.err != nil {
				t.Fatal(fresh.err)
			}
		})
	}
}

func TestResumePreloadBoundAndFailures(t *testing.T) {
	m := testModel(t, "")
	huge := filepath.Join(t.TempDir(), "huge.jsonl")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(resumePreloadLimit + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if m.requestResumePreload(huge) != nil || m.resumePreload.job != nil {
		t.Fatal("speculative file size bound ignored")
	}
	file := previewHistory(t)
	if err = os.WriteFile(file, []byte("broken history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := m.requestResumePreload(file)
	m.receiveResumePreload(cmd().(resumePreloadMsg))
	if m.requestResumePreload(file) != nil {
		t.Fatal("unchanged parse failure retried on every hover")
	}
	tab := m.addTab(file)
	m.receiveResumePreview(m.previewSession(tab)().(resumePreviewMsg))
	if tab.resume.preview || !strings.Contains(tab.status, "无法读取") {
		t.Fatal("preload error masqueraded as successful history")
	}
}

func TestResumePreloadCandidatesMatchSidebarAndSelector(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 140, 42
	files := []string{previewHistory(t), previewHistory(t)}
	list := []session.Summary{{ID: "a", Path: files[0], Name: "first", Modified: 2}, {ID: "b", Path: files[1], Name: "second", Modified: 1}}
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(m.current()), sessions: list}
	if got := m.resumePreloadCandidate(); got != files[0] {
		t.Fatalf("recent candidate %q", got)
	}
	layout := m.sidebarLayout()
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX = m.mainWidth() + 3
	m.sidebarHistory.pointerY = layout.HistoryStart + render.SidebarHistoryRows
	if got := m.resumePreloadCandidate(); got != files[1] {
		t.Fatalf("hover candidate %q", got)
	}
	d := m.newDialog("session", "Sessions", "")
	d.options = []option{{Label: "first", Value: files[0]}, {Label: "second", Value: files[1]}}
	m.current().dialogs = []*dialog{d}
	if got := m.resumePreloadCandidate(); got != files[0] {
		t.Fatalf("selector candidate %q", got)
	}
	d.selected = 1
	if got := m.resumePreloadCandidate(); got != files[1] {
		t.Fatalf("selected candidate %q", got)
	}
	d.editor.SetValue("first")
	d.selected = 0
	if got := m.resumePreloadCandidate(); got != files[0] {
		t.Fatalf("filtered candidate %q", got)
	}
	m.opts.NoSession = true
	if m.ensureResumePreload() != nil {
		t.Fatal("no-session mode speculatively read history")
	}
}
