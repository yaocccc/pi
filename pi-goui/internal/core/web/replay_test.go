package web

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
)

func TestReplayPreservesGeometryTransitions(t *testing.T) {
	term := &terminal{cols: 120, rows: 40, history: []byte("wide\nmobile\nwide-again"), historySizes: []historySize{{0, 120, 40}, {5, 40, 50}, {12, 120, 40}}}
	p := newPeer(nil)
	term.attach(p)
	var cols, rows int
	var got []string
	for len(p.output) > 0 {
		f := <-p.output
		if f.kind == websocket.MessageText {
			var size struct{ Cols, Rows int }
			if err := json.Unmarshal(f.data, &size); err != nil {
				t.Fatal(err)
			}
			cols, rows = size.Cols, size.Rows
		} else {
			got = append(got, string(f.data))
			if string(f.data) == "mobile\n" {
				if cols != 40 || rows != 50 {
					t.Fatal("mobile replay used current desktop geometry")
				}
			} else if cols != 120 || rows != 40 {
				t.Fatal("desktop replay used mobile geometry")
			}
		}
	}
	if len(got) != 3 || !bytes.Equal([]byte(got[0]+got[1]+got[2]), term.history) {
		t.Fatalf("replay lost bytes: %q", got)
	}
	term.trimHistory(7)
	if len(term.historySizes) != 2 || term.historySizes[0] != (historySize{0, 40, 50}) || term.historySizes[1] != (historySize{5, 120, 40}) || !term.truncated {
		t.Fatalf("trim lost starting geometry: %+v", term.historySizes)
	}
}
