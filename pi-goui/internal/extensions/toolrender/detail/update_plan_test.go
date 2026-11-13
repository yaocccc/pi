package detail_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestUpdatePlanRecordedStepsAndEvidence(t *testing.T) {
	plan := `[{"id":"a","goal":"中文😀 goal\nsecond line","status":"completed","extra":9007199254740993123},{"id":"b","goal":"Working","status":"in_progress"},{"id":"c","goal":"Later","status":"pending"}]`
	item := session.Item{HasResult: true, Arguments: json.RawMessage(`{"steps":` + plan + `,"progress":{"verification":["tests passed"],"decisions":["keep unknown"]}}`), Details: json.RawMessage(`{"task_status":"active","plan":` + plan + `,"boundary":true,"completed_step_ids":["a"],"progress_recorded":true,"future":{"keep":true}}`), Text: `<sol-pi-plan task_status="active">{"steps":` + plan + `}</sol-pi-plan>` + "\nAdvice retained"}
	args, details := decodeMap(item.Arguments), decodeMap(item.Details)
	original := decodeMap(item.Details)
	width := 40
	rows := renderAdapter(t, "update_plan", item, args, details, width, testHooks(t))
	got := strings.Join(rows, "\n")
	for _, want := range []string{"✓ 1 · completed", "● 2 · in_progress", "○ 3 · pending", "中文😀 goal\nsecond line", "extra: 9007199254740993123", "tests passed", "Advice retained", "Progress recorded\nyes"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "<sol-pi-plan") {
		t.Fatal("duplicate machine snapshot")
	}
	if !reflect.DeepEqual(details["future"], original["future"]) || !reflect.DeepEqual(args["steps"], original["plan"]) {
		t.Fatal("mutated unknown/nested input")
	}
}

func TestUpdatePlanPreviewUnknownAndSnapshotSafety(t *testing.T) {
	for _, width := range []int{1, 2, 20, 40, 60} {
		item := session.Item{Running: true, Arguments: json.RawMessage(`{"steps":[{"goal":"Future","status":"unknown","id":"x"},null],"unknown":false}`), Text: "\nraw\n\n"}
		width := width
		rows := renderAdapter(t, "update_plan", item, decodeMap(item.Arguments), map[string]any{}, width, testHooks(t))
		got := strings.Join(rows, "\n")
		for _, want := range []string{"Plan preview · not recorded", "? 1 · unknown", "unknown: no", "\nraw\n\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("width %d: missing %q: %s", width, want, got)
			}
		}
	}
	for _, text := range []string{`<sol-pi-plan task_status="active">{"steps":[],"extra":true}</sol-pi-plan>`, `<sol-pi-plan task_status="active">{"steps":["different"]}</sol-pi-plan>`} {
		width := 40
		rows := renderAdapter(t, "update_plan", session.Item{Text: text}, map[string]any{}, decodeMap([]byte(`{"plan":[]}`)), width, testHooks(t))
		if !strings.Contains(strings.Join(rows, "\n"), text) {
			t.Fatal("discarded unfamiliar snapshot")
		}
	}
}
