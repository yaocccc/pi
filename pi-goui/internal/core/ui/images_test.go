package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestInlineImagePasteAndPrompt(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	first := map[string]any{"type": "image", "data": "AA==", "mimeType": "image/png"}
	second := map[string]any{"type": "image", "data": "AQ==", "mimeType": "image/png"}
	tab.setValue("你照这个图改 ，然后参考这个图 ")
	at := len([]rune("你照这个图改 "))
	tab.editor.CompleteRange(at, at, "", 0)
	m.Update(clipboardImageMsg{id: tab.id, image: first})
	end := len([]rune(tab.editor.Value()))
	tab.editor.CompleteRange(end, end, "", 0)
	m.Update(clipboardImageMsg{id: tab.id, image: second})
	want := "你照这个图改 [image1]，然后参考这个图 [image2]"
	assertValue(t, tab, want)
	if !strings.Contains(m.View().Content, "38;2;0;135;255") || strings.Contains(m.View().Content, "\x1b[4m") {
		t.Fatal("draft image reference must be blue without underline")
	}
	if strings.Contains(strings.Join(m.extraLines(tab), "\n"), "image(s) attached") {
		t.Fatal("separate attachment row remains")
	}
	client, wire := fakeClient(t)
	tab.client = client
	cmd := m.submit(tab, false)
	if cmd == nil {
		t.Fatal("no prompt")
	}
	m.Update(cmd())
	records := wireRecords(t, wire, 1)
	record := records[0]
	images, ok := record["images"].([]any)
	if record["message"] != want || !ok || len(images) != 2 || images[0].(map[string]any)["data"] != "AA==" || images[1].(map[string]any)["data"] != "AQ==" {
		t.Fatalf("wrong RPC: %v", record)
	}
	raw, _ := json.Marshal(map[string]any{"messages": []map[string]any{{"role": "user", "content": []any{map[string]any{"type": "text", "text": record["message"]}, images[0], images[1]}}}})
	var state session.State
	if err := state.LoadMessages(raw); err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 || state.Items[0].Text != want || len(state.Items[0].Images) != 2 {
		t.Fatalf("message split/duplicated: %+v", state.Items)
	}
}

func TestImageReferencesFilterReorderAndDeduplicate(t *testing.T) {
	images := []map[string]any{{"data": "one"}, {"data": "two"}, {"data": "three"}}
	text, selected := referencedImages("参考 [image3] 再看 [image1]，重复 [image3]，普通 [image99]", images)
	if text != "参考 [image1] 再看 [image2]，重复 [image1]，普通 [image99]" || len(selected) != 2 || selected[0]["data"] != "three" || selected[1]["data"] != "one" {
		t.Fatalf("incorrect mapping: %s %v", text, selected)
	}
	_, selected = referencedImages("removed all references", images)
	if len(selected) != 0 {
		t.Fatal("deleted image reference still sends bytes")
	}
}

func TestPendingImageKeepsOriginalPastePointAndSession(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.client, _ = fakeClient(t)
	first.setValue("照这个图改 ")
	cmd := m.startImagePaste(first, func() tea.Msg {
		return clipboardImageMsg{id: first.id, image: map[string]any{"type": "image", "data": "AA==", "mimeType": "image/png"}}
	}, false)
	m.editDraft(first, "", tea.PasteMsg{Content: "，然后继续"})
	if m.submit(first, false) != nil {
		t.Fatal("sent prompt while clipboard was pending")
	}
	second := m.addTab("")
	second.setValue("other draft")
	m.activate(1)
	m.Update(cmd())
	assertValue(t, first, "照这个图改 [image1]，然后继续")
	assertValue(t, second, "other draft")
	if first.editor.CursorOffset() != len([]rune(first.editor.Value())) || first.imagePending != 0 {
		t.Fatal("paste moved caret or left pending state")
	}
	m.activate(0)
	cmd = m.startImagePaste(first, func() tea.Msg { return clipboardImageMsg{id: first.id, image: map[string]any{"data": "unused"}} }, false)
	press(m, 'c', tea.ModCtrl, "")
	m.Update(cmd())
	if first.editor.Value() != "" || len(first.images) != 0 || first.imagePending != 0 {
		t.Fatal("late image resurrected cancelled draft")
	}
}

func TestImageClickRoutesImageButDragDoesNot(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	img := session.Image{Data: "image bytes", MimeType: "image/png"}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tab.state.Items = []session.Item{{ID: "u", Role: "user", Text: "看这里 [image1]", Images: []session.Image{img}}}
	tab.dirty = true
	m.document(tab)
	if len(tab.doc.ImageLinks) != 1 {
		t.Fatal("image link missing")
	}
	link := tab.doc.ImageLinks[0]
	opened := 0
	m.imagePreviews = &previewStub{open: func(got session.Image) error {
		opened++
		if got != img {
			t.Fatal("preview image changed")
		}
		return nil
	}}
	click := tea.Mouse{Button: tea.MouseLeft, X: link.Start, Y: link.Row - tab.offset + transcriptTop}
	m.beginSelection(tab, click)
	cmd := m.mouseRelease(click)
	if cmd == nil {
		t.Fatal("link click did not open")
	}
	result := cmd().(imagePreviewMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	if opened != 1 {
		t.Fatal("wrong viewer invocation count")
	}
	m.beginSelection(tab, click)
	drag := click
	drag.X = link.End - 1
	m.mouseMotion(drag)
	if cmd = m.mouseRelease(drag); cmd != nil {
		_ = cmd()
	}
	if opened != 1 {
		t.Fatal("drag opened image")
	}
	if !strings.Contains(ansi.Strip(tab.doc.Text), "[image1]") {
		t.Fatal("click changed message")
	}
	tab.selection = transcriptSelection{}
	tab.dialogs = []*dialog{m.newDialog("confirm", "Confirm", "")}
	m.beginSelection(tab, click)
	if m.mouseRelease(click) != nil {
		t.Fatal("modal allowed image click through")
	}
}
