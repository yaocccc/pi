package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
)

func TestLargeInteractiveReflowPublishesLatestLayout(t *testing.T) {
	m, events := streamingModel(t, 24<<10)
	tab := m.current()
	old := tab.doc.FullText()
	m.Update(tea.WindowSizeMsg{Width: 94, Height: 32})
	job := tab.documentJob
	if job == nil || tab.doc.FullText() != old {
		t.Fatal("resize synchronously replaced a readable transcript")
	}
	m.Update(events[1])
	m.Update(tea.WindowSizeMsg{Width: 76, Height: 32})
	if tab.documentJob != job || tab.doc.FullText() != old {
		t.Fatal("overlapping layouts or lost document")
	}
	m.Update(job.render())
	if tab.doc.FullText() != old {
		t.Fatal("stale width published")
	}
	m.Update(tickMsg(time.Now()))
	if tab.documentJob == nil {
		t.Fatal("latest resize was not scheduled")
	}
	finishStreamingDocument(m)
	want := render.Transcript(tab.state.Items, m.cfg.Theme, m.documentOptions(tab))
	if tab.dirty || !equalDocuments(tab.doc, want) {
		t.Fatal("did not publish complete final document")
	}
}

func TestOriginJumpWaitsForLargeSourceLayout(t *testing.T) {
	m, source, branch, _ := branchFixture(t)
	source.state.Items[0].Text += strings.Repeat("long source. ", 1600)
	m.branchOpenResult(m.requestOrigin(branch)().(branchOpenMsg))
	if m.current() != source || source.originJump == nil {
		t.Fatal("source not activated or jump consumed before pending reflow")
	}
	m.Update(tickMsg(time.Now()))
	finishStreamingDocument(m)
	if source.originJump != nil || source.follow || !strings.Contains(source.toast.text, "已定位") {
		t.Fatal("jump did not resolve against the newly published geometry")
	}
}

func TestResumeWarmRefreshPreservesSelection(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	cwd := m.tabDirectory(tab)
	dir := filepath.Join(m.cfg.AgentDir, "sessions", "saved")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	save := func(name string) string {
		p := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(p, []byte(fmt.Sprintf("{\"type\":\"session\",\"id\":%q,\"cwd\":%q}\n", name, cwd)), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := save("first")
	m.Update(m.listSessions(tab, false)())
	tab.dialogs = nil
	cmd := m.listSessions(tab, false)
	if len(tab.dialogs) != 1 {
		t.Fatal("cached chooser not visible before refresh")
	}
	d := tab.dialogs[0]
	opts := m.filtered(d)
	d.selected = -1
	for i, row := range opts {
		if row.Value == p {
			d.selected = i
		}
	}
	if d.selected < 0 {
		t.Fatal("cached selection missing")
	}
	save("second")
	m.Update(cmd())
	opts = m.filtered(d)
	if len(tab.dialogs) != 1 || tab.dialogs[0] != d || opts[d.selected].Value != p || len(opts) != 3 {
		t.Fatalf("refresh replaced selection/dialog: %+v", opts)
	}
	cmd = m.listSessions(tab, false)
	tab.dialogs = nil
	m.Update(cmd())
	if len(tab.dialogs) != 0 {
		t.Fatal("late refresh reopened dismissed chooser")
	}
}

// Time only input dispatch. Complete background work outside timing so each
// sample starts from a fully published document, at the previous width.
func BenchmarkLargeResizeDispatch(b *testing.B) {
	m, _ := streamingModel(b, 64<<10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(tea.WindowSizeMsg{Width: 100 + i%2, Height: 36})
		b.StopTimer()
		finishStreamingDocument(m)
		b.StartTimer()
	}
}

func BenchmarkWarmResumeScan(b *testing.B) {
	m, _ := streamingModel(b, 128)
	tab := m.current()
	dir := filepath.Join(m.cfg.AgentDir, "sessions", "bench")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	for i := range 8 {
		body := fmt.Sprintf("{\"type\":\"session\",\"id\":\"%d\",\"cwd\":%q}\n", i, m.tabDirectory(tab)) + "{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":\"question\"}}\n" + strings.Repeat("{\"type\":\"message\",\"message\":{\"role\":\"assistant\",\"content\":\""+strings.Repeat("x", 1000)+"\"}}\n", 1000)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.jsonl", i)), []byte(body), 0600); err != nil {
			b.Fatal(err)
		}
	}
	scan := func() { tab.dialogs = nil; cmd := m.listSessions(tab, false); m.Update(cmd()) }
	scan()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scan()
	}
}

func equalDocuments(a, b render.Document) bool {
	if a.LineCount() != b.LineCount() {
		return false
	}
	for y := 0; y < a.LineCount(); y++ {
		as, ah := a.Span(y)
		bs, bh := b.Span(y)
		if a.Line(y) != b.Line(y) || as != bs || ah != bh {
			return false
		}
	}
	a.DocumentRows = b.DocumentRows
	return reflect.DeepEqual(a, b)
}
