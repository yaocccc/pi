package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestToolActivityCardLifecycleAndCache(t *testing.T) {
	items := []session.Item{
		{ID: "a", Role: "tool", ToolName: "oldest", HasResult: true, Text: "old result"},
		{ID: "b", Role: "tool", ToolName: "second", HasResult: true, Text: "short summary"},
		{ID: "c", Role: "tool", ToolName: "third", HasResult: true, Error: true, Text: "failed summary"},
		{ID: "d", Role: "tool", ToolName: "fourth", Running: true},
	}
	opts := Options{Width: 100, ShowToolActivity: true}
	var cache TranscriptCache
	render := func() Document {
		t.Helper()
		got := cache.Transcript(items, testTheme(), opts)
		if want := Transcript(items, testTheme(), opts); !reflect.DeepEqual(got, want) {
			t.Fatal("cached activity rendering differs")
		}
		return got
	}
	doc := render()
	plain := ansi.Strip(doc.Text)
	for _, want := range []string{"✓ second", "✗ third", "⋯ fourth"} {
		if !strings.Contains(plain, want) {
			t.Fatal("missing " + want)
		}
	}
	if strings.Contains(plain, "✓ oldest") || strings.Contains(plain, "Running") || strings.Contains(plain, "……") {
		t.Fatal("activity must be one card with only latest three calls")
	}
	if len(doc.Blocks) != 4 || doc.Blocks[0].Expanded {
		t.Fatal("activity must keep the group collapsed and expose three tool fold targets")
	}
	items[3].Running = false
	items[3].HasResult = true
	if !strings.Contains(ansi.Strip(render().Text), "✓ fourth") {
		t.Fatal("status update missing")
	}
	opts.Expanded = map[string]bool{"tool-group:a": false}
	if len(render().ActivityRows) == 0 {
		t.Fatal("manual collapse hid independent activity")
	}
	opts.Expanded["tool-group:a"] = true
	if len(render().ActivityRows) == 0 {
		t.Fatal("explicit expansion hid independent activity")
	}
	opts.Expanded = nil
	opts.ExpandTools = true
	if len(render().ActivityRows) == 0 {
		t.Fatal("global expansion hid independent activity")
	}
	opts.ExpandTools = false
	opts.ShowToolActivity = false
	if len(render().ActivityRows) > 0 {
		t.Fatal("saved history showed live activity")
	}
	opts.ShowToolActivity = true
	items = append(items, session.Item{ID: "thought", Role: "thinking", Text: "thinking"})
	opts.ShowThinking = true
	if len(render().ActivityRows) == 0 {
		t.Fatal("thinking ended activity before prose")
	}
	items = append(items, session.Item{ID: "reply", Role: "assistant", Text: "Here is the explanation"})
	if len(render().ActivityRows) > 0 {
		t.Fatal("prose did not end activity")
	}
	items = append(items, session.Item{ID: "e", Role: "tool", ToolName: "newcall", Running: true})
	if plain := ansi.Strip(render().Text); strings.Contains(plain, "Running") || !strings.Contains(plain, "⋯ newcall") || strings.Contains(plain, "✓ second") {
		t.Fatal("new group did not replace activity")
	}
	// A later section must also invalidate the prior section's activity cache.
	items = append(items, session.Item{ID: "u", Role: "user", Text: "new request"})
	if len(render().ActivityRows) > 0 {
		t.Fatal("new user message left stale activity")
	}
}

func TestLiveCallsPreviewAndExpansionAreExclusive(t *testing.T) {
	items := []session.Item{
		{ID: "a", Role: "tool", ToolName: "oldest", HasResult: true},
		{ID: "b", Role: "tool", ToolName: "second", HasResult: true},
		{ID: "c", Role: "tool", ToolName: "third", HasResult: true},
		{ID: "d", Role: "tool", ToolName: "fourth", Running: true},
	}
	for _, expanded := range []bool{false, true} {
		opts := Options{Width: 100, ShowToolActivity: true, Expanded: map[string]bool{"tool-group:a": expanded}}
		doc := Transcript(items, testTheme(), opts)
		plain := ansi.Strip(doc.Text)
		rows := strings.Split(plain, "\n")
		group := blockByID(t, doc, "tool-group:a")
		if strings.Trim(rows[group.End], " │") == "" {
			t.Fatal("blank line between header and calls")
		}
		if strings.Contains(plain, "✓ oldest") != expanded {
			t.Fatal("expanded state did not control complete call list")
		}
		for _, label := range []string{"✓ second", "✓ third", "⋯ fourth"} {
			if strings.Count(plain, label) != 1 {
				t.Fatalf("duplicated or missing call %q: %s", label, plain)
			}
		}
		if expanded && strings.Trim(rows[group.End], " │") == "…" {
			t.Fatal("expanded group retained omission row")
		}
	}
}

