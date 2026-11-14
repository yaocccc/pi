package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-goui/internal/core/ui"
)

func TestSharedConfigIsolationAndProjects(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_WEBUI_TOKEN", "web-secret-not-for-child")
	t.Setenv("PI_CODING_AGENT_DIR", "old-agent")
	opts := ui.Options{Bin: exe, Cwd: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "saved.jsonl"), PiArgs: []string{"--models", "test/*"}}
	agent := t.TempDir()
	cfg, err := sharedConfig(opts, agent, "web-secret-not-for-child")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentDir != agent || cfg.Command[1] != "--internal-shared-tui" || cfg.Command[0] != exe {
		t.Fatalf("%+v", cfg)
	}
	env := strings.Join(cfg.Env, "\n")
	if strings.Contains(env, "web-secret") || strings.Contains(env, "old-agent") || !strings.Contains(env, "PI_CODING_AGENT_DIR="+agent) {
		t.Fatal("child environment leaked credentials/root")
	}
	first := strings.Join(cfg.Command, "\n")
	if !strings.Contains(first, "--session\n"+opts.SessionFile) {
		t.Fatal("initial explicit session lost")
	}
	if !cfg.SharedDesktop || cfg.NewInstanceCommandForDirectory == nil {
		t.Fatal("shared host lacks independent instance configuration")
	}
	if args := strings.Join(cfg.NewInstanceCommandForDirectory(opts.Cwd), "\n"); strings.Contains(args, "--session") {
		t.Fatal("independent instance would reopen desktop session")
	}
	other := t.TempDir()
	next := strings.Join(cfg.CommandForDirectory(other), "\n")
	if strings.Contains(next, "--session") || strings.Contains(next, opts.Cwd) || !strings.Contains(next, "--cwd\n"+other) {
		t.Fatal("project args not scoped")
	}
	opts.PiArgs[1] = "changed"
	if !strings.Contains(strings.Join(cfg.CommandForDirectory(other), "\n"), "test/*") {
		t.Fatal("mutable args captured")
	}
	for _, arg := range cfg.Command {
		if arg == "--share" || arg == "--token" || arg == "--listen" || strings.Contains(arg, "web-secret") {
			t.Fatal("host flag/credential leaked")
		}
	}
}
func TestSharedClientFollowsCurrentProject(t *testing.T) {
	var selected []sharedProject
	active := sharedProject{Cwd: "/browser-project", ProjectID: "browser-id"}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Origin") == "" {
			t.Error("missing authentication/origin")
		}
		if r.Method == http.MethodPost {
			var request sharedProject
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			selected = append(selected, request)
			active = sharedProject{Cwd: request.Cwd, ProjectID: "new"}
		}
		_ = json.NewEncoder(w).Encode(active)
	}))
	defer h.Close()
	client := sharedClient{base: h.URL, token: "test", http: h.Client()}
	got, err := client.current(context.Background(), "/original-desktop")
	if err != nil || got != active || len(selected) != 0 {
		t.Fatalf("current project overwritten: %+v %v", got, err)
	}
	active = sharedProject{}
	got, err = client.current(context.Background(), "/original-desktop")
	if err != nil || got.ProjectID != "new" || len(selected) != 1 || selected[0].Cwd != "/original-desktop" {
		t.Fatalf("selection: %+v %v", got, err)
	}
}
