package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
	"pi-goui/internal/core/ui"
	rpc "pi-goui/internal/runtime"
	"pi-goui/internal/runtime/desktop"
)

func run() error {
	if handled, err := rpc.RunSharedSessionHelper(os.Args[1:]); handled {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	internal := len(args) > 0 && args[0] == "--internal-shared-tui"
	if internal {
		args = args[1:]
	}
	fs := flag.NewFlagSet("pi-goui", flag.ContinueOnError)
	var opts ui.Options
	var check, cont bool
	var sharing bool
	var obsoleteListen, obsoleteToken string
	fs.BoolVar(&sharing, "share", !internal && desktop.DesktopSharingSupported(), "allow a separately started WebUI to discover this desktop locally")
	fs.StringVar(&obsoleteListen, "listen", "", "unsupported here; start webui --listen separately")
	fs.StringVar(&obsoleteToken, "token", "", "unsupported here; configure the separate webui service")
	fs.StringVar(&opts.Cwd, "cwd", cwd, "working directory for all pi sessions")
	fs.StringVar(&opts.Bin, "pi", "", "pi executable (otherwise PI_BIN or PATH)")
	fs.StringVar(&opts.SessionFile, "session", "", "native session JSONL file to open")
	fs.BoolVar(&opts.NoSession, "no-session", false, "do not persist sessions")
	fs.BoolVar(&cont, "continue", false, "resume latest session in this working directory")
	fs.BoolVar(&cont, "c", false, "resume latest session")
	fs.BoolVar(&check, "check", false, "check real RPC without sending a prompt or saving a session")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: pi-goui [flags] [-- additional pi flags]\nIndependent pi frontend · one RPC process per open session")
		fs.PrintDefaults()
	}
	if err = fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if internal && sharing {
		return fmt.Errorf("shared TUI child cannot start another host")
	}
	var sharedFlag string
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "listen" || f.Name == "token" {
			sharedFlag = f.Name
		}
	})
	if sharedFlag != "" {
		return fmt.Errorf("--%s belongs to the separate webui service; start webui independently", sharedFlag)
	}
	opts.PiArgs, err = piArgs(fs.Args())
	if err != nil {
		return err
	}
	opts.Cwd, err = filepath.Abs(opts.Cwd)
	if err != nil {
		return err
	}
	st, err := os.Stat(opts.Cwd)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory: %s", opts.Cwd)
	}
	cfg, err := config.Load(opts.Cwd)
	if err != nil {
		return err
	}
	// Use the same explicit cycle scope in the frontend's reverse-cycle action.
	for i, arg := range opts.PiArgs {
		if arg == "--models" && i+1 < len(opts.PiArgs) {
			var patterns []any
			for _, pattern := range strings.Split(opts.PiArgs[i+1], ",") {
				patterns = append(patterns, strings.TrimSpace(pattern))
			}
			cfg.Settings["enabledModels"] = patterns
		}
	}
	if opts.NoSession && (cont || opts.SessionFile != "") {
		return fmt.Errorf("--no-session cannot be combined with --continue or --session")
	}
	if cont && opts.SessionFile == "" {
		list, e := session.List(cfg.AgentDir, opts.Cwd)
		if e != nil {
			return e
		}
		if len(list) > 0 {
			opts.SessionFile = list[0].Path
		}
	}
	if opts.SessionFile != "" {
		opts.SessionFile, err = filepath.Abs(opts.SessionFile)
		if err != nil {
			return err
		}
	}
	if check {
		c, err := rpc.Start(rpc.Config{Bin: opts.Bin, Cwd: opts.Cwd, AgentDir: cfg.AgentDir, NoSession: true, Args: opts.PiArgs})
		if err != nil {
			return err
		}
		defer c.Close()
		// Always consume unsolicited events, even in check mode.
		go func() {
			for range c.Events() {
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data, err := c.Call(ctx, map[string]any{"type": "get_state"})
		if err != nil {
			return err
		}
		var s session.State
		if err = s.LoadState(data); err != nil {
			return err
		}
		entries, err := c.Call(ctx, map[string]any{"type": "get_entries"})
		if err != nil {
			return err
		}
		if err = s.LoadEntries(entries); err != nil {
			return err
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		fmt.Printf("RPC OK · model=%s/%s · thinking=%s · streaming=%v · full-history=yes\n", s.Model.Provider, s.Model.ID, s.Thinking, raw["isStreaming"])
		return nil
	}
	rpc.EnableSessionSharing()
	if sharing {
		return runShared(opts, cfg.AgentDir)
	}
	member, err := rpc.JoinSessionMember(cfg.AgentDir)
	if err != nil {
		return err
	}
	defer member.Close()
	model := ui.New(opts, cfg)
	defer model.Close()
	defer fmt.Fprint(os.Stdout, ansi.SetPointerShape("default"))
	_, err = tea.NewProgram(model).Run()
	return err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pi-goui:", err)
		os.Exit(1)
	}
}
