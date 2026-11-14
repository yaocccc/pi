//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pi-goui/internal/core/web"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestSharedReadOnlyFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("builds isolated CLI")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "pi-goui")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	fake, err := filepath.Abs("../../tests/fake_rpc.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"home", "agent", "state", "project"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "HOME="+filepath.Join(root, "home"), "PI_CODING_AGENT_DIR="+filepath.Join(root, "agent"), "GOUI_TEST_DIR="+filepath.Join(root, "state"), "PI_OFFLINE=1")
	for _, options := range [][]string{{"--check"}, {"--share=false", "--check"}, {"--internal-shared-tui", "--check"}} {
		cmd := exec.Command(binary, append(options, "--pi", fake, "--cwd", filepath.Join(root, "project"))...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "RPC OK") || strings.Contains(string(out), "Login URL") {
			t.Fatalf("read-only CLI %v: %v %s", options, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "home", ".pi", "goui-instances")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only CLI registered an endpoint", err)
	}
	for _, args := range [][]string{{"--listen=:8888", "--check"}, {"--token=unused", "--check"}, {"--internal-shared-tui", "--share"}} {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err == nil || (!strings.Contains(string(out), "separate webui service") && !strings.Contains(string(out), "cannot start another host")) {
			t.Fatalf("unsafe flags accepted: %v %v %s", args, err, out)
		}
	}
}

