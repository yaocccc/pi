package extensions

import (
	"encoding/json"
	"testing"

	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/hostui"
)

func TestInstalledExtensionsExposeOnlyFreshInterfaceList(t *testing.T) {
	var factory func() []extension.Extension = New
	first, second := factory(), factory()
	if len(first) != 3 || len(second) != 3 {
		t.Fatal("missing fixed capability implementations")
	}
	first[0] = nil
	if second[0] == nil {
		t.Fatal("factory exposed a mutable global slice")
	}
	runtime := extension.New(second)
	if runtime.NewInspectorContent(session.Item{Role: "tool", ToolName: "unrelated"}, "foreign-key") != nil {
		t.Fatal("installed adapter claimed another provider's inspector")
	}
}

func TestSuiteCreatesIndependentProjectionsAndViews(t *testing.T) {
	a, b := extension.New(New()), extension.New(New())
	if a.Tools == b.Tools || a.Tools == nil || b.Tools == nil {
		t.Fatal("suites must own distinct renderer providers")
	}
	var first, second session.State
	first.SetExtensionProjection(a.NewProjection())
	second.SetExtensionProjection(a.NewProjection())
	first.Apply(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"plan","toolName":"update_plan","result":{"details":{"plan":[{"id":"a","goal":"first session","status":"pending"}]}}}`))
	if len(first.Plan) != 1 || len(second.Plan) != 0 || second.PlanRevision != 0 {
		t.Fatal("projections must be separate even within one suite")
	}
	if a.Tools.Policy == nil || a.Tools.Layout == nil || a.Tools.Detail == nil || a.Tools.FullDetail == nil {
		t.Fatal("suite did not assemble all renderer hooks")
	}
}

func TestSuiteBindsNestedAndFallbackInspectorsToSameViews(t *testing.T) {
	suite := extension.New(New())
	item := session.Item{ID: "nested", Role: "tool", ToolName: "worker", HasResult: true,
		Details: json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[{"index":0,"title":"Nested task","mode":"implement","preset":"deep","status":"completed"}]}`)}
	parent := suite.NewInspectorContent(item, "parent").(*hostui.Inspector)
	parent.Worker = &hostui.WorkerSession{
		Visible: []session.Item{item},
		Doc:     render.Document{Blocks: []render.Block{{ID: "task", InspectorParent: "nested", InspectorTask: "index:batch:0"}}},
	}
	id, child, ok := parent.Child("task")
	if !ok || id != "nested" || child == nil || child.(*hostui.Inspector).Tools != suite.Tools || child.(*hostui.Inspector).Fallback == "" {
		t.Fatalf("nested fallback lost assembly: id=%q child=%+v", id, child)
	}
	opened, ok := suite.NewInspector(item, "index:batch:0", true, nil)
	if !ok || opened.(*hostui.Inspector).Tools != suite.Tools {
		t.Fatal("top-level inspector lost assembly")
	}
}
