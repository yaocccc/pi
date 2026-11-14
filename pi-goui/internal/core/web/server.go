package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/coder/websocket"
)

// Server owns its root TUI and any independently created browser instances.
// lifecycle serializes start/restart/close, never terminal output or HTTP auth.
type Server struct {
	cfg       Config
	token     string
	ctx       context.Context
	cancel    context.CancelFunc
	handler   http.Handler
	lifecycle sync.Mutex
	terminal  *terminal
	cwd       string
	projectID string
	recent    []string
	clients   sync.WaitGroup
	closeOnce sync.Once

	// Lock order: instancesMu before lifecycle (root or child). Registry
	// removal joins child shutdown, so Close also waits for active deletions.
	instancesMu sync.Mutex
	instances   map[string]*Server
}

func New(cfg Config) (*Server, error) {
	if len(cfg.Command) == 0 || cfg.Command[0] == "" {
		return nil, errors.New("TUI command is required")
	}
	cwd, err := filepath.Abs(cfg.Cwd)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("working directory is not a directory: %s", cwd)
	}
	cfg.Cwd = cwd
	cfg.Command = append([]string(nil), cfg.Command...)
	if cfg.Env != nil {
		cfg.Env = append([]string{}, cfg.Env...)
	}
	if cfg.Token == "" {
		cfg.Token, err = randomID()
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, token: cfg.Token, ctx: ctx, cancel: cancel}
	s.instances = make(map[string]*Server)
	mux := http.NewServeMux()
	mux.Handle("GET /api/instances", s.secure(http.HandlerFunc(s.listInstances)))
	mux.Handle("POST /api/instances", s.secure(http.HandlerFunc(s.createInstance)))
	mux.Handle("/api/instances/", s.secure(http.HandlerFunc(s.routeInstance)))
	mux.HandleFunc("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"authenticated": s.authenticated(r)})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.Handle("POST /api/logout", s.secure(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/info", s.secure(http.HandlerFunc(s.info)))
	mux.Handle("GET /api/projects", s.secure(http.HandlerFunc(s.projects)))
	mux.Handle("POST /api/projects/select", s.secure(http.HandlerFunc(s.selectProject)))
	mux.Handle("POST /api/terminal/close", s.secure(http.HandlerFunc(s.closeProject)))
	mux.Handle("GET /api/terminal", s.secure(http.HandlerFunc(s.connect)))
	mux.Handle("POST /api/terminal/restart", s.secure(http.HandlerFunc(s.restart)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, errors.New("not found")) })
	mux.Handle("/", frontendHandler())
	s.handler = securityHeaders(mux)
	return s, nil
}
func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) Token() string         { return s.token }
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.instancesMu.Lock()
		for id, child := range s.instances {
			_ = child.Close()
			delete(s.instances, id)
		}
		s.instancesMu.Unlock()
		s.lifecycle.Lock()
		if s.terminal != nil {
			s.terminal.stop()
		}
		s.lifecycle.Unlock()
		s.clients.Wait()
	})
	return nil
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	cols, rows, err := querySize(r)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	// Add under the lifecycle lock so Close cannot race Wait with a new Add.
	s.lifecycle.Lock()
	if s.ctx.Err() != nil {
		s.lifecycle.Unlock()
		writeError(w, 503, errors.New("server is shutting down"))
		return
	}
	projectID := r.URL.Query().Get("projectId")
	if projectID == "" || projectID != s.projectID {
		s.lifecycle.Unlock()
		writeError(w, 409, errors.New("select a project before connecting"))
		return
	}
	s.clients.Add(1)
	s.lifecycle.Unlock()
	defer s.clients.Done()
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 10)
	p := newPeer(c)
	p.cols, p.rows = cols, rows
	s.lifecycle.Lock()
	if s.ctx.Err() != nil || projectID != s.projectID {
		s.lifecycle.Unlock()
		return
	}
	if s.terminal == nil {
		cfg := s.cfg
		cfg.Cwd = s.cwd
		s.terminal, err = startTerminal(cfg, cols, rows)
	}
	t := s.terminal
	if err == nil {
		t.attach(p)
	}
	s.lifecycle.Unlock()
	if err != nil {
		ctx, cancel := context.WithTimeout(r.Context(), writeTimeout)
		defer cancel()
		_ = c.Write(ctx, websocket.MessageText, control("exit", err.Error(), -1))
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); p.writeLoop(ctx) }()
	// Only a lone reconnect needs a size round trip after bounded replay.
	// Other peers follow the desktop/oldest connection's stable geometry.
	if err = t.resize(p, cols, rows, true); err != nil {
		p.finish(control("exit", err.Error(), -1))
	}
	for {
		kind, data, readErr := c.Read(ctx)
		if readErr != nil {
			break
		}
		if kind == websocket.MessageBinary {
			err = t.input(p, data)
		} else {
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &msg) != nil {
				err = errors.New("invalid terminal control message")
			} else {
				switch msg.Type {
				case "ping":
					// Application heartbeat lets browsers detect a half-open
					// connection too. Never forward it to the native TUI.
					p.send(frame{kind: websocket.MessageText, data: control("pong", "", 0)})
				case "activity":
					if !validSize(msg.Cols, msg.Rows) {
						err = errors.New("invalid activity dimensions")
					} else {
						err = t.activate(p, msg.Cols, msg.Rows)
					}
				case "resize":
					if !validSize(msg.Cols, msg.Rows) {
						err = errors.New("invalid resize message")
					} else {
						err = t.resize(p, msg.Cols, msg.Rows, false)
					}
				default:
					err = errors.New("invalid terminal control message")
				}
			}
		}
		if err != nil {
			p.finish(control("exit", err.Error(), -1))
			break
		}
	}
	t.detach(p)
	cancel()
	<-writerDone
}
func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	var body projectRequest
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.ctx.Err() != nil {
		writeError(w, 503, errors.New("server is shutting down"))
		return
	}
	if body.ProjectID == "" || body.ProjectID != s.projectID {
		writeError(w, 409, errors.New("project changed; select it again"))
		return
	}
	s.stopTerminal("restart")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
