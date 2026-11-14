package ui

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"
)

// sharedSessionEvent updates only the owning tab's backend projection. Editor
// drafts, attachments, selection, layout and the active tab remain local.
func (m *Model) sharedSessionEvent(t *tab, raw json.RawMessage) (bool, tea.Cmd) {
	var event struct {
		Type, ID string
		Initial  bool
	}
	if json.Unmarshal(raw, &event) != nil {
		return false, nil
	}
	switch event.Type {
	case "goui_session_changed", "goui_session_snapshot":
		// The attach handshake precedes backend events. openedMsg already
		// requests its authoritative snapshot; repeating it invalidates that
		// in-flight result and loads the complete history a second time.
		if event.Type == "goui_session_snapshot" && event.Initial {
			return true, nil
		}
		if t.dead {
			return true, nil
		}
		t.revision++
		return true, m.refresh(t)
	case "goui_session_ui_resolved":
		if event.ID == "" {
			return true, nil
		}
		kept := t.dialogs[:0]
		removed := false
		for _, d := range t.dialogs {
			if d.requestID == event.ID {
				d.editor.Area.Blur()
				removed = true
				continue
			}
			kept = append(kept, d)
		}
		t.dialogs = kept
		if removed && t == m.current() {
			return true, m.focus(t)
		}
		return true, nil
	}
	return false, nil
}
