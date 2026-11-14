package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	t.Setenv("PI_WEBUI_TOKEN", "")
	o, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Listen != "127.0.0.1:8888" || o.Token != "" {
		t.Fatalf("defaults: %+v", o)
	}
	t.Setenv("PI_WEBUI_TOKEN", "environment-secret")
	o, err = parseOptions([]string{"--listen", ":8888", "--token", "explicit", "--", "--model=provider/id", "--no-extensions"}, io.Discard)
	if err != nil || o.Token != "explicit" || !reflect.DeepEqual(o.RPC.Args, []string{"--model", "provider/id", "--no-extensions"}) {
		t.Fatalf("options: %+v %v", o, err)
	}
	o, err = parseOptions(nil, io.Discard)
	if err != nil || o.Token != "environment-secret" {
		t.Fatalf("token env: %v", err)
	}
	for _, args := range [][]string{{"--no-session", "--session", "file"}, {"--tls-cert", "cert"}, {"--tls-key", "key"}, {"--request-timeout", "0s"}, {"--max-sessions", "0"}, {"--listen", "broken"}, {"prompt"}, {"--", "--mode", "text"}, {"--", "--session", "x"}, {"--", "--continue"}, {"--", "--resume"}, {"--", "--session-id", "id"}, {"--", "--session-dir", "x"}, {"--", "--no-session"}, {"--", "--print"}, {"--", "prompt"}, {"--", "--export", "file"}, {"--", "--model"}, {"--", "--model="}} {
		if _, err = parseOptions(args, io.Discard); err == nil {
			t.Errorf("accepted unsafe options %q", args)
		}
	}
}

func TestLoginKeyAlias(t *testing.T) {
	t.Setenv("PI_WEBUI_TOKEN", "environment-secret")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"key", []string{"--key", "lan-key"}, "lan-key"},
		{"equals", []string{"--key=lan-key"}, "lan-key"},
		{"token", []string{"--token", "legacy-key"}, "legacy-key"},
		{"environment", nil, "environment-secret"},
		{"key-last", []string{"--token", "old", "--key", "new"}, "new"},
		{"token-last", []string{"--key", "old", "--token", "new"}, "new"},
		{"empty", []string{"--key="}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseOptions(tc.args, io.Discard)
			if err != nil || o.Token != tc.want {
				t.Fatalf("token = %q, want %q; error: %v", o.Token, tc.want, err)
			}
		})
	}
	if _, err := parseOptions([]string{"--key"}, io.Discard); err == nil {
		t.Fatal("accepted --key without a value")
	}
}

