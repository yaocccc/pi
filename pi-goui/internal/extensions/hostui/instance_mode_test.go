package hostui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pi-goui/internal/core/extension"
	"pi-goui/internal/extensions/codexfast"
	rpc "pi-goui/internal/runtime"
)

type modeWriter struct {
	level, path string
	calls       []string
	unsupported bool
	absent      bool
}

func (w *modeWriter) CallExtension(_ context.Context, method string, params any) (json.RawMessage, error) {
	w.calls = append(w.calls, method)
	if w.absent || (method == "codex-fast.apply" && w.unsupported) {
		return nil, rpc.ErrExtensionUnavailable
	}
	switch method {
	case "codex-fast.get":
	case "codex-fast.apply", "codex-fast.set":
		w.level = params.(map[string]string)["level"]
		if method == "codex-fast.set" {
			raw, _ := json.Marshal(map[string]string{"level": w.level})
			if err := os.WriteFile(w.path, raw, 0600); err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("unexpected method")
	}
	return json.Marshal(codexfast.State{Level: w.level, Levels: CodexFastLevels})
}
func TestInstanceStartupDefaultAndWriterLifetimes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-fast.json")
	if err := os.WriteFile(path, []byte(`{"level":"fast"}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := func() extension.Capabilities { return NewAdapter(dir).Capabilities() }
	a, b := start(), start()
	first := a.NewSession().(*Session)
	other := a.NewSession().(*Session)
	second := b.NewSession().(*Session)
	prep := func(s *Session, w *modeWriter) {
		t.Helper()
		f := s.Prepare(w)
		if f == nil {
			t.Fatal("missing restore")
		}
		commit, err := f(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		commit()
	}
	wa := &modeWriter{level: "fast", path: path}
	prep(first, wa)
	// In-flight writer initialization captured the old revision.
	wo := &modeWriter{level: "fast", path: path}
	stale := other.Prepare(wo)
	result := first.Request(1, wa, false, "ultrafast", first.Codex.Checked)().(CodexResult)
	first.Codex.Apply(result, false, func(message string) { t.Fatal(message) })
	commit, err := stale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	commit()
	prep(other, wo)
	if wo.level != "ultrafast" || other.Codex.displayState().Level != "ultrafast" || second.Codex.displayState().Level != "fast" {
		t.Fatal("settings not scoped to TUI")
	}
	for i := 0; i < 2; i++ {
		wb := &modeWriter{level: "ultrafast", path: path}
		prep(second, wb)
		if wb.level != "fast" || len(wb.calls) != 2 || wb.calls[1] != "codex-fast.apply" {
			t.Fatalf("restart: %+v", wb)
		}
	}
	if s := start().NewSession().(*Session); s.Codex.displayState().Level != "ultrafast" {
		t.Fatal("restoring another instance overwrote default")
	}
	if !first.Event(`pi-goui:event:v1:{"name":"codex.settings-saved","data":{"level":"normal"}}`, func(string) {}) || other.Codex.displayState().Level != "normal" {
		t.Fatal("saved settings command fact not shared by this instance")
	}
}
func TestInstanceRestoreFailures(t *testing.T) {
	s := NewAdapter(t.TempDir()).Capabilities().NewSession().(*Session)
	w := &modeWriter{level: "normal", unsupported: true}
	if _, err := s.Prepare(w)(context.Background()); err == nil || s.Prepare(w) == nil {
		t.Fatal("unsupported restore certified")
	}
	w.absent = true
	c, err := s.Prepare(w)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c()
	if s.Codex.displayState().Available() || s.Prepare(w) != nil {
		t.Fatal("absent capability cannot enable control or block ordinary prompts")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex-fast.json"), []byte(`{"level":"future"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s = NewAdapter(dir).Capabilities().NewSession().(*Session)
	if s.Codex.displayState().Available() {
		t.Fatal("invented level for unknown configuration")
	}
	if _, err := s.Prepare(&modeWriter{level: "normal"})(context.Background()); err == nil {
		t.Fatal("unknown startup default accepted")
	}
}
