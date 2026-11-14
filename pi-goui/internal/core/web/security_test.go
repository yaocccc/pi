package web

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticationAndOrigin(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/api/info", "/api/projects", "/api/terminal?cols=80&rows=24"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	login := func(token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.test/api/login", strings.NewReader(`{"token":"`+token+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := login("wrong", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, origin := range []string{"http://evil.test", "null", "https://example.test", "http://example.test/path", "http://user@example.test"} {
		if w := login(s.Token(), origin); w.Code != 403 {
			t.Fatalf("origin %s: %d", origin, w.Code)
		}
	}
	w := login(s.Token(), "http://example.test")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Secure {
		t.Fatal(cookies)
	}
	for _, test := range []struct {
		method, path, origin string
		upgrade              bool
		want                 int
	}{
		{"GET", "/api/auth", "", false, 200},
		{"POST", "/api/logout", "", false, 403},
		{"POST", "/api/logout", "http://example.test", false, 200},
		{"GET", "/api/terminal?cols=80&rows=24", "", true, 403},
		{"GET", "/api/info", "http://evil.test", false, 403},
	} {
		r := httptest.NewRequest(test.method, "http://example.test"+test.path, nil)
		r.AddCookie(cookies[0])
		r.Header.Set("Origin", test.origin)
		if test.upgrade {
			r.Header.Set("Upgrade", "websocket")
		}
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, r)
		if out.Code != test.want {
			t.Fatalf("%s %s: %d %s", test.method, test.path, out.Code, out.Body)
		}
	}
	r := httptest.NewRequest("GET", "/api/info", nil)
	r.Header.Set("Authorization", "Bearer "+s.Token())
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal(out.Code)
	}
	// API auth never accepts URL credentials; the frontend exchanges pk for a
	// cookie using POST /api/login. Info/auth bodies must not leak the token.
	r = httptest.NewRequest("GET", "/api/info?token="+s.Token(), nil)
	out = httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if out.Code != 401 {
		t.Fatal(out.Code)
	}
	if w = request(t, s, "GET", "/api/info", nil); bytes.Contains(w.Body.Bytes(), []byte(s.Token())) {
		t.Fatal("token leaked")
	}
}
func TestLoginLinkNavigation(t *testing.T) {
	s := testServer(t)
	for _, c := range []struct {
		path, mode, destination string
		want                    int
	}{
		{"/?pk=link-token", "navigate", "document", 200},
		{"/?pk=link-token", "navigate", "iframe", 403},
		{"/?pk=link-token", "cors", "empty", 403},
		{"/api/info?pk=link-token", "navigate", "document", 403},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", c.mode)
		r.Header.Set("Sec-Fetch-Dest", c.destination)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("%s %s %s: %d", c.path, c.mode, c.destination, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/info?pk="+s.Token(), nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("pk must not bypass API cookie authentication")
	}
}

func TestSecurityHeadersTLSAndBodyLimits(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest("POST", "https://example.test/api/login", strings.NewReader(`{"token":"`+s.Token()+`"}`))
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !w.Result().Cookies()[0].Secure {
		t.Fatal(w.Code, w.Body)
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security"} {
		if w.Header().Get(header) == "" {
			t.Fatal("missing " + header)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self';") {
		t.Fatal("scripts policy weakened")
	}
	for _, body := range []string{`{"token":"x"} {}`, `{"extra":true}`, strings.Repeat("x", 8192)} {
		r = httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	r = httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"token":"`+s.Token()+`"}`))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("non JSON login accepted")
	}
}
