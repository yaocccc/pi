package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserImageProjectionOneRecordOneItem(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"你照这个图改 [image1]，然后参考这个图 [image2]"},{"type":"image","data":"PRIVATE_FIRST","mimeType":"image/png"},{"type":"image","data":"PRIVATE_SECOND","mimeType":"image/jpeg"}]`)
	r := record{id: "user", msg: message{Role: "user", Content: raw}}
	p := r.project()
	if len(p.blocks) != 1 {
		t.Fatalf("user record split into %d blocks", len(p.blocks))
	}
	item := p.blocks[0].item
	if item.ID != "user/0" || item.Text != "你照这个图改 [image1]，然后参考这个图 [image2]" || len(item.Images) != 2 {
		t.Fatalf("wrong projection: %+v", item)
	}
	if item.Images[0].Data != "PRIVATE_FIRST" || item.Images[1].MimeType != "image/jpeg" {
		t.Fatal("saved image order/data lost")
	}
	if strings.Contains(item.Text, "PRIVATE_") || string(raw) != string(r.msg.Content) {
		t.Fatal("image leaked or source mutated")
	}
	if r.project() != p {
		t.Fatal("record projection cache lost")
	}
	var state State
	messageBytes, _ := json.Marshal(r.msg)
	entries := []map[string]any{{"id": "user", "type": "message", "message": json.RawMessage(messageBytes)}}
	payload, _ := json.Marshal(map[string]any{"entries": entries, "leafId": "user"})
	if err := state.LoadEntries(payload); err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 || state.MessageCount != 1 || state.Items[0].MessageIndex != 1 {
		t.Fatal("image blocks changed message count")
	}
	if err := state.LoadEntries(payload); err != nil {
		t.Fatal(err)
	}
	if strings.Count(state.Items[0].Text, "[image1]") != 1 {
		t.Fatal("replay duplicated references")
	}
}

func TestLegacyUserImagesKeepInlineOrderAndParagraphs(t *testing.T) {
	for _, tc := range []struct {
		content, want string
		count         int
	}{
		{`[{"type":"text","text":"照这个图改"},{"type":"image","data":"one"},{"type":"text","text":"然后参考这个图"},{"type":"image","data":"two"}]`, "照这个图改 [image1] 然后参考这个图 [image2]", 2},
		{`[{"type":"image","data":"one"},{"type":"image","data":"two"}]`, "[image1] [image2]", 2},
		{`[{"type":"text","text":"first\n\nparagraph"},{"type":"text","text":"second"}]`, "first\n\nparagraph\nsecond", 0},
		{`[{"type":"text","text":"[image2] then [image1]"},{"type":"image","data":"one"},{"type":"image","data":"two"}]`, "[image2] then [image1]", 2},
		{`"plain user input"`, "plain user input", 0},
	} {
		text, images := userContent(json.RawMessage(tc.content))
		if text != tc.want || len(images) != tc.count {
			t.Fatalf("got %q / %d, want %q / %d", text, len(images), tc.want, tc.count)
		}
		if userText(json.RawMessage(tc.content)) != text {
			t.Fatal("history preview differs from message projection")
		}
	}
	raw := json.RawMessage(`[{"type":"text","text":"tool result"},{"type":"image","data":"PRIVATE"}]`)
	if got := textOf(raw); got != "tool result\n[image]" {
		t.Fatalf("tool output changed: %q", got)
	}
}
