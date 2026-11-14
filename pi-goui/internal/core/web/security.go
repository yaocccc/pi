package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const cookieName = "pi_webui"

func randomID() (string, error) {
	var value [32]byte
	_, err := rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:]), err
}
func (s *Server) authenticated(r *http.Request) bool {
	value := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		value = strings.TrimPrefix(h, "Bearer ")
	} else if c, err := r.Cookie(cookieName); err == nil {
		value = c.Value
	}
	return s.validToken(value)
}
func (s *Server) validToken(value string) bool {
	a, b := sha256.Sum256([]byte(value)), sha256.Sum256([]byte(s.token))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ctx.Err() != nil {
			writeError(w, 503, errors.New("server is shutting down"))
			return
		}
		if !s.authenticated(r) {
			writeError(w, 401, errors.New("authentication required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		// xterm creates dynamic styles for terminal cell colors and geometry. Script
		// and connection sources remain same-origin; raw terminal output is not HTML.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				writeError(w, 403, errors.New("cross-origin requests are forbidden"))
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
			// A user may open a QR/login link from another site. Only allow
			// top-level page navigation; API and embedded requests stay strict.
			navigation := r.Method == http.MethodGet && r.URL.Path == "/" && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
			if !navigation {
				writeError(w, 403, errors.New("cross-origin requests are forbidden"))
				return
			}
		}
		mutating := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if (mutating || strings.EqualFold(r.Header.Get("Upgrade"), "websocket")) && r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") != "same-origin" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if _, err := r.Cookie(cookieName); err == nil {
				writeError(w, 403, errors.New("origin required for cookie-authenticated writes"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	if !s.validToken(body.Token) {
		writeError(w, 401, errors.New("invalid token"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
