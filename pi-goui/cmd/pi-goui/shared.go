package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pi-goui/internal/core/ui"
	"pi-goui/internal/core/web"
	"pi-goui/internal/runtime/desktop"
)

// The foreground desktop process owns the host. All browser and desktop
// connections share its original TUI and Pi RPCs without taking each other over.
func runShared(opts ui.Options, agentDir string) (retErr error) {
	if err := web.CheckAttachTerminal(os.Stdin, os.Stdout); err != nil {
		return err
	}
	cfg, err := sharedConfig(opts, agentDir, "")
	if err != nil {
		return err
	}
	app, err := web.New(cfg)
	if err != nil {
		return err
	}
	defer app.Close()
	endpoint, err := desktop.OpenDesktop(cfg.AgentDir, app.Token())
	if err != nil {
		return err
	}
	defer endpoint.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	served := make(chan error, 1)
	go func() { served <- server.Serve(endpoint.Listener); stop() }()
	defer func() {
		// Close also terminates connections stuck sending a request body.
		_ = server.Close()
		if serveErr := <-served; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			retErr = errors.Join(retErr, serveErr)
		}
	}()
	const base = "http://desktop.local"
	client := &sharedClient{base: base, token: app.Token(), http: &http.Client{Timeout: 15 * time.Second, Transport: endpoint.HTTPClient.Transport}}
	defer client.http.CloseIdleConnections()
	for {
		project, err := client.current(ctx, cfg.Cwd)
		if err == nil {
			result, attachErr := web.Attach(ctx, web.AttachOptions{URL: base, Token: app.Token(), ProjectID: project.ProjectID, Input: os.Stdin, Output: os.Stdout, HTTPClient: endpoint.HTTPClient})
			err = attachErr
			if attachErr == nil && result.Type == "exit" {
				if result.Code != 0 {
					return fmt.Errorf("shared TUI exited (%d): %s", result.Code, result.Message)
				}
				return nil
			}
			if attachErr == nil {
				fmt.Fprintf(os.Stdout, "\nDesktop detached (%s). The shared host is still running.\n", result.Type)
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Shared connection: %v\n", err)
		}
		fmt.Fprintln(os.Stdout, "Press Enter to attach to the current project, or Ctrl+C to stop the shared host.")
		retake, err := web.WaitForRetake(ctx, os.Stdin)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if !retake {
			return nil
		}
	}
}

func sharedConfig(opts ui.Options, agentDir, token string) (web.Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return web.Config{}, err
	}
	agentDir, err = filepath.Abs(agentDir)
	if err != nil {
		return web.Config{}, err
	}
	bin := opts.Bin
	if bin == "" {
		bin = os.Getenv("PI_BIN")
	}
	if bin == "" {
		bin = "pi"
	}
	bin, err = exec.LookPath(bin)
	if err != nil {
		return web.Config{}, fmt.Errorf("pi executable: %w", err)
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return web.Config{}, err
	}
	// Freeze mutable option slices; every project gets a fresh argument array.
	piArgs := append([]string(nil), opts.PiArgs...)
	makeCommand := func(cwd string, resume bool) []string {
		args := []string{exe, "--internal-shared-tui", "--pi", bin, "--cwd", cwd}
		if resume && opts.SessionFile != "" && cwd == opts.Cwd {
			args = append(args, "--session", opts.SessionFile)
		}
		if opts.NoSession {
			args = append(args, "--no-session")
		}
		if len(piArgs) > 0 {
			args = append(args, "--")
			args = append(args, piArgs...)
		}
		return args
	}
	command := func(cwd string) []string { return makeCommand(cwd, true) }
	independent := func(cwd string) []string { return makeCommand(cwd, false) }
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PI_WEBUI_TOKEN", "PI_CODING_AGENT_DIR", "TERM", "COLORTERM":
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PI_CODING_AGENT_DIR="+agentDir, "TERM=xterm-256color", "COLORTERM=truecolor")
	return web.Config{Command: command(opts.Cwd), CommandForDirectory: command, SharedDesktop: true, NewInstanceCommandForDirectory: independent, Cwd: opts.Cwd, AgentDir: agentDir, Token: token, Env: env}, nil
}

type sharedProject struct {
	Cwd       string `json:"cwd"`
	ProjectID string `json:"projectId"`
}
type sharedClient struct {
	base, token string
	http        *http.Client
}

func (c *sharedClient) request(ctx context.Context, method, path string, body, result any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Origin", c.base)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return errors.New(failure.Error)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(result)
}

// Never select the old desktop directory over a browser's current project. A
// concurrent selection is rejected by the server rather than silently replaced.
func (c *sharedClient) current(ctx context.Context, fallback string) (sharedProject, error) {
	var project sharedProject
	if err := c.request(ctx, http.MethodGet, "/api/info", nil, &project); err != nil {
		return project, err
	}
	if project.ProjectID == "" {
		err := c.request(ctx, http.MethodPost, "/api/projects/select", sharedProject{Cwd: fallback}, &project)
		return project, err
	}
	return project, nil
}
