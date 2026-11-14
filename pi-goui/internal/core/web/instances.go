package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type instanceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Desktop   bool   `json:"desktop"`
	Cwd       string `json:"cwd"`
	ProjectID string `json:"projectId"`
	Status    string `json:"status"`
}

// instanceSnapshot takes lifecycle, then terminal.mu, just like connect.
func (s *Server) instanceSnapshot(id string) instanceInfo {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	cwd := s.cwd
	if cwd == "" {
		cwd = s.cfg.Cwd
	}
	status := "idle"
	if s.terminal != nil {
		s.terminal.mu.Lock()
		status = "running"
		if s.terminal.exited {
			status = "exited"
		}
		s.terminal.mu.Unlock()
	}
	return instanceInfo{ID: id, Name: filepath.Base(cwd), Desktop: s.cfg.SharedDesktop,
		Cwd: cwd, ProjectID: s.projectID, Status: status}
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	s.instancesMu.Lock()
	items := make([]instanceInfo, 0, len(s.instances)+1)
	main := s.instanceSnapshot("main")
	if main.Desktop || s.cfg.OfferDefaultInstance || main.ProjectID != "" {
		items = append(items, main)
	}
	ids := make([]string, 0, len(s.instances))
	for id := range s.instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		items = append(items, s.instances[id].instanceSnapshot(id))
	}
	s.instancesMu.Unlock()
	items = append(items, s.discoverDesktops(r)...)
	writeJSON(w, http.StatusOK, struct {
		Instances []instanceInfo `json:"instances"`
	}{items})
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cwd string `json:"cwd"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	if !filepath.IsAbs(body.Cwd) {
		writeError(w, 400, errors.New("choose an absolute directory on the server"))
		return
	}
	cwd := filepath.Clean(body.Cwd)
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		writeError(w, 400, errors.New("project directory does not exist or is not a directory"))
		return
	}
	s.instancesMu.Lock()
	defer s.instancesMu.Unlock()
	if s.ctx.Err() != nil {
		writeError(w, 503, errors.New("server is shutting down"))
		return
	}
	factory := s.cfg.NewInstanceCommandForDirectory
	if factory == nil {
		writeError(w, 503, errors.New("independent instance command is not configured"))
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	projectID, err := randomID()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	// Never reuse the root Command or CommandForDirectory: either may pin an
	// explicit history file already owned by the desktop. Children cannot in
	// turn create descendants through their scoped route.
	cfg := s.cfg
	cfg.Cwd, cfg.SharedDesktop = cwd, false
	cfg.DiscoverDesktops = false
	cfg.Command, cfg.CommandForDirectory = factory(cwd), factory
	cfg.NewInstanceCommandForDirectory = nil
	child, err := New(cfg)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	child.cwd, child.projectID = cwd, projectID
	child.recent = []string{cwd}
	s.instances[id] = child
	s.lifecycle.Lock()
	s.rememberDirectory(cwd)
	s.lifecycle.Unlock()
	writeJSON(w, 200, child.instanceSnapshot(id))
}

func (s *Server) routeInstance(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/instances/")
	id, suffix, hasSuffix := strings.Cut(path, "/")
	if id == "" {
		writeError(w, 404, errors.New("instance not found"))
		return
	}
	if !hasSuffix && r.Method == http.MethodDelete {
		if strings.HasPrefix(id, "desktop-") {
			s.proxyDesktop(w, r, id, "/api/terminal/close")
			return
		}
		s.deleteInstance(w, r, id)
		return
	}
	// Explicit allowlist: no nested registry, login/logout, assets or close
	// endpoint. Authentication and Origin are checked by the root handler.
	var endpoint func(*Server, http.ResponseWriter, *http.Request)
	switch r.Method + " " + suffix {
	case "GET api/info":
		endpoint = (*Server).info
	case "GET api/projects":
		endpoint = (*Server).projects
	case "POST api/projects/select":
		endpoint = (*Server).selectProject
	case "GET api/terminal":
		endpoint = (*Server).connect
	case "POST api/terminal/restart":
		endpoint = (*Server).restart
	default:
		writeError(w, 404, errors.New("not found"))
		return
	}
	if strings.HasPrefix(id, "desktop-") {
		s.proxyDesktop(w, r, id, "/"+suffix)
		return
	}
	s.instancesMu.Lock()
	target := s.instances[id]
	if id == "main" {
		target = s
	}
	s.instancesMu.Unlock()
	if target == nil {
		writeError(w, 404, errors.New("instance not found"))
		return
	}
	// Do not hold registry locks for WebSockets. Deletion cancels the child
	// before releasing it; its lifecycle lock rejects any late start/restart.
	target.secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint(target, w, r)
	})).ServeHTTP(w, r)
}

func (s *Server) deleteInstance(w http.ResponseWriter, r *http.Request, id string) {
	if id == "main" {
		// Closing the desktop project must not stop this host or its children.
		s.closeProject(w, r)
		return
	}
	var body projectRequest
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	s.instancesMu.Lock()
	defer s.instancesMu.Unlock()
	child := s.instances[id]
	if child == nil {
		writeError(w, 404, errors.New("instance not found"))
		return
	}
	child.lifecycle.Lock()
	if body.ProjectID == "" || body.ProjectID != child.projectID {
		child.lifecycle.Unlock()
		writeError(w, 409, errors.New("project changed; refresh the instance list"))
		return
	}
	child.cancel()
	child.stopTerminal("project_closed")
	child.lifecycle.Unlock()
	_ = child.Close()
	delete(s.instances, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
