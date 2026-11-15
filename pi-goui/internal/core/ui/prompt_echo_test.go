package ui

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

func echoUserEvent(text string, timestamp int) rpc.Event {
	raw, _ := json.Marshal(map[string]any{"type": "message_end", "message": map[string]any{
		"role": "user", "timestamp": timestamp, "content": []map[string]any{{"type": "text", "text": text}},
	}})
	return rpc.Event{Data: raw}
}

func echoUserBlocks(owner *tab) int {
	count := 0
	for _, block := range owner.doc.Blocks {
		if block.Role == "user" {
			count++
		}
	}
	return count
}

func TestPromptEchoBeforeStartupIsLocalAndImmediate(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 42})
	owner := m.current()
	owner.setValue("FIRST-LOCAL-ECHO [image1]")
	owner.images = []map[string]any{{"type": "image", "mimeType": "image/png", "data": "AA=="}}
	cmd := m.submit(owner, false)
	if cmd == nil || owner.client != nil || owner.resume.submit == nil {
		t.Fatal("test must stop before starting the captured operation")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "FIRST-LOCAL-ECHO") || strings.Contains(view, "发送中") || strings.Contains(view, "A quiet place") || echoUserBlocks(owner) != 1 {
		t.Fatalf("missing normal immediate user bubble: %s", view)
	}
	if len(owner.state.Items) != 0 || owner.state.MessageCount != 0 || len(owner.doc.ImageLinks) != 1 || owner.editor.Value() != "" {
		t.Fatal("projection polluted native history or lost its attachment")
	}
	if _, err := os.Stat(owner.state.File); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexecuted submission created native history: %v", err)
	}
	owner.setValue("later draft")
	m.document(owner)
	if echoUserBlocks(owner) != 1 || owner.editor.Value() != "later draft" {
		t.Fatal("later typing replaced the captured bubble")
	}
}

func TestPromptEchoSurvivesPreviewAndInflightLayout(t *testing.T) {
	m, owner := previewModel(t)
	before, _ := os.ReadFile(owner.state.File)
	m.Update(m.previewSession(owner)())
	job := owner.documentJob
	if job == nil {
		t.Fatal("expected an asynchronous history layout")
	}
	owner.setValue("NEW-ECHO")
	m.submitChecked(owner, false, true)
	if !strings.Contains(ansi.Strip(owner.doc.Text), "NEW-ECHO") {
		t.Fatal("echo waited for in-flight history layout")
	}
	owner.setValue("later")
	// Publish the compatible native layout; it cannot remove the local echo.
	m.receiveDocument(job.render().(documentResult))
	if !strings.Contains(ansi.Strip(owner.doc.Text), "NEW-ECHO") || strings.Count(ansi.Strip(owner.doc.Text), "NEW-ECHO") != 1 {
		t.Fatal("native layout erased or duplicated the echo")
	}
	if len(owner.state.Items) != 1 || owner.state.Items[0].Text != "SAVED-LOCAL-HISTORY" || owner.editor.Value() != "later" {
		t.Fatal("preview or echo changed native state or newer input")
	}
	after, _ := os.ReadFile(owner.state.File)
	if string(before) != string(after) {
		t.Fatal("local echo wrote the session file")
	}
}

