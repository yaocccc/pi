package session

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"pi-goui/internal/extensions/sessionview"
	"strconv"
	"strings"
	"testing"
)

func TestNativeSessionKeepsOneExtensionCompositionBoundary(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, protocol := range []string{"update_plan", "sol-pi-", "web-search-content-ready", "livePlanEntry", "livePlanTool", "parsePlan", "setPlan"} {
			if strings.Contains(string(source), protocol) {
				t.Errorf("%s contains extension implementation %q", name, protocol)
			}
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "pi-goui/internal/extensions" || strings.HasPrefix(path, "pi-goui/internal/extensions/") {
				t.Errorf("session must use neutral projection injection: %s -> %s", name, path)
			}
		}
	}
}

func TestExtensionHistoryUsesReconciledResultDetails(t *testing.T) {
	var s State
	s.SetExtensionProjection(&sessionview.State{})
	root := planEntry("root", "null", pendingPlan)
	if err := s.LoadEntries(json.RawMessage(`{"entries":[` + root + `],"leafId":"root"}`)); err != nil {
		t.Fatal(err)
	}
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"toolResult","toolName":"update_plan","toolCallId":"live","details":{"plan":` + donePlan + `}}}`))
	revision := s.PlanRevision
	// Native history can omit details already known from the live result.
	result := `{"type":"message","id":"result","parentId":"root","message":{"role":"toolResult","toolName":"update_plan","toolCallId":"live","content":"saved"}}`
	if err := s.LoadEntries(json.RawMessage(`{"entries":[` + root + `,` + result + `],"leafId":"result"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 1 || s.Plan[0].Status != "completed" || s.PlanRevision != revision {
		t.Fatalf("reconciliation discarded live details: %#v revision=%d", s.Plan, s.PlanRevision)
	}
}

func TestInvalidHistoryCannotPartiallyInstallExtensionSnapshot(t *testing.T) {
	var s State
	s.SetExtensionProjection(&sessionview.State{})
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"toolResult","toolName":"update_plan","toolCallId":"live","details":{"plan":` + donePlan + `}}}`))
	revision := s.PlanRevision
	root := planEntry("old", "null", pendingPlan)
	invalid := `{"type":"message","id":"bad","parentId":"old","message":"not a message object"}`
	if err := s.LoadEntries(json.RawMessage(`{"entries":[` + root + `,` + invalid + `],"leafId":"bad"}`)); err == nil {
		t.Fatal("invalid native history was accepted")
	}
	if len(s.Plan) != 1 || s.Plan[0].Status != "completed" || s.PlanRevision != revision {
		t.Fatal("failed native load committed a provisional extension projection")
	}
}
