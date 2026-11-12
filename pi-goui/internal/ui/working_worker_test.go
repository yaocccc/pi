package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
)

func TestWorkingWorkerUsage(t *testing.T) {
	now := time.Unix(1700000000, 0)
	w := workingState{}
	workingApply(&w, `{"type":"agent_start"}`, now)
	workingApply(&w, workingFinal(1, 4), now)
	details := func(input int) string {
		return fmt.Sprintf(`{"kind":"worker-ui","batchId":"batch-a","tasks":[{"index":0,"usage":{"input":%d,"cacheRead":20,"cacheWrite":5,"output":10}},{"index":1,"usage":{"input":30,"output":6}}]}`, input)
	}
	for _, input := range []int{50, 100, 50} {
		workingApply(&w, `{"type":"tool_execution_update","toolCallId":"start","partialResult":{"details":`+details(input)+`}}`, now)
	}
	workingApply(&w, `{"type":"tool_execution_end","toolCallId":"continue","result":{"details":`+details(100)+`}}`, now)
	workingApply(&w, `{"type":"message_end","message":{"role":"toolResult","toolCallId":"continue","details":`+details(100)+`}}`, now)
	// Separate batches must count even when task indices match.
	workingApply(&w, `{"type":"tool_execution_end","result":{"details":`+strings.ReplaceAll(details(100), "batch-a", "batch-b")+`}}`, now)
	line := ansi.Strip(w.line(now.Add(time.Second), config.Theme{}))
	if !strings.Contains(line, "1 turns · ↑295 ↓36 · 36.0 TPS") {
		t.Fatal(line)
	}
	workingApply(&w, `{"type":"agent_settled"}`, now.Add(time.Second))
	frozen := w.line(now.Add(time.Second), config.Theme{})
	workingApply(&w, `{"type":"tool_execution_update","partialResult":{"details":`+details(900)+`}}`, now)
	if w.line(now.Add(time.Hour), config.Theme{}) != frozen {
		t.Fatal("late worker update changed settled metrics")
	}
	workingApply(&w, `{"type":"agent_start"}`, now.Add(time.Hour))
	if len(w.workers) != 0 || strings.Contains(w.line(now.Add(time.Hour), config.Theme{}), "↑") {
		t.Fatal("worker usage leaked into next run")
	}
}