func TestPromptEchoConfirmationUsesNewNativeIdentity(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	owner.client, _ = fakeClient(t)
	old := echoUserEvent("same text", 1)
	owner.state.Apply(old.Data)
	m.document(owner)
	owner.setValue("same text")
	cmd := m.sendPrompt(owner, owner.editor.Value(), nil, false, true)
	if cmd == nil || echoUserBlocks(owner) != 2 || len(owner.state.Items) != 1 {
		t.Fatal("same-text history suppressed the new local echo")
	}
	e := owner.promptEcho
	m.Update(eventBatchMsg{id: owner.id, client: owner.client, events: []rpc.Event{old}})
	if owner.promptEcho != e {
		t.Fatal("old native record confirmed the new prompt")
	}
	// Backend transformations remain authoritative; confirmation does not rely
	// on matching text, which would lose input-hook rewrites and same-text sends.
	staleLayout := m.scheduleDocument(owner)
	if staleLayout == nil {
		t.Fatal("expected native-only layout before confirmation")
	}
	current := echoUserEvent("transformed by backend", 2)
	m.Update(eventBatchMsg{id: owner.id, client: owner.client, events: []rpc.Event{current}})
	if owner.promptEcho != nil || echoUserBlocks(owner) != 2 || len(owner.state.Items) != 2 {
		t.Fatal("native confirmation did not replace exactly one projected bubble")
	}
	m.Update(staleLayout())
	if !strings.Contains(ansi.Strip(owner.doc.Text), "transformed by backend") || strings.Count(ansi.Strip(owner.doc.Text), "same text") != 1 {
		t.Fatal("confirmation retained local text instead of native history")
	}
	m.Update(eventBatchMsg{id: owner.id, client: owner.client, events: []rpc.Event{current}})
	m.document(owner)
	if echoUserBlocks(owner) != 2 {
		t.Fatal("repeated native event duplicated the confirmed message")
	}
	owner.setValue("newer draft")
	m.Update(replyMsg{id: owner.id, client: owner.client, generation: owner.cycle.generation, op: "prompt", echo: e, err: errors.New("unknown outcome")})
	if len(owner.state.Items) != 2 || echoUserBlocks(owner) != 2 || !strings.Contains(owner.editor.Value(), "newer draft") {
		t.Fatal("late failure erased authoritative history or newer input")
	}
}

func TestPromptEchoSnapshotConfirmationAndQueuedDisposition(t *testing.T) {
	for _, kind := range []string{"snapshot", "queued", "handled", "loaded queue"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			owner := m.current()
			owner.client, _ = fakeClient(t)
			owner.setValue("captured")
			m.sendPrompt(owner, "captured", nil, false, true)
			e := owner.promptEcho
			switch kind {
			case "snapshot":
				v := snapshotMsg{id: owner.id, revision: owner.revision, state: json.RawMessage(`{"sessionId":"native"}`), entries: json.RawMessage(`{"entries":[{"type":"message","id":"native-user","message":{"role":"user","timestamp":7,"content":[{"type":"text","text":"captured"}]}}]}`)}
				m.Update(prepareSnapshotMessage(owner, v))
				m.document(owner)
				if len(owner.state.Items) != 1 || echoUserBlocks(owner) != 1 {
					t.Fatal("authoritative snapshot duplicated or lost the prompt")
				}
			case "loaded queue":
				owner.state.Steering = []string{"earlier"}
				m.sendPrompt(owner, "another", nil, false, true)
			default:
				m.reply(owner, replyMsg{op: "prompt", echo: e, data: json.RawMessage(`{"disposition":"` + kind + `"}`)})
			}
			if owner.promptEcho != nil {
				t.Fatal("authoritative result left a local prompt projection")
			}
		})
	}
}

func TestPromptEchoStartupFailureRestoresDraftAndImages(t *testing.T) {
	for _, failure := range []error{errors.New("cannot launch"), &rpc.SessionBusyError{Status: rpc.SessionStatus{Busy: true, OwnerPID: 42}}} {
		t.Run(failure.Error(), func(t *testing.T) {
			m, owner := previewModel(t)
			owner.setValue("first [image1]")
			owner.images = []map[string]any{{"data": "first", "mimeType": "image/png"}}
			m.submitChecked(owner, false, true)
			owner.setValue("later [image1]")
			owner.images = []map[string]any{{"data": "later", "mimeType": "image/png"}}
			m.Update(openedMsg{id: owner.id, generation: owner.cycle.generation, err: failure})
			if owner.promptEcho != nil || owner.editor.Value() != "first [image1]\n\nlater [image2]" || len(owner.images) != 2 || len(owner.state.Items) != 0 {
				t.Fatal("startup failure left echo/history or damaged retained drafts")
			}
			if owner.resume.submit != nil || owner.client != nil {
				t.Fatal("startup failure retained an automatic retry")
			}
		})
	}
}