func TestExpandedLiveGroupOnlyDefaultsRecentWorkersOpen(t *testing.T) {
	old, recent := workerFixture(), workerFixture()
	old.ID, recent.ID = "old-worker", "recent-worker"
	items := []session.Item{old,
		{ID: "a", Role: "tool", ToolName: "read"},
		{ID: "b", Role: "tool", ToolName: "bash"},
		{ID: "c", Role: "tool", ToolName: "edit"}, recent}
	for _, global := range []bool{false, true} {
		opts := Options{Width: 120, ShowToolActivity: true, ExpandTools: global, Expanded: map[string]bool{"tool-group:old-worker": true}}
		doc := Transcript(items, testTheme(), opts)
		if blockByID(t, doc, old.ID).Expanded || !blockByID(t, doc, recent.ID).Expanded {
			t.Fatal("only recent Worker should default open")
		}
		for _, id := range []string{"a", "b", "c"} {
			blockByID(t, doc, id)
		}
		if len(opts.Expanded) != 1 {
			t.Fatal("default state mutated explicit folds")
		}
		opts.Expanded[old.ID], opts.Expanded[recent.ID] = true, false
		doc = Transcript(items, testTheme(), opts)
		if !blockByID(t, doc, old.ID).Expanded || blockByID(t, doc, recent.ID).Expanded {
			t.Fatal("manual Worker folds must override recent-call defaults")
		}
	}
}

func TestToolActivityDefaultsFileChangesOpen(t *testing.T) {
	for _, name := range []string{"write", "edit"} {
		for _, groupOpen := range []bool{false, true} {
			items := []session.Item{
				{ID: "old", Role: "tool", ToolName: name},
				{ID: "read", Role: "tool", ToolName: "read"},
				{ID: "bash", Role: "tool", ToolName: "bash"},
				{ID: "recent", Role: "tool", ToolName: name, Running: true},
			}
			opts := Options{Width: 100, ShowToolActivity: true, Expanded: map[string]bool{"tool-group:old": groupOpen}}
			doc := Transcript(items, testTheme(), opts)
			if !blockByID(t, doc, "recent").Expanded {
				t.Fatalf("%s should default open (group open=%v)", name, groupOpen)
			}
			if groupOpen && blockByID(t, doc, "old").Expanded {
				t.Fatalf("older %s should not default open", name)
			}
			if blockByID(t, doc, "read").Expanded || blockByID(t, doc, "bash").Expanded {
				t.Fatal("unrelated tools should remain collapsed")
			}
			opts.Expanded["recent"] = false
			if blockByID(t, Transcript(items, testTheme(), opts), "recent").Expanded {
				t.Fatalf("%s ignored manual collapse", name)
			}
			delete(opts.Expanded, "recent")
			opts.ShowToolActivity = false
			opts.Expanded["tool-group:old"] = true
			items[3].Running = false
			if blockByID(t, Transcript(items, testTheme(), opts), "recent").Expanded {
				t.Fatalf("%s remained auto-expanded after activity ended", name)
			}
		}
	}
}

func TestToolActivityUsesAvailableWidth(t *testing.T) {
	for _, name := range []string{"edit", "write", "read"} {
		item := session.Item{ID: "tool", Role: "tool", ToolName: name, Running: true}
		for _, width := range []int{1, 20, 80, 120, 180} {
			opts := Options{ExpandTools: true, Expanded: map[string]bool{item.ID: true}}
			rows, _ := toolActivityLayout([]session.Item{item}, testTheme(), opts, width)
			indent := min(2, width-1)
			want, _ := toolLayout(item, testTheme(), opts, width-indent)
			if len(rows) != len(want) {
				t.Fatalf("%s at width %d: different layout height", name, width)
			}
			for i := range rows {
				if rows[i] != strings.Repeat(" ", indent)+want[i] {
					t.Fatalf("%s at width %d: row %d differs from full-width layout", name, width, i)
				}
			}
		}
	}
}

