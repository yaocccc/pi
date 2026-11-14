package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func createTestInstance(t *testing.T, s *Server, cwd string) instanceInfo {
	t.Helper()
	w := request(t, s, "POST", "/api/instances", map[string]string{"cwd": cwd})
	var info instanceInfo
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || info.ID == "" || info.ProjectID == "" {
		t.Fatalf("create instance: %d %s", w.Code, w.Body)
	}
	return info
}

func instanceList(t *testing.T, s *Server) []instanceInfo {
	t.Helper()
	w := request(t, s, "GET", "/api/instances", nil)
	var list struct {
		Instances []instanceInfo `json:"instances"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Instances == nil {
		t.Fatalf("list instances: %d %s", w.Code, w.Body)
	}
	return list.Instances
}

func TestInstanceOffersExplicitDefault(t *testing.T) {
	s, err := New(Config{Command: []string{"explicit-session"}, Cwd: t.TempDir(), OfferDefaultInstance: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list := instanceList(t, s)
	if len(list) != 1 || list[0].ID != "main" || list[0].Desktop || list[0].Status != "idle" || s.terminal != nil {
		t.Fatalf("explicit default session is missing or started early: %+v", list)
	}
}

func TestInstanceLazyListAndCommands(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		s, err := New(Config{Command: []string{"root-explicit-session"}, Cwd: t.TempDir(), SharedDesktop: desktop, DiscoverDesktops: true,
			CommandForDirectory:            func(string) []string { t.Error("root factory used for independent instance"); return nil },
			NewInstanceCommandForDirectory: func(cwd string) []string { return []string{"isolated-command", cwd} },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		list := instanceList(t, s)
		if len(list) != 0 && !desktop || desktop && (len(list) != 1 || list[0].ID != "main" || !list[0].Desktop || list[0].ProjectID != "" || list[0].Status != "idle") {
			t.Fatalf("initial list: %+v", list)
		}
		cwd := t.TempDir()
		info := createTestInstance(t, s, cwd)
		child := s.instances[info.ID]
		if info.Desktop || info.Cwd != cwd || info.Status != "idle" || child.terminal != nil || s.terminal != nil || s.cwd != "" || s.projectID != "" {
			t.Fatalf("creation started or changed a TUI: %+v", info)
		}
		if child.cfg.DiscoverDesktops || child.cfg.Command[0] != "isolated-command" || child.cfg.CommandForDirectory(cwd)[0] != "isolated-command" || child.cfg.NewInstanceCommandForDirectory != nil {
			t.Fatal("child inherited root command or recursive creation")
		}
		for _, suffix := range []string{"api/instances", "api/auth", "api/login", "api/terminal/close", ""} {
			if w := request(t, s, "GET", "/api/instances/"+info.ID+"/"+suffix, nil); w.Code != 404 {
				t.Fatalf("unexpected scoped endpoint %s: %d", suffix, w.Code)
			}
		}
		// Main and child share the service cookie, with the same CSRF rules.
		r := httptest.NewRequest("GET", "/api/instances/"+info.ID+"/api/info", nil)
		r.AddCookie(&http.Cookie{Name: cookieName, Value: s.Token()})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("shared cookie rejected: %d", w.Code)
		}
		r = httptest.NewRequest("DELETE", "/api/instances/"+info.ID, bytes.NewBufferString(`{"projectId":"`+info.ProjectID+`"}`))
		r.AddCookie(&http.Cookie{Name: cookieName, Value: s.Token()})
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("missing Origin write accepted: %d", w.Code)
		}
		r = httptest.NewRequest("GET", "/api/instances/"+info.ID+"/api/info", nil)
		r.Header.Set("Authorization", "Bearer "+s.Token())
		r.Header.Set("Origin", "https://other.example")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin read accepted: %d", w.Code)
		}
		if w := request(t, s, "DELETE", "/api/instances/"+info.ID, projectRequest{ProjectID: "stale"}); w.Code != 409 {
			t.Fatal(w.Code)
		}
		if w := request(t, s, "DELETE", "/api/instances/"+info.ID, projectRequest{ProjectID: info.ProjectID}); w.Code != 200 {
			t.Fatal(w.Body)
		}
		if child.ctx.Err() == nil {
			t.Fatal("removed child not canceled")
		}
		for _, suffix := range []string{"/api/info", "/api/terminal?cols=80&rows=24&projectId=" + info.ProjectID} {
			if w := request(t, s, "GET", "/api/instances/"+info.ID+suffix, nil); w.Code != 404 {
				t.Fatal(w.Code)
			}
		}
		next := createTestInstance(t, s, cwd)
		if next.ID == info.ID || next.ProjectID == info.ProjectID {
			t.Fatal("deleted identity reused")
		}
	}
}

func TestInstanceValidationAndMainClose(t *testing.T) {
	s := testServer(t)
	if w := request(t, s, "POST", "/api/instances", map[string]string{"cwd": s.cfg.Cwd}); w.Code != 503 {
		t.Fatal("unsafe fallback to root command", w.Code)
	}
	s.cfg.NewInstanceCommandForDirectory = func(string) []string { return []string{"independent"} }
	for _, cwd := range []string{"", "relative", filepath.Join(s.cfg.Cwd, "missing")} {
		if w := request(t, s, "POST", "/api/instances", map[string]string{"cwd": cwd}); w.Code != 400 {
			t.Fatalf("cwd %q: %d", cwd, w.Code)
		}
	}
	child := createTestInstance(t, s, t.TempDir())
	if w := request(t, s, "DELETE", "/api/instances/main", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if s.ctx.Err() != nil || s.projectID != "" || s.instances[child.ID].ctx.Err() != nil {
		t.Fatal("main close stopped host/child")
	}
	if list := instanceList(t, s); len(list) != 1 || list[0].ID != child.ID {
		t.Fatal(list)
	}
}

func TestInstanceShutdownJoinsInFlightCreation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := testServer(t, func(c *Config) {
		c.NewInstanceCommandForDirectory = func(string) []string {
			close(entered)
			<-release
			return []string{"independent"}
		}
	})
	created := make(chan int, 1)
	go func() { created <- request(t, s, "POST", "/api/instances", map[string]string{"cwd": s.cfg.Cwd}).Code }()
	<-entered
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	<-s.ctx.Done()
	select {
	case <-closed:
		t.Fatal("Close skipped a creation holding the registry lock")
	default:
	}
	close(release)
	if code := <-created; code != 200 {
		t.Fatal(code)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join creation")
	}
	if len(s.instances) != 0 {
		t.Fatal("late creation survived shutdown")
	}
}

func TestInstanceConcurrentRegistryShutdown(t *testing.T) {
	s := testServer(t, func(c *Config) {
		c.NewInstanceCommandForDirectory = func(string) []string { return []string{"independent"} }
	})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(t, s, "POST", "/api/instances", map[string]string{"cwd": s.cfg.Cwd})
			if w.Code == 503 {
				return
			}
			if w.Code != 200 {
				t.Errorf("create: %d", w.Code)
				return
			}
			var info instanceInfo
			_ = json.Unmarshal(w.Body.Bytes(), &info)
			w = request(t, s, "DELETE", "/api/instances/"+info.ID, projectRequest{ProjectID: info.ProjectID})
			if w.Code != 200 && w.Code != 503 && w.Code != 404 {
				t.Errorf("delete: %d", w.Code)
			}
		}()
	}
	_ = s.Close()
	wg.Wait()
	if len(s.instances) != 0 {
		t.Fatal("registry survived Close")
	}
	if w := request(t, s, "POST", "/api/instances", map[string]string{"cwd": s.cfg.Cwd}); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
