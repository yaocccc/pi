//go:build unix

package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-goui/internal/runtime/desktop"

	"github.com/coder/websocket"
)

func desktopFixture(t *testing.T, agent string) (*Server, *desktop.DesktopEndpoint, string) {
	t.Helper()
	s, _, log := instanceTerminalServer(t)
	s.cfg.SharedDesktop = true
	s.cfg.AgentDir = agent
	endpoint, err := desktop.OpenDesktop(agent, s.Token())
	if err != nil {
		t.Fatal(err)
	}
	host := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = host.Serve(endpoint.Listener) }()
	t.Cleanup(func() {
		_ = s.Close()
		_ = host.Close()
		_ = endpoint.Close()
		<-done
	})
	return s, endpoint, log
}

func desktopBrowserHost(t *testing.T, agent string) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(Config{Command: []string{"unused"}, Cwd: t.TempDir(), AgentDir: agent, DiscoverDesktops: true})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { _ = s.Close(); h.Close() })
	return s, h
}

func privateDesktopDial(t *testing.T, s *Server, endpoint *desktop.DesktopEndpoint) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://desktop.local/api/terminal?cols=80&rows=24&projectId="+s.projectID,
		&websocket.DialOptions{HTTPClient: endpoint.HTTPClient, HTTPHeader: http.Header{"Authorization": {"Bearer " + s.Token()}}})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func TestDesktopDiscoveryBothStartOrdersAndProxyLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, browserFirst := range []bool{false, true} {
		t.Run(map[bool]string{true: "web-first", false: "desktop-first"}[browserFirst], func(t *testing.T) {
			agent := t.TempDir()
			var s *Server
			var h *httptest.Server
			if browserFirst {
				s, h = desktopBrowserHost(t, agent)
				if len(instanceList(t, s)) != 0 {
					t.Fatal("unexpected desktops")
				}
			}
			desktop, endpoint, log := desktopFixture(t, agent)
			if !browserFirst {
				s, h = desktopBrowserHost(t, agent)
			}
			list := instanceList(t, s)
			if len(list) != 1 || list[0].ID != "desktop-"+endpoint.ID || !list[0].Desktop || list[0].Status != "idle" || desktop.terminal != nil {
				t.Fatalf("discovery not lazy: %+v", list)
			}
			// A different agent root cannot discover this desktop.
			other, _ := desktopBrowserHost(t, t.TempDir())
			if len(instanceList(t, other)) != 0 {
				t.Fatal("agent scopes mixed")
			}
			local := privateDesktopDial(t, desktop, endpoint)
			readUntil(t, local, "PID:")
			original := desktop.terminal
			list = instanceList(t, s)
			if len(list) != 1 || list[0].Status != "running" || list[0].ProjectID != desktop.projectID {
				t.Fatal(list)
			}
			// Listing never steals control or emits credentials/socket paths.
			sendInput(t, local, []byte("local"))
			readUntil(t, local, "INPUT:6c6f63616c")
			w := request(t, s, "GET", "/api/instances", nil)
			if strings.Contains(w.Body.String(), desktop.Token()) || strings.Contains(w.Body.String(), "desktop.sock") || strings.Contains(w.Body.String(), "pi-goui-desktop-") {
				t.Fatal("private discovery credentials leaked")
			}
			remote := dialInstance(t, s, h, list[0])
			readUntil(t, remote, "PID:")
			sendInput(t, remote, []byte("remote"))
			readUntil(t, remote, "INPUT:72656d6f7465")
			readUntil(t, local, "INPUT:72656d6f7465")
			sendInput(t, local, []byte("local-parallel"))
			readUntil(t, local, "INPUT:6c6f63616c2d706172616c6c656c")
			readUntil(t, remote, "INPUT:6c6f63616c2d706172616c6c656c")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := remote.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":111,"rows":31}`)); err != nil {
				t.Fatal(err)
			}
			readUntil(t, remote, "SIZE:111:31")
			// Stopping web joins proxy handlers, never the externally owned PTY.
			closed := make(chan struct{})
			go func() { _ = s.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(3 * time.Second):
				t.Fatal("web shutdown left a proxy handler blocked")
			}
			if desktop.ctx.Err() != nil || desktop.terminal != original {
				t.Fatal("web shutdown stopped desktop")
			}
			select {
			case <-original.done:
				t.Fatal("desktop PTY exited with web host")
			default:
			}
			// The original local connection survives: no reconnect or Enter needed.
			sendInput(t, local, []byte("retake"))
			readUntil(t, local, "INPUT:726574616b65")
			// A new web host discovers exactly the same stable desktop/RPC.
			s, h = desktopBrowserHost(t, agent)
			list = instanceList(t, s)
			remote = dialInstance(t, s, h, list[0])
			readUntil(t, remote, "PID:")
			if strings.Count(logContents(t, log), "START:") != 1 {
				t.Fatal("web restart duplicated desktop backend")
			}
			path := "/api/instances/" + list[0].ID
			if w := request(t, s, "DELETE", path, projectRequest{ProjectID: "stale"}); w.Code != 409 {
				t.Fatal("stale project accepted", w.Code)
			}
			if w := request(t, s, "DELETE", path, projectRequest{ProjectID: list[0].ProjectID}); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			readUntil(t, remote, `"type":"project_closed"`)
			readUntil(t, local, `"type":"project_closed"`)
			if desktop.ctx.Err() != nil {
				t.Fatal("project close killed desktop host")
			}
			select {
			case <-original.done:
			default:
				t.Fatal("explicit close left desktop PTY alive")
			}
			if w := request(t, s, "GET", path+"/api/terminal?cols=80&rows=24&projectId="+list[0].ProjectID, nil); w.Code != 409 {
				t.Fatal("old project identity resurrected", w.Code)
			}
			selected := request(t, s, "POST", path+"/api/projects/select", projectRequest{Cwd: t.TempDir()})
			var next projectRequest
			if selected.Code != 200 || json.Unmarshal(selected.Body.Bytes(), &next) != nil || next.ProjectID == list[0].ProjectID || desktop.terminal != nil {
				t.Fatalf("proxied directory selection was not lazy/isolated: %d %s", selected.Code, selected.Body)
			}
			list[0].ProjectID, list[0].Cwd = next.ProjectID, next.Cwd
			remote = dialInstance(t, s, h, list[0])
			readUntil(t, remote, "PID:")
			if w := request(t, s, "POST", path+"/api/terminal/restart", next); w.Code != 200 {
				t.Fatal("proxied restart failed", w.Code, w.Body)
			}
			readUntil(t, remote, `"type":"restart"`)
			if desktop.ctx.Err() != nil || desktop.terminal != nil {
				t.Fatal("restart either killed host or started without attachment")
			}
			if err := endpoint.Close(); err != nil {
				t.Fatal(err)
			}
			if len(instanceList(t, s)) != 0 {
				t.Fatal("unregistered desktop still listed")
			}
			if w := request(t, s, "GET", path+"/api/info", nil); w.Code != 404 {
				t.Fatal("old registration remains routable", w.Code)
			}
		})
	}
}

func TestDesktopDiscoveryCanonicalAgentAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	_, endpoint, _ := desktopFixture(t, agent)
	alias := filepath.Join(t.TempDir(), "agent-alias")
	if err := os.Symlink(agent, alias); err != nil {
		t.Fatal(err)
	}
	host, _ := desktopBrowserHost(t, alias)
	list := instanceList(t, host)
	if len(list) != 1 || list[0].ID != "desktop-"+endpoint.ID {
		t.Fatalf("same-agent alias not discovered: %+v", list)
	}
	path := "/api/instances/desktop-" + endpoint.ID + "/api/info"
	if w := request(t, host, "GET", path, nil); w.Code != http.StatusOK {
		t.Fatal("same-agent alias cannot proxy", w.Code)
	}
	for _, scope := range []string{t.TempDir(), ""} {
		other, _ := desktopBrowserHost(t, scope)
		if len(instanceList(t, other)) != 0 {
			t.Fatal("foreign or empty agent discovered desktop")
		}
		if w := request(t, other, "GET", path, nil); w.Code != http.StatusNotFound {
			t.Fatal("foreign or empty agent proxied desktop", w.Code)
		}
	}
}

func TestDesktopDiscoveryIgnoresBrokenOrHungEndpoints(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	_, endpoint, _ := desktopFixture(t, agent)
	hung, err := desktop.OpenDesktop(agent, "hung-private-token")
	if err != nil {
		t.Fatal(err)
	}
	defer hung.Close()
	// It listens but never accepts or replies. The valid registration remains
	// discoverable without waiting on this endpoint's HTTP response indefinitely.
	invalid, _ := randomID()
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".pi", "goui-instances", invalid+".json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s, h := desktopBrowserHost(t, agent)
	start := time.Now()
	list := instanceList(t, s)
	if time.Since(start) > 2*time.Second || len(list) != 1 || list[0].ID != "desktop-"+endpoint.ID {
		t.Fatalf("hung registration blocked good desktop: %+v", list)
	}
	path := "/api/instances/" + list[0].ID
	for _, suffix := range []string{"/api/login", "/api/instances", "/api/auth", "/api/terminal/close"} {
		if w := request(t, s, "GET", path+suffix, nil); w.Code != 404 {
			t.Fatalf("proxy escaped allowlist: %s %d", suffix, w.Code)
		}
	}
	for _, auth := range []bool{false, true} {
		r, _ := http.NewRequest("GET", h.URL+path+"/api/info", nil)
		want := 401
		if auth {
			r.Header.Set("Authorization", "Bearer "+s.Token())
			r.Header.Set("Origin", "https://evil.invalid")
			want = 403
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("proxy auth/origin: %d want %d", res.StatusCode, want)
		}
	}
}

func TestDesktopProxyConcurrentShutdown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	desktop, endpoint, log := desktopFixture(t, agent)
	local := privateDesktopDial(t, desktop, endpoint)
	readUntil(t, local, "PID:")
	s, h := desktopBrowserHost(t, agent)
	info := instanceList(t, s)[0]
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c, res, err := websocket.Dial(ctx, strings.Replace(h.URL, "http", "ws", 1)+"/api/instances/"+info.ID+"/api/terminal?cols=80&rows=24&projectId="+info.ProjectID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + s.Token()}}})
			if err == nil {
				_ = c.CloseNow()
			} else if res != nil {
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
			}
		})
	}
	_ = s.Close()
	wg.Wait()
	if desktop.ctx.Err() != nil || strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("proxy race stopped or duplicated desktop")
	}
	local = privateDesktopDial(t, desktop, endpoint)
	readUntil(t, local, "PID:")
	sendInput(t, local, []byte("alive"))
	readUntil(t, local, "INPUT:616c697665")
}
