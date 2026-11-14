// Command webui hosts the original TUI in a browser terminal.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
	"pi-goui/internal/core/ui"
	"pi-goui/internal/core/web"
	rpc "pi-goui/internal/runtime"
)

var version = "dev"

func main() {
	if handled, err := rpc.RunSharedSessionHelper(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "webui session:", err)
			os.Exit(1)
		}
		return
	}
	// The PTY re-exec uses this same binary; Bubble Tea owns its signal handlers
	// and deferred Close reclaims all RPC sessions on SIGTERM or /quit.
	if len(os.Args) > 1 && os.Args[1] == "--internal-tui" {
		if err := runTerminal(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "webui terminal:", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "webui:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	o, err := parseOptions(args, errOut)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Version {
		fmt.Fprintln(out, "webui", version)
		return nil
	}
	bin := o.RPC.Bin
	if bin == "" {
		bin = os.Getenv("PI_BIN")
	}
	if bin == "" {
		bin = "pi"
	}
	// Resolve once: a failed explicit executable must never fall back to PATH pi.
	o.RPC.Bin, err = exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("Pi executable %q: %w", bin, err)
	}
	o.RPC.Bin, err = filepath.Abs(o.RPC.Bin)
	if err != nil {
		return err
	}
	if o.Check {
		return checkRPC(ctx, o, out)
	}
	var tlsConfig *tls.Config
	if o.TLSCert != "" {
		certificate, e := tls.LoadX509KeyPair(o.TLSCert, o.TLSKey)
		if e != nil {
			return fmt.Errorf("TLS: %w", e)
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	listener, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", o.Listen, err)
	}
	defer listener.Close()
	terminal, err := terminalConfig(o)
	if err != nil {
		return err
	}
	app, err := web.New(terminal)
	if err != nil {
		return err
	}
	defer app.Close()
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20, TLSConfig: tlsConfig}
	scheme := "http"
	if tlsConfig != nil {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	ip := net.ParseIP(host)
	lan := ip == nil || !ip.IsLoopback()
	displayHost := host
	if ip != nil && ip.IsUnspecified() {
		displayHost = "localhost"
	}
	fmt.Fprintf(out, "Pi WebUI: %s://%s\nWorking directory: %s\nLogin token: %s\n", scheme, net.JoinHostPort(displayHost, port), o.RPC.Cwd, app.Token())
	if lan {
		fmt.Fprintf(out, "LAN listener: %s (open your server's LAN IP in the browser).\n", listener.Addr())
		fmt.Fprintln(errOut, "WARNING: authorized browsers can execute tools with this user's permissions; WebUI is not a sandbox.")
		if tlsConfig == nil {
			fmt.Fprintln(errOut, "WARNING: HTTP exposes the token and session data to the network. Use HTTPS or a trusted tunnel on untrusted networks.")
		}
	}
	web.PrintLoginQRCode(out, errOut, listener.Addr().(*net.TCPAddr), scheme, app.Token())
	done := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			done <- server.ServeTLS(listener, "", "")
		} else {
			done <- server.Serve(listener)
		}
	}()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Stop accepting first, then close the PTY and hijacked WebSockets.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone := make(chan error, 1)
		go func() { shutdownDone <- server.Shutdown(shutdownCtx) }()
		closeErr := app.Close()
		shutdownErr := <-shutdownDone
		if shutdownErr != nil {
			_ = server.Close()
		}
		<-done
		return errors.Join(closeErr, shutdownErr)
	}
}

