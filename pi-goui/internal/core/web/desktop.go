package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"sort"
	"strings"
	"sync"
	"time"

	"pi-goui/internal/runtime/desktop"
)

// beginDesktopRequest joins discovery and proxy handlers to host shutdown.
// Add and the closed check use the same lock as Close's Wait barrier.
func (s *Server) beginDesktopRequest(parent context.Context) (context.Context, func(), bool) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.ctx.Err() != nil {
		return nil, nil, false
	}
	s.clients.Add(1)
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel(); s.clients.Done() }, true
}

func (s *Server) discoverDesktops(r *http.Request) []instanceInfo {
	if !s.cfg.DiscoverDesktops || !desktop.DesktopSharingSupported() || s.cfg.AgentDir == "" {
		return nil
	}
	ctx, done, ok := s.beginDesktopRequest(r.Context())
	if !ok {
		return nil
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	records := desktop.ReadDesktopRecords(s.cfg.AgentDir)
	var mu sync.Mutex
	var result []instanceInfo
	var workers sync.WaitGroup
	jobs := make(chan desktop.Record)
	for range min(16, len(records)) {
		workers.Go(func() {
			for record := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if info, ok := probeDesktop(ctx, record); ok {
					mu.Lock()
					result = append(result, info)
					mu.Unlock()
				}
			}
		})
	}
	for _, record := range records {
		select {
		case jobs <- record:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func probeDesktop(parent context.Context, record desktop.Record) (instanceInfo, bool) {
	ctx, cancel := context.WithTimeout(parent, 350*time.Millisecond)
	defer cancel()
	client := record.Client()
	defer client.CloseIdleConnections()
	r, _ := http.NewRequestWithContext(ctx, "GET", "http://desktop.local/api/instances", nil)
	r.Header.Set("Authorization", "Bearer "+record.Token)
	res, err := client.Do(r)
	if err != nil {
		return instanceInfo{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return instanceInfo{}, false
	}
	var list struct {
		Instances []instanceInfo `json:"instances"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&list) != nil {
		return instanceInfo{}, false
	}
	for _, info := range list.Instances {
		if info.ID == "main" && info.Desktop {
			info.ID = "desktop-" + record.ID
			return info, true
		}
	}
	return instanceInfo{}, false
}

// proxyDesktop is reached only after the root's auth/Origin checks and scoped
// endpoint allowlist. An instance id can select a checked registration, never
// a client-supplied URL, socket, token, or arbitrary upstream endpoint.
func (s *Server) proxyDesktop(w http.ResponseWriter, r *http.Request, id, path string) {
	if !s.cfg.DiscoverDesktops {
		writeError(w, 404, errors.New("instance not found"))
		return
	}
	record, ok := desktop.ReadDesktopRecord(s.cfg.AgentDir, strings.TrimPrefix(id, "desktop-"))
	if !ok {
		writeError(w, 404, errors.New("desktop instance is no longer available"))
		return
	}
	ctx, done, ok := s.beginDesktopRequest(r.Context())
	if !ok {
		writeError(w, 503, errors.New("server is shutting down"))
		return
	}
	defer done()
	client := record.Client()
	defer client.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{
		Transport: client.Transport,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme, p.Out.URL.Host = "http", "desktop.local"
			p.Out.URL.Path, p.Out.URL.RawPath = path, ""
			// Desktop sizing priority is only available on the private local
			// attach; browsers cannot claim it through this public proxy.
			p.Out.Host = "desktop.local"
			if r.Method == http.MethodDelete {
				p.Out.Method = http.MethodPost
			}
			for _, key := range []string{"Cookie", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Referer"} {
				p.Out.Header.Del(key)
			}
			p.Out.Header.Set("Authorization", "Bearer "+record.Token)
		},
		ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, 502, errors.New("desktop instance is unavailable"))
		},
	}
	// ReverseProxy cancels the upstream on shutdown, but a copy goroutine may
	// already be blocked writing to an unread browser. Close the hijacked
	// frontend too so both directions and the handler can finish promptly.
	proxy.ServeHTTP(desktopProxyWriter{ResponseWriter: w, ctx: ctx}, r.WithContext(ctx))
}

type desktopProxyWriter struct {
	http.ResponseWriter
	ctx context.Context
}

func (w desktopProxyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w desktopProxyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buf, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		context.AfterFunc(w.ctx, func() { _ = conn.Close() })
	}
	return conn, buf, err
}
