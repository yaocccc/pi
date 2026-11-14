package web

import (
	"bytes"
	"github.com/charmbracelet/x/ansi"
)

// inputActivity retains partial terminal replies across OS reads. A lone Esc
// is resolved by the caller after a short input-idle timeout.
type inputActivity struct{ pending []byte }

func (a *inputActivity) feed(data []byte) bool {
	a.pending = append(a.pending, data...)
	active := false
	for len(a.pending) > 0 {
		// DecodeSequence treats a trailing ESC inside an OSC/DCS string as
		// cancellation; retain it until the next read can complete ST (ESC \\).
		if a.pending[len(a.pending)-1] == 0x1b && (bytes.HasPrefix(a.pending, []byte("\x1b]")) || bytes.HasPrefix(a.pending, []byte("\x1bP")) || bytes.HasPrefix(a.pending, []byte("\x1b_"))) {
			break
		}
		seq, _, n, state := ansi.DecodeSequence(a.pending, 0, nil)
		if n <= 0 || state != 0 {
			break
		}
		active = terminalInputActivity(seq) || active
		a.pending = a.pending[n:]
	}
	if len(a.pending) > 8192 {
		a.pending = nil
	}
	return active
}
func (a *inputActivity) idle() bool {
	if bytes.Equal(a.pending, []byte{0x1b}) {
		a.pending = nil
		return true
	}
	return false
}

// Terminal replies are not human activity and must not compete for shared
// geometry. Keys (including CSI-u), mouse reports and paste remain activity.
func terminalInputActivity(data []byte) bool {
	if bytes.Equal(data, []byte{0x1b}) {
		return true
	}
	for len(data) > 0 {
		seq, _, n, state := ansi.DecodeSequence(data, 0, nil)
		if n <= 0 {
			return false
		}
		if state != 0 {
			return false
		}
		data = data[n:]
		if bytes.HasPrefix(seq, []byte("\x1b[")) {
			final := seq[len(seq)-1]
			if final == 'c' || final == 'R' || final == 'n' || final == 'y' || final == 't' || (final == 'u' && bytes.HasPrefix(seq, []byte("\x1b[?"))) || (final == 'm' && bytes.HasPrefix(seq, []byte("\x1b[>"))) {
				continue
			}
		} else if bytes.HasPrefix(seq, []byte("\x1b]")) || bytes.HasPrefix(seq, []byte("\x1bP")) || bytes.HasPrefix(seq, []byte("\x1b_")) {
			continue
		}
		return true
	}
	return false
}
