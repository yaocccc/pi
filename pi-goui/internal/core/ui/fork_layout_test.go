package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func layoutForkFixture(t *testing.T, turns int) session.Fork {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "fork.jsonl")
	var b strings.Builder
	fmt.Fprintf(&b, "{\"type\":\"session\",\"id\":\"branch\",\"cwd\":%q}\n", root)
	b.WriteString(`{"type":"custom","id":"p","parentId":null,"customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":[{"id":"1","goal":"inspect","status":"completed"}]}}` + "\n")
	b.WriteString(`{"type":"model_change","id":"m","parentId":"p","provider":"test","modelId":"model"}` + "\n")
	b.WriteString(`{"type":"thinking_level_change","id":"a0","parentId":"m","thinkingLevel":"high"}` + "\n")
	for i := 1; i <= turns; i++ {
		fmt.Fprintf(&b, "{\"type\":\"message\",\"id\":\"u%d\",\"parentId\":\"a%d\",\"message\":{\"role\":\"user\",\"content\":\"question\"}}\n", i, i-1)
		fmt.Fprintf(&b, "{\"type\":\"message\",\"id\":\"a%d\",\"parentId\":\"u%d\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"provider\":\"test\",\"model\":\"model\",\"usage\":{\"input\":2,\"output\":1}}}\n", i, i)
	}
	fmt.Fprintf(&b, "{\"type\":\"session_info\",\"id\":\"name\",\"parentId\":\"a%d\",\"name\":\"Branch\"}\n", turns)
	writePreviewConfig(t, path, b.String())
	return session.Fork{ID: "branch", Path: path, Cwd: root, Name: "Branch", Origin: session.Header{ID: "branch", Cwd: root}}
}

func TestForkLayoutPublishesPreparedHistoryOnce(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	f := layoutForkFixture(t, 512)
	layout, err := m.forkLayoutReader(f.Cwd)(f)
	if err != nil {
		t.Fatal(err)
	}
	source := m.current()
	source.state.Model = layout.state.Model
	source.state.Items = append([]session.Item(nil), layout.state.Items...)
	source.state.Items[len(source.state.Items)-1].Text = "SOURCE-ONLY OVERLAY"
	m.document(source)
	sourceText := source.doc.FullText()
	branch, cmd, err := m.openForkLayout(source, f, layout)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	if branch.state.ID != f.ID || branch.state.Name != f.Name || branch.state.Cwd != f.Cwd || branch.state.Model.ID != "model" || branch.state.Thinking != "high" || branch.state.Stats.Input != 1024 || len(branch.state.Plan) != 1 || branch.state.Plan[0].Status != "completed" {
		t.Fatalf("lost branch metadata: model=%v thinking=%s input=%d plan=%v", branch.state.Model, branch.state.Thinking, branch.state.Stats.Input, branch.state.Plan)
	}
	if branch.state.MessageCount != 1024 || !branch.resume.preview || branch.cycle.phase != cycleIdle || branch.cycle.previewPending || !branch.cycle.probePending || branch.client != nil {
		t.Fatal("must publish all entries without starting a writer or a second preview")
	}
	job := branch.documentJob
	if job == nil || !branch.streamDirty || branch.doc.FullText() != "" {
		t.Fatal("first frame formatted the entire transcript synchronously")
	}
	m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	if branch.documentJob != job || branch.doc.FullText() != "" {
		t.Fatal("input forced full layout before background result")
	}
	m.receiveDocument(job.render().(documentResult))
	cold := render.Transcript(branch.state.Items, m.cfg.Theme, m.documentOptions(branch))
	if cold.FullText() == "" || branch.doc.FullText() != cold.FullText() || sourceText == "" || sourceText == cold.FullText() || source.doc.FullText() != sourceText {
		t.Fatal("seeded layout did not preserve independent fork and source documents")
	}
	probe := func() tea.Cmd {
		stamp, err := previewStamp(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		return m.sessionPresence(sessionPresenceMsg{owner: branch, file: f.Path, generation: branch.cycle.generation, sequence: branch.cycle.probeSequence, stamp: stamp})
	}
	probe()
	if branch.cycle.previewPending {
		t.Fatal("first presence poll rereads unchanged fork history")
	}
	w, err := os.OpenFile(f.Path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.WriteString(`{"type":"message","id":"new","parentId":"name","message":{"role":"user","content":"new"}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	refresh := probe()
	if !branch.cycle.previewPending || branch.client != nil {
		t.Fatal("changed history did not request an ordinary idle refresh")
	}
	for _, msg := range resumeCommandMessages(refresh) {
		if v, ok := msg.(resumePreviewMsg); ok {
			m.receiveResumePreview(v)
		}
	}
	if branch.state.MessageCount != 1025 || branch.cycle.previewPending || branch.state.ID != f.ID {
		t.Fatal("ordinary idle refresh did not adopt the appended entry")
	}
}

func TestForkLayoutRejectsDifferentSessionID(t *testing.T) {
	m := testModel(t, "")
	f := layoutForkFixture(t, 1)
	f.ID = "other-session"
	if layout, err := m.forkLayoutReader(f.Cwd)(f); err == nil || layout != nil {
		t.Fatal("replaced file with different session ID prepared as original fork")
	}
}

func TestForkLayoutRejectsStalePreparedFile(t *testing.T) {
	for _, change := range []string{"append", "mtime", "inode"} {
		t.Run(change, func(t *testing.T) {
			m := testModel(t, "")
			f := layoutForkFixture(t, 1)
			layout, err := m.forkLayoutReader(f.Cwd)(f)
			if err != nil {
				t.Fatal(err)
			}
			before := m.current()
			stamp := layout.stamp.info.ModTime()
			switch change {
			case "append":
				w, err := os.OpenFile(f.Path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = w.WriteString("\n")
				if err != nil {
					t.Fatal(err)
				}
				if err = w.Close(); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				if err := os.Chtimes(f.Path, stamp, stamp.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			case "inode":
				raw, err := os.ReadFile(f.Path)
				if err != nil {
					t.Fatal(err)
				}
				replacement := f.Path + ".new"
				if err = os.WriteFile(replacement, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Chtimes(replacement, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(replacement, f.Path); err != nil {
					t.Fatal(err)
				}
			}
			if branch, _, err := m.openForkLayout(before, f, layout); err == nil || branch != nil || m.current() != before {
				t.Fatal("stale preview opened a new tab")
			}
		})
	}
}
