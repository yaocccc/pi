package ui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-goui/internal/core/rpc"
)

// Fork preparation happens before returning to Update, so the fixture must
// supply its real native snapshot over RPC rather than replace it afterwards.
func forkFixtureClient(t *testing.T, snapshot json.RawMessage) (*rpc.Client, string) {
	t.Helper()
	cfg, wire := fakeConfig(t)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIGOUI_FORK_SNAPSHOT", path)
	script, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Bin, []byte(strings.ReplaceAll(string(script), "TestUIRPCProcess", "TestForkRPCProcess")), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, wire
}

func setForkFixtureSnapshot(t *testing.T, snapshot json.RawMessage) {
	t.Helper()
	if err := os.WriteFile(os.Getenv("PIGOUI_FORK_SNAPSHOT"), snapshot, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedForkRejectsReplacedBackend(t *testing.T) {
	m, source, _, _ := uiForkFixture(t)
	v := m.requestFork(source, "old")().(forkSnapshotMsg)
	write := m.forkSnapshot(v)
	if write == nil {
		t.Fatal("valid prepared request rejected")
	}
	created := write().(forkCreatedMsg)
	if created.err != nil || created.prepared == nil {
		t.Fatalf("fork result was not prepared: %v", created.err)
	}
	source.client, _ = fakeClient(t)
	if m.forkCreated(created) != nil || len(m.tabs) != 1 || source.forkPending {
		t.Fatal("stale creation opened a tab or retained the obsolete pending request")
	}
}

func TestPreparedForkSequenceCannotClearNewRequest(t *testing.T) {
	m, source, _, _ := uiForkFixture(t)
	old := m.requestFork(source, "old")().(forkSnapshotMsg)
	source.forkPending = false // Cancel before issuing a replacement request.
	newer := m.requestFork(source, "old")().(forkSnapshotMsg)
	if old.sequence == newer.sequence || m.forkSnapshot(old) != nil || !source.forkPending {
		t.Fatal("old preparation affected the replacement operation")
	}
	if m.forkSnapshot(newer) == nil {
		t.Fatal("replacement operation was lost")
	}
}

func TestPreparedForkRevalidatesEnrichedDetailsWithoutBlockingLaterStream(t *testing.T) {
	m, source, _, raw := uiForkFixture(t)
	var data struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	data.Entries[1] = json.RawMessage(strings.Replace(string(data.Entries[1]), `"parentId":"old"`, `"parentId":"result"`, 1))
	result := json.RawMessage(`{"type":"message","id":"result","parentId":"old","message":{"role":"toolResult","toolName":"bash","toolCallId":"call","content":"output","details":{"old":true}}}`)
	data.Entries = append(data.Entries[:1], append([]json.RawMessage{result}, data.Entries[1:]...)...)
	raw, _ = json.Marshal(data)
	if err := source.state.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	setForkFixtureSnapshot(t, raw)
	v := m.requestFork(source, "old")().(forkSnapshotMsg)
	enriched := json.RawMessage(strings.Replace(string(raw), `"old":true`, `"enriched":true`, 1))
	if err := source.state.LoadEntries(enriched); err != nil {
		t.Fatal(err)
	}
	verify := m.forkSnapshot(v)
	if verify == nil {
		t.Fatal("changed record version was rejected before revalidation")
	}
	next, ok := verify().(forkSnapshotMsg)
	if !ok || next.err != nil {
		t.Fatal("enrichment did not use a successful async content check")
	}
	source.state.Apply(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"later"}}`))
	source.revision++
	write := m.forkSnapshot(next)
	if write == nil {
		t.Fatal("later streaming invalidated the verified historical prefix")
	}
	created, ok := write().(forkCreatedMsg)
	if !ok || created.err != nil || created.prepared == nil {
		t.Fatal("verified prefix was not persisted and prepared")
	}
}

func TestForkRPCProcess(t *testing.T) {
	if os.Getenv("PIGOUI_UI_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		if path := os.Getenv("PIGOUI_UI_WIRE"); path != "" {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				os.Exit(3)
			}
			_, _ = f.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
			_ = f.Close()
		}
		data := json.RawMessage(`{}`)
		if request["type"] == "get_entries" {
			snapshot, err := os.ReadFile(os.Getenv("PIGOUI_FORK_SNAPSHOT"))
			if err != nil {
				os.Exit(3)
			}
			data = snapshot
		}
		if err := encoder.Encode(map[string]any{"type": "response", "id": request["id"], "command": request["type"], "success": true, "data": data}); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
