package ui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func hotpathFiles(n int) []fileEntry {
	out := make([]fileEntry, n)
	for i := range out {
		out[i] = fileEntry{
			name: strings.Repeat("nested/", i%5) + fmt.Sprintf("file-%05d.go", n-i),
			dir:  i%3 == 0, modTime: time.Unix(int64(i%7), 0),
		}
	}
	return out
}

func TestFileChoicesTopPageMatchesFullSort(t *testing.T) {
	m := testModel(t, "")
	entries := hotpathFiles(10000)
	for _, query := range []string{"", "file", "f09", "nested", "no-match"} {
		mention := fileMention{query: query}
		key, _, _, needle, _ := fileRequest(m.tabDirectory(m.current()), mention)
		m.files.cache = map[string]fileCache{key: {entries: entries}}
		var want []fileEntry
		for _, e := range entries {
			if _, ok := fuzzyScore(e.name, needle); ok {
				want = append(want, e)
			}
		}
		// Keep the original full-sort policy independent of the bounded heap.
		sort.Slice(want, func(i, j int) bool {
			a, b := want[i], want[j]
			if da, db := strings.Count(a.name, "/"), strings.Count(b.name, "/"); da != db {
				return da < db
			}
			if !a.modTime.Equal(b.modTime) {
				return a.modTime.After(b.modTime)
			}
			sa, _ := fuzzyScore(a.name, needle)
			sb, _ := fuzzyScore(b.name, needle)
			if sa != sb {
				return sa < sb
			}
			if a.dir != b.dir {
				return a.dir
			}
			return a.name < b.name
		})
		want = want[:min(64, len(want))]
		got := m.fileChoices(mention)
		navigation := 0
		if query == "" {
			navigation = 1
		}
		if len(got) != len(want)+navigation {
			t.Fatalf("%q: got %d choices, want %d matches + %d navigation", query, len(got), len(want), navigation)
		}
		for i, e := range want {
			name, description := e.name, "file"
			if e.dir {
				name, description = name+"/", "directory"
			}
			if got[i].Name != name || got[i].Description != description {
				t.Fatalf("%q match %d: got %+v, want %q (%s)", query, i, got[i], name, description)
			}
		}
		if navigation != 0 && got[len(got)-1].Description != "parent directory" {
			t.Fatal("parent navigation no longer follows ordinary matches")
		}
	}
}

type sidebarCountingSession struct {
	extension.Session
	calls int
}

func (s *sidebarCountingSession) Sidebar(ctx *render.SidebarContext, state session.State, dead bool) {
	s.calls++
	s.Session.Sidebar(ctx, state, dead)
}

func TestSidebarContextWorkDoesNotGrowWithHiddenTabs(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 140, 40
	tab := m.current()
	if len(tab.extensions) == 0 {
		t.Fatal("fixture needs an extension session")
	}
	counter := &sidebarCountingSession{Session: tab.extensions[0]}
	tab.extensions[0] = counter
	for len(m.tabs) < 10 {
		m.addTab("")
	}
	m.active, m.sidebarOffset = 0, 0
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX = m.mainWidth() + 3
	m.sidebarHistory.pointerY = m.sidebarLayout().SessionsStart
	counter.calls = 0
	m.View()
	first := counter.calls
	for len(m.tabs) < 50 {
		m.addTab("")
	}
	m.active, m.sidebarOffset = 0, 0
	counter.calls = 0
	m.View()
	if counter.calls != first {
		t.Fatalf("hidden tabs repeated sidebar work: %d calls for 10 tabs, %d for 50", first, counter.calls)
	}
	layout := m.sidebarLayout()
	if got := m.sidebarSessionHoverIndex(layout); got != 0 {
		t.Fatalf("visible row hover changed: %d", got)
	}
	m.sidebarHistory.pointerY = layout.SessionsStart + layout.Capacity*render.SidebarSessionRows
	if got := m.sidebarSessionHoverIndex(layout); got != -1 {
		t.Fatalf("hover outside the visible session cards: %d", got)
	}

	m.gitCwd, m.gitChecked = m.tabDirectory(tab), time.Now()
	m.gitInfo = &render.GitStatus{Branch: "hotpath-branch"}
	counter.calls = 0
	if !strings.Contains(m.inputFooter(tab).text, "hotpath-branch") {
		t.Fatal("footer lost the current directory's git branch")
	}
	if cmd := m.requestGit(m.gitChecked); cmd != nil {
		t.Fatal("fresh git status was not throttled")
	}
	if counter.calls != 0 {
		t.Fatal("simple git/footer lookup rebuilt sidebar context")
	}
	m.gitCwd = "other-directory"
	if strings.Contains(m.inputFooter(tab).text, "hotpath-branch") {
		t.Fatal("footer used git status for a different directory")
	}
}

