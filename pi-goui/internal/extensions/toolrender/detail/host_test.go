package detail

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/session"
	"strings"
	"testing"
)

func TestUnknownViewLeavesGenericFallbackUntouched(t *testing.T) {
	rows := []string{"original"}
	renderer := New(extensionapi.DetailHost{Rows: &rows})
	details := map[string]any{"unknown": "kept"}
	if renderer.Render("future_extension", session.Item{}, nil, details) || len(rows) != 1 || details["unknown"] != "kept" {
		t.Fatal("unknown view was consumed")
	}
}

func TestIsolatedWorkerSnapshotIdentityAndPrivacy(t *testing.T) {
	item := session.Item{ToolName: "functions.worker", Text: "OTHER_TASK_SECRET", Details: json.RawMessage(`{"kind":"worker-ui","batchId":"b","snapshotAt":42,"controlErrors":[{"message":"OTHER_TASK_SECRET"}],"tasks":[{"index":7,"objective":"chosen","future":"KEPT"},{"index":9,"objective":"other"}]}`)}
	tasks := WorkerTasks(item)
	if len(tasks) != 2 || tasks[0].Key != "index:b:7" || tasks[0].Title != "chosen" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	if tasks[0].Item.Text != "" || strings.Contains(string(tasks[0].Item.Details), "OTHER_TASK_SECRET") || !strings.Contains(string(tasks[0].Item.Details), "KEPT") {
		t.Fatal("snapshot lost unknown fields or leaked another task")
	}
	item.Details = json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":7},{"index":7}]}`)
	for _, task := range WorkerTasks(item) {
		if task.Key != "" {
			t.Fatal("ambiguous identity must not offer inspection")
		}
	}
}
