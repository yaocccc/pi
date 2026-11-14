package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"pi-goui/internal/core/session"
)

type projectRequest struct {
	Cwd       string `json:"cwd,omitempty"`
	ProjectID string `json:"projectId"`
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	cwd := s.cwd
	if cwd == "" {
		cwd = s.cfg.Cwd
	}
	writeJSON(w, 200, projectRequest{Cwd: cwd, ProjectID: s.projectID})
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	var native []string
	var warning string
	if s.cfg.AgentDir != "" {
		var err error
		native, err = session.RecentDirectories(s.cfg.AgentDir, 10)
		if err != nil {
			warning = err.Error()
		}
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	dirs := make([]string, 0, 10)
	seen := make(map[string]bool)
	candidates := append(append(append([]string{}, s.recent...), native...), s.cfg.Cwd)
	for _, cwd := range candidates {
		if seen[cwd] || len(dirs) == 10 {
			continue
		}
		if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
			continue
		}
		seen[cwd] = true
		dirs = append(dirs, cwd)
	}
	writeJSON(w, 200, struct {
		Directories []string `json:"directories"`
		Cwd         string   `json:"cwd"`
		ProjectID   string   `json:"projectId"`
		DefaultCwd  string   `json:"defaultCwd"`
		Warning     string   `json:"warning,omitempty"`
	}{dirs, s.cwd, s.projectID, s.cfg.Cwd, warning})
}

func (s *Server) selectProject(w http.ResponseWriter, r *http.Request) {
	var body projectRequest
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
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.ctx.Err() != nil {
		writeError(w, 503, errors.New("server is shutting down"))
		return
	}
	if s.projectID != "" && cwd == s.cwd {
		writeJSON(w, 200, projectRequest{Cwd: s.cwd, ProjectID: s.projectID})
		return
	}
	if body.ProjectID != s.projectID {
		writeError(w, 409, errors.New("project changed; refresh the directory list"))
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	s.stopTerminal("project_changed")
	s.cwd, s.projectID = cwd, id
	s.rememberDirectory(cwd)
	writeJSON(w, 200, projectRequest{Cwd: cwd, ProjectID: id})
}

// rememberDirectory requires lifecycle to be held.
func (s *Server) rememberDirectory(cwd string) {
	recent := []string{cwd}
	for _, dir := range s.recent {
		if dir != cwd && len(recent) < 10 {
			recent = append(recent, dir)
		}
	}
	s.recent = recent
}

func (s *Server) closeProject(w http.ResponseWriter, r *http.Request) {
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
	if body.ProjectID != s.projectID {
		writeError(w, 409, errors.New("project changed; refresh the directory list"))
		return
	}
	s.stopTerminal("project_closed")
	s.cwd, s.projectID = "", ""
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// stopTerminal is called with lifecycle held. It notifies the controller before
// stopping the PTY and all backends; stale sockets retain no new-project access.
func (s *Server) stopTerminal(kind string) {
	if s.terminal == nil {
		return
	}
	s.terminal.mu.Lock()
	for _, p := range s.terminal.peers {
		p.finish(control(kind, "", 0))
	}
	s.terminal.mu.Unlock()
	s.terminal.stop()
	s.terminal = nil
}
