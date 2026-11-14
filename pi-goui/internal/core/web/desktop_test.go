package web

import (
	"bufio"
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

type desktopHijackFixture struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w desktopHijackFixture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestDesktopProxyCancelsBlockedBrowserWrite(t *testing.T) {
	frontend, browser := net.Pipe()
	defer frontend.Close()
	defer browser.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := desktopProxyWriter{ResponseWriter: desktopHijackFixture{httptest.NewRecorder(), frontend}, ctx: ctx}
	conn, _, err := writer.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := conn.Write(make([]byte, 1<<20)); done <- err }()
	// A client that never reads must not keep WebUI.Close waiting forever.
	select {
	case <-done:
		t.Fatal("write unexpectedly completed without a browser reader")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled hijacked connection accepted output")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown failed to interrupt blocked browser output")
	}
}