func TestToolActivityOmissionRow(t *testing.T) {
	for _, count := range []int{1, 3, 4, 10} {
		items := make([]session.Item, count)
		for i := range items {
			items[i] = session.Item{Role: "tool", ToolName: "read"}
		}
		rows, _ := toolActivityLayout(items, testTheme(), Options{}, 80)
		if strings.ContainsAny(ansi.Strip(strings.Join(rows, "\n")), "╭╮╰╯│─") {
			t.Fatal("activity must not have a card border")
		}
		first := strings.Trim(ansi.Strip(rows[0]), " │")
		if (first == "…") != (count > 3) {
			t.Fatalf("count %d: first row %q", count, first)
		}
		want := min(count, 3)
		if count > 3 {
			want++
		}
		if len(rows) != want {
			t.Fatalf("count %d: got %d rows, want %d", count, len(rows), want)
		}
	}
}

func TestRunningActivityIncludesFullActiveWorker(t *testing.T) {
	worker := workerFixture()
	worker.Running = true
	items := []session.Item{{ID: "old", Role: "tool", ToolName: "old-call"}, worker,
		{ID: "a", Role: "tool", ToolName: "read"}, {ID: "b", Role: "tool", ToolName: "bash"}}
	opts := Options{Width: 120, ShowToolActivity: true, Expanded: map[string]bool{"tool-group:old": false}}
	doc := Transcript(items, testTheme(), opts)
	plain := ansi.Strip(doc.Text)
	for _, want := range []string{"╭─ worker ", "Improve worker cards", "Implemented task cards", "Verify widths 中文", "Missing fixture", "≡ Activity"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing Worker detail %q: %s", want, plain)
		}
	}
	if doc.Blocks[0].Expanded || !blockByID(t, doc, worker.ID).Expanded {
		t.Fatal("preview must keep outer group folded and default Worker open")
	}
	opts.Expanded[worker.ID] = false
	folded := Transcript(items, testTheme(), opts)
	if blockByID(t, folded, worker.ID).Expanded || strings.Contains(folded.Text, "Implemented task cards") {
		t.Fatal("preview ignored manual Worker collapse")
	}
	delete(opts.Expanded, worker.ID)
	for _, width := range []int{1, 4, 8, 20, 80, 160} {
		opts.Width = width
		checkWidth(t, Transcript(items, testTheme(), opts).Text, width)
	}
	items[1].Running = false
	if !strings.Contains(Transcript(items, testTheme(), opts).Text, "Implemented task cards") {
		t.Fatal("completed Worker within latest three must stay expanded")
	}
	items = append(items, session.Item{ID: "c", Role: "tool", ToolName: "edit", Running: true})
	for _, running := range []bool{true, false} {
		items[1].Running = running
		if strings.Contains(Transcript(items, testTheme(), opts).Text, "Implemented task cards") {
			t.Fatal("Worker outside latest three must be omitted regardless of status")
		}
	}
	items = []session.Item{worker}
	items[0].Running = false
	if !strings.Contains(Transcript(items, testTheme(), opts).Text, "Implemented task cards") {
		t.Fatal("completed Worker must stay expanded until activity ends")
	}
	items = append(items, session.Item{ID: "answer", Role: "assistant", Text: "Final conclusion"})
	if strings.Contains(Transcript(items, testTheme(), opts).Text, "Implemented task cards") {
		t.Fatal("Worker card remained after activity ended")
	}
	items = items[:1]
	opts.ShowToolActivity = false
	if strings.Contains(Transcript(items, testTheme(), opts).Text, "Implemented task cards") {
		t.Fatal("Worker card remained when the session became idle")
	}
}

func TestToolActivityNarrowGeometry(t *testing.T) {
	items := []session.Item{{ID: "t", Role: "tool", ToolName: "工具", Text: strings.Repeat("界👩‍💻", 30), Running: true}}
	for _, width := range []int{1, 2, 4, 8, 12, 20, 80, 160} {
		doc := Transcript(items, testTheme(), Options{Width: width, ShowToolActivity: true})
		checkWidth(t, doc.Text, width)
	}
}