func TestSidebarSummariesPreserveIdentityAndPrecedence(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.state.File, first.state.ID, first.state.Name = "/session/a/../one.jsonl", "id", "first"
	first.origin = session.Header{ID: "id", ParentSession: "/parent.jsonl"}
	second := m.addTab("/session/one.jsonl")
	second.state.ID, second.state.Name = "id", "second"
	m.sidebarHistory.sessions = []session.Summary{
		{Path: "/session/one.jsonl", ID: "id", Name: "disk"},
		{Path: "/session/one.jsonl", ID: "other", Name: "other identity"},
		{Path: "", ID: "id", Name: "empty path"},
	}
	before := append([]session.Summary(nil), m.sidebarHistory.sessions...)
	got := m.sidebarSummaries()
	if got[0].Name != "first" || got[0].ParentSession != "/parent.jsonl" || got[1].Name != "other identity" || got[2].Name != "empty path" {
		t.Fatalf("identity or first-tab precedence changed: %+v", got)
	}
	if !reflect.DeepEqual(before, m.sidebarHistory.sessions) {
		t.Fatal("presentation merge modified stored summaries")
	}
}

func TestWorkingFinishedPrefilterPreservesContentChecks(t *testing.T) {
	var w workingState
	now := time.Unix(1, 0)
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, workingFinal(1, 4), now)
	// The same timestamp alone is not a completed-message identity: new
	// content must still be counted, even with a misleading delta attached.
	workingApply(&w, `{"type":"message_update","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"abcdefgh"}]},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"wrong"}}`, now)
	if !w.Current || (w.view().Output-w.Total.Output) != 2 {
		t.Fatal("timestamp collision or unverified delta replaced the snapshot")
	}
	workingApply(&w, `{"type":"message_update","message":{"role":"assistant","timestamp":2,"content":[{"type":"text","text":"abcdefghijkl"}]}}`, now)
	workingApply(&w, workingFinal(1, 6), now)
	if !w.Current || w.CurrentTimestamp != "2" || (w.view().Output-w.Total.Output) != 3 || w.Total.Output != 6 {
		t.Fatal("late usage correction disturbed the current message")
	}
	workingApply(&w, `{"type":"message_update","message":{"role":"assistant","timestamp":1,"content":[{"text":"completed","type":"text"}]}}`, now)
	if w.CurrentTimestamp != "2" || (w.view().Output-w.Total.Output) != 3 {
		t.Fatal("canonical duplicate reopened an earlier message")
	}
}

func TestWorkingFinishedIdentityCanonicalizesNullTimestamp(t *testing.T) {
	var w workingState
	now := time.Unix(1, 0)
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"output":2}}}`, now)
	workingApply(&w, `{"type":"message_update","message":{"role":"assistant","timestamp":null,"content":[{"text":"done","type":"text"}]}}`, now)
	if w.Current || w.Total.Output != 2 || w.Turn != 1 {
		t.Fatal("missing versus null timestamp bypassed canonical deduplication")
	}
}

func BenchmarkUIFileChoicesTopPage(b *testing.B) {
	m := &Model{opts: Options{Cwd: "/synthetic"}, tabs: []*tab{{}}}
	mention := fileMention{}
	key, _, _, _, _ := fileRequest(m.tabDirectory(m.current()), mention)
	m.files.cache = map[string]fileCache{key: {entries: hotpathFiles(10000)}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.fileChoices(mention)
	}
}

func BenchmarkUIWorkingSnapshot(b *testing.B) {
	var w workingState
	now := time.Unix(1, 0)
	w.apply(json.RawMessage(`{"type":"agent_start"}`), now)
	w.apply(json.RawMessage(workingFinal(1, 4)), now)
	raw := json.RawMessage(fmt.Sprintf(`{"type":"message_update","message":{"role":"assistant","timestamp":2,"content":[{"type":"text","text":%q}]}}`, strings.Repeat("x", 1<<20)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.apply(raw, now)
	}
}
