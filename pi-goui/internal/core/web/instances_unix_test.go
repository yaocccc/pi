//go:build unix

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func instanceTerminalServer(t *testing.T, args ...string) (*Server, *httptest.Server, string) {
	t.Helper()
	s, h, log := terminalServer(t, args...)
	s.cfg.Env = append(s.cfg.Env, "HOME="+t.TempDir(), "PI_CODING_AGENT_DIR="+t.TempDir(), "PI_OFFLINE=1")
	return s, h, log
}

func dialInstance(t *testing.T, s *Server, h *httptest.Server, info instanceInfo) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/instances/"+info.ID+"/api/terminal?cols=80&rows=24&projectId="+info.ProjectID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + s.Token()}, "Origin": []string{h.URL}}})
	if err != nil {
		t.Fatalf("dial instance: %v (%v)", err, res)
	}
	c.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func TestInstanceIndependentPTYLifecycle(t *testing.T) {
	s, h, log := instanceTerminalServer(t)
	s.cfg.SharedDesktop = true
	command := append([]string{}, s.cfg.Command...)
	s.cfg.NewInstanceCommandForDirectory = func(string) []string { return command }
	main := dialTerminal(t, s, h)
	readUntil(t, main, "PID:")
	rootTerminal, rootID, rootCwd := s.terminal, s.projectID, s.cwd
	a := createTestInstance(t, s, t.TempDir())
	b := createTestInstance(t, s, t.TempDir())
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("creation started a PTY")
	}
	list := instanceList(t, s)
	if len(list) != 3 || list[0].ID != "main" || !list[0].Desktop || list[0].Status != "running" {
		t.Fatal(list)
	}
	ca := dialInstance(t, s, h, a)
	readUntil(t, ca, "PID:")
	cb := dialInstance(t, s, h, b)
	readUntil(t, cb, "PID:")
	assertInput := func(c *websocket.Conn) { sendInput(t, c, []byte("live")); readUntil(t, c, "INPUT:6c697665") }
	assertInput(main)
	assertInput(ca)
	assertInput(cb)
	childA, childB := s.instances[a.ID], s.instances[b.ID]
	if childA.terminal.cmd.Dir != a.Cwd || childB.terminal.cmd.Dir != b.Cwd {
		t.Fatal("instance working directories mixed")
	}
	oldA := childA.terminal
	w := request(t, s, "POST", "/api/instances/"+a.ID+"/api/projects/select", projectRequest{Cwd: t.TempDir(), ProjectID: a.ProjectID})
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var next projectRequest
	if err := json.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ca, `"type":"project_changed"`)
	select {
	case <-oldA.done:
	default:
		t.Fatal("switched PTY survived")
	}
	for _, action := range []string{"/api/terminal/restart", "/api/projects/select"} {
		w = request(t, s, "POST", "/api/instances/"+a.ID+action, projectRequest{Cwd: a.Cwd, ProjectID: a.ProjectID})
		if w.Code != 409 {
			t.Fatalf("stale %s: %d", action, w.Code)
		}
	}
	if w = request(t, s, "DELETE", "/api/instances/"+a.ID, projectRequest{ProjectID: a.ProjectID}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w = request(t, s, "GET", "/api/instances/"+a.ID+"/api/terminal?cols=80&rows=24&projectId="+a.ProjectID, nil); w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.Cwd, a.ProjectID = next.Cwd, next.ProjectID
	ca = dialInstance(t, s, h, a)
	readUntil(t, ca, "PID:")
	currentA := childA.terminal
	if w = request(t, s, "DELETE", "/api/instances/"+a.ID, projectRequest{ProjectID: a.ProjectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	select {
	case <-currentA.done:
	default:
		t.Fatal("deleted PTY survived")
	}
	assertInput(main)
	assertInput(cb)
	if s.terminal != rootTerminal || s.projectID != rootID || s.cwd != rootCwd {
		t.Fatal("child changes altered desktop")
	}
	if w = request(t, s, "GET", "/api/instances/"+a.ID+"/api/terminal?cols=80&rows=24&projectId="+a.ProjectID, nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	// Main deletion leaves child B attached and running; root host remains up.
	if w = request(t, s, "DELETE", "/api/instances/main", projectRequest{ProjectID: rootID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	assertInput(cb)
	if s.ctx.Err() != nil {
		t.Fatal("closing desktop stopped host")
	}
	last := childB.terminal
	_ = s.Close()
	select {
	case <-last.done:
	default:
		t.Fatal("host Close left child running")
	}
	if len(s.instances) != 0 {
		t.Fatal("host Close left registry entries")
	}
	if strings.Count(logContents(t, log), "START:") != 4 || strings.Count(logContents(t, log), "STOP:") != 4 {
		t.Fatal("unexpected starts or missing process cleanup", logContents(t, log))
	}
}

func TestInstanceExitStatusAndScopedRestart(t *testing.T) {
	s, h, log := instanceTerminalServer(t)
	command := append([]string{}, s.cfg.Command...)
	s.cfg.NewInstanceCommandForDirectory = func(string) []string { return command }
	info := createTestInstance(t, s, t.TempDir())
	c := dialInstance(t, s, h, info)
	readUntil(t, c, "PID:")
	sendInput(t, c, []byte("quit"))
	readUntil(t, c, `"type":"exit"`)
	var found bool
	for _, item := range instanceList(t, s) {
		if item.ID == info.ID {
			found = item.Status == "exited"
		}
	}
	if !found {
		t.Fatal("exited child not listed with final status")
	}
	c = dialInstance(t, s, h, info)
	readUntil(t, c, `"type":"exit"`)
	if strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("listing/reconnecting restarted exited child")
	}
	if w := request(t, s, "POST", "/api/instances/"+info.ID+"/api/terminal/restart", projectRequest{ProjectID: info.ProjectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if s.terminal != nil {
		t.Fatal("child restart started desktop")
	}
	c = dialInstance(t, s, h, info)
	readUntil(t, c, "PID:")
	if strings.Count(logContents(t, log), "START:") != 2 {
		t.Fatal("explicit scoped restart failed")
	}
}

func TestInstanceShutdownReclaimsIndependentProcessGroups(t *testing.T) {
	s, h, log := instanceTerminalServer(t, "--descendants")
	command := append([]string{}, s.cfg.Command...)
	s.cfg.NewInstanceCommandForDirectory = func(string) []string { return command }
	info := createTestInstance(t, s, t.TempDir())
	c := dialInstance(t, s, h, info)
	readUntil(t, c, "PID:")
	var pids []int
	for _, line := range strings.Split(logContents(t, log), "\n") {
		kind, raw, _ := strings.Cut(line, ":")
		if kind == "START" || kind == "CHILD" || kind == "GRANDCHILD" {
			pid, err := strconv.Atoi(raw)
			if err != nil {
				t.Fatal(err)
			}
			pids = append(pids, pid)
		}
	}
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	if len(pids) != 3 {
		t.Fatal("expected isolated TUI/RPC/tool groups", pids)
	}
	_ = s.Close()
	for _, pid := range pids {
		eventually(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			state, err := exec.CommandContext(ctx, "/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				return ok && exit.ExitCode() == 1
			}
			return strings.HasPrefix(strings.TrimSpace(string(state)), "Z")
		})
	}
}

func TestInstanceConcurrentConnectDeleteAndClose(t *testing.T) {
	s, h, _ := instanceTerminalServer(t)
	command := append([]string{}, s.cfg.Command...)
	s.cfg.NewInstanceCommandForDirectory = func(string) []string { return command }
	const count = 8
	infos := make([]instanceInfo, count)
	children := make([]*Server, count)
	for i := range infos {
		infos[i] = createTestInstance(t, s, t.TempDir())
		children[i] = s.instances[infos[i].ID]
	}
	// Ensure shutdown races against existing processes as well as pending
	// handshakes, not just an empty registry whose requests all receive 503.
	for _, info := range infos[:2] {
		c := dialInstance(t, s, h, info)
		readUntil(t, c, "PID:")
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, info := range infos {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, res, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/instances/"+info.ID+"/api/terminal?cols=80&rows=24&projectId="+info.ProjectID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + s.Token()}}})
			if err == nil {
				defer c.CloseNow()
				_, _, _ = c.Read(ctx)
			} else if res != nil && res.StatusCode != 404 && res.StatusCode != 503 {
				t.Errorf("dial: %v (%d)", err, res.StatusCode)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			w := request(t, s, "DELETE", "/api/instances/"+info.ID, projectRequest{ProjectID: info.ProjectID})
			if w.Code != 200 && w.Code != 404 && w.Code != 503 {
				t.Errorf("delete: %d", w.Code)
			}
		}()
	}
	close(start)
	_ = s.Close()
	wg.Wait()
	for _, child := range children {
		if child.ctx.Err() == nil {
			t.Fatal("uncanceled child")
		}
		child.lifecycle.Lock()
		term := child.terminal
		child.lifecycle.Unlock()
		if term != nil {
			select {
			case <-term.done:
			default:
				t.Fatal("orphan child PTY")
			}
		}
	}
}
