package session

import (
	"strings"
	"testing"
)

func TestHideWebContentReadyNotification(t *testing.T) {
	const notice = `{"role":"custom","customType":"web-search-content-ready","display":true,"content":"fetch completed","timestamp":1}`
	const other = `{"role":"custom","customType":"web-search-error","display":true,"content":"fetch failed","timestamp":2}`
	for _, source := range []string{"messages", "entries", "live"} {
		t.Run(source, func(t *testing.T) {
			var s State
			switch source {
			case "messages":
				if err := s.LoadMessages([]byte(`{"messages":[` + notice + `,` + other + `]}`)); err != nil {
					t.Fatal(err)
				}
			case "entries":
				if err := s.LoadEntries([]byte(`{"entries":[{"type":"message","id":"a","parentId":null,"message":` + notice + `},{"type":"message","id":"b","parentId":"a","message":` + other + `}],"leafId":"b"}`)); err != nil {
					t.Fatal(err)
				}
			case "live":
				apply(&s, `{"type":"message_end","message":`+notice+`}`)
				apply(&s, `{"type":"message_end","message":`+other+`}`)
			}
			if strings.Contains(texts(&s), "fetch completed") || !strings.Contains(texts(&s), "fetch failed") {
				t.Fatalf("unexpected transcript: %s", texts(&s))
			}
			if s.MessageCount != 2 || len(s.Items) != 1 || s.Items[0].MessageIndex != 2 {
				t.Fatalf("notification changed message ordinals: %+v", s.Items)
			}
		})
	}
}
