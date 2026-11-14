//go:build unix

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
)

func terminalServer(t *testing.T, args ...string) (*Server, *httptest.Server, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for isolated PTY test")
	}
	script, err := filepath.Abs("testdata/terminal.py")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "processes.log")
	s := testServer(t, func(c *Config) {
		c.Command = append([]string{python, "-u", script}, args...)
		c.Env = append(os.Environ(), "WEBUI_TEST_LOG="+log, "TERM=bad", "COLORTERM=bad")
	})
	h := httptest.NewServer(s.Handler())
	t.Cleanup(h.Close)
	return s, h, log
}
func dialTerminal(t *testing.T, s *Server, h *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/terminal?cols=80&rows=24&projectId="+s.projectID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + s.Token()}, "Origin": []string{h.URL}}})
	if err != nil {
		t.Fatalf("dial: %v (%v)", err, res)
	}
	c.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}
func readUntil(t *testing.T, c *websocket.Conn, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var out strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting %q: %v; got %q", want, err, out.String())
		}
		out.Write(data)
		if strings.Contains(out.String(), want) {
			return out.String()
		}
	}
}
func sendInput(t *testing.T, c *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatal(err)
	}
}
func logContents(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestPTYInputResizeReconnectAndSharedClients(t *testing.T) {
	s, h, log := terminalServer(t)
	c := dialTerminal(t, s, h)
	out := readUntil(t, c, "TERM:xterm-256color COLOR:truecolor")
	if !strings.Contains(out, "PID:") {
		t.Fatal(out)
	}
	raw := []byte{'a', 0x03, 0x1b, '[', 'A', 0xe4, 0xb8, 0xad}
	sendInput(t, c, raw)
	readUntil(t, c, fmt.Sprintf("INPUT:%x", raw))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":110,"rows":38}`)); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "SIZE:110:38")
	next := dialTerminal(t, s, h)
	readUntil(t, next, "PID:")
	sendInput(t, c, []byte("original-still-live"))
	readUntil(t, c, "INPUT:6f726967696e616c2d7374696c6c2d6c697665")
	readUntil(t, next, "INPUT:6f726967696e616c2d7374696c6c2d6c697665")
	sendInput(t, next, []byte("live"))
	readUntil(t, next, "INPUT:6c697665")
	readUntil(t, c, "INPUT:6c697665")
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("shared connection spawned another TUI")
	}
	_ = next.CloseNow()
	// Disconnect only detaches: the same PTY keeps its input/output state.
	again := dialTerminal(t, s, h)
	readUntil(t, again, "INPUT:6c697665")
	sendInput(t, again, []byte("size"))
	readUntil(t, again, "SIZE:80:24")
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("reconnect spawned another TUI")
	}
	_ = s.Close()
	if !strings.Contains(logContents(t, log), "STOP:") {
		t.Fatal("no graceful TERM cleanup")
	}
}

