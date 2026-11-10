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
	"pi-goui/internal/config"
	"pi-goui/internal/rpc"
	"pi-goui/internal/session"
	"pi-goui/internal/ui"
)

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("pi-goui", flag.ContinueOnError)
	var opts ui.Options
	var check, cont bool
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
	if err = fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
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
	model := ui.New(opts, cfg)
	defer model.Close()
	_, err = tea.NewProgram(model).Run()
	return err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pi-goui:", err)
		os.Exit(1)
	}
}
