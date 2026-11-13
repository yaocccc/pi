package detail

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"pi-goui/internal/core/session"
)

func (v *Renderer) ObsRecall(item session.Item, args, details map[string]any) {
	v.Heading("Stored observation")
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
		v.Heading("Request")
		v.Value(params, "")
	}
	for _, field := range []struct{ key, label string }{
		{"id", "Observation ID"}, {"offset", "Byte offset"},
		{"bytes", "Chunk bytes"}, {"lines", "Chunk lines"},
		{"nextOffset", "Next byte offset"},
	} {
		v.Field(details, field.key, field.label)
	}
	if eofOK {
		if eof {
			v.Line("✓ End of observation", v.Theme.Success)
		} else {
			v.Line("→ More available · continue at next byte offset", v.Theme.Muted)
		}
		delete(details, "eof")
	}
	if text != "" {
		v.Heading("Excerpt")
		v.Line(text, v.Theme.Text)
	} else if item.HasResult {
		v.Line("(empty excerpt)", v.Theme.Muted)
	} else if item.Running {
		v.Line("● Reading stored observation", v.Theme.Warning)
	} else {
		v.Line("No excerpt recorded", v.Theme.Muted)
	}
}
