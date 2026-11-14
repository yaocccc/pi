//go:build unix

package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubmitPromptKnownFailureAndUnknownOutcome(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "shared"}[shared], func(t *testing.T) {
			cfg, root := sharedFixture(t)
			var c *Client
			var err error
			if shared {
				c, err = startShared(cfg)
			} else {
				c, err = startDirect(cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			restored, err := c.SubmitPrompt(ctx, map[string]any{"type": "not-a-pi-command", "message": "fixture-reject"}, "original", true)
			if err == nil || restored != "queued\n\nlater\n\noriginal" {
				t.Fatalf("known rejection lost queue: %q %v", restored, err)
			}
			restored, err = c.SubmitPrompt(ctx, map[string]any{"message": "fixture-unknown"}, "original", true)
			if err == nil || restored != "" || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("unknown result could resend: %q %v", restored, err)
			}
			wire, _ := os.ReadFile(filepath.Join(root, "wire"))
			if strings.Count(string(wire), "fixture-unknown") != 1 {
				t.Fatal("uncertain prompt was retried")
			}
			if strings.Contains(string(wire), "goui_submit_prompt") || strings.Contains(string(wire), "not-a-pi-command") {
				t.Fatal("control command leaked to Pi")
			}
		})
	}
}

func TestPendingUIFailureRetainsQuestionAndCancelDeduplicates(t *testing.T) {
	stopped := make(chan struct{})
	close(stopped)
	backend := &Client{stop: stopped}
	b := &sessionBroker{}
	s := &brokerSession{backend: backend, pendingUI: map[string]json.RawMessage{"q": json.RawMessage(`{"id":"q"}`)}, clients: map[string]*brokerPeer{}}
	p := &brokerPeer{id: "peer", session: s, out: make(chan json.RawMessage, 8), done: make(chan struct{})}
	s.clients[p.id] = p
	answer := map[string]any{"type": "extension_ui_response", "id": "q", "cancelled": true}
	b.command(p, answer, make(chan struct{}))
	if _, ok := s.pendingUI["q"]; !ok {
		t.Fatal("failed write lost pending question")
	}
	if len(p.out) != 0 {
		t.Fatal("failed answer was resolved")
	}
	backend = &Client{writes: make(chan *writeRequest)}
	s.backend = backend
	writes := make(chan int, 1)
	go func() { r := <-backend.writes; r.result <- nil; writes <- 1 }()
	b.command(p, answer, make(chan struct{}))
	if len(s.pendingUI) != 0 {
		t.Fatal("successful cancellation not resolved")
	}
	<-writes
	b.command(p, answer, make(chan struct{})) // Must not attempt a second write.
	if len(p.out) != 1 {
		t.Fatal("duplicate resolved notification")
	}
}

func TestSessionSnapshotAndEditorScope(t *testing.T) {
	cfg, _ := sharedFixture(t)
	a, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	sharedCall(t, a, map[string]any{"type": "fixture_ui_state"})
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-a.Events():
			var v struct{ Method string }
			json.Unmarshal(event.Data, &v)
			if v.Method == "set_editor_text" {
				goto editorSeen
			}
		case <-timer.C:
			t.Fatal("initiating client lost editor command")
		}
	}
editorSeen:
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for _, client := range []*Client{b, c} {
		snapshot, status, widget, title := false, false, false, false
		deadline := time.NewTimer(150 * time.Millisecond)
	loop:
		for {
			select {
			case event := <-client.Events():
				var v struct {
					Type, Method, StatusKey, StatusText, WidgetKey, Title string
					WidgetLines                                           []string
				}
				json.Unmarshal(event.Data, &v)
				if v.Type == "goui_session_snapshot" {
					snapshot = true
				}
				if v.Method == "setStatus" && v.StatusKey == "fixture" && v.StatusText == "ready" {
					status = true
				}
				if v.Method == "setWidget" && v.WidgetKey == "fixture" && len(v.WidgetLines) == 1 && v.WidgetLines[0] == "widget ready" {
					widget = true
				}
				if v.Method == "setTitle" && v.Title == "fixture title" {
					title = true
				}
				if v.Method == "set_editor_text" {
					t.Fatal("another client's draft was polluted")
				}
			case <-deadline.C:
				break loop
			}
		}
		if !snapshot || !status || !widget || !title {
			t.Fatal("snapshot/state handoff missing", snapshot, status, widget, title)
		}
	}
}

func TestExecutorReconstructsMissingDiscoveryRecord(t *testing.T) {
	cfg, _ := sharedFixture(t)
	a, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	root := filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors")
	dirs, e := os.ReadDir(root)
	if e != nil || len(dirs) != 1 {
		t.Fatal(e, len(dirs))
	}
	descriptor := filepath.Join(root, dirs[0].Name(), "endpoint.json")
	if e = os.Remove(descriptor); e != nil {
		t.Fatal(e)
	}
	// Discovery during the gap may fail closed, but must never create a writer.
	if early, err := startShared(cfg); err == nil {
		if early.PID() != a.PID() {
			t.Fatal("missing descriptor duplicated writer")
		}
		early.Close()
	}
	sharedEventually(t, func() bool { _, e := os.Stat(descriptor); return e == nil })
	b, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if a.PID() != b.PID() {
		t.Fatal("reconstructed metadata started another writer")
	}
	executorPID := a.shared.record.PID
	a.Close()
	b.Close()
	sharedEventually(t, func() bool { return sharedGone(executorPID) })
	// The retained descriptor is a safe tombstone, not an unavailable live PID.
	fresh, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Close()
}

func TestLegacyLiveBrokerFailsClosedAndBirthFencesPIDReuse(t *testing.T) {
	agent := filepath.Join(t.TempDir(), "agent")
	root, err := sharedRoot(agent)
	if err != nil {
		t.Fatal(err)
	}
	record := sharedRecord{PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Token: "fixture", Socket: filepath.Join(t.TempDir(), "rpc.sock")}
	if !recordedProcessLive(record, record.PID) {
		t.Fatal("live process birth not recognized")
	}
	stale := record
	stale.Birth = "different-birth"
	if recordedProcessLive(stale, stale.PID) {
		t.Fatal("recycled PID mistaken for former owner")
	}
	if err = publishSharedRecord(root, record); err != nil {
		t.Fatal(err)
	}
	if member, err := JoinSessionMember(agent); err == nil {
		member.Close()
		t.Fatal("legacy live broker accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "executors")); !os.IsNotExist(err) {
		t.Fatal("failed admission created executors")
	}
}
