package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
)

func TestWorkingLineStartsDuringSubmissionPreparation(t *testing.T) {
	for name, prepare := range map[string]func(*tab){
		"starting": func(t *tab) { t.cycle.phase = cycleStarting },
		"loading": func(t *tab) {
			t.cycle.phase = cycleActive
			t.resume.pending = true
			t.resume.submit = &resumeSubmission{original: "held prompt"}
		},
		"sending":        func(t *tab) { t.sending = true },
		"awaiting start": func(t *tab) { t.awaitingStart = true },
	} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "")
			owner := m.current()
			owner.status = "Loading current session…"
			prepare(owner)
			for _, present := range []bool{false, true} {
				owner.working = workingState{Present: present, Started: time.Now().Add(-time.Second), Ended: time.Now()}
				want := render.AnimateWorkingLine("working", m.cfg.Theme, m.frame)
				if got := m.workingLine(owner); got != want {
					t.Fatalf("preparation working line: got %q, want %q", got, want)
				}
				if owner.working.Active || owner.state.Busy {
					t.Fatal("presentation started native work or fabricated statistics")
				}
			}
			owner.working = workingState{Active: true, Present: true, Started: time.Now()}
			line := ansi.Strip(m.workingLine(owner))
			if !strings.Contains(line, "turns") || strings.Contains(line, "Loading") {
				t.Fatalf("native working metrics replaced by preparation status: %q", line)
			}
		})
	}
}

func TestPreparingWorkingDoesNotHideFailureOrDialog(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := testModel(t, "")
		owner := m.current()
		owner.cycle.phase = cycleStarting
		owner.resume.submit = &resumeSubmission{original: "held prompt"}
		want := "needs input"
		if failed {
			owner.resume.failed = true
			owner.status = "Cannot start pi"
			want = owner.status
		} else {
			owner.dialogs = append(owner.dialogs, m.newDialog("input", "Question", ""))
		}
		if got := ansi.Strip(m.workingLine(owner)); !strings.Contains(got, want) {
			t.Fatalf("preparation hid actionable status: %q", got)
		}
	}
}

func TestWorkingLineShowsOtherOwnerWithWorkingStyle(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.status = "Idle"
	tab.cycle.presence.Busy = true
	tab.cycle.presence.OwnerPID = 123
	now := time.Now()
	for _, working := range []workingState{
		{},
		{Present: true, Started: now.Add(-time.Second), Ended: now},
	} {
		tab.working = working
		for _, frame := range []int{0, 3} {
			m.frame = frame
			label := "working · held by another instance · PID 123 · Read-only"
			want := render.AnimateWorkingLine(label, m.cfg.Theme, frame)
			if got := m.workingLine(tab); got != want {
				t.Fatalf("other owner working line: got %q, want %q", got, want)
			}
			if tab.working.Active || tab.status != "Idle" {
				t.Fatal("rendering changed local execution state")
			}
		}
	}
	tab.cycle.presence.Busy = false
	tab.working = workingState{}
	if got := ansi.Strip(m.workingLine(tab)); got != "idle" {
		t.Fatalf("released ownership did not restore idle: %q", got)
	}
}

func TestWorkingLineDoesNotPresentUnknownOwnershipAsWorking(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.cycle.presence.Busy = true
	for _, probeError := range []error{nil, errors.New("lock inspection failed")} {
		tab.cycle.probeErr = probeError
		tab.cycle.presence.Uncertain = probeError == nil
		tab.working = workingState{Present: true, Started: time.Now(), Ended: time.Now()}
		want := m.paint(render.Clean(tab.ownershipStatus()), m.cfg.Theme.Muted)
		if got := m.workingLine(tab); got != want || strings.Contains(ansi.Strip(got), "working") {
			t.Fatalf("unknown ownership shown as active or with stale statistics: %q", got)
		}
	}
}

func TestWorkingLineShowsIdleWithoutHidingMetrics(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	now := time.Now()
	for _, working := range []workingState{
		{},
		{Active: true, Present: true, Started: now},
		{Present: true, Started: now.Add(-time.Second), Ended: now},
	} {
		tab.working = working
		tab.status = ""
		want := ansi.Strip(m.workingLine(tab))
		if !working.Present {
			want = "idle"
		}
		for _, status := range []string{"Idle", "idle", "IDLE"} {
			tab.status = status
			if got := ansi.Strip(m.workingLine(tab)); got != want {
				t.Fatalf("idle changed working line: got %q, want %q", got, want)
			}
			if tab.status != status {
				t.Fatal("rendering mutated session status")
			}
		}
		if working.Present && !strings.Contains(want, "turns") {
			t.Fatalf("working metrics hidden: %q", want)
		}
	}
}

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
