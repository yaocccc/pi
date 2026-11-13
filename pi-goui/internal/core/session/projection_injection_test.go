package session

import (
	"encoding/json"
	"testing"

	"pi-goui/internal/extensions/sessionview"
)

func TestSessionProjectionIsOptionalAndInstanceScoped(t *testing.T) {
	var enabled, other, native State
	first, second := &sessionview.State{}, &sessionview.State{}
	enabled.SetExtensionProjection(first)
	other.SetExtensionProjection(second)
	event := json.RawMessage(`{"type":"message_end","message":{"role":"toolResult","toolName":"update_plan","toolCallId":"one","details":{"plan":[{"id":"one","goal":"original","status":"pending"}]}}}`)
	enabled.Apply(event)
	native.Apply(event)
	if len(enabled.Plan) != 1 || len(other.Plan) != 0 || len(native.Plan) != 0 || native.PlanRevision != 0 {
		t.Fatalf("projection leaked or zero-state enabled it: enabled=%v other=%v native=%v", enabled.Plan, other.Plan, native.Plan)
	}
	if native.MessageCount != enabled.MessageCount || len(native.Items) != len(enabled.Items) {
		t.Fatal("optional projection altered native tool result preservation")
	}
	enabled.Plan[0].Goal = "changed by display consumer"
	if first.View().Plan[0].Goal != "original" {
		t.Fatal("display data aliases authoritative projection state")
	}
}

func TestSessionProjectionVisibilityDoesNotPoisonRecordCache(t *testing.T) {
	raw := json.RawMessage(`{"messages":[{"role":"custom","customType":"web-search-content-ready","content":[{"type":"text","text":"background"}]},{"role":"assistant","content":[{"type":"text","text":"visible"}]}]}`)
	var s State
	if err := s.LoadMessages(raw); err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 2 || s.MessageCount != 2 {
		t.Fatalf("native fallback lost records: %+v", s.Items)
	}
	s.SetExtensionProjection(&sessionview.State{})
	if len(s.Items) != 1 || s.Items[0].Text != "visible" || s.Items[0].MessageIndex != 2 || s.MessageCount != 2 {
		t.Fatalf("hidden record changed ordinals: %+v", s.Items)
	}
	s.SetExtensionProjection(nil)
	if len(s.Items) != 2 || s.Items[0].Text != "background" || s.Items[1].MessageIndex != 2 || s.MessageCount != 2 {
		t.Fatalf("detaching projection lost cached native output: %+v", s.Items)
	}
}
