package web

import "testing"

func TestInputActivityAcrossReads(t *testing.T) {
	var a inputActivity
	for _, part := range []string{"\x1b", "[?1;", "2c"} {
		if a.feed([]byte(part)) {
			t.Fatal("fragmented reply claimed activity")
		}
	}
	if a.idle() {
		t.Fatal("completed reply claimed activity on idle")
	}
	if a.feed([]byte("\x1b")) || !a.idle() || a.idle() {
		t.Fatal("lone Escape must claim activity exactly once on idle")
	}
	if a.feed([]byte("\x1b]11;rgb:")) || a.feed([]byte("0000/0000/0000\x1b")) || a.feed([]byte("\\")) {
		t.Fatal("fragmented color reply claimed activity")
	}
	if !a.feed([]byte("a")) {
		t.Fatal("real key after fragmented reply lost activity")
	}
}

func TestTerminalInputActivityDistinguishesKeysAndReplies(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		activity   bool
	}{
		{"text", "hello", true},
		{"CJK", "中", true},
		{"Enter", "\r", true},
		{"Escape", "\x1b", true},
		{"arrow", "\x1b[A", true},
		{"CSI-u-key", "\x1b[97;5u", true},
		{"mouse-press", "\x1b[<0;10;12M", true},
		{"mouse-release", "\x1b[<0;10;12m", true},
		{"paste", "\x1b[200~text\x1b[201~", true},
		{"DA", "\x1b[?1;2c", false},
		{"secondary-DA", "\x1b[>0;95;0c", false},
		{"CPR", "\x1b[12;30R", false},
		{"status", "\x1b[0n", false},
		{"mode", "\x1b[?2026;2$y", false},
		{"window-size", "\x1b[8;40;120t", false},
		{"keyboard-report", "\x1b[?1u", false},
		{"modify-keys", "\x1b[>4;2m", false},
		{"background-color", "\x1b]11;rgb:2222/2e2e/3232\x1b\\", false},
		{"DCS", "\x1bP1$r0m\x1b\\", false},
		{"mixed-replies", "\x1b[?1;2c\x1b[12;30R", false},
		{"reply-followed-by-key", "\x1b[?1;2ca", true},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalInputActivity([]byte(tc.data)); got != tc.activity {
				t.Fatalf("activity=%v, want %v for %q", got, tc.activity, tc.data)
			}
		})
	}
}
