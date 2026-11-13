package session

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func jsStringLength(s string) int {
	n := 2
	for _, r := range s {
		switch r {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			n += 2
		default:
			if r < 0x20 {
				n += 6
			} else if r > 0xffff {
				n += 2
			} else {
				n++
			}
		}
	}
	return n
}

// JSON.stringify's encoded length, without allocating another copy of large
// content strings or introducing Go's HTML/U+2028 escaping differences.
func jsJSONLength(v any) int {
	switch v := v.(type) {
	case nil:
		return 4
	case string:
		return jsStringLength(v)
	case bool:
		if v {
			return 4
		}
		return 5
	case float64:
		if v == 0 {
			return 1
		}
		format := byte('e')
		a := math.Abs(v)
		if a >= 1e-6 && a < 1e21 {
			format = 'f'
		}
		s := strconv.FormatFloat(v, format, -1, 64)
		s = strings.ReplaceAll(s, "e-0", "e-")
		s = strings.ReplaceAll(s, "e+0", "e+")
		return len(s)
	case []any:
		n := 2
		for i, x := range v {
			if i > 0 {
				n++
			}
			n += jsJSONLength(x)
		}
		return n
	case map[string]any:
		n := 2
		i := 0
		for k, x := range v {
			if i > 0 {
				n++
			}
			i++
			n += jsStringLength(k) + 1 + jsJSONLength(x)
		}
		return n
	}
	return 0
}
func argumentChars(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 2
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return utf16Length(string(raw))
	}
	return jsJSONLength(v)
}
func resultChars(raw json.RawMessage) int {
	n := 0
	for _, b := range blocks(raw) {
		switch b.Type {
		case "text":
			n += utf16Length(b.Text)
		case "image":
			n += 4800
		}
	}
	return n
}
func estimateItem(t *Item) {
	if !t.argumentCharsKnown {
		if t.bashExecution {
			var args struct{ Command string }
			_ = json.Unmarshal(t.Arguments, &args)
			t.argumentChars = utf16Length(args.Command)
		} else {
			t.argumentChars = argumentChars(t.Arguments)
		}
		t.argumentCharsKnown = true
	}
	if t.bashExecution {
		t.EstimatedTokens = (t.argumentChars + t.resultChars + 3) / 4
		return
	}
	t.EstimatedTokens = (t.argumentChars+3)/4 + (t.resultChars+3)/4
}

// Missing fields in a streaming snapshot do not erase known fields. Explicit
// empty content and details:null are authoritative replacements.
func mergeTool(t, old Item) Item {
	if t.ToolName == "" {
		t.ToolName = old.ToolName
	}
	if len(t.Arguments) == 0 {
		t.Arguments = old.Arguments
		t.argumentChars = old.argumentChars
		t.argumentCharsKnown = old.argumentCharsKnown
	}
	if len(t.Details) == 0 {
		t.Details = old.Details
	}
	if !t.HasResult && old.HasResult {
		t.HasResult = true
	}
	if !t.contentKnown && old.contentKnown {
		t.contentKnown = true
		t.Text = old.Text
		t.resultChars = old.resultChars
	}
	if !t.errorKnown {
		t.Error = old.Error
		t.errorKnown = old.errorKnown
	}
	return t
}
func (t *Item) setResult(content, details json.RawMessage, isError *bool) {
	t.HasResult = true
	if len(content) > 0 {
		t.contentKnown = true
		t.Text = textOf(content)
		t.resultChars = resultChars(content)
	}
	if len(details) > 0 {
		t.Details = details
	}
	if isError != nil {
		t.Error = *isError
		t.errorKnown = true
	}
}
