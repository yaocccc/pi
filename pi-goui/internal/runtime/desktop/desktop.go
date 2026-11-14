// Package desktop provides private local desktop registration and discovery.
package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
)

// DesktopEndpoint is a private, same-user desktop registration, not a web
// listener. Its client addresses http://desktop.local over the Unix socket.
// The owner must close the HTTP server and its TUI as well as this endpoint.
type DesktopEndpoint struct {
	ID         string
	Listener   net.Listener
	HTTPClient *http.Client
	closeOnce  sync.Once
	cleanup    func()
}

func (e *DesktopEndpoint) Close() error {
	e.closeOnce.Do(func() {
		_ = e.Listener.Close()
		e.HTTPClient.CloseIdleConnections()
		e.cleanup()
	})
	return nil
}

// OpenDesktop registers a private endpoint scoped to the canonical agent root.
// No model credentials or settings are read, copied or modified.
func OpenDesktop(agentDir, token string) (*DesktopEndpoint, error) {
	return openDesktop(agentDir, token)
}

// Record contains private registry metadata, not an HTTP response. Its Token
// and Socket must never be exposed in public instance listings.
type Record struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Socket   string `json:"socket"`
	AgentDir string `json:"agentDir"`
	Token    string `json:"token"`
}

// Client returns a client restricted to this record's private Unix socket.
func (record Record) Client() *http.Client {
	return desktopClient(record.Socket)
}

func randomID() (string, error) {
	var value [32]byte
	_, err := rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:]), err
}
