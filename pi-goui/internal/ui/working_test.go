package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
)

func workingApply(w *workingState, raw string, now time.Time) { w.apply(json.RawMessage(raw), now) }
func workingFinal(stamp, output int) string {
	return fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","timestamp":%d,"content":[{"type":"text","text":"completed"}],"usage":{"input":10,"cacheRead":20,"cacheWrite":5,"output":%d}}}`, stamp, output)
}
func TestWorkingLifecycleAndMetrics(t *testing.T) {
	now := time.Unix(1700000000, 0)
	w := workingState{}
	theme := config.Theme{Accent: "#123456", Success: "#345678", Muted: "#567890"}
	if w.line(now, theme) != "" {
		t.Fatal("working before a run")
	}
	workingApply(&w, `{"type":"response","command":"prompt","success":true}`, now)
	if w.present {
		t.Fatal("prompt acknowledgment started work")
	}
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, `{"type":"turn_start","turnIndex":0}`, now)
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"abcdefgh"}}`, now)
	line := ansi.Strip(w.line(now.Add(time.Second), theme))
	for _, s := range []string{"Turn 1", "1s", "↓2", "2.0 TPS"} {
		if !strings.Contains(line, s) {
			t.Fatalf("missing %q: %s", s, line)
		}
	}
	workingApply(&w, workingFinal(1, 4), now.Add(2*time.Second))
	line = ansi.Strip(w.line(now.Add(2*time.Second), theme))
	if !strings.Contains(line, "Turn 1 · ↑35 ↓4 · 2.0 TPS · 2s") {
		t.Fatal(line)
	}
	workingApply(&w, `{"type":"agent_end"}`, now.Add(2*time.Second))
	if !w.active {
		t.Fatal("agent_end falsely completed")
	}
	workingApply(&w, `{"type":"agent_settled"}`, now.Add(3*time.Second))
	line = w.line(now.Add(4*time.Second), theme)
	if !strings.HasPrefix(ansi.Strip(line), "✓") || line != w.line(now.Add(time.Hour), theme) {
		t.Fatal("completion did not freeze")
	}
	if strings.Contains(line, "[48;") {
		t.Fatal("background color")
	}
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"late"}}`, now.Add(5*time.Second))
	if line != w.line(now.Add(6*time.Second), theme) {
		t.Fatal("late event changed completed metrics")
	}
	workingApply(&w, `{"type":"agent_start"}`, now.Add(time.Hour))
	if w.total.output != 0 || w.turn != 0 || !w.active {
		t.Fatal("new run leaked previous metrics")
	}
}
func TestWorkingSnapshotsDuplicatesAndTurns(t *testing.T) {
	now := time.Unix(1, 0)
	var w workingState
	workingApply(&w, `{"type":"agent_start"}`, now)
	snapshot := `{"type":"message_update","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"abcdefgh"}],"usage":{"input":2,"output":0}}}`
	workingApply(&w, snapshot, now)
	workingApply(&w, snapshot, now)
	if w.estimate() != 2 || w.turn != 1 {
		t.Fatalf("snapshots accumulate: %+v", w)
	}
	workingApply(&w, workingFinal(1, 4), now)
	workingApply(&w, workingFinal(1, 4), now)
	if w.total.input != 35 || w.total.output != 4 || w.turn != 1 {
		t.Fatal("duplicate final counted")
	}
	workingApply(&w, `{"type":"message_update","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"completed"}]}}`, now)
	if w.current {
		t.Fatal("late final snapshot reopened message")
	}
	workingApply(&w, `{"type":"message_start","message":{"role":"assistant","timestamp":2,"content":[]}}`, now)
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"12345678"}}`, now)
	workingApply(&w, workingFinal(1, 4), now)
	if !w.current || w.estimate() != 2 || w.turn != 2 {
		t.Fatal("late duplicate cleared next turn")
	}
	workingApply(&w, workingFinal(2, 6), now)
	if w.total.output != 10 || w.total.input != 70 {
		t.Fatalf("final totals %+v", w.total)
	}
	var other workingState
	if other.line(now, config.Theme{}) != "" {
		t.Fatal("session state leaked")
	}
}
func TestWorkingNativeFinalAndUnicodeToolEstimate(t *testing.T) {
	now := time.Unix(1, 0)
	var w workingState
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"😀😀"}}`, now)
	if w.estimate() != 1 {
		t.Fatal("estimate must count JS UTF16 characters")
	}
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1,"toolCall":{"name":"read","arguments":{"path":"a"}}}}`, now)
	expected := float64((4 + 4 + len(`{"path":"a"}`) + 3) / 4)
	if w.estimate() != expected {
		t.Fatalf("tool estimate %v, want %v", w.estimate(), expected)
	}
	final := `{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"done"}],"usage":{"input":1,"output":0}}`
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"done","message":`+final+`}}`, now)
	workingApply(&w, `{"type":"message_end","message":`+final+`}`, now)
	if w.total.output != 0 || w.total.input != 1 || w.current {
		t.Fatal("authoritative zero or duplicate done/end mishandled")
	}
}
func TestWorkingAnimationAndBoundedDedup(t *testing.T) {
	now := time.Unix(1, 0)
	var w workingState
	workingApply(&w, `{"type":"agent_start"}`, now)
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		line := w.line(now.Add(time.Duration(i)*workingFrameInterval), config.Theme{})
		seen[strings.Fields(line)[0]] = true
	}
	if len(seen) != 10 {
		t.Fatal("expected ten reference frames")
	}
	if strings.Fields(w.line(now, config.Theme{}))[0] != strings.Fields(w.line(now.Add(10*workingFrameInterval), config.Theme{}))[0] {
		t.Fatal("animation period")
	}
	for i := 0; i < workingDedupLimit+5; i++ {
		workingApply(&w, workingFinal(i, 1), now)
	}
	if len(w.finished) != workingDedupLimit || len(w.order) != workingDedupLimit || w.total.output != workingDedupLimit+5 {
		t.Fatal("dedup memory or totals not bounded correctly")
	}
}
func TestWorkingExplicitTurnsAndCorrection(t *testing.T) {
	now := time.Unix(1, 0)
	var w workingState
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, `{"type":"turn_start","turnIndex":0}`, now)
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"abcd"}}`, now)
	workingApply(&w, `{"type":"turn_start","turnIndex":0}`, now)
	if w.turn != 1 || w.estimate() != 1 {
		t.Fatal("duplicate turn reset live data")
	}
	workingApply(&w, workingFinal(1, 4), now)
	workingApply(&w, workingFinal(1, 6), now)
	if w.total.output != 6 {
		t.Fatal("usage correction added instead of replaced")
	}
	workingApply(&w, `{"type":"turn_start","turnIndex":1}`, now)
	workingApply(&w, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"abcd"}}`, now)
	if w.turn != 2 || w.estimate() != 1 {
		t.Fatal("explicit next turn")
	}
	workingApply(&w, `{"type":"agent_start"}`, now.Add(time.Second))
	if w.total.output != 6 {
		t.Fatal("duplicate start reset active run")
	}
}
