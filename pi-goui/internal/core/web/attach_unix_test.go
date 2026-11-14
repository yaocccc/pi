//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/coder/websocket"
	"github.com/creack/pty"
)

type attachCapture struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *attachCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}
func attachTTY(t *testing.T, drain bool) (*os.File, *os.File, *attachCapture) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 90, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	capture := &attachCapture{}
	if drain {
		fd, err := openAttachFD(master)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 32768)
			for {
				n, err := fd.read(ctx, buf)
				if err != nil {
					return
				}
				capture.mu.Lock()
				_, _ = capture.b.Write(buf[:n])
				capture.mu.Unlock()
			}
		}()
		t.Cleanup(func() { cancel(); <-done; _ = fd.restore() })
	}
	return master, slave, capture
}
func awaitAttach(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("attach condition timed out")
}
func assertTTYRestored(t *testing.T, file *os.File, before *term.State) {
	t.Helper()
	after, err := term.GetState(file.Fd())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal state not restored: %v", err)
	}
}

func TestAttachSharedInputAndLatestActivityResize(t *testing.T) {
	s, h, log := terminalServer(t)
	master, slave, output := attachTTY(t, true)
	before, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result AttachResult
		err    error
	}
	done := make(chan outcome, 1)
	start := func() {
		go func() {
			r, e := Attach(ctx, AttachOptions{URL: h.URL, Token: s.Token(), ProjectID: s.projectID, Input: slave, Output: slave})
			done <- outcome{r, e}
		}()
	}
	start()
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "SIZE:90:30") })
	if _, err := master.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:68656c6c6f") })
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 110, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "SIZE:110:40") })
	browser := dialTerminal(t, s, h)
	readUntil(t, browser, "PID:")
	sendInput(t, browser, []byte("browser-live"))
	readUntil(t, browser, "INPUT:62726f777365722d6c697665")
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:62726f777365722d6c697665") })
	select {
	case out := <-done:
		t.Fatalf("browser join detached desktop: %+v", out)
	default:
	}
	if _, err := master.Write([]byte("desktop-live")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, browser, "INPUT:6465736b746f702d6c697665")
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:6465736b746f702d6c697665") })
	// Actual input reclaims local geometry without disconnecting another peer.
	eventually(t, func() bool {
		s.lifecycle.Lock()
		defer s.lifecycle.Unlock()
		s.terminal.mu.Lock()
		defer s.terminal.mu.Unlock()
		return s.terminal.cols == 110 && s.terminal.rows == 40
	})
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("shared clients started a second TUI")
	}
	cancel()
	select {
	case out := <-done:
		if !errors.Is(out.err, context.Canceled) {
			t.Fatalf("cancel: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("input reader or resize waiter leaked")
	}
	assertTTYRestored(t, slave, before)
	sendInput(t, browser, []byte("browser-after-desktop-detach"))
	readUntil(t, browser, "INPUT:62726f777365722d61667465722d6465736b746f702d646574616368")
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("shared clients duplicated TUI")
	}
}

func TestAttachFragmentedTerminalReplyDoesNotClaimGeometry(t *testing.T) {
	s, h, _ := terminalServer(t)
	master, slave, output := attachTTY(t, true) // Physical desktop is 90x30.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Attach(ctx, AttachOptions{URL: h.URL, Token: s.Token(), ProjectID: s.projectID, Input: slave, Output: slave})
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Attach reader leaked")
		}
	})
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "SIZE:90:30") })
	browser := dialTerminal(t, s, h) // New viewer is 80x24.
	readUntil(t, browser, `"type":"ready"`)
	eventually(t, func() bool {
		s.lifecycle.Lock()
		defer s.lifecycle.Unlock()
		s.terminal.mu.Lock()
		defer s.terminal.mu.Unlock()
		return s.terminal.cols == 80 && s.terminal.rows == 24
	})
	// Terminal reads need not align to ANSI replies. A split DA response is
	// still protocol traffic, not the user returning to the desktop.
	if _, err := master.Write([]byte("\x1b[?1;")); err != nil {
		t.Fatal(err)
	}
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:1b5b3f313b") })
	if _, err := master.Write([]byte("2c")); err != nil {
		t.Fatal(err)
	}
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:3263") })
	s.lifecycle.Lock()
	term := s.terminal
	s.lifecycle.Unlock()
	term.mu.Lock()
	cols, rows := term.cols, term.rows
	term.mu.Unlock()
	if cols != 80 || rows != 24 {
		t.Fatalf("fragmented terminal reply stole geometry: got %dx%d, want 80x24", cols, rows)
	}
}

