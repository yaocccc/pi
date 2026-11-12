package tooldetail

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"pi-goui/internal/session"
)

func (v *renderer) obsRecall(item session.Item, args, details map[string]any) {
	v.heading("Stored observation")
	// Remove only a byte-for-byte matching generated header; never search or
	// trim the excerpt itself, whose whitespace and trailing lines are data.
	text := item.Text
	id, idOK := details["id"].(string)
	eof, eofOK := details["eof"].(bool)
	numeric := true
	for _, key := range []string{"offset", "nextOffset", "bytes", "lines"} {
		if _, ok := details[key].(json.Number); !ok {
			numeric = false
		}
	}
	if idOK && eofOK && numeric {
		header := fmt.Sprintf("[obs_recall id=%s offset=%s next_offset=%s eof=%t]\n[chunk_bytes=%s chunk_lines=%s; use next_offset to continue]\n",
			id, details["offset"], details["nextOffset"], eof, details["bytes"], details["lines"])
		text = strings.TrimPrefix(text, header)
	}
	params := copyMap(args)
	for _, key := range []string{"id", "offset"} {
		if value, ok := details[key]; ok && reflect.DeepEqual(value, params[key]) {
			delete(params, key)
		}
	}
	if len(params) > 0 {
		v.heading("Request")
		v.value(params, "")
	}
	for _, field := range []struct{ key, label string }{
		{"id", "Observation ID"}, {"offset", "Byte offset"},
		{"bytes", "Chunk bytes"}, {"lines", "Chunk lines"},
		{"nextOffset", "Next byte offset"},
	} {
		v.field(details, field.key, field.label)
	}
	if eofOK {
		if eof {
			v.line("✓ End of observation", v.theme.Success)
		} else {
			v.line("→ More available · continue at next byte offset", v.theme.Muted)
		}
		delete(details, "eof")
	}
	if text != "" {
		v.heading("Excerpt")
		v.line(text, v.theme.Text)
	} else if item.HasResult {
		v.line("(empty excerpt)", v.theme.Muted)
	} else if item.Running {
		v.line("● Reading stored observation", v.theme.Warning)
	} else {
		v.line("No excerpt recorded", v.theme.Muted)
	}
}
