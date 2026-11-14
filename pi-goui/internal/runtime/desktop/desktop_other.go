//go:build !unix

package desktop

import (
	"errors"
	"net/http"
)

func DesktopSharingSupported() bool { return false }
func openDesktop(string, string) (*DesktopEndpoint, error) {
	return nil, errors.New("automatic desktop discovery requires Unix sockets; use WSL on Windows")
}
func ReadDesktopRecords(string) []Record              { return nil }
func ReadDesktopRecord(string, string) (Record, bool) { return Record{}, false }
func desktopClient(string) *http.Client               { return &http.Client{} }
