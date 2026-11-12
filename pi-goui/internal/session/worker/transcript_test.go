package worker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const transcriptTestID = "12345678-1234-4234-8234-123456789abc"

func transcriptFixture(t *testing.T) (string, string, Transcript) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "worker-transcripts")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, transcriptTestID+".jsonl")
	return filepath.Join(dir, "agent"), path, Transcript{Version: 1, ID: transcriptTestID}
}
func transcriptHeader() string {
	return `{"type":"worker_transcript_header","version":1,"id":"` + transcriptTestID + `","cwd":"/tmp","entries":[]}` + "\n"
}
func putTranscript(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWorkerTranscriptIncrementalPartialAndReplacement(t *testing.T) {
	dir, path, d := transcriptFixture(t)
	user := `{"type":"message_end","message":{"role":"user","content":"中文 🧪","timestamp":1}}`
	putTranscript(t, path, transcriptHeader()+user[:45])
	c, b, e := (Cursor{}).Read(dir, d)
	if e != nil || !b.Reset || len(b.Records) != 1 || !c.Partial() {
		t.Fatalf("first: %+v %v", b, e)
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString(user[45:] + "\n")
	_ = f.Close()
	c, b, e = c.Read(dir, d)
	if e != nil || b.Reset || len(b.Records) != 1 || c.Partial() || string(b.Records[0]) != user {
		t.Fatalf("append: %+v %v", b, e)
	}
	_, b, e = c.Read(dir, d)
	if e != nil || len(b.Records) != 0 || b.Reset {
		t.Fatal("replayed unchanged log", e)
	}
	putTranscript(t, path, transcriptHeader())
	c, b, e = c.Read(dir, d)
	if e != nil || !b.Reset {
		t.Fatal("truncate not reset", e)
	}
	replacement := path + ".new"
	putTranscript(t, replacement, transcriptHeader()+user+"\n")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	_, b, e = c.Read(dir, d)
	if e != nil || !b.Reset || len(b.Records) != 2 {
		t.Fatal("replace not reset", e)
	}
}
func TestWorkerTranscriptSecurityAndLargeRecords(t *testing.T) {
	dir, path, d := transcriptFixture(t)
	for _, bad := range []Transcript{{1, "../secret", false, ""}, {2, d.ID, false, ""}, {1, "abc", false, ""}} {
		if _, _, err := (Cursor{}).Read(dir, bad); err == nil {
			t.Fatal("accepted bad descriptor")
		}
	}
	if _, _, err := (Cursor{}).Read(dir, d); err == nil {
		t.Fatal("missing file accepted")
	}
	other := filepath.Join(t.TempDir(), "private")
	putTranscript(t, other, transcriptHeader())
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Cursor{}).Read(dir, d); err == nil {
		t.Fatal("symlink file accepted")
	}
	_ = os.Remove(path)
	raw, _ := json.Marshal(map[string]any{"type": "message_end", "message": map[string]any{"role": "user", "content": strings.Repeat("x", 5<<20), "timestamp": 1}})
	putTranscript(t, path, transcriptHeader()+string(raw)+"\n")
	c, b, err := (Cursor{}).Read(dir, d)
	if err != nil || !b.More || len(b.Records) != 1 {
		t.Fatal("first large chunk", err)
	}
	c, b, err = c.Read(dir, d)
	if err != nil || b.More || len(b.Records) != 1 || len(b.Records[0]) < 5<<20 || c.Partial() {
		t.Fatal("large record lost", err)
	}
	root := filepath.Join(t.TempDir(), "worker-transcripts")
	if err := os.Symlink(filepath.Dir(path), root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Cursor{}).Read(filepath.Join(filepath.Dir(root), "agent"), d); err == nil {
		t.Fatal("symlink root accepted")
	}
	putTranscript(t, path, strings.Replace(transcriptHeader(), d.ID, "00000000-0000-0000-0000-000000000000", 1))
	if _, _, err := (Cursor{}).Read(dir, d); err == nil {
		t.Fatal("foreign header accepted")
	}
}
func TestWorkerTranscriptRealChildEvents(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	output, err := exec.Command(node, "../../../../extensions/worker/fixtures/transcript-child.mjs", "complete", "real prompt").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir, path, d := transcriptFixture(t)
	var data strings.Builder
	data.WriteString(transcriptHeader())
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		var e struct{ Type string }
		if json.Unmarshal([]byte(line), &e) != nil {
			t.Fatal("fixture malformed")
		}
		if e.Type != "session" {
			data.WriteString(line + "\n")
		}
	}
	data.WriteString(`{"type":"worker_transcript_end","status":"completed","complete":true}` + "\n")
	putTranscript(t, path, data.String())
	_, b, err := (Cursor{}).Read(dir, d)
	if err != nil {
		t.Fatal(err)
	}
	var replay Replay
	replay.Apply(ReplayRead{Batch: b})
	if !replay.Stopped || replay.Status != "completed" {
		t.Fatal("real child recording did not finish")
	}
	text := ""
	for _, item := range replay.View().Items {
		text += item.Text + "\n"
	}
	for _, want := range []string{"real prompt", "中文 🧪", "full content"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