func TestSharedDesktopLifecycle(t *testing.T) {
	for _, webFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("web_first_%v", webFirst), func(t *testing.T) { testSharedDesktopLifecycle(t, webFirst) })
	}
}
func testSharedDesktopLifecycle(t *testing.T, webFirst bool) {
	if testing.Short() {
		t.Skip("builds and drives isolated desktop host")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required for fake RPC")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "pi-goui")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	fake, err := filepath.Abs("../../tests/fake_rpc.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"home", "agent", "project", "other", "state"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "HOME" && key != "PI_CODING_AGENT_DIR" && key != "PI_WEBUI_TOKEN" && key != "GOUI_TEST_DIR" {
			env = append(env, entry)
		}
	}
	env = append(env, "HOME="+filepath.Join(root, "home"), "PI_CODING_AGENT_DIR="+filepath.Join(root, "agent"), "GOUI_TEST_DIR="+filepath.Join(root, "state"), "PI_WEBUI_TOKEN=shared-test-token", "PI_OFFLINE=1")

	// The in-process WebUI and desktop subprocess share the isolated home.
	t.Setenv("HOME", filepath.Join(root, "home"))
	var app *web.Server
	var public *httptest.Server
	var client sharedClient
	var browserConnections []*websocket.Conn
	startWeb := func() {
		t.Helper()
		var err error
		app, err = web.New(web.Config{Command: []string{"unused"}, Cwd: filepath.Join(root, "project"), AgentDir: filepath.Join(root, "agent"), DiscoverDesktops: true})
		if err != nil {
			t.Fatal(err)
		}
		public = httptest.NewServer(app.Handler())
		client = sharedClient{base: public.URL, token: app.Token(), http: &http.Client{Timeout: 5 * time.Second}}
	}
	stopWeb := func() {
		if public != nil {
			closing := public
			public = nil
			done := make(chan struct{})
			go func() { _ = app.Close(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				// Failure cleanup must not strand the separate desktop process.
				for _, c := range browserConnections {
					_ = c.CloseNow()
				}
				closing.Close()
				t.Fatal("WebUI Close did not cancel and join upgraded proxy connections")
			}
			closing.Close()
		}
	}
	defer stopWeb()
	if webFirst {
		startWeb()
	}
	cmd := exec.Command(binary, "--pi", fake, "--cwd", filepath.Join(root, "project"), "--no-session")
	cmd.Env = env
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 110, Rows: 35})
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-waited:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-waited
				t.Error("shared host did not terminate")
			}
		})
	}
	defer stop()
	defer master.Close()
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var output strings.Builder
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32768)
		for ctx.Err() == nil {
			p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			if n, e := unix.Poll(p, 30); e != nil || n == 0 {
				continue
			}
			n, e := unix.Read(fd, buf)
			if e == unix.EAGAIN || e == unix.EINTR {
				continue
			}
			if e != nil || n == 0 {
				return
			}
			mu.Lock()
			output.Write(buf[:n])
			mu.Unlock()
		}
	}()
	defer func() { cancel(); <-readDone }()
	text := func() string { mu.Lock(); defer mu.Unlock(); return output.String() }
	await := func(label string, fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			if fn() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s timed out; desktop tail: %s", label, text()[max(0, len(text())-2000):])
	}
	type record struct {
		Kind, Cwd string
		PID       int
		Command   struct{ Type, Message string }
	}
	wire := func() []record {
		b, _ := os.ReadFile(filepath.Join(root, "state", "wire.jsonl"))
		var records []record
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var r record
			if json.Unmarshal([]byte(line), &r) == nil {
				records = append(records, r)
			}
		}
		return records
	}
	starts := func() []record {
		var out []record
		for _, r := range wire() {
			if r.Kind == "start" {
				out = append(out, r)
			}
		}
		return out
	}

	await("original TUI starts without acknowledgement", func() bool { return len(starts()) == 1 && strings.Contains(text(), "Message pi") })
	first := starts()[0].PID
	if !webFirst {
		startWeb()
	}
	type discovered struct {
		ID      string
		Desktop bool
		sharedProject
	}
	var current discovered
	find := func() bool {
		var result struct{ Instances []discovered }
		if err := client.request(ctx, http.MethodGet, "/api/instances", nil, &result); err != nil {
			return false
		}
		for _, item := range result.Instances {
			if item.Desktop && item.ProjectID != "" {
				current = item
				return true
			}
		}
		return false
	}
	await("automatic discovery", find)
	registry := filepath.Join(root, "home", ".pi", "goui-instances")
	entries, err := os.ReadDir(registry)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one registered desktop, no recursive child host: %v %v", entries, err)
	}
	var registration struct{ Socket string }
	metadata, err := os.ReadFile(filepath.Join(registry, entries[0].Name()))
	if err != nil || json.Unmarshal(metadata, &registration) != nil || registration.Socket == "" {
		t.Fatal("missing private desktop socket metadata", err)
	}
	if current.Cwd != filepath.Join(root, "project") {
		t.Fatalf("discovered wrong cwd: %+v", current)
	}
	for _, forbidden := range []string{"Login URL", "Pi shared TUI:", "Press Enter to open", "shared-test-token", "QR code"} {
		if strings.Contains(text(), forbidden) {
			t.Fatalf("desktop emitted web startup material: %s", forbidden)
		}
	}
	scoped := func(suffix string) string { return "/api/instances/" + current.ID + "/api/" + suffix }
	dial := func(id string) *websocket.Conn {
		t.Helper()
		dialCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		c, _, err := websocket.Dial(dialCtx, strings.Replace(client.base, "http", "ws", 1)+scoped("terminal")+"?cols=90&rows=30&projectId="+id, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + app.Token()}, "Origin": {client.base}}})
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadLimit(8 << 20)
		browserConnections = append(browserConnections, c)
		t.Cleanup(func() { _ = c.CloseNow() })
		return c
	}
	readControl := func(c *websocket.Conn, want string) {
		t.Helper()
		rctx, done := context.WithTimeout(ctx, 8*time.Second)
		defer done()
		for {
			kind, b, e := c.Read(rctx)
			if e != nil {
				t.Fatalf("%s: %v", want, e)
			}
			if kind == websocket.MessageText {
				var msg struct{ Type string }
				_ = json.Unmarshal(b, &msg)
				if msg.Type == want {
					return
				}
			}
		}
	}
	browser := dial(current.ProjectID)
	readControl(browser, "ready")
	if len(starts()) != 1 {
		t.Fatal("browser join duplicated backend")
	}
	if err := browser.Write(ctx, websocket.MessageBinary, []byte("SHARED_WEB_PROMPT\r")); err != nil {
		t.Fatal(err)
	}
	await("browser input to original RPC", func() bool {
		for _, r := range wire() {
			if r.PID == first && r.Command.Type == "prompt" && r.Command.Message == "SHARED_WEB_PROMPT" {
				return true
			}
		}
		return false
	})
	await("desktop sees browser output", func() bool { return strings.Contains(text(), "SHARED_WEB_PROMPT") })
	time.Sleep(100 * time.Millisecond)
	if _, err := unix.Write(fd, []byte("DESKTOP_PARALLEL_PROMPT\r")); err != nil {
		t.Fatal(err)
	}
	await("desktop still controls same original RPC", func() bool {
		for _, r := range wire() {
			if r.PID == first && r.Command.Message == "DESKTOP_PARALLEL_PROMPT" {
				return true
			}
		}
		return false
	})
	if strings.Contains(text(), "Desktop detached (taken_over)") {
		t.Fatal("browser join detached desktop")
	}
	if err := browser.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	readControl(browser, "pong")
	if _, err := unix.Write(fd, []byte{0x1d}); err != nil {
		t.Fatal(err)
	}
	await("local detach prompt", func() bool { return strings.Contains(text(), "Desktop detached (detached)") })
	if strings.Contains(text(), "Login URL") || strings.Contains(text(), "QR code") {
		t.Fatal("local detach printed a QR")
	}
	if _, err := unix.Write(fd, []byte("\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if len(starts()) != 1 {
		t.Fatal("desktop retake duplicated backend")
	}
	browser = dial(current.ProjectID)
	readControl(browser, "ready")
	if strings.Contains(text(), "Desktop detached (taken_over)") {
		t.Fatal("parallel browser unexpectedly detached desktop")
	}
	var other sharedProject
	if err := client.request(ctx, http.MethodPost, scoped("projects/select"), sharedProject{Cwd: filepath.Join(root, "other"), ProjectID: current.ProjectID}, &other); err != nil {
		t.Fatal(err)
	}
	readControl(browser, "project_changed")
	await("explicit directory change detaches stopped TUI", func() bool { return strings.Contains(text(), "Desktop detached (project_changed)") })
	if _, err := unix.Write(fd, []byte("\n")); err != nil {
		t.Fatal(err)
	}
	await("retake follows browser directory", func() bool { all := starts(); return len(all) == 2 && all[1].Cwd == filepath.Join(root, "other") })
	second := starts()[1].PID
	browser = dial(other.ProjectID)
	readControl(browser, "ready")
	if strings.Contains(text(), "Desktop detached (taken_over)") {
		t.Fatal("joining changed desktop connection")
	}

	oldID, oldProject := current.ID, other.ProjectID
	stopWeb()
	if syscall.Kill(second, 0) != nil {
		t.Fatal("stopping WebUI killed desktop backend")
	}
	// No Enter/reconnection: the original desktop connection stays attached.
	time.Sleep(100 * time.Millisecond)
	if _, err := unix.Write(fd, []byte("AFTER_WEB_STOP\r")); err != nil {
		t.Fatal(err)
	}
	await("desktop remains writable without WebUI", func() bool {
		for _, r := range wire() {
			if r.PID == second && r.Command.Message == "AFTER_WEB_STOP" {
				return true
			}
		}
		return false
	})
	startWeb()
	await("rediscovery after WebUI restart", find)
	if current.ID != oldID || current.ProjectID != oldProject || len(starts()) != 2 {
		t.Fatal("WebUI restart replaced desktop identity/backend")
	}
	browser = dial(current.ProjectID)
	readControl(browser, "ready")
	if err := browser.Write(ctx, websocket.MessageBinary, []byte("AFTER_WEB_RESTART\r")); err != nil {
		t.Fatal(err)
	}
	await("restarted WebUI writes original desktop RPC", func() bool {
		for _, r := range wire() {
			if r.PID == second && r.Command.Message == "AFTER_WEB_RESTART" {
				return true
			}
		}
		return false
	})
	if strings.Contains(text(), "Desktop detached (taken_over)") {
		t.Fatal("restart detached desktop")
	}
	// Explicit local detach remains supported; Ctrl+C there stops the host.
	if _, err := unix.Write(fd, []byte{0x1d}); err != nil {
		t.Fatal(err)
	}
	await("final local detach", func() bool { return strings.Count(text(), "Desktop detached (detached)") >= 2 })
	if _, err := unix.Write(fd, []byte{3}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		once.Do(func() {})
		if err != nil {
			t.Fatalf("host exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl+C did not stop host")
	}
	for _, pid := range []int{first, second} {
		await(fmt.Sprintf("RPC %d cleanup", pid), func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) })
	}
	if len(starts()) != 2 {
		t.Fatal("unexpected backend count")
	}
	await("desktop removed from discovery", func() bool { return !find() })
	if _, err := os.Stat(registration.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("desktop socket survived exit", err)
	}
	if entries, err := os.ReadDir(registry); err != nil || len(entries) != 0 {
		t.Fatal("desktop registration survived exit", entries, err)
	}
	check := exec.Command(binary, "--check", "--pi", fake, "--cwd", filepath.Join(root, "project"))
	check.Env = env
	if out, err := check.CombinedOutput(); err != nil || !strings.Contains(string(out), "RPC OK") || strings.Contains(string(out), "Login URL") {
		t.Fatalf("share --check must remain read-only without listener/TTY: %v %s", err, out)
	}
	if entries, err := os.ReadDir(registry); err != nil || len(entries) != 0 {
		t.Fatal("--check registered a desktop", entries, err)
	}
	help := exec.Command(binary, "--help")
	help.Env = env
	if out, err := help.CombinedOutput(); err != nil || strings.Contains(string(out), "shared-test-token") || strings.Contains(string(out), "Login URL") {
		t.Fatalf("help leaked a credential or started host: %v %s", err, out)
	}
	redirected := exec.Command(binary, "--share", "--pi", fake, "--cwd", filepath.Join(root, "project"))
	redirected.Env = env
	if out, err := redirected.CombinedOutput(); err == nil || !strings.Contains(string(out), "requires terminal stdin and stdout") || strings.Contains(string(out), "Login URL") {
		t.Fatalf("redirected share must reject before hosting: %v %s", err, out)
	}

	for _, flag := range []string{"--listen=:8888", "--token=not-used"} {
		rejected := exec.Command(binary, flag, "--check")
		rejected.Env = env
		if out, err := rejected.CombinedOutput(); err == nil || !strings.Contains(string(out), "separate webui service") {
			t.Fatalf("obsolete flag not rejected: %v %s", err, out)
		}
	}
	t.Log("real desktop PTY and native TUI -> isolated fake RPC; separate HTTP server and real Unix-socket WebSocket proxy, both startup orders and WebUI restart; no model calls")
}
