//go:build unix

package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if handled, err := RunSharedSessionHelper(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	// Helpers above inherit the parent's HOME. All ordinary tests and their
	// fixture subprocesses share a disposable registry, never the real HOME.
	ownedHome := ""
	if os.Getenv("GOUI_RUNTIME_TEST_HOME") == "" {
		home, err := os.MkdirTemp("", "goui-runtime-test-home-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ownedHome = home
		_ = os.Setenv("HOME", home)
		_ = os.Setenv("GOUI_RUNTIME_TEST_HOME", home)
	}
	code := m.Run()
	if ownedHome != "" {
		_ = os.RemoveAll(ownedHome)
	}
	os.Exit(code)
}

func testSharedRoot(t *testing.T, agent string) string {
	t.Helper()
	root, err := sharedRoot(agent)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSharedPiHelper(t *testing.T) {
	if os.Getenv("GOUI_SHARED_PI_TEST") != "1" {
		return
	}
	var file, newPath string
	for i, a := range os.Args {
		if i+1 < len(os.Args) {
			switch a {
			case "--session":
				file = os.Args[i+1]
			case "--fake-new-path":
				newPath = os.Args[i+1]
			}
		}
	}
	logPath := os.Getenv("GOUI_SHARED_LOG")
	log := func(kind string, v any) {
		f, e := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if e == nil {
			_ = json.NewEncoder(f).Encode(map[string]any{"kind": kind, "pid": os.Getpid(), "data": v})
			f.Close()
		}
	}
	emit := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	log("start", file)
	answered := 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var cmd map[string]any
		if json.Unmarshal(scan.Bytes(), &cmd) != nil {
			os.Exit(2)
		}
		log("command", cmd)
		kind, _ := cmd["type"].(string)
		var data any = map[string]any{}
		switch kind {
		case "get_state":
			stateFile := file
			if stateFile == "" && os.Getenv("GOUI_SHARED_ADVERTISE_PATH") == "1" {
				stateFile = newPath
			}
			data = map[string]any{"sessionFile": stateFile, "sessionId": "fixture-session", "isStreaming": false, "model": map[string]any{"id": "fixture", "provider": "test"}}
		case "get_entries":
			data = map[string]any{"entries": []any{}}
		case "get_commands":
			data = map[string]any{"commands": []any{map[string]any{"name": extensionCommand, "source": "extension"}}}
		case "prompt":
			message, _ := cmd["message"].(string)
			if strings.Contains(message, "fixture-reject") {
				emit(map[string]any{"type": "response", "id": cmd["id"], "command": kind, "success": false, "error": "rejected"})
				continue
			}
			if strings.Contains(message, "fixture-unknown") {
				os.Exit(0)
			}
			if strings.HasPrefix(message, "/"+extensionCommand+" ") {
				var body map[string]any
				_ = json.Unmarshal([]byte(strings.TrimPrefix(message, "/"+extensionCommand+" ")), &body)
				reply, _ := json.Marshal(map[string]any{"id": body["id"], "result": body["params"]})
				emit(map[string]any{"type": "extension_ui_request", "method": "notify", "message": extensionReplyPrefix + string(reply)})
				break
			}
			if file == "" {
				file = newPath
				_ = os.WriteFile(file, []byte("{\"type\":\"session\",\"id\":\"fixture-session\"}\n"), 0600)
			}
			if message == "ownership-create-pause" {
				_ = os.WriteFile(file, []byte("{\"type\":\"session\",\"id\":\"fixture-session\"}\n"), 0600)
				_ = os.WriteFile(logPath+".created", nil, 0600)
				for {
					if _, err := os.Stat(logPath + ".release"); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			emit(map[string]any{"type": "agent_start"})
			emit(map[string]any{"type": "fixture_message", "message": message})
			emit(map[string]any{"type": "agent_settled"})
			if message == "ask" {
				emit(map[string]any{"type": "extension_ui_request", "method": "confirm", "id": "fixture-question", "title": "Shared question"})
			}
		case "extension_ui_response":
			answered++
			emit(map[string]any{"type": "fixture_answer", "count": answered})
			continue
		case "spawn-independent-tool":
			exe, _ := os.Executable()
			tool := exec.Command(exe, "-test.run=^TestRPCChild$", "--")
			tool.Env = append(os.Environ(), "GOUI_RPC_TEST_CHILD=blocked")
			tool.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if e := tool.Start(); e != nil {
				os.Exit(8)
			}
			emit(map[string]any{"type": "fixture_tool", "pid": tool.Process.Pid})
		case "get_fixture_agent":
			data = map[string]any{"agentDir": os.Getenv("PI_CODING_AGENT_DIR")}
		case "get_fixture_event":
			emit(cmd["event"])
		case "fixture_slow":
			time.Sleep(800 * time.Millisecond)
		case "fixture_ui_state":
			emit(map[string]any{"type": "extension_ui_request", "method": "setStatus", "statusKey": "fixture", "statusText": "ready"})
			emit(map[string]any{"type": "extension_ui_request", "method": "setWidget", "widgetKey": "fixture", "widgetLines": []string{"widget ready"}})
			emit(map[string]any{"type": "extension_ui_request", "method": "setTitle", "title": "fixture title"})
			emit(map[string]any{"type": "extension_ui_request", "method": "set_editor_text", "text": "local"})
		case "clear_queue":
			data = map[string]any{"steering": []string{"queued"}, "followUp": []string{"later"}}
		case "set_session_name":
			data = map[string]any{"name": cmd["name"]}
		}
		emit(map[string]any{"type": "response", "id": cmd["id"], "command": kind, "success": true, "data": data})
	}
	os.Exit(0)
}
func sharedFixture(t *testing.T) (Config, string) {
	return sharedFixtureMember(t, true)
}
func sharedFixtureMember(t *testing.T, join bool) (Config, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOUI_SHARED_PI_TEST", "1")
	t.Setenv("GOUI_SHARED_LOG", filepath.Join(root, "wire"))
	exe, _ := os.Executable()
	file := filepath.Join(root, "session.jsonl")
	cfg := Config{Bin: exe, Cwd: root, AgentDir: filepath.Join(root, "agent"), SessionFile: file, Args: []string{"-test.run=^TestSharedPiHelper$", "--", "--fake-new-path", file}}
	if e := os.WriteFile(file, []byte("{\"type\":\"session\",\"id\":\"fixture-session\"}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if join {
		member, err := JoinSessionMember(cfg.AgentDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { member.Close() })
	}
	return cfg, root
}
func sharedCall(t *testing.T, c *Client, command map[string]any) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, e := c.Call(ctx, command)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func sharedEvent(t *testing.T, c *Client, kind string) json.RawMessage {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-c.Events():
			if !ok {
				t.Fatal("shared client stopped")
			}
			var v struct{ Type string }
			_ = json.Unmarshal(e.Data, &v)
			if v.Type == kind {
				return e.Data
			}
		case <-timer.C:
			t.Fatalf("missing shared event %s", kind)
		}
	}
}
func sharedEventually(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("shared lifecycle condition timed out")
}
func sharedGone(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e == nil {
		_, after, _ := strings.Cut(string(data), ") ")
		return strings.HasPrefix(after, "Z ")
	}
	return false
}

func TestSharedSessionSingleBackendEventsAndMutationLease(t *testing.T) {
	cfg, root := sharedFixture(t)
	a, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if !a.Shared() || a.PID() != b.PID() {
		t.Fatal("duplicate shared backend")
	}
	pid := a.PID()
	sharedCall(t, a, map[string]any{"type": "prompt", "message": "from-a"})
	for _, c := range []*Client{a, b} {
		event := sharedEvent(t, c, "fixture_message")
		if !strings.Contains(string(event), "from-a") {
			t.Fatal(string(event))
		}
	}
	if release, e := a.BeginFileMutation(context.Background()); e == nil {
		release()
		t.Fatal("mutation accepted with another view")
	}
	var wg sync.WaitGroup
	for i, c := range []*Client{a, b} {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			raw, e := c.CallExtension(ctx, "fixture", map[string]any{"owner": i})
			if e != nil || !strings.Contains(string(raw), fmt.Sprintf(`"owner":%d`, i)) {
				t.Errorf("extension response crossed clients: %s %v", raw, e)
			}
		}(i, c)
	}
	wg.Wait()
	sharedCall(t, a, map[string]any{"type": "prompt", "message": "ask"})
	sharedEvent(t, a, "extension_ui_request")
	sharedEvent(t, b, "extension_ui_request")
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	sharedEvent(t, c, "extension_ui_request")
	for _, p := range []*Client{a, b} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		e = p.Send(ctx, map[string]any{"type": "extension_ui_response", "id": "fixture-question", "confirmed": true})
		cancel()
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []*Client{a, b, c} {
		sharedEvent(t, p, "goui_session_ui_resolved")
	}
	c.Close()
	b.Close()
	// Wait until the server has observed both socket closes, without opening
	// another attachment or terminating the sole live backend.
	var release func()
	sharedEventually(t, func() bool {
		var err error
		release, err = a.BeginFileMutation(context.Background())
		return err == nil
	})
	if _, e = startShared(cfg); e == nil {
		t.Fatal("new attachment bypassed lease")
	}
	a.Close()
	if _, e = startShared(cfg); e == nil {
		t.Fatal("lease disappeared when Client.Close returned")
	}
	release()
	sharedEventually(t, func() bool { return sharedGone(pid) })
	wire, _ := os.ReadFile(filepath.Join(root, "wire"))
	if strings.Count(string(wire), `"type":"extension_ui_response"`)+strings.Count(string(wire), `"type": "extension_ui_response"`) != 1 {
		t.Fatalf("question answered twice: %s", wire)
	}
	sharedEventually(t, func() bool { return sharedGone(a.shared.record.PID) })
}

func TestSharedSessionNewFileAlias(t *testing.T) {
	cfg, _ := sharedFixture(t)
	file := cfg.SessionFile
	cfg.SessionFile = ""
	_ = os.Remove(file)
	a, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	sharedCall(t, a, map[string]any{"type": "prompt", "message": "creates-native-file"})
	cfg.SessionFile = file
	b, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if a.PID() != b.PID() {
		t.Fatal("new session resume duplicated backend")
	}
	pid := a.PID()
	a.Close()
	sharedCall(t, b, map[string]any{"type": "prompt", "message": "after-first-left"})
	sharedEvent(t, b, "fixture_message")
	if sharedGone(pid) {
		t.Fatal("first client exit killed shared backend")
	}
	b.Close()
	sharedEventually(t, func() bool { return sharedGone(pid) })
}

func TestSharedClientProcess(t *testing.T) {
	raw := os.Getenv("GOUI_SHARED_CLIENT_CONFIG")
	if raw == "" {
		return
	}
	var cfg Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		os.Exit(2)
	}
	member, e := JoinSessionMember(cfg.AgentDir)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(3)
	}
	c, e := startShared(cfg)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(3)
	}
	go func() {
		for range c.Events() {
		}
	}()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]int{"pid": c.PID()})
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, e = c.Call(ctx, map[string]any{"type": "prompt", "message": scan.Text()})
		cancel()
		if e != nil {
			os.Exit(4)
		}
		fmt.Fprintln(os.Stdout, "ok")
	}
	c.Close()
	member.Close()
	os.Exit(0)
}
func TestSharedIndependentProcessesRetainBackend(t *testing.T) {
	cfg, root := sharedFixture(t)
	exe, _ := os.Executable()
	raw, _ := json.Marshal(cfg)
	type child struct {
		cmd *exec.Cmd
		in  *os.File
		out *bufio.Reader
		pid int
	}
	start := func() child {
		cmd := exec.Command(exe, "-test.run=^TestSharedClientProcess$", "--")
		cmd.Env = append(os.Environ(), "GOUI_SHARED_CLIENT_CONFIG="+string(raw))
		in, e := cmd.StdinPipe()
		if e != nil {
			t.Fatal(e)
		}
		out, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stderr = os.Stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		r := bufio.NewReader(out)
		line, e := r.ReadBytes('\n')
		if e != nil {
			t.Fatal(e)
		}
		var v struct{ PID int }
		if e = json.Unmarshal(line, &v); e != nil {
			t.Fatalf("child: %s %v", line, e)
		}
		return child{cmd: cmd, in: in.(*os.File), out: r, pid: v.PID}
	}
	a := start()
	b := start()
	if a.pid != b.pid {
		t.Fatal("independent processes spawned duplicate Pi")
	}
	a.in.Close()
	if e := a.cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	if _, e := fmt.Fprintln(b.in, "second-process-still-live"); e != nil {
		t.Fatal(e)
	}
	line, e := b.out.ReadString('\n')
	if e != nil || line != "ok\n" {
		t.Fatalf("surviving process: %q %v", line, e)
	}
	b.in.Close()
	if e = b.cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	sharedEventually(t, func() bool { return sharedGone(a.pid) })
	wire, _ := os.ReadFile(filepath.Join(root, "wire"))
	if strings.Count(string(wire), `"kind":"start"`)+strings.Count(string(wire), `"kind": "start"`) != 1 {
		t.Fatalf("expected one real backend: %s", wire)
	}
}
