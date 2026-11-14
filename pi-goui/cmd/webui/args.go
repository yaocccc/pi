package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pi-goui/internal/core/config"
	rpc "pi-goui/internal/runtime"
)

type options struct {
	RPC                     rpc.Config
	Listen, TLSCert, TLSKey string
	Token                   string
	RequestTimeout          time.Duration
	Check, Version          bool
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var o options
	cwd, err := os.Getwd()
	if err != nil {
		return o, err
	}
	fs := flag.NewFlagSet("webui", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&o.Listen, "listen", "127.0.0.1:8888", "HTTP address (use :8888 explicitly for LAN access)")
	fs.StringVar(&o.Token, "token", os.Getenv("PI_WEBUI_TOKEN"), "login token (otherwise PI_WEBUI_TOKEN or a random token)")
	fs.StringVar(&o.Token, "key", os.Getenv("PI_WEBUI_TOKEN"), "login key (alias for --token)")
	fs.StringVar(&o.RPC.Bin, "pi", "", "Pi executable (otherwise PI_BIN or PATH)")
	fs.StringVar(&o.RPC.Cwd, "cwd", cwd, "default project directory offered by the browser picker")
	fs.StringVar(&o.RPC.AgentDir, "agent-dir", config.AgentDir(), "Pi agent directory (otherwise PI_CODING_AGENT_DIR or ~/.pi/agent)")
	fs.StringVar(&o.RPC.SessionFile, "session", "", "native session JSONL to open initially")
	fs.BoolVar(&o.RPC.NoSession, "no-session", false, "do not persist sessions")
	fs.DurationVar(&o.RequestTimeout, "request-timeout", 30*time.Second, "read-only --check RPC request timeout")
	fs.StringVar(&o.TLSCert, "tls-cert", "", "HTTPS certificate PEM (requires --tls-key)")
	fs.StringVar(&o.TLSKey, "tls-key", "", "HTTPS private key PEM (requires --tls-cert)")
	fs.BoolVar(&o.Check, "check", false, "check read-only RPC with persistence disabled, then exit")
	fs.BoolVar(&o.Version, "version", false, "print build version and exit")
	fs.Usage = func() {
		fmt.Fprintln(output, "Usage: webui [flags] [-- additional Pi startup flags]\nBrowser terminal · the original TUI in a shared PTY")
		fs.PrintDefaults()
	}
	// Require the separator, rather than silently treating positionals as Pi options.
	own, extra := args, []string(nil)
	for i, a := range args {
		if a == "--" {
			own, extra = args[:i], args[i+1:]
			break
		}
	}
	if err = fs.Parse(own); err != nil {
		return o, err
	}
	if len(fs.Args()) != 0 {
		return o, fmt.Errorf("unexpected positional arguments; use -- before Pi startup options")
	}
	if o.Version {
		return o, nil
	}
	if o.RPC.Args, err = piArgs(extra); err != nil {
		return o, err
	}
	if o.RPC.NoSession && o.RPC.SessionFile != "" {
		return o, fmt.Errorf("--no-session cannot be combined with --session")
	}
	if (o.TLSCert == "") != (o.TLSKey == "") {
		return o, fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	if o.RequestTimeout <= 0 {
		return o, fmt.Errorf("--request-timeout must be positive")
	}
	if _, _, err = net.SplitHostPort(o.Listen); err != nil {
		return o, fmt.Errorf("--listen: %w", err)
	}
	for _, p := range []*string{&o.RPC.Cwd, &o.RPC.AgentDir, &o.RPC.SessionFile} {
		if *p != "" {
			*p, err = filepath.Abs(*p)
			if err != nil {
				return o, err
			}
		}
	}
	st, err := os.Stat(o.RPC.Cwd)
	if err != nil {
		return o, err
	}
	if !st.IsDir() {
		return o, fmt.Errorf("not a directory: %s", o.RPC.Cwd)
	}
	return o, nil
}

// Keep the same startup-only allowlist as cmd/pi-goui. Never pass through
// mode, session selection, print/export, or positional prompt arguments.
func piArgs(args []string) ([]string, error) {
	values := map[string]bool{"--provider": true, "--model": true, "--api-key": true, "--system-prompt": true, "--append-system-prompt": true, "--name": true, "-n": true, "--models": true, "--tools": true, "-t": true, "--exclude-tools": true, "-xt": true, "--thinking": true, "--extension": true, "-e": true, "--skill": true, "--prompt-template": true, "--theme": true, "--use-theme": true}
	switches := map[string]bool{"--no-tools": true, "-nt": true, "--no-builtin-tools": true, "-nbt": true, "--no-extensions": true, "-ne": true, "--no-mcp": true, "--no-skills": true, "-ns": true, "--no-prompt-templates": true, "-np": true, "--no-themes": true, "--no-context-files": true, "-nc": true, "--verbose": true, "--approve": true, "-a": true, "--no-approve": true, "-na": true, "--offline": true}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, value, eq := strings.Cut(args[i], "=")
		if switches[name] && !eq {
			out = append(out, name)
			continue
		}
		if !values[name] {
			return nil, fmt.Errorf("unsupported Pi startup option %q: mode/session flags and positional prompts must not be passed through", name)
		}
		if !eq {
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[i]
		}
		if value == "" {
			return nil, fmt.Errorf("%s requires a nonempty value", name)
		}
		out = append(out, name, value)
	}
	return out, nil
}
