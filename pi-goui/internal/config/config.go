// Package config reads native pi configuration without modifying it.
package config

import (
	tea "charm.land/bubbletea/v2"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Defaults captured from pi 1.5.0's KeybindingsManager, including pi-tui actions.
//
//go:embed defaults.json
var defaultJSON []byte

//go:embed aliases.json
var aliasJSON []byte

//go:embed pi.json
var themeJSON []byte

func AgentDir() string {
	if p := os.Getenv("PI_CODING_AGENT_DIR"); p != "" {
		// Match native config.js -> normalizePath: do not expand ~user or trim
		// whitespace; backslash is a home separator only on Windows.
		if p == "~" || strings.HasPrefix(p, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(p, `~\`) {
			h, _ := os.UserHomeDir()
			if p == "~" {
				return h
			}
			return filepath.Join(h, p[2:])
		}
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".pi", "agent")
}
func CanonicalKey(s string) string {
	s = strings.ToLower(s)
	mods := map[string]bool{}
	for {
		found := false
		for _, m := range []string{"ctrl", "alt", "shift", "super", "meta", "hyper"} {
			if strings.HasPrefix(s, m+"+") {
				mods[m] = true
				s = strings.TrimPrefix(s, m+"+")
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	switch s {
	case "esc":
		s = "escape"
	case "pgup":
		s = "pageup"
	case "pgdown", "pgdn":
		s = "pagedown"
	case " ":
		s = "space"
	case "return":
		s = "enter"
	}
	var prefix string
	for _, m := range []string{"ctrl", "alt", "shift", "super", "meta", "hyper"} {
		if mods[m] {
			prefix += m + "+"
		}
	}
	return prefix + s
}
func KeyChord(k tea.KeyPressMsg) string {
	if k.Code == tea.KeyExtended {
		return ""
	}
	if k.Code >= 'A' && k.Code <= 'Z' {
		k.Code += 'a' - 'A'
		k.Mod |= tea.ModShift
	}
	return CanonicalKey(k.Keystroke())
}
func (b Bindings) Match(action, chord string) bool {
	if chord == "" {
		return false
	}
	chord = CanonicalKey(chord)
	for _, k := range b.bindings[action] {
		if k == chord {
			return true
		}
	}
	return false
}
func (b Bindings) Keys(action string) []string { return append([]string(nil), b.bindings[action]...) }
func (b Bindings) Actions() []string {
	a := make([]string, 0, len(b.bindings))
	for k := range b.bindings {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func bindingValue(raw json.RawMessage) ([]string, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, fmt.Errorf("binding must be a string or string array")
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{CanonicalKey(one)}, nil
	}
	var many []string
	if string(raw) == "null" || json.Unmarshal(raw, &many) != nil {
		return nil, fmt.Errorf("binding must be a string or string array")
	}
	for i := range many {
		many[i] = CanonicalKey(many[i])
	}
	return many, nil
}
func DefaultBindings() Bindings {
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(defaultJSON, &raw)
	b := Bindings{bindings: map[string][]string{}}
	for k, v := range raw {
		b.bindings[k], _ = bindingValue(v)
	}
	windows := runtime.GOOS == "windows" || runtime.GOOS == "linux" && (os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "")
	if windows {
		for k, v := range map[string][]string{"app.model.cycleBackward": {"alt+p"}, "app.message.followUp": {"ctrl+q"}, "app.message.dequeue": {"alt+q"}, "app.clipboard.pasteImage": {"alt+v"}, "tui.editor.undo": {"alt+z"}, "tui.altScreen.previousPrompt": {"ctrl+up"}, "tui.altScreen.nextPrompt": {"ctrl+down"}, "tui.altScreen.search": {"ctrl+f"}} {
			b.bindings[k] = v
		}
	}
	if runtime.GOOS == "windows" {
		b.bindings["app.suspend"] = nil
		b.bindings["tui.editor.undo"] = []string{"ctrl+z"}
	}
	return b
}
func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(strings.TrimPrefix(string(data), "\ufeff")), out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
func merge(dst, src map[string]any) { mergeLevel(dst, src, true) }
func mergeLevel(dst, src map[string]any, settingsRoot bool) {
	for k, v := range src {
		if settingsRoot && k == "defaultTools" {
			if old, ok := dst[k].([]any); ok {
				if next, ok := v.([]any); ok {
					modifiers := true
					for _, entry := range next {
						s, ok := entry.(string)
						if !ok || len(s) < 1 || (s[0] != '+' && s[0] != '-') {
							modifiers = false
							break
						}
					}
					if modifiers {
						dst[k] = append(append([]any(nil), old...), next...)
						continue
					}
				}
			}
		}
		if a, ok := v.(map[string]any); ok {
			if b, ok := dst[k].(map[string]any); ok {
				mergeLevel(b, a, false)
				continue
			}
		}
		dst[k] = v
	}
}
func Load(cwd string) (Config, error) {
	c := Config{AgentDir: AgentDir(), Keys: DefaultBindings(), Settings: map[string]any{}, ThemeName: "pi"}
	var errs []error
	for _, p := range []string{filepath.Join(c.AgentDir, "settings.json"), filepath.Join(cwd, ".pi", "settings.json")} {
		var s map[string]any
		if e := readJSON(p, &s); e != nil {
			errs = append(errs, e)
		} else {
			merge(c.Settings, s)
		}
	}
	var raw map[string]json.RawMessage
	if e := readJSON(filepath.Join(c.AgentDir, "keybindings.json"), &raw); e != nil {
		errs = append(errs, e)
	} else {
		var aliases map[string]string
		_ = json.Unmarshal(aliasJSON, &aliases)
		for key, value := range raw {
			action := key
			if next, ok := aliases[key]; ok {
				if _, explicit := raw[next]; explicit {
					continue
				}
				action = next
			}
			if _, known := c.Keys.bindings[action]; !known {
				continue
			}
			keys, e := bindingValue(value)
			if e != nil {
				errs = append(errs, fmt.Errorf("keybindings.json %s: %w", key, e))
				continue
			}
			c.Keys.bindings[action] = keys
		}
	}
	c.Theme, _ = parseTheme(themeJSON)
	if data, e := os.ReadFile(filepath.Join(c.AgentDir, "themes", "pi.json")); e == nil {
		if t, e := parseTheme(data); e == nil {
			c.Theme = t
		} else {
			errs = append(errs, e)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		errs = append(errs, e)
	}
	var frontend struct {
		Theme string `json:"theme"`
	}
	if err := readJSON(filepath.Join(c.AgentDir, "goui.json"), &frontend); err != nil {
		errs = append(errs, err)
	} else if frontend.Theme != "" && frontend.Theme != "pi" {
		if theme, err := LoadTheme(c.AgentDir, frontend.Theme); err != nil {
			errs = append(errs, err)
		} else {
			c.Theme, c.ThemeName = theme, frontend.Theme
		}
	}
	return c, errors.Join(errs...)
}
func parseTheme(data []byte) (Theme, error) {
	var raw struct {
		Vars, Colors map[string]string
		Export       map[string]string
	}
	if e := json.Unmarshal(themeJSON, &raw); e != nil {
		return Theme{}, e
	}
	if e := json.Unmarshal(data, &raw); e != nil {
		return Theme{}, fmt.Errorf("pi theme: %w", e)
	}
	if raw.Vars == nil || raw.Colors == nil {
		return Theme{}, fmt.Errorf("pi theme: vars and colors must be objects")
	}
	resolve := func(s string) string {
		for i := 0; i < 32; i++ {
			if v, ok := raw.Vars[s]; ok {
				s = v
			} else {
				break
			}
		}
		return s
	}
	get := func(k, fallback string) string {
		if s := raw.Colors[k]; s != "" {
			return resolve(s)
		}
		return resolve(fallback)
	}
	return Theme{Accent: get("accent", "#5fd7ff"), Border: get("border", "#4e4e4e"), Muted: get("muted", "#949494"), Dim: get("dim", "#666666"), Text: get("text", "#c0c0c0"), Background: resolve("bg"), UserBackground: get("userMessageBg", "#303030"), AssistantBackground: resolve("bg"), ToolBackground: get("toolPendingBg", "#1d1b24"), Success: get("success", "#87af00"), Error: get("error", "#d70000"), Warning: get("warning", "#d75f00"), Thinking: get("thinkingText", "#949494")}, nil
}