// Validate the externally visible grid on all connections, not just owner
// bookkeeping. Output, heartbeat and passive viewport changes must not fight
// real input from the most recently active client.
func TestPTYThreeClientsLatestActivityOwnsSharedGeometry(t *testing.T) {
	s, h, log := terminalServer(t)
	a := dialTerminal(t, s, h)
	readUntil(t, a, "PID:")
	term, _ := heartbeatPeer(t, s)
	pid := term.cmd.Process.Pid
	control := func(c *websocket.Conn, kind string, cols, rows int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		b, _ := json.Marshal(map[string]any{"type": kind, "cols": cols, "rows": rows})
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	grid := func(cols, rows int) {
		t.Helper()
		eventually(t, func() bool { term.mu.Lock(); defer term.mu.Unlock(); return term.cols == cols && term.rows == rows })
	}
	broadcastSize := func(cols, rows int, clients ...*websocket.Conn) {
		t.Helper()
		for _, c := range clients {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			for {
				kind, b, err := c.Read(ctx)
				if err != nil {
					cancel()
					t.Fatalf("missing size %dx%d: %v", cols, rows, err)
				}
				if kind != websocket.MessageText {
					continue
				}
				var msg struct {
					Type       string
					Cols, Rows int
				}
				if err := json.Unmarshal(b, &msg); err != nil {
					cancel()
					t.Fatal(err)
				}
				if msg.Type == "taken_over" || msg.Type == "exit" {
					cancel()
					t.Fatalf("shared client was displaced: %s", b)
				}
				if msg.Type == "size" && msg.Cols == cols && msg.Rows == rows {
					break
				}
			}
			cancel()
		}
	}
	control(a, "resize", 120, 40)
	broadcastSize(120, 40, a)
	b := dialTerminal(t, s, h)
	readUntil(t, b, `"type":"ready"`)
	broadcastSize(80, 24, a, b) // New view selects its own local geometry.
	control(a, "activity", 120, 40)
	broadcastSize(120, 40, a, b)
	control(b, "resize", 91, 31)
	control(b, "ping", 0, 0)
	readUntil(t, b, `"type":"pong"`)
	grid(120, 40)
	c := dialTerminal(t, s, h)
	readUntil(t, c, `"type":"ready"`)
	broadcastSize(80, 24, a, b, c)
	control(c, "activity", 150, 45)
	broadcastSize(150, 45, a, b, c)
	control(a, "resize", 125, 42) // Cache inactive local dimensions for fallback.
	control(a, "ping", 0, 0)
	readUntil(t, a, `"type":"pong"`)
	grid(150, 45)
	for i, sender := range []*websocket.Conn{a, b, c} {
		text := fmt.Sprintf("shared-writer-%d", i)
		sendInput(t, sender, []byte(text))
		for _, reader := range []*websocket.Conn{a, b, c} {
			readUntil(t, reader, fmt.Sprintf("INPUT:%x", text))
		}
	}
	grid(150, 45) // Raw input/query responses have no implicit activity claim.
	_ = b.CloseNow()
	eventually(t, func() bool { term.mu.Lock(); defer term.mu.Unlock(); return len(term.peers) == 2 })
	grid(150, 45) // Removing a non-owner must not change the active grid.
	_ = c.CloseNow()
	broadcastSize(125, 42, a)
	_ = a.CloseNow()
	eventually(t, func() bool { term.mu.Lock(); defer term.mu.Unlock(); return len(term.peers) == 0 })
	select {
	case <-term.done:
		t.Fatal("all views closed and killed PTY")
	default:
	}
	d := dialTerminal(t, s, h)
	readUntil(t, d, fmt.Sprintf("INPUT:%x", "shared-writer-2"))
	if term.cmd.Process.Pid != pid || strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("all-view reconnect restarted backend")
	}
	e := dialTerminal(t, s, h)
	readUntil(t, e, "PID:")
	if w := request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	readUntil(t, d, `"type":"restart"`)
	readUntil(t, e, `"type":"restart"`)
	select {
	case <-term.done:
	default:
		t.Fatal("explicit restart did not stop original backend")
	}
}

func TestPTYExitRequiresExplicitRestart(t *testing.T) {
	s, h, log := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	sendInput(t, c, []byte("quit"))
	out := readUntil(t, c, `"type":"exit"`)
	if !strings.Contains(out, "FINAL OUTPUT") || !strings.Contains(out, `"code":7`) {
		t.Fatal(out)
	}
	c = dialTerminal(t, s, h)
	readUntil(t, c, `"type":"exit"`)
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("exited TUI restarted implicitly")
	}
	if w := request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	c = dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	if strings.Count(logContents(t, log), "START:") != 2 {
		t.Fatal("explicit restart did not start fresh TUI")
	}
	if w := request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	readUntil(t, c, `"type":"restart"`)
	if !strings.Contains(logContents(t, log), "STOP:") {
		t.Fatal("restart did not stop live process")
	}
}

func TestTerminalProtocolRejectsCrossOriginAndInvalidMessages(t *testing.T) {
	s, h, _ := terminalServer(t)
	for _, header := range []http.Header{
		{"Authorization": []string{"Bearer " + s.Token()}, "Origin": []string{"http://evil.test"}},
		{"Origin": []string{h.URL}},
		{"Cookie": []string{cookieName + "=" + s.Token()}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, res, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/terminal?cols=80&rows=24&projectId="+s.projectID, &websocket.DialOptions{HTTPHeader: header})
		cancel()
		if err == nil {
			_ = c.CloseNow()
			t.Fatal("unsafe handshake accepted")
		}
		if res == nil || (res.StatusCode != 401 && res.StatusCode != 403) {
			t.Fatalf("unexpected failure: %v %v", res, err)
		}
	}
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":900,"rows":24}`))
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			break
		}
	}
	// Rejecting a browser's protocol error must not kill the shared TUI.
	c = dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	sendInput(t, c, []byte("ok"))
	readUntil(t, c, "INPUT:6f6b")
}

