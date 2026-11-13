// Package codexfast adapts the optional third-party Codex Fast capability.
// It has no knowledge of an agent directory, configuration file, or Pi internals.
package codexfast

import (
	"context"
	"encoding/json"
	"fmt"
)

type Caller interface {
	CallExtension(context.Context, string, any) (json.RawMessage, error)
}
type State struct {
	Level  string   `json:"level"`
	Levels []string `json:"levels"`
}

func (s State) Available() bool {
	if s.Level == "" || len(s.Levels) == 0 || len(s.Levels) > 32 {
		return false
	}
	for _, level := range s.Levels {
		if level == s.Level {
			return true
		}
	}
	return false
}
func request(ctx context.Context, c Caller, method string, params any) (State, error) {
	raw, err := c.CallExtension(ctx, method, params)
	if err != nil {
		return State{}, err
	}
	var state State
	if err = json.Unmarshal(raw, &state); err != nil {
		return State{}, err
	}
	if !state.Available() {
		return State{}, fmt.Errorf("invalid Codex Fast state")
	}
	return state, nil
}
func Get(ctx context.Context, c Caller) (State, error) { return request(ctx, c, "codex-fast.get", nil) }
func Set(ctx context.Context, c Caller, level string) (State, error) {
	return request(ctx, c, "codex-fast.set", map[string]string{"level": level})
}
