package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestWorkingLineHidesQueuedStatus(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	for _, active := range []bool{false, true} {
		tab.working = workingState{Active: active, Present: active, Started: time.Now()}
		for _, status := range []string{"Queued", "queued"} {
			tab.status = status
			line := ansi.Strip(m.workingLine(tab))
			if strings.Contains(strings.ToLower(line), "queued") {
				t.Fatalf("queued status visible: %q", line)
			}
			if active && !strings.Contains(line, "turns") {
				t.Fatalf("working metrics hidden: %q", line)
			}
			if tab.status != status {
				t.Fatal("rendering mutated queue status")
			}
		}
		tab.status = "Connection failed"
		if !strings.Contains(ansi.Strip(m.workingLine(tab)), tab.status) {
			t.Fatal("unrelated status hidden")
		}
	}
}
