package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func summaryFixture(cwd, name, parent, first string) []byte {
	header, _ := json.Marshal(Header{Type: "session", ID: "source", Cwd: cwd, ParentSession: parent,
		Fork: &ForkProvenance{Version: 1, SourceID: "parent", UserEntryID: "u", CutoffEntryID: "a", Turn: 1}})
	user, _ := json.Marshal(map[string]any{"type": "message", "id": "u", "message": map[string]any{"role": "user", "content": first}})
	info, _ := json.Marshal(map[string]any{"type": "session_info", "id": "name", "name": name})
	return []byte(string(header) + "\n" + string(user) + "\n" + string(info) + "\n")
}

func writeSummaryFixture(t testing.TB, root string, raw []byte) string {
	t.Helper()
	path := filepath.Join(root, "sessions", "cwd", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func onlySummary(t *testing.T, root string) Summary {
	t.Helper()
	got, err := List(root, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("List: len=%d err=%v", len(got), err)
	}
	return got[0]
}

func TestListSummaryFastPathMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, record, want string
	}{
		{"plain rename", `{"type":"session_info","name":"latest"}`, "latest"},
		{"escaped type", `{"type":"session_\u0069nfo","name":"escaped"}`, "escaped"},
		{"escaped key", `{"\u0074ype":"session_info","name":"escaped key"}`, "escaped key"},
		{"empty name", `{"type":"session_info","name":""}`, ""},
		{"message mentioning type", `{"type":"message","name":"wrong","message":{"role":"assistant","content":"session_info"}}`, "Original"},
		{"ordinary payload", `{"type":"message","name":"wrong","message":{"role":"toolResult","content":"large output"}}`, "Original"},
		{"duplicate type", `{"type":"message","type":"session_\u0069nfo","name":"last type"}`, "last type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			raw := append(summaryFixture(t.TempDir(), "Original", "/parent", "first"), []byte(tc.record+"\n")...)
			writeSummaryFixture(t, root, raw)
			if got := onlySummary(t, root); got.Name != tc.want || got.FirstMessage != "first" {
				t.Fatalf("summary = %+v, want name %q", got, tc.want)
			}
		})
	}
}
