package web

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T, opts ...func(*Config)) *Server {
	t.Helper()
	cfg := Config{Command: []string{"unused-until-connected"}, Cwd: t.TempDir(), Token: "test-secret"}
	for _, opt := range opts {
		opt(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Terminal-focused tests explicitly choose their initial project.
	if w := request(t, s, "POST", "/api/projects/select", projectRequest{Cwd: cfg.Cwd}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	return s
}
func request(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+s.Token())
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestConfigAndRetiredAPI(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty command accepted")
	}
	s := testServer(t, func(c *Config) { c.Token = "" })
	if len(s.Token()) < 32 {
		t.Fatal("weak generated token")
	}
	for _, path := range []string{"/api/sessions", "/api/saved", "/api/sessions/one/command"} {
		if w := request(t, s, "GET", path, nil); w.Code != 404 {
			t.Fatalf("retired API %s: %d", path, w.Code)
		}
	}
	w := request(t, s, "GET", "/api/info", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(s.cfg.Cwd)) {
		t.Fatal(w.Body)
	}
	if w = request(t, s, "GET", "/api/terminal?cols=0&rows=24", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	_ = s.Close()
	_ = s.Close()
	if w = request(t, s, "POST", "/api/terminal/restart", projectRequest{ProjectID: s.projectID}); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