func TestPromptEchoPromptFailureAndDisconnectRestoreOnce(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "disconnect then reply"}[disconnect], func(t *testing.T) {
			m := testModel(t, "")
			owner := m.current()
			owner.client, _ = fakeClient(t)
			owner.setValue("first [image1]")
			owner.images = []map[string]any{{"data": "first"}}
			m.sendPrompt(owner, owner.editor.Value(), owner.images, false, true)
			e := owner.promptEcho
			owner.setValue("later [image1]")
			owner.images = []map[string]any{{"data": "later"}}
			if disconnect {
				m.closeEventStream(owner, errors.New("connection lost"))
			}
			m.reply(owner, replyMsg{op: "prompt", echo: e, err: errors.New("rejected")})
			if owner.promptEcho != nil || echoUserBlocks(owner) != 0 || owner.editor.Value() != "first [image1]\n\nlater [image2]" || len(owner.images) != 2 {
				t.Fatal("failure duplicated, erased or failed to restore the draft")
			}
		})
	}
}

func TestPromptEchoRejectsForeignClientCycleAndTab(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	owner.client, _ = fakeClient(t)
	owner.setValue("local")
	m.sendPrompt(owner, "local", nil, false, true)
	e := owner.promptEcho
	oldClient, _ := fakeClient(t)
	m.Update(eventBatchMsg{id: owner.id, client: oldClient, events: []rpc.Event{echoUserEvent("foreign", 2)}})
	m.Update(replyMsg{id: owner.id, client: owner.client, generation: owner.cycle.generation + 1, op: "prompt", echo: e, err: errors.New("stale")})
	other := m.addTab("")
	m.Update(eventBatchMsg{id: other.id, events: []rpc.Event{echoUserEvent("other tab", 3)}})
	if owner.promptEcho != e || len(owner.state.Items) != 0 || owner.editor.Value() != "" {
		t.Fatal("foreign event or reply changed the pending echo")
	}
	m.activate(0)
	m.document(owner)
	if echoUserBlocks(owner) != 1 || !strings.Contains(ansi.Strip(m.View().Content), "local") {
		t.Fatal("tab navigation lost the echo")
	}
}

func TestPromptEchoDoesNotProjectControlsOrQueuedInputs(t *testing.T) {
	for _, kind := range []string{"slash", "busy", "steering", "followup", "readonly"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			owner := m.current()
			text := "input"
			switch kind {
			case "slash":
				text = "/model"
			case "busy":
				owner.state.Busy = true
			case "steering":
				owner.state.Steering = []string{"earlier"}
			case "followup":
				owner.state.FollowUp = []string{"earlier"}
			case "readonly":
				owner.cycle.presence = rpc.SessionStatus{Busy: true, OwnerPID: 42}
			}
			owner.setValue(text)
			if kind == "readonly" {
				m.submit(owner, false)
			} else {
				m.showPromptEcho(owner, text, nil, nil)
			}
			if owner.promptEcho != nil || len(owner.state.Items) != 0 {
				t.Fatal("control, queue or rejected input was shown as a prompt")
			}
		})
	}
}

func TestPromptEchoGeometryKeepsNativeDocumentImmutable(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	owner.state.Items = []session.Item{{ID: "native", Role: "assistant", Text: "history"}}
	m.document(owner)
	base := owner.nativeDocument
	m.showPromptEcho(owner, "echo [image1]", []map[string]any{{"data": "AA==", "mimeType": "image/png"}}, nil)
	if len(base.Blocks) != 1 || base.Text != owner.nativeDocument.Text || len(base.ImageLinks) != 0 || len(owner.doc.ImageLinks) != 1 {
		t.Fatal("echo mutated the published native document")
	}
	if len(owner.doc.Content) != len(strings.Split(owner.doc.Text, "\n")) {
		t.Fatal("echo broke selection row geometry")
	}
	last := owner.doc.Blocks[len(owner.doc.Blocks)-1]
	if last.Start < len(strings.Split(base.Text, "\n")) || last.End > len(owner.documentLines()) || owner.doc.ImageLinks[0].Row < last.Start {
		t.Fatal("echo block or attachment link has an incorrect row offset")
	}
	m.clearPromptEcho(owner, owner.promptEcho)
	if owner.doc.Text != base.Text || len(owner.doc.Blocks) != 1 {
		t.Fatal("removing echo changed native history")
	}
}
