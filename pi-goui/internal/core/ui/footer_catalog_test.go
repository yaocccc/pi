package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestColdHoverPreviewAndSelectionBeforeFirstMessage(t *testing.T) {
	for _, tc := range []struct {
		kind, label, op string
		available       bool
	}{
		{"model", "two", "set_model", true}, {"thinking", "low", "set_thinking_level", true},
		{"model", "retired", "", false}, {"thinking", "max", "", false},
	} {
		t.Run(tc.kind+"/"+tc.label, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
			owner := m.current()
			writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "settings.json"), `{"defaultProvider":"fake","defaultModel":"one","defaultThinkingLevel":"high"}`)
			writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "models-store.json"), `{"fake":{"models":[{"id":"one","thinkingLevelMap":{"low":"low","max":"max","minimal":null}},{"id":"retired"}]}}`)
			writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "models.json"), `{"providers":{"fake":{"models":[{"id":"two"}]}}}`)
			preview := m.previewSession(owner)
			m.Update(preview())
			f := m.inputFooter(owner)
			label := f.model
			if tc.kind == "thinking" {
				label = f.thinking
			}
			m.Update(tea.MouseMotionMsg{X: label.x, Y: label.y})
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Local") || !strings.Contains(view, tc.label) || strings.Contains(view, "Loading…") || strings.Contains(view, "minimal") {
				t.Fatalf("wrong offline %s menu: %s", tc.kind, view)
			}
			if owner.client != nil || owner.cycle.intent != nil || owner.cycle.inflight != 0 || owner.cycle.phase != cycleIdle {
				t.Fatal("hover started a writer")
			}
			row := -1
			for i, o := range owner.footerHover.options {
				if o.Label == tc.label {
					row = i
					break
				}
			}
			if row < 0 {
				t.Fatal("no matching option")
			}
			box, start, _ := m.footerPopup(owner, tc.kind)
			x, y := box.x+2, box.y+1+row-start
			m.Update(tea.MouseMotionMsg{X: x, Y: y})
			m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if owner.cycle.phase != cycleStarting || owner.cycle.intent == nil {
				t.Fatal("click did not request session")
			}
			owner.client, _ = fakeClient(t)
			owner.resume.pending = false
			owner.cycle.phase = cycleActive
			lookup := commandResult(m.runCycleIntent(owner)).(footerReplyMsg)
			if lookup.reply.op != footerListOp(tc.kind) {
				t.Fatalf("applied preview without validation: %s", lookup.reply.op)
			}
			set := m.footerReply(lookup)
			if !tc.available {
				if len(owner.dialogs) != 1 || owner.dialogs[0].kind != tc.kind || owner.cycle.inflight != 0 {
					t.Fatal("stale option was applied instead of offering live list")
				}
				return
			}
			reply := commandResult(set).(footerReplyMsg)
			if reply.reply.err != nil || reply.reply.op != tc.op || reply.selected.Label != tc.label {
				t.Fatalf("wrong confirmed selection: %+v", reply)
			}
		})
	}
}