// terminalConfig re-execs this binary with only the TUI's RPC options. No web
// token, listener, TLS key, or global settings change is handed to the child.
func terminalConfig(o options) (web.Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return web.Config{}, err
	}
	if o.RPC.AgentDir == "" {
		o.RPC.AgentDir, err = filepath.Abs(config.AgentDir())
		if err != nil {
			return web.Config{}, err
		}
	}
	makeCommand := func(cwd string, resume bool) []string {
		args := []string{exe, "--internal-tui", "--pi", o.RPC.Bin, "--cwd", cwd, "--agent-dir", o.RPC.AgentDir}
		// An explicitly supplied session belongs to the original project only.
		if resume && o.RPC.SessionFile != "" && cwd == o.RPC.Cwd {
			args = append(args, "--session", o.RPC.SessionFile)
		}
		if o.RPC.NoSession {
			args = append(args, "--no-session")
		}
		if len(o.RPC.Args) != 0 {
			args = append(args, "--")
			args = append(args, o.RPC.Args...)
		}
		return args
	}
	command := func(cwd string) []string { return makeCommand(cwd, true) }
	independent := func(cwd string) []string { return makeCommand(cwd, false) }
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PI_WEBUI_TOKEN", "TERM", "COLORTERM":
			continue
		case "PI_CODING_AGENT_DIR":
			if o.RPC.AgentDir != "" {
				continue
			}
		}
		env = append(env, entry)
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	if o.RPC.AgentDir != "" {
		env = append(env, "PI_CODING_AGENT_DIR="+o.RPC.AgentDir)
	}
	return web.Config{Command: command(o.RPC.Cwd), CommandForDirectory: command, NewInstanceCommandForDirectory: independent, OfferDefaultInstance: o.RPC.SessionFile != "", DiscoverDesktops: true, Cwd: o.RPC.Cwd, AgentDir: o.RPC.AgentDir, Env: env, Token: o.Token}, nil
}

// runTerminal uses exactly the same model and renderer as cmd/pi-goui. Keeping
// it here avoids requiring a second executable to be installed.
func runTerminal(args []string) error {
	o, err := parseOptions(args, io.Discard)
	if err != nil {
		return err
	}
	if o.RPC.AgentDir != "" {
		if err := os.Setenv("PI_CODING_AGENT_DIR", o.RPC.AgentDir); err != nil {
			return err
		}
	}
	cfg, err := config.Load(o.RPC.Cwd)
	if err != nil {
		return err
	}
	// Match cmd/pi-goui's forward/reverse model cycle scope.
	for i, arg := range o.RPC.Args {
		if arg == "--models" && i+1 < len(o.RPC.Args) {
			var patterns []any
			for _, pattern := range strings.Split(o.RPC.Args[i+1], ",") {
				patterns = append(patterns, strings.TrimSpace(pattern))
			}
			cfg.Settings["enabledModels"] = patterns
		}
	}
	rpc.EnableSessionSharing()
	member, err := rpc.JoinSessionMember(cfg.AgentDir)
	if err != nil {
		return err
	}
	defer member.Close()
	m := ui.New(ui.Options{Bin: o.RPC.Bin, Cwd: o.RPC.Cwd, SessionFile: o.RPC.SessionFile, NoSession: o.RPC.NoSession, PiArgs: o.RPC.Args}, cfg)
	defer m.Close()
	defer fmt.Fprint(os.Stdout, ansi.SetPointerShape("default"))
	_, err = tea.NewProgram(m).Run()
	return err
}

func checkRPC(ctx context.Context, o options, out io.Writer) error {
	cfg := o.RPC
	cfg.NoSession = true
	cfg.SessionFile = ""
	c, err := rpc.Start(cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	// Drain unsolicited events; diagnostics are delivered by Call on process exit.
	go func() {
		for range c.Events() {
		}
	}()
	var state session.State
	for _, command := range []string{"get_state", "get_entries"} {
		callCtx, cancel := context.WithTimeout(ctx, o.RequestTimeout)
		raw, e := c.Call(callCtx, map[string]any{"type": command})
		cancel()
		if e != nil {
			return fmt.Errorf("RPC check %s: %w", command, e)
		}
		// Call unwraps response.data and turns success:false into an error.
		if command == "get_state" {
			e = state.LoadState(raw)
		} else {
			e = state.LoadEntries(raw)
		}
		if e != nil {
			return fmt.Errorf("RPC check %s: %w", command, e)
		}
	}
	fmt.Fprintln(out, "RPC check OK (get_state, get_entries; no session persisted)")
	return nil
}