func TestReplayBoundAndSlowClient(t *testing.T) {
	term := &terminal{parser: ansi.NewParser(), modes: make(map[int]bool)}
	term.trackModes()
	// Mode sequences can span PTY reads; OSC payloads are not escape commands.
	term.record([]byte("\x1b[?100"))
	term.record([]byte("0h\x1b[?1006h\x1b[?2004h\x1b[>4;2m\x1b="))
	term.record([]byte("\x1b]52;c;[?1000l\x07"))
	p, healthy := newPeer(nil), newPeer(nil)
	term.peers = []*peer{p, healthy}
	chunk := bytes.Repeat([]byte("x"), 16<<10)
	for i := 0; i < 400; i++ {
		term.record(chunk)
		select {
		case frame := <-healthy.output:
			if !bytes.Equal(frame.data, chunk) {
				t.Fatal("healthy peer lost output while another was slow")
			}
		default:
			t.Fatal("healthy peer blocked behind slow peer")
		}
	}
	select {
	case <-healthy.stopped:
		t.Fatal("slow peer disconnected healthy peer")
	default:
	}
	if len(term.history) != historyLimit || !term.truncated {
		t.Fatalf("history len=%d truncated=%v", len(term.history), term.truncated)
	}
	select {
	case <-p.stopped:
	default:
		t.Fatal("unbounded slow-consumer queue")
	}
	if len(p.output) != cap(p.output) {
		t.Fatal("fixture did not fill output queue")
	}
	term.record([]byte("end"))
	replacement := newPeer(nil)
	term.attach(replacement)
	ready := <-replacement.output
	if !bytes.Contains(ready.data, []byte(`"type":"ready"`)) {
		t.Fatal("replay must follow canonical size metadata")
	}
	frame := <-replacement.output
	if !bytes.Contains(frame.data, []byte("end")) {
		t.Fatal("latest replay not preserved")
	}
	if len(frame.data) > historyLimit+1024 {
		t.Fatal("unbounded replay")
	}
	for _, mode := range []string{"\x1b[?1000h", "\x1b[?1006h", "\x1b[?2004h", "\x1b[>4;2m", "\x1b="} {
		if !bytes.Contains(frame.data, []byte(mode)) {
			t.Fatalf("lost terminal mode %q on truncated replay", mode)
		}
	}
}

func TestConcurrentConnectionsRestartAndClose(t *testing.T) {
	s, h, log := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/terminal?cols=80&rows=24&projectId="+s.projectID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + s.Token()}}})
			if err == nil {
				defer c.CloseNow()
				_, _, _ = c.Read(ctx)
			}
		}()
	}
	wg.Wait()
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("parallel connections spawned duplicate processes")
	}
	s.lifecycle.Lock()
	pid := s.terminal.cmd.Process.Pid
	s.lifecycle.Unlock()
	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("TUI process still alive: %v", err)
	}
}

func TestPTYWriteDeadlineAndForcedShutdown(t *testing.T) {
	s, h, _ := terminalServer(t, "--no-read", "--ignore-term")
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	s.lifecycle.Lock()
	term := s.terminal
	s.lifecycle.Unlock()
	term.mu.Lock()
	p := term.peers[0]
	term.mu.Unlock()
	start := time.Now()
	err := term.input(p, bytes.Repeat([]byte("x"), 64<<10))
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("PTY write deadline failed: %v %v", err, time.Since(start))
	}
	_ = s.Close()
	if err := syscall.Kill(term.cmd.Process.Pid, 0); err != syscall.ESRCH {
		t.Fatalf("forced shutdown leaked child: %v", err)
	}
}

