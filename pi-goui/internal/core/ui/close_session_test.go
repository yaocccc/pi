package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Execute every member of a batch, including cleanup after focus commands.
func runCloseCommand(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		quit := false
		for _, child := range msg {
			quit = runCloseCommand(child) || quit
		}
		return quit
	}
	return false
}
func TestExitClosesOnlySelectedIdleSession(t *testing.T) {
	for _, selected := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			m := testModel(t, "")
			m.addTab("")
			m.addTab("")
			originals := append([]*tab(nil), m.tabs...)
			for i, other := range m.tabs {
				other.setValue(fmt.Sprintf("draft-%d", i))
				other.state.Busy = i != selected
			}
			target := originals[selected]
			target.setValue("")
			m.activate(selected)
			if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) {
				t.Fatal("closing one tab quit the frontend")
			}
			if len(m.tabs) != 2 || m.byID(target.id) != nil {
				t.Fatal("selected tab not removed")
			}
			if m.active != min(selected, 1) {
				t.Fatalf("bad active index %d", m.active)
			}
			for i, other := range originals {
				if i == selected {
					continue
				}
				if m.byID(other.id) != other || !other.state.Busy || other.editor.Value() != fmt.Sprintf("draft-%d", i) {
					t.Fatal("remaining session or draft changed")
				}
			}
		})
	}
}
func TestExitBusySessionRequiresConsent(t *testing.T) {
	for _, phase := range []string{"busy", "compacting", "sending"} {
		t.Run(phase, func(t *testing.T) {
			m := testModel(t, "")
			m.addTab("")
			target := m.current()
			switch phase {
			case "busy":
				target.state.Busy = true
			case "compacting":
				target.state.Compacting = true
			case "sending":
				target.sending = true
			}
			runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}))
			if len(m.tabs) != 2 || len(target.dialogs) != 1 || target.dialogs[0].kind != "close" {
				t.Fatal("in-flight session not protected")
			}
			// Default choice is Cancel.
			runCloseCommand(m.confirmDialog(target, target.dialogs[0]))
			if len(m.tabs) != 2 || len(target.dialogs) != 0 {
				t.Fatal("cancel removed session")
			}
			runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}))
			target.dialogs[0].selected = 1
			if runCloseCommand(m.confirmDialog(target, target.dialogs[0])) || len(m.tabs) != 1 || m.byID(target.id) != nil {
				t.Fatal("confirmed close did not preserve other session")
			}
		})
	}
}
func TestExitLastSessionAndSlashClose(t *testing.T) {
	m := testModel(t, "")
	if !runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) {
		t.Fatal("last idle session did not quit")
	}
	m = testModel(t, "")
	target := m.current()
	target.state.Busy = true
	if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) || len(target.dialogs) != 1 {
		t.Fatal("last busy session quit without consent")
	}
	target.dialogs[0].selected = 1
	if !runCloseCommand(m.confirmDialog(target, target.dialogs[0])) {
		t.Fatal("confirmed last session did not quit")
	}
	m = testModel(t, "")
	m.addTab("")
	target = m.current()
	cmd, handled := m.slash(target, "/close")
	if !handled || runCloseCommand(cmd) || len(target.dialogs) != 1 || len(m.tabs) != 2 {
		t.Fatal("/close must still confirm even when idle")
	}
}
func TestExitRespectsDraftRemappingAndModalScope(t *testing.T) {
	m := testModel(t, "")
	m.addTab("")
	target := m.current()
	target.setValue("abc")
	target.editor.Area.CursorStart()
	if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) || len(m.tabs) != 2 || target.editor.Value() != "bc" {
		t.Fatalf("nonempty Ctrl+D should delete forward: %q", target.editor.Value())
	}
	for _, keys := range []string{`{"app.exit":[]}`, `{"app.exit":["alt+q"]}`} {
		m = testModel(t, keys)
		m.addTab("")
		if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) || len(m.tabs) != 2 {
			t.Fatal("disabled/replaced default still closed session")
		}
		if strings.Contains(keys, "alt+q") {
			if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})) || len(m.tabs) != 1 {
				t.Fatal("remapped exit did not close active session")
			}
		}
	}
	m = testModel(t, "")
	m.addTab("")
	target = m.current()
	d := m.newDialog("input", "Extension input", "")
	d.requestID = "scoped"
	d.editor.SetValue("abc")
	target.dialogs = append(target.dialogs, d)
	m.focus(target)
	if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) || len(m.tabs) != 2 || len(target.dialogs) != 1 {
		t.Fatal("exit shortcut escaped modal context")
	}
}
func TestCloseSessionLeavesOtherRPCClientUsable(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.client, _ = fakeClient(t)
	second := m.addTab("")
	second.client, _ = fakeClient(t)
	m.activate(0)
	if runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})) {
		t.Fatal("unexpected quit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := first.client.Call(ctx, map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("closed tab's client remains open")
	}
	if _, err := second.client.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatalf("other client stopped: %v", err)
	}
	if m.current() != second {
		t.Fatal("remaining client not active")
	}
}
func TestClosedStartingSessionLateClientIsCleanedUp(t *testing.T) {
	m := testModel(t, "")
	removed := m.current()
	m.addTab("")
	m.activate(0)
	runCloseCommand(m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}))
	client, _ := fakeClient(t)
	_, cmd := m.Update(openedMsg{id: removed.id, client: client})
	runCloseCommand(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.Call(ctx, map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("late startup client leaked")
	}
	if len(m.tabs) != 1 {
		t.Fatal("late startup recreated removed tab")
	}
}