func TestTerminalConfig(t *testing.T) {
	t.Setenv("PI_WEBUI_TOKEN", "web-only-secret")
	t.Setenv("PI_CODING_AGENT_DIR", "old-agent")
	o, err := parseOptions([]string{"--pi", "/explicit/pi", "--key", "explicit-web-key", "--agent-dir", t.TempDir(), "--no-session", "--", "--models", "test/*", "--offline"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := terminalConfig(o)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DiscoverDesktops || cfg.Token != "explicit-web-key" || cfg.Cwd != o.RPC.Cwd || cfg.Command[1] != "--internal-tui" {
		t.Fatalf("terminal config: %+v", cfg)
	}
	child, err := parseOptions(cfg.Command[2:], io.Discard)
	if err != nil || !reflect.DeepEqual(child.RPC, o.RPC) {
		t.Fatalf("TUI options changed: %+v, %v", child.RPC, err)
	}
	env := strings.Join(cfg.Env, "\n")
	if strings.Contains(env, "PI_WEBUI_TOKEN=") || strings.Contains(env, "PI_CODING_AGENT_DIR=old-agent") || !strings.Contains(env, "PI_CODING_AGENT_DIR="+o.RPC.AgentDir) || !strings.Contains(env, "TERM=xterm-256color") || !strings.Contains(env, "COLORTERM=truecolor") {
		t.Fatalf("unexpected terminal environment: %s", env)
	}
	for _, arg := range cfg.Command {
		if strings.Contains(arg, "web-only-secret") || strings.Contains(arg, "explicit-web-key") || arg == "--listen" || arg == "--token" || arg == "--key" {
			t.Fatal("web credentials/options leaked into TUI command")
		}
	}
}

func TestTerminalCommandForSelectedDirectory(t *testing.T) {
	agent, original, next := t.TempDir(), t.TempDir(), t.TempDir()
	o, err := parseOptions([]string{"--cwd", original, "--agent-dir", agent, "--session", filepath.Join(original, "saved.jsonl"), "--", "--offline"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := terminalConfig(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{original, next} {
		args := cfg.CommandForDirectory(cwd)
		child, err := parseOptions(args[2:], io.Discard)
		if err != nil || child.RPC.Cwd != cwd || child.RPC.AgentDir != agent {
			t.Fatalf("selected child: %+v %v", child.RPC, err)
		}
		if (child.RPC.SessionFile != "") != (cwd == original) {
			t.Fatal("explicit session leaked to a different project")
		}
		independent, err := parseOptions(cfg.NewInstanceCommandForDirectory(cwd)[2:], io.Discard)
		if err != nil || independent.RPC.Cwd != cwd || independent.RPC.SessionFile != "" {
			t.Fatalf("independent instance reused explicit session: %+v %v", independent.RPC, err)
		}
	}
}

func TestHelpVersionAndExplicitPiFailure(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out, &out); err != nil || out.Len() == 0 {
			t.Fatalf("%v: %v, %s", args, err, out.String())
		}
		if strings.Contains(out.String(), "?pk=") || strings.Contains(out.String(), "\x1b[30;47m") {
			t.Fatalf("non-start mode printed a login link/QR: %q", out.String())
		}
	}
	t.Setenv("PI_BIN", "sh")
	missing := filepath.Join(t.TempDir(), "missing-pi")
	err := run(context.Background(), []string{"--pi", missing, "--check"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("explicit binary fell back: %v", err)
	}
}

func fakePi(t *testing.T, success bool) (string, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pi")
	log := filepath.Join(dir, "commands")
	script := "#!" + python + "\nimport sys,json\nwith open(" + strconvQuote(log) + ",'a') as f: f.write(json.dumps(sys.argv[1:])+'\\n')\nfor line in sys.stdin:\n r=json.loads(line)\n with open(" + strconvQuote(log) + ",'a') as f: f.write(r['type']+'\\n')\n print(json.dumps({'type':'response','id':r['id'],'command':r['type'],'success':" + map[bool]string{true: "True", false: "False"}[success] + ",'data':{'entries':[]},'error':'unsupported'}),flush=True)\n"
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func strconvQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}

func TestReadOnlyCheck(t *testing.T) {
	bin, log := fakePi(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := run(ctx, []string{"--pi", bin, "--check", "--session", "ignored.jsonl"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "?pk=") || strings.Contains(output.String(), "\x1b[30;47m") {
		t.Fatal("read-only check printed login credentials")
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 3 || lines[1] != "get_state" || lines[2] != "get_entries" || strings.Contains(lines[0], "ignored") || !strings.Contains(lines[0], "--no-session") {
		t.Fatalf("unsafe check: %s", b)
	}
	bin, _ = fakePi(t, false)
	if err := run(ctx, []string{"--pi", bin, "--check"}, io.Discard, io.Discard); err == nil {
		t.Fatal("RPC failure hidden")
	}
}

func TestServerCancellation(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	bin, _ := fakePi(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	output := make(startupOutput, 4)
	go func() { done <- run(ctx, []string{"--pi", bin, "--listen", "127.0.0.1:0"}, output, io.Discard) }()
	var address string
	select {
	case message := <-output:
		address = strings.TrimPrefix(strings.Split(message, "\n")[0], "Pi WebUI: ")
		_, token, ok := strings.Cut(message, "Login token: ")
		if !ok || strings.TrimSpace(token) == "" {
			t.Fatal("startup did not print a login token")
		}
	case err := <-done:
		t.Fatalf("server exited before startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server startup timed out")
	}
	for _, prefix := range []string{"Login URL (contains token): " + address + "/?pk=", "\x1b[30;47m"} {
		select {
		case message := <-output:
			if !strings.HasPrefix(message, prefix) {
				t.Fatalf("startup output %q does not begin with %q", message, prefix)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("startup did not print the login URL and QR")
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(address + "/api/auth")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("auth status %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("HTTP server did not shut down")
	}
	if response, err := client.Get(address + "/api/auth"); err == nil {
		response.Body.Close()
		t.Fatal("listener still accepts requests after cancellation")
	}
}

func TestListenFailure(t *testing.T) {
	bin, _ := fakePi(t, true)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var output bytes.Buffer
	err = run(context.Background(), []string{"--pi", bin, "--listen", listener.Addr().String()}, &output, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("listen failure: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("printed startup credentials before listener succeeded")
	}
}

type startupOutput chan string

func (w startupOutput) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }
