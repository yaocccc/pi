package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func inputList(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- path-%02d.go\n", i)
	}
	return b.String()
}

func TestWorkerInputContractFormatting(t *testing.T) {
	files := []string{}
	for i := 0; i < 12; i++ {
		files = append(files, fmt.Sprintf("file-%d.go", i))
	}
	raw, _ := json.Marshal(map[string]any{"objective": "修复 **问题**", "mode": "implement", "allowedPaths": files, "preExistingUserChanges": files, "acceptanceCriteria": []string{"tests pass"}})
	original := workerContractPrefix + string(raw) + "\n最终仅返回 JSON；格式：\n{\"status\":\"completed\"}"
	got := workerInputMarkdown(original)
	formatted, actions := workerInputItems([]session.Item{{ID: "contract", Role: "user", Text: original}}, nil)
	if len(actions) != 2 || strings.Contains(formatted[0].Text, "file-11.go") || !strings.Contains(formatted[0].Text, "展开剩余 7 项") {
		t.Fatal("actual contract file arrays not folded")
	}
	for _, want := range []string{"目标", "修复 **问题**", "允许修改", "已有改动", "file-11.go", "tests pass", "最终仅返回 JSON；格式：", "\"status\": \"completed\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, original := range []string{"normal **markdown**", `{"path":"a.go"}`, `{"objective":"work","allowedPaths":[`, `{"objective":"work"} garbage`} {
		if workerInputMarkdown(original) != original {
			t.Fatal("rewrote non-contract", original)
		}
	}
	got = workerInputMarkdown(`{"objective":"work","unknown":{"n":90071992547409931234},"allowedPaths":[1,null,{"a":true}]}`)
	for _, want := range []string{"90071992547409931234", "null", "true"} {
		if !strings.Contains(got, want) {
			t.Fatal("lost unknown value", want)
		}
	}
}

func TestWorkerInputListsPreserveBlocks(t *testing.T) {
	marker := func() string { return "\ue000" }
	for _, source := range []string{inputList(8), inputList(12) + "lazy continuation of last item", "```md\n" + inputList(12) + "```", "~~~\n" + inputList(12) + "~~~", "    - code\n    - code"} {
		got, actions := workerInputLists(source, "u", nil, marker)
		if got != source || len(actions) != 0 {
			t.Fatal("changed short list/code", got)
		}
	}
	source := inputList(12)
	folded, actions := workerInputLists(source, "u", nil, marker)
	if len(actions) != 1 || !strings.Contains(folded, "path-04.go") || strings.Contains(folded, "path-05.go") || !strings.Contains(folded, "展开剩余 7 项") {
		t.Fatal("bad folded list", folded)
	}
	expanded, actions := workerInputLists(source, "u", map[string]bool{actions[0].key: true}, marker)
	if !strings.Contains(expanded, "path-11.go") || !strings.Contains(expanded, "收起列表") {
		t.Fatal("missing expanded content")
	}
	// Appending items to a streamed list preserves identity and expansion.
	grown, grownActions := workerInputLists(source+"- added.go\n", "u", map[string]bool{actions[0].key: true}, marker)
	if grownActions[0].key != actions[0].key || !strings.Contains(grown, "added.go") || !strings.Contains(grown, "收起列表") {
		t.Fatal("streamed list lost expansion")
	}
	_, other := workerInputLists(source, "other-message", nil, marker)
	if other[0].key == actions[0].key {
		t.Fatal("messages share list state")
	}
	nested := ""
	for i := 0; i < 10; i++ {
		nested += fmt.Sprintf("%d. outer-%d\n   - inner-%d\n\n   continuation-%d\n", i+1, i, i, i)
	}
	got, _ := workerInputLists(nested, "u", nil, marker)
	if !strings.Contains(got, "inner-4") || !strings.Contains(got, "continuation-4") || strings.Contains(got, "inner-5") {
		t.Fatal("split nested item", got)
	}
}

func TestWorkerInputListInteractionAndCache(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, parent, _ := workerPageFixture(t)
			receiveWorkerCommand(t, m, m.openWorkerSession(parent, "w", "index:batch:0"))
			// Match the actual panel body width used by mouse coordinates.
			m.width = width + 40 + 2*m.inspectorInset()
			m.panelResize.inspector = width + 2*m.inspectorInset()
			m.height = 60
			w := m.inspector.worker
			original := "# Files\n\n" + inputList(12) + "\n# Other files\n\n" + inputList(10) + "\n展开剩余 7 项\n\ue000 user-supplied marker"
			w.visible = []session.Item{{ID: "input", Role: "user", Text: original, MessageIndex: 1}}
			w.revision++
			rows := m.inspectorRows()
			if len(w.inputActions) != 2 {
				t.Fatalf("no action metadata: %+v\n%s", w.inputActions, strings.Join(rows, "\n"))
			}
			if strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "path-11.go") {
				t.Fatal("list not collapsed")
			}
			again := m.inspectorRows()
			if &rows[0] != &again[0] {
				t.Fatal("static hover rebuilt document")
			}
			a := w.inputActions[0]
			// Scroll to the action rather than relying on auto-follow position.
			m.inspector.followPaused = true
			m.inspector.offset = min(max(0, a.rect.y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			left, _ := m.inspectorBounds()
			x, y := left+m.inspectorInset()+a.rect.x, 2+a.rect.y-m.inspector.offset
			if m.pointerShapeAt(x, y) != "pointer" {
				t.Fatal("no action pointer")
			}
			if _, ok := m.workerInputTarget(x-1, y); ok {
				t.Fatal("padding became target")
			}
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if !w.inputExpanded[a.key] || w.inputExpanded[w.inputActions[1].key] {
				t.Fatal("lists not independently toggled")
			}
			if !strings.Contains(ansi.Strip(w.doc.Text), "path-11.go") {
				t.Fatal("expanded content missing")
			}
			if w.visible[0].Text != original {
				t.Fatal("mutated original")
			}
			// Width reflow does not lose the fold preference.
			m.workerConversationRows(width + 1)
			if !w.inputExpanded[a.key] {
				t.Fatal("reflow reset fold")
			}
			m.workerConversationRows(width)
			a = w.inputActions[0]
			m.inspector.offset = min(max(0, a.rect.y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			x, y = left+m.inspectorInset()+a.rect.x, 2+a.rect.y-m.inspector.offset
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorMotion(tea.Mouse{Button: tea.MouseLeft, X: x + 1, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x + 1, Y: y})
			if !w.inputExpanded[a.key] {
				t.Fatal("drag selection toggled list")
			}
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if w.inputExpanded[a.key] {
				t.Fatal("collapse failed")
			}
			m.Update(tea.WindowSizeMsg{Width: 30, Height: 40})
			m.inspectorRows()
			a = w.inputActions[0]
			m.inspector.offset = min(max(0, a.rect.y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			x, y = m.inspectorInset()+a.rect.x, 2+a.rect.y-m.inspector.offset
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if !w.inputExpanded[a.key] {
				t.Fatal("fullscreen action failed")
			}
			m.closeInspector()
			receiveWorkerCommand(t, m, m.openWorkerSession(parent, "w", "index:batch:0"))
			if len(m.inspector.worker.inputExpanded) != 0 {
				t.Fatal("new task view inherited list folds")
			}
		})
	}
}
