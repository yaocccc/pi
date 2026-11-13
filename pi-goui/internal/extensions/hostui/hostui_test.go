package hostui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"pi-goui/internal/core/input"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/codexfast"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAdapterHasNoHostUIImport(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", entry.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range f.Imports {
			path, _ := strconv.Unquote(item.Path.Value)
			if path == "pi-goui/internal/core/ui" || strings.HasPrefix(path, "pi-goui/internal/core/ui/") {
				t.Fatalf("%s imports host implementation %s", entry.Name(), path)
			}
		}
	}
}
func TestQuestionnaireIsUsableWithoutApplicationModel(t *testing.T) {
	editor := input.New()
	d := &Dialog{Editor: &editor}
	var response map[string]any
	resized := 0
	h := DialogHost{Resize: func() { resized++ }, Close: func(v map[string]any) tea.Cmd { response = v; return nil }}
	if !h.OpenQuestionnaire(d, `{"questions":[{"question":"Choice?","options":["A","B"]}]}`) {
		t.Fatal("not opened")
	}
	d.Selected = 1
	h.SubmitQuestion(d)
	var answer struct {
		Answers []struct {
			Answers   []string
			Completed bool
		}
		Cancelled bool
	}
	if err := json.Unmarshal([]byte(response["value"].(string)), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Cancelled || len(answer.Answers) != 1 || !answer.Answers[0].Completed || answer.Answers[0].Answers[0] != "B" || resized == 0 {
		t.Fatalf("%+v", answer)
	}
}
func TestCodexDisplayStateIsPerBackend(t *testing.T) {
	a := CodexState{State: codexfast.State{Level: "normal", Levels: []string{"normal", "fast"}}}
	b := a
	if !a.Event(`pi-goui:event:v1:{"name":"codex.settings-saved","data":{"level":"fast"}}`, func(string) {}) {
		t.Fatal("event")
	}
	if a.Mode(false) != "fast" || b.Mode(false) != "normal" {
		t.Fatal("state leaked")
	}
	a.Apply(CodexResult{Version: 0, State: b.State}, false, func(string) {})
	if a.State.Level != "fast" {
		t.Fatal("late reply overwrote a newer fact")
	}
	if (CodexView{}).Visible() {
		t.Fatal("missing capability must stay hidden")
	}
}
func TestMissingTranscriptUsesUnavailableResultNotFiles(t *testing.T) {
	item := session.Item{Details: json.RawMessage(`{"tasks":[{"transcript":{"version":1,"id":"11111111-1111-4111-8111-111111111111"},"status":"running"}]}`)}
	w, reason := NewWorkerSession(item, false, nil)
	if reason != "" {
		t.Fatal(reason)
	}
	cmd, reason := w.Poll(item, time.Now())
	if cmd == nil || reason != "" {
		t.Fatal("missing poll")
	}
	result := cmd().(WorkerTranscriptResult)
	if result.Owner != w || result.Err == nil {
		t.Fatal("missing backend was not unavailable")
	}
	_, reason = w.Receive(result, &item)
	if reason != "Worker 会话读取能力不可用" || w.Unavailable(reason) {
		t.Fatalf("fallback: %s", reason)
	}
}
