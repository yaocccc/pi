package codexfast

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type caller struct {
	method string
	params any
	raw    json.RawMessage
	err    error
}

func (c *caller) CallExtension(_ context.Context, method string, params any) (json.RawMessage, error) {
	c.method, c.params = method, params
	return c.raw, c.err
}
func TestOptionalCapabilityDTOAndOperations(t *testing.T) {
	c := &caller{raw: json.RawMessage(`{"level":"fast","levels":["normal","fast","ultrafast"]}`)}
	state, err := Get(context.Background(), c)
	if err != nil || !state.Available() || c.method != "codex-fast.get" {
		t.Fatalf("get: %+v %v", state, err)
	}
	_, err = Set(context.Background(), c, "not-a-valid-level")
	if err != nil || c.method != "codex-fast.set" || c.params.(map[string]string)["level"] != "not-a-valid-level" {
		t.Fatal("adapter must delegate authoritative validation")
	}
	c.raw = json.RawMessage(`{"level":"fast","levels":["normal"]}`)
	if _, err = Get(context.Background(), c); err == nil {
		t.Fatal("malformed state accepted")
	}
	c.err = errors.New("read failed")
	if state, err = Get(context.Background(), c); err == nil || state.Available() {
		t.Fatal("failure invented state")
	}
}