func TestProcessSnapshotOwnership(t *testing.T) {
	before, err := parseProcessTable("10 1 10 Mon Jan 1 00:00:00 2024\n20 10 20 Mon Jan 1 00:00:01 2024\n30 20 30 Mon Jan 1 00:00:02 2024\n40 1 40 Mon Jan 1 00:00:03 2024")
	if err != nil {
		t.Fatal(err)
	}
	known := processTable{10: before[10]}
	known.collect(before)
	if len(known) != 3 {
		t.Fatalf("unrelated sibling collected, or grandchild missed: %v", known)
	}
	after, err := parseProcessTable("20 1 20 Mon Jan 1 00:00:01 2024\n30 1 30 Tue Jan 2 00:00:02 2024\n50 20 50 Mon Jan 1 00:00:04 2024\n60 30 60 Tue Jan 2 00:00:03 2024")
	if err != nil {
		t.Fatal(err)
	}
	known.collect(after)
	if _, ok := known[50]; !ok {
		t.Fatal("missed new tool under surviving reparented RPC")
	}
	if _, ok := known[60]; ok || known[30].started == after[30].started {
		t.Fatal("recycled PID granted ownership of unrelated processes")
	}
	for _, invalid := range []string{"", "invalid", "x 1 1 Mon Jan 1 00:00:00 2024", "0 1 1 Mon Jan 1 00:00:00 2024"} {
		if _, err := parseProcessTable(invalid); err == nil {
			t.Fatalf("invalid snapshot accepted: %q", invalid)
		}
	}
}

func TestPTYStopReclaimsIndependentDescendantGroups(t *testing.T) {
	for _, action := range []string{"close", "restart", "graceful"} {
		t.Run(action, func(t *testing.T) {
			args := []string{"--descendants"}
			if action != "graceful" {
				args = append(args, "--ignore-term")
			}
			s, h, log := terminalServer(t, args...)
			pids := func() []int {
				var out []int
				for _, line := range strings.Split(logContents(t, log), "\n") {
					kind, value, ok := strings.Cut(line, ":")
					if ok && (kind == "START" || kind == "CHILD" || kind == "GRANDCHILD") {
						pid, err := strconv.Atoi(value)
						if err != nil {
							t.Fatal(err)
						}
						out = append(out, pid)
					}
				}
				return out
			}
			// Even a failed regression must not leave its isolated fixtures alive.
			t.Cleanup(func() {
				for _, pid := range pids() {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			c := dialTerminal(t, s, h)
			readUntil(t, c, "PID:")
			old := pids()
			if len(old) != 3 {
				t.Fatalf("expected TUI, RPC and tool: %v", old)
			}
			for _, pid := range old {
				if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
					t.Fatalf("fixture %d is not an independent group: %d, %v", pid, pgid, err)
				}
			}
			started := time.Now()
			if action == "restart" {
				if w := request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
					t.Fatal(w.Body)
				}
			} else {
				_ = s.Close()
			}
			if action != "graceful" && time.Since(started) < 3*time.Second {
				t.Fatal("fixture did not exercise the forced shutdown deadline")
			}
			for _, pid := range old {
				deadline := time.Now().Add(time.Second)
				for {
					// Reparented zombies are dead; only their new parent can reap them.
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					state, err := exec.CommandContext(ctx, "/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
					cancel()
					if err != nil {
						if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
							t.Fatalf("could not verify descendant %d: %v", pid, err)
						}
						break // ps exits 1 when the requested PID no longer exists.
					}
					if strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("stop left independent descendant %d running: %s", pid, state)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if action == "restart" {
				c = dialTerminal(t, s, h)
				readUntil(t, c, "PID:")
				if len(pids()) != 6 {
					t.Fatal("restart did not create exactly one fresh tree")
				}
				_ = s.Close()
			}
		})
	}
}

func TestImmediateExitKeepsOutputAndStatus(t *testing.T) {
	s, h, _ := terminalServer(t, "--exit-immediate")
	c := dialTerminal(t, s, h)
	out := readUntil(t, c, `"type":"exit"`)
	if !strings.Contains(out, "PID:") || !strings.Contains(out, `"code":7`) {
		t.Fatal(out)
	}
}

func TestExplicitCommandFailureDoesNotFallback(t *testing.T) {
	s, h, _ := terminalServer(t)
	s.cfg.Command = []string{filepath.Join(t.TempDir(), "missing-tui")}
	c := dialTerminal(t, s, h)
	out := readUntil(t, c, `"type":"exit"`)
	if !strings.Contains(out, "missing-tui") {
		t.Fatal(out)
	}
}
