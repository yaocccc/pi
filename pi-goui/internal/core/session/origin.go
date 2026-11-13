package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ForkProvenance is UI-only metadata, not a conversation entry or model input.
// ParentSession remains the native direct-parent path on the session header.
type ForkProvenance struct {
	Version       int    `json:"version"`
	SourceID      string `json:"sourceId"`
	UserEntryID   string `json:"userEntryId"`
	CutoffEntryID string `json:"cutoffEntryId"`
	Turn          int    `json:"turn"`
}

type Header struct {
	Type          string          `json:"type"`
	ID            string          `json:"id"`
	Cwd           string          `json:"cwd"`
	ParentSession string          `json:"parentSession,omitempty"`
	Fork          *ForkProvenance `json:"piGouiFork,omitempty"`
}

// ReadHeader rejects symlinked paths and checks the opened file identity.
// expectedID prevents an old relationship
// from opening a different session that has replaced the file at that path.
func ReadHeader(path, expectedID string) (Header, error) {
	var h Header
	info, err := regularSessionPath(path)
	if err != nil {
		return h, err
	}
	f, err := os.Open(path)
	if err != nil {
		return h, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return h, fmt.Errorf("session file changed while opening")
	}
	raw, err := readRecord(bufio.NewReader(f))
	if err != nil && err != io.EOF {
		return h, err
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return Header{}, err
	}
	if h.Type != "session" || h.ID == "" || (expectedID != "" && h.ID != expectedID) {
		return Header{}, fmt.Errorf("session identity changed or invalid header")
	}
	final, err := regularSessionPath(path)
	if err != nil || !os.SameFile(info, final) {
		return Header{}, fmt.Errorf("session file changed while reading")
	}
	normalizeOrigin(&h)
	return h, nil
}

func normalizeOrigin(h *Header) {
	if p := h.Fork; p != nil && (p.Version != 1 || p.SourceID == "" || p.UserEntryID == "" || p.CutoffEntryID == "" || p.Turn < 1 || h.ParentSession == "") {
		// Unknown/invalid UI metadata cannot establish an exact location.
		h.Fork = nil
	}
}