func TestAttachLocalDetachKeepsTerminalAlive(t *testing.T) {
	s, h, log := terminalServer(t)
	master, slave, output := attachTTY(t, true)
	before, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := Attach(ctx, AttachOptions{URL: h.URL, Token: s.Token(), ProjectID: s.projectID, Input: slave, Output: slave})
		if err == nil && result.Type != "detached" {
			err = fmt.Errorf("unexpected detach result: %+v", result)
		}
		done <- err
	}()
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "PID:") })
	if _, err := master.Write([]byte{0x1d}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("local detach did not cancel the client")
	}
	assertTTYRestored(t, slave, before)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	if got := logContents(t, log); strings.Count(got, "START:") != 1 || strings.Contains(got, "INPUT:1d") {
		t.Fatalf("local escape leaked or started a second backend: %s", got)
	}
}

func TestDesktopAttachPrivateClient(t *testing.T) {
	s, endpoint, log := desktopFixture(t, t.TempDir())
	master, slave, output := attachTTY(t, true)
	before, err := term.GetState(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := Attach(ctx, AttachOptions{URL: "http://desktop.local", Token: s.Token(), ProjectID: s.projectID, Input: slave, Output: slave, HTTPClient: endpoint.HTTPClient})
		if err == nil && result.Type != "detached" {
			err = fmt.Errorf("unexpected private attach result: %+v", result)
		}
		done <- err
	}()
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "PID:") })
	if _, err := master.Write([]byte("private")); err != nil {
		t.Fatal(err)
	}
	awaitAttach(t, func() bool { return strings.Contains(output.text(), "INPUT:70726976617465") })
	if _, err := master.Write([]byte{0x1d}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("private attach did not detach")
	}
	assertTTYRestored(t, slave, before)
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("private attach duplicated TUI")
	}
}

func TestAttachExitIsNotTakeover(t *testing.T) {
	s, h, _ := terminalServer(t, "--exit-immediate")
	_, slave, _ := attachTTY(t, true)
	before, _ := term.GetState(slave.Fd())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Attach(ctx, AttachOptions{URL: h.URL, Token: s.Token(), ProjectID: s.projectID, Input: slave, Output: slave})
	if err != nil || result.Type != "exit" || result.Code != 7 {
		t.Fatalf("exit: %+v %v", result, err)
	}
	assertTTYRestored(t, slave, before)
}

func TestAttachCancellationWithBlockedOutput(t *testing.T) {
	_, slave, _ := attachTTY(t, false)
	before, _ := term.GetState(slave.Fd())
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"ready"}`))
		_ = c.Write(ctx, websocket.MessageBinary, bytes.Repeat([]byte("x"), 2<<20))
		_, _, _ = c.Read(ctx)
	}))
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Attach(ctx, AttachOptions{URL: h.URL, ProjectID: "project", Input: slave, Output: slave})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("blocked output was not cancellable")
	}
	assertTTYRestored(t, slave, before)
}

func TestAttachWaitForRetakeIsCancellable(t *testing.T) {
	master, slave, _ := attachTTY(t, true)
	before, _ := term.GetState(slave.Fd())
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		ok, err := WaitForRetake(ctx, slave)
		cancel()
		if ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%v %v", ok, err)
		}
	}
	if _, err := master.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ok, err := WaitForRetake(ctx, slave)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	assertTTYRestored(t, slave, before)
}
