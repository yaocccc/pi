package session

import (
	"encoding/json"
	"testing"
)

func TestPartialWriteArguments(t *testing.T) {
	cases := []struct{ data, want string }{
		{`{"path":"a.txt","content":"中文`, "中文"},
		{`{"content":"a\nquote\"tab\t`, "a\nquote\"tab\t"},
		{`{"content":"a\`, "a"},
		{`{"content":"a\u4e`, "a"},
		{`{"content":"a\u4e2d`, "a中"},
		{`{"content":"a\ud83d`, "a"},
		{`{"content":"a\ud83d\ude`, "a"},
		{`{"content":"a\ud83d\ude00`, "a😀"},
		{`{"content":"C:\\tmp\\file"}`, `C:\tmp\file`},
	}
	for _, tc := range cases {
		var args map[string]string
		if err := json.Unmarshal(partialWriteArguments(tc.data), &args); err != nil {
			t.Fatal(err)
		}
		if args["content"] != tc.want {
			t.Errorf("%s: got %q want %q", tc.data, args["content"], tc.want)
		}
	}
}

func TestStreamingWriteArgumentsAndFinalAuthority(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":0,"id":"w","toolName":"write"}}`)
	delta := func(text string) {
		raw, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "toolcall_delta", "contentIndex": 0, "delta": text}})
		s.Apply(raw)
	}
	check := func(want string) {
		t.Helper()
		if len(s.Items) != 1 {
			t.Fatalf("items: %+v", s.Items)
		}
		item := s.Items[0]
		var args map[string]string
		if err := json.Unmarshal(item.Arguments, &args); err != nil {
			t.Fatal(err)
		}
		if item.ID != "tool:w" || item.ToolName != "write" || item.HasResult || args["content"] != want {
			t.Fatalf("item %+v args %+v", item, args)
		}
	}
	delta(`{"path":"a.txt","content":"first\n中`)
	check("first\n中")
	delta(`文\ud83d`)
	check("first\n中文")
	delta(`\ude00`)
	check("first\n中文😀")
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":0,"toolCall":{"type":"toolCall","id":"w","name":"write","arguments":{"path":"a.txt","content":"authoritative"}}}}`)
	check("authoritative")
}
