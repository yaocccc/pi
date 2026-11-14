package web

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectPickerBeforeTerminal(t *testing.T) {
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.MkdirAll(filepath.Join(agent, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	var newest string
	for i := 0; i < 12; i++ {
		cwd := filepath.Join(root, fmt.Sprintf("project-%02d", i))
		if err := os.Mkdir(cwd, 0700); err != nil {
			t.Fatal(err)
		}
		header, _ := json.Marshal(map[string]string{"type": "session", "cwd": cwd})
		file := filepath.Join(agent, "sessions", fmt.Sprintf("%02d.jsonl", i))
		if err := os.WriteFile(file, append(header, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+100), 0)
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		newest = cwd
	}
	s, err := New(Config{Command: []string{"not-started"}, Cwd: root, AgentDir: agent})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := request(t, s, "GET", "/api/projects", nil)
	var list struct {
		Directories []string
		ProjectID   string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(list.Directories) != 10 || list.Directories[0] != newest || list.ProjectID != "" || s.terminal != nil {
		t.Fatalf("picker: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "GET", "/api/terminal?cols=80&rows=24", nil); w.Code != 409 || s.terminal != nil {
		t.Fatalf("terminal started before directory selection: %d", w.Code)
	}
	selected := request(t, s, "POST", "/api/projects/select", projectRequest{Cwd: newest})
	if selected.Code != 200 || s.projectID == "" || s.cwd != newest || s.terminal != nil {
		t.Fatalf("selection must be lazy: %d %s", selected.Code, selected.Body)
	}
	id := s.projectID
	for _, cwd := range []string{"", "relative", filepath.Join(root, "missing"), filepath.Join(agent, "sessions", "00.jsonl")} {
		if w := request(t, s, "POST", "/api/projects/select", projectRequest{Cwd: cwd, ProjectID: id}); w.Code != 400 {
			t.Fatalf("invalid directory %q: %d", cwd, w.Code)
		}
		if s.projectID != id || s.cwd != newest {
			t.Fatal("invalid selection changed active project")
		}
	}
	if w := request(t, s, "POST", "/api/projects/select", projectRequest{Cwd: newest}); w.Code != 200 || s.projectID != id {
		t.Fatal("continuing same project must reuse its identity")
	}
}
