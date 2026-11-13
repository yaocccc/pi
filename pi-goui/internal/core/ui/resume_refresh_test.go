package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func resumeThreadFixture() []session.Summary {
	return []session.Summary{
		{ID: "root", Path: "/root.jsonl", Name: "Root", Modified: 1},
		{ID: "child", Path: "/child.jsonl", Name: "Child", ParentSession: "/root.jsonl", Modified: 30},
		{ID: "grand", Path: "/grand.jsonl", Name: "Grand", ParentSession: "/child.jsonl", Modified: 40},
		{ID: "other", Path: "/other.jsonl", Name: "Other", Modified: 20},
	}
}

func TestResumePreservesAllThreadLevels(t *testing.T) {
	list := append(resumeThreadFixture(), session.Summary{ID: "great", Path: "/great.jsonl", Name: "Great", ParentSession: "/grand.jsonl", Modified: 110})
	for _, keyboard := range []bool{false, true} {
		for _, currentID := range []string{"child", "great"} {
			t.Run(fmt.Sprintf("keyboard=%v/current=%s", keyboard, currentID), func(t *testing.T) {
				m := testModel(t, `{"app.session.resume":["ctrl+y"]}`)
				current := m.current()
				for _, s := range list {
					if s.ID == currentID {
						current.state.ID, current.state.File, current.state.Name = s.ID, s.Path, s.Name
					}
				}
				current.sessionGuard.path = current.state.File
				current.sessionGuard.checked = time.Now()
				var cmd tea.Cmd
				if keyboard {
					cmd = m.key(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
				} else {
					var handled bool
					cmd, handled = m.slash(current, "/resume")
					if !handled {
						t.Fatal("resume not handled")
					}
				}
				if cmd == nil {
					t.Fatal("resume did not request sessions")
				}
				result := commandResult(cmd).(sessionsMsg)
				result.list, result.err = list, nil
				m.Update(result)
				d := current.dialogs[0]
				options := m.filtered(d)
				if len(options) != len(list) {
					t.Fatalf("resume hid a relationship node: got %d rows, want %d", len(options), len(list))
				}
				rows := ansi.Strip(strings.Join(m.popupRows(d, m.popupGeometry(d)), "\n"))
				for depth, id := range []string{"root", "child", "grand", "great"} {
					s := options[depth].Data.(session.Summary)
					if s.ID != id || !strings.HasPrefix(options[depth].Label, session.ThreadPrefix(depth)) {
						t.Fatalf("incorrect hierarchy at depth %d: %+v", depth, options[depth])
					}
					if !strings.Contains(rows, session.ThreadPrefix(depth)+s.Name) {
						t.Fatalf("popup lost indentation at depth %d: %q", depth, rows)
					}
					if id == currentID && (!strings.Contains(options[depth].Label, "[current]") || options[depth].Value != fmt.Sprintf("@%d", current.id)) {
						t.Fatal("current relationship node is not marked or targets another session")
					}
				}
			})
		}
	}
}

func TestResumeFreshDiskRelationshipsOverrideCachedHeader(t *testing.T) {
	m := testModel(t, "")
	child := m.current()
	child.state.ID, child.state.File, child.state.Name = "child", "/child.jsonl", "Live name"
	// The ID still matches, but the cached header predates the latest list read.
	child.origin = session.Header{ID: "child", ParentSession: "/old-parent.jsonl"}
	m.sessionDialog(child, resumeThreadFixture())
	rows := m.filtered(child.dialogs[0])
	if len(rows) != 4 || rows[0].Data.(session.Summary).ID != "root" || rows[1].Value != fmt.Sprintf("@%d", child.id) || !strings.HasPrefix(rows[1].Label, "↳ Live name") || !strings.HasPrefix(rows[2].Label, "  ↳ Grand") {
		t.Fatalf("cached header masked fresh relationships: %+v", rows)
	}
	if child.state.Name != "Live name" {
		t.Fatal("relationship refresh replaced the live session name")
	}
}

func TestResumeSeesNestedForksWithoutRestart(t *testing.T) {
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
	openResume := func() []option {
		t.Helper()
		current := m.current()
		current.dialogs = nil
		cmd, handled := m.slash(current, "/resume")
		if !handled || cmd == nil {
			t.Fatal("resume not requested")
		}
		result := commandResult(cmd).(sessionsMsg)
		if result.err != nil {
			t.Fatal(result.err)
		}
		m.Update(result)
		return m.filtered(current.dialogs[0])
	}
	if rows := openResume(); len(rows) != 1 {
		t.Fatalf("initial list = %+v", rows)
	}
	for depth := 1; depth <= 2; depth++ {
		parent := m.current()
		parent.dialogs = nil
		fork, err := session.CreateFork(parent.state.File, parent.state.ID, parent.state.Cwd, "Original", "new", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		m.forkCreated(prepareForkMessage(t, parent.id, fork)) // Do not start real RPC.
		rows := openResume()
		if len(rows) != depth+1 {
			t.Fatalf("fork depth %d not visible without restart: %+v", depth, rows)
		}
		for level, row := range rows {
			status := "open"
			if level == depth {
				status = "current"
			}
			if row.Label != session.ThreadPrefix(level)+"Original  ["+status+"]" {
				t.Fatalf("level %d has incorrect relationship: %+v", level, row)
			}
		}
	}
}
