package web

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTerminalWideRuneTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/unicode-wide-ranges.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual [][2]int
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	start := -1
	for r := 0; r <= 0x110000; r++ {
		wide := r < 0x110000 && ansi.StringWidth(string(rune(r))) == 2
		if wide && start < 0 {
			start = r
		} else if !wide && start >= 0 {
			actual = append(actual, [2]int{start, r - 1})
			start = -1
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("Go width rules changed: regenerate unicode-wide-ranges.json and the vendored grapheme addon's gouiWideRanges")
	}
}

// The Chromium test checks these same fixtures against xterm's live cursor
// position and frame edges, so terminal and TUI cannot silently disagree.
func TestTerminalUnicodeWidths(t *testing.T) {
	raw, err := os.ReadFile("testdata/unicode-widths.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text  string `json:"text"`
		Width int    `json:"width"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := ansi.StringWidth(c.Text); got != c.Width {
			t.Errorf("TUI width %q = %d, want %d", c.Text, got, c.Width)
		}
	}
}
