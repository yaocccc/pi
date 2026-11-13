package ui

import (
	"encoding/json"
	"pi-goui/internal/core/extension"
	"testing"
	"time"
)

type usageContribution struct{}

func (usageContribution) Capabilities() extension.Capabilities {
	return extension.Capabilities{NewUsage: func() extension.Usage { return &hostUsageCounter{} }}
}

type hostUsageCounter struct{ observed int }

func (u *hostUsageCounter) Observe(json.RawMessage)    { u.observed++ }
func (u *hostUsageCounter) Totals() (float64, float64) { return float64(u.observed), 0 }

func TestHostAcceptsEmptyInterfaceList(t *testing.T) {
	m := testModel(t, "{}")
	m.extensions = extension.New(nil)
	tab := m.addTab("")
	if len(tab.extensions) != 0 || len(tab.working.extensions) != 0 || m.extensions.Tools != nil {
		t.Fatal("empty list installed a concrete implementation")
	}
	tab.state.Apply(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"external","toolName":"update_plan","result":{"details":{"plan":[{"id":"a","goal":"not installed","status":"pending"}]}}}`))
	if len(tab.state.Plan) != 0 {
		t.Fatal("implicit extension projection")
	}
	d := &dialog{kind: "input", title: "native"}
	if err := m.openExtensionDialog(tab, d, "native", "payload"); err != "" || d.extension.Handler != nil || d.title != "native" {
		t.Fatal("native modal changed")
	}
	if _, handled := m.extensionMessage("native"); handled {
		t.Fatal("native message intercepted")
	}
	m.document(tab)
}

func TestWorkingInterfaceFactoriesSurviveRunsAndDismissal(t *testing.T) {
	m := testModel(t, "{}")
	m.extensions = extension.New([]extension.Extension{usageContribution{}})
	first, second := m.addTab(""), m.addTab("")
	now := time.Unix(1, 0)
	workingApply(&first.working, `{"type":"agent_start"}`, now)
	initial := first.working.extensions[0].(*hostUsageCounter)
	workingApply(&first.working, `{"type":"tool_execution_end"}`, now)
	if initial.observed != 1 || second.working.extensions[0].(*hostUsageCounter).observed != 0 {
		t.Fatal("tab-local usage was omitted or shared")
	}
	workingApply(&first.working, `{"type":"agent_settled"}`, now)
	workingApply(&first.working, `{"type":"agent_start"}`, now)
	next := first.working.extensions[0].(*hostUsageCounter)
	if next == initial || next.observed != 0 {
		t.Fatal("new run retained old usage instance")
	}
	workingApply(&first.working, `{"type":"agent_settled"}`, now)
	first.dismissCompletedWorking()
	workingApply(&first.working, `{"type":"agent_start"}`, now)
	afterDismiss := first.working.extensions[0].(*hostUsageCounter)
	workingApply(&first.working, `{"type":"tool_execution_end"}`, now)
	if afterDismiss == next || afterDismiss.observed != 1 {
		t.Fatal("dismissal lost factory or reused run state")
	}
}
