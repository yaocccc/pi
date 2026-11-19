package hostui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"pi-goui/internal/core/extension"
	"pi-goui/internal/extensions/codexfast"
	rpc "pi-goui/internal/runtime"
)

// The startup default is a public read-only contract. This object belongs to
// one adapter (one TUI), never a session or a package-global singleton.
type instanceMode struct {
	level      string
	revision   uint64
	startupErr error
}

func newInstanceMode(agentDir string) *instanceMode {
	m := &instanceMode{revision: 1}
	m.level, m.startupErr = readStartupLevel(agentDir)
	return m
}
func readStartupLevel(agentDir string) (string, error) {
	if agentDir == "" {
		return "", errors.New("missing agent directory")
	}
	path := filepath.Join(agentDir, "codex-fast.json")
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "normal", nil
	}
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > 4096 {
		return "", errors.New("invalid Codex startup configuration")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return "", err
	}
	var cfg struct {
		Level           json.RawMessage
		Fast, Ultrafast *bool
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return "", err
	}
	if cfg.Level != nil {
		var level string
		if json.Unmarshal(cfg.Level, &level) == nil && slices.Contains(CodexFastLevels, level) {
			return level, nil
		}
	} else if cfg.Fast != nil || cfg.Ultrafast != nil {
		if cfg.Ultrafast != nil && *cfg.Ultrafast {
			return "ultrafast", nil
		}
		if cfg.Fast != nil && *cfg.Fast {
			return "fast", nil
		}
		return "normal", nil
	}
	return "", errors.New("unknown Codex startup configuration")
}

func (c *CodexState) saved(level string) {
	if c.instance == nil || !slices.Contains(CodexFastLevels, level) {
		return
	}
	if c.instance.level != level {
		c.instance.level = level
		c.instance.startupErr = nil
		c.instance.revision++
	}
	// Do not certify a writer here: an older apply may still be in flight.
}

// Capture immutable inputs on the UI loop. A successful apply changes only
// this writer's memory, even when another TUI has since saved a new default.
func (s *Session) Prepare(client extension.Client) extension.Preparation {
	c := &s.Codex
	m := c.instance
	if m == nil || client == nil || (client == c.prepared && (c.unavailable || c.applied == m.revision)) {
		return nil
	}
	level, revision, startupErr := m.level, m.revision, m.startupErr
	return func(ctx context.Context) (func(), error) {
		_, err := codexfast.Get(ctx, client)
		absent := errors.Is(err, rpc.ErrExtensionUnavailable)
		var state codexfast.State
		if !absent {
			if err != nil {
				return nil, fmt.Errorf("read Codex mode: %w", err)
			}
			if startupErr != nil {
				return nil, fmt.Errorf("read Codex startup default: %w", startupErr)
			}
			state, err = codexfast.Apply(ctx, client, level)
			if err != nil {
				return nil, fmt.Errorf("restore instance Codex mode %q (update codex-fast and pi-ui): %w", level, err)
			}
		}
		return func() {
			c.prepared, c.applied, c.unavailable = client, revision, absent
			c.State = state
			c.client = client
			c.Version++
		}, nil
	}
}

func (c *CodexState) displayState() codexfast.State {
	if c.instance == nil {
		return c.State
	}
	if c.unavailable {
		return codexfast.State{}
	}
	if c.instance.level == "" {
		return codexfast.State{}
	}
	return codexfast.State{Level: c.instance.level, Levels: CodexFastLevels}
}
