package ui

import (
	"path/filepath"
	"testing"

	"pi-goui/internal/core/session"
)

func TestRecentReusesEmptySlot(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	target := savedSession(t)
	m.openRecent(target)
	if len(m.tabs) != 1 || m.current() == old || m.byID(old.id) != nil || m.current().state.File != sessionPath(target) {
		t.Fatal("empty slot not replaced")
	}
	replacement := m.current()
	m.openRecent(target)
	if len(m.tabs) != 1 || m.current() != replacement {
		t.Fatal("selected target duplicated")
	}
}

func TestRecentPreservesNonemptyTab(t *testing.T) {
	for _, kind := range []string{"messages", "message-count", "draft", "images", "busy", "compacting", "sending", "awaiting", "steering", "followup", "pending-image", "clipboard", "refreshing", "loading-saved"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			old := m.current()
			switch kind {
			case "messages":
				old.state.Items = []session.Item{{Role: "user", Text: "keep message"}}
			case "message-count":
				old.state.MessageCount = 1
			case "draft":
				old.editor.SetValue("keep draft")
			case "images":
				old.images = []map[string]any{{"data": "keep image"}}
			case "busy":
				old.state.Busy = true
			case "compacting":
				old.state.Compacting = true
			case "sending":
				old.sending = true
			case "awaiting":
				old.awaitingStart = true
			case "steering":
				old.state.Steering = []string{"keep steering"}
			case "followup":
				old.state.FollowUp = []string{"keep followup"}
			case "pending-image":
				old.imagePending = 1
			case "clipboard":
				m.clipboardTarget = old
			case "refreshing":
				old.refreshing = true
			case "loading-saved":
				old.newSession = false
				old.state.File = savedSession(t)
			}
			target := savedSession(t)
			m.openRecent(target)
			if len(m.tabs) != 2 || m.tabs[0] != old || old.dead || len(old.dialogs) != 0 || m.current().state.File != sessionPath(target) {
				t.Fatal("nonempty tab was replaced or target not opened")
			}
			if kind == "messages" && old.state.Items[0].Text != "keep message" || kind == "draft" && old.editor.Value() != "keep draft" || kind == "busy" && !old.state.Busy || kind == "images" && len(old.images) != 1 {
				t.Fatal("original work changed")
			}
		})
	}
}

func TestRecentRejectsInvalidTarget(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	m.openRecent(filepath.Join(t.TempDir(), "missing"))
	if len(m.tabs) != 1 || m.current() != old {
		t.Fatal("missing file opened")
	}
	m.opts.NoSession = true
	m.openRecent(savedSession(t))
	if len(m.tabs) != 1 || m.current() != old {
		t.Fatal("ephemeral session opened saved target")
	}
}
