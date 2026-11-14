//go:build unix

package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectSwitchClosesOldTerminalAndRejectsStaleClients(t *testing.T) {
	s, h, log := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	old, oldID := s.terminal, s.projectID
	next := t.TempDir()
	w := request(t, s, "POST", "/api/projects/select", projectRequest{Cwd: next, ProjectID: oldID})
	if w.Code != 200 || s.cwd != next || s.projectID == oldID || s.terminal != nil {
		t.Fatalf("switch: %d %s", w.Code, w.Body)
	}
	readUntil(t, c, `"type":"project_changed"`)
	select {
	case <-old.done:
	default:
		t.Fatal("old process still running after switch returned")
	}
	for _, path := range []string{"/api/terminal/close", "/api/terminal/restart", "/api/projects/select"} {
		if w := request(t, s, "POST", path, projectRequest{Cwd: s.cfg.Cwd, ProjectID: oldID}); w.Code != 409 {
			t.Fatalf("stale %s: %d", path, w.Code)
		}
	}
	if w := request(t, s, "GET", "/api/terminal?cols=80&rows=24&projectId="+oldID, nil); w.Code != 409 || s.terminal != nil {
		t.Fatal("stale reconnect must not start or take over the new project")
	}
	c = dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	if s.terminal.cmd.Dir != next {
		t.Fatalf("new TUI cwd = %s", s.terminal.cmd.Dir)
	}
	current, id := s.terminal, s.projectID
	if w := request(t, s, "POST", "/api/terminal/close", projectRequest{ProjectID: id}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	readUntil(t, c, `"type":"project_closed"`)
	select {
	case <-current.done:
	default:
		t.Fatal("close returned before process exit")
	}
	if s.projectID != "" || s.cwd != "" || s.terminal != nil {
		t.Fatal("close did not clear project")
	}
	if w := request(t, s, "GET", "/api/terminal?cols=80&rows=24&projectId="+id, nil); w.Code != 409 {
		t.Fatal("closed project reconnected")
	}
	if strings.Count(logContents(t, log), "START:") != 2 {
		t.Fatal("close/rejected reconnect spawned another TUI")
	}
	w = request(t, s, "GET", "/api/projects", nil)
	var list struct{ Directories []string }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Directories) == 0 || list.Directories[0] != next {
		t.Fatalf("recent selection lost: %s (%v)", w.Body, err)
	}
}
