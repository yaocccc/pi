package hostui

import (
	"context"
	"encoding/json"
	"errors"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/extensions/codexfast"
	"pi-goui/internal/runtime"
	"reflect"
	"testing"
	"time"
)

type activityClient struct {
	raw    json.RawMessage
	err    error
	method string
}

func (c *activityClient) CallExtension(_ context.Context, method string, _ any) (json.RawMessage, error) {
	c.method = method
	return c.raw, c.err
}

func TestReleaseChecksBackendActivityAndOnlyFallsBackWhenBridgeIsAbsent(t *testing.T) {
	for _, test := range []struct {
		name, raw         string
		err               error
		known, idle, fail bool
	}{
		{name: "native", err: runtime.ErrExtensionUnavailable},
		{name: "idle", raw: `{"version":1,"idle":true,"reasons":[]}`, known: true, idle: true},
		{name: "background", raw: `{"version":1,"idle":false,"reasons":["Worker: cleaning up"]}`, known: true},
		{name: "old bridge", err: &runtime.ExtensionError{Code: "unavailable", Message: "method unavailable"}, known: true, fail: true},
		{name: "old SDK", err: &runtime.ExtensionError{Code: "activity-unavailable", Message: "idle barrier unavailable"}, known: true, fail: true},
		{name: "missing idle", raw: `{"version":1,"reasons":[]}`, known: true, fail: true},
		{name: "missing reasons", raw: `{"version":1,"idle":true}`, known: true, fail: true},
		{name: "contradictory", raw: `{"version":1,"idle":true,"reasons":["busy"]}`, known: true, fail: true},
		{name: "unknown busy", raw: `{"version":1,"idle":false,"reasons":[]}`, known: true, fail: true},
		{name: "unsupported", raw: `{"version":2,"idle":true,"reasons":[]}`, known: true, fail: true},
		{name: "timeout", err: context.DeadlineExceeded, fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &activityClient{raw: json.RawMessage(test.raw), err: test.err}
			status, err := checkRelease(context.Background(), client)
			if status.Known != test.known || status.Idle != test.idle || (err != nil) != test.fail {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			if client.method != "session.activity" {
				t.Fatalf("unexpected query %q", client.method)
			}
			if status.Known && !status.Idle && len(status.Reasons) == 0 {
				t.Fatal("an occupied session needs an explicit reason")
			}
			if errors.Is(test.err, context.DeadlineExceeded) && !errors.Is(err, test.err) {
				t.Fatal("lost transport cancellation")
			}
		})
	}
}

func TestCodexPendingAndLateRepliesKeepBackendOwnership(t *testing.T) {
	old := &activityClient{raw: json.RawMessage(`{"level":"normal","levels":["normal","fast"]}`)}
	next := &activityClient{raw: json.RawMessage(`{"level":"fast","levels":["normal","fast"]}`)}
	s := &Session{}
	sessions := extension.Sessions{s}
	oldRequest := s.Codex.Request(1, old, false, "", time.Now())
	if !sessions.Pending() {
		t.Fatal("admitted control request was not reported")
	}
	nextRequest := s.Codex.Request(1, next, false, "", time.Now())
	if nextRequest == nil {
		t.Fatal("the new backend inherited the old pending request")
	}
	s.Codex.Apply(oldRequest().(CodexResult), false, func(string) { t.Fatal("stale response notified") })
	if !sessions.Pending() || s.Codex.State.Available() {
		t.Fatal("old response cleared or replaced the new request")
	}
	s.Codex.Apply(nextRequest().(CodexResult), false, func(string) { t.Fatal("unexpected notification") })
	if sessions.Pending() || !reflect.DeepEqual(s.Codex.State, codexfast.State{Level: "fast", Levels: []string{"normal", "fast"}}) {
		t.Fatalf("current response not applied: %+v", s.Codex)
	}
	if !extension.InspectorPending(&Inspector{Worker: &WorkerSession{Pending: true}}) || extension.InspectorPending(&Inspector{}) {
		t.Fatal("inspector request activity not exposed")
	}
}
