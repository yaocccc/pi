package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ThemeNames is ordered for the frontend picker. pi preserves the native palette.
func ThemeNames() []string { return []string{"pi", "catppuccin", "tokyo-night", "nord", "rose-pine"} }

func LoadTheme(agentDir, name string) (Theme, error) {
	t, _ := parseTheme(themeJSON)
	if name == "pi" {
		data, err := os.ReadFile(filepath.Join(agentDir, "themes", "pi.json"))
		if os.IsNotExist(err) {
			return t, nil
		}
		if err != nil {
			return Theme{}, err
		}
		return parseTheme(data)
	}
	// accent, border, muted, dim, text, background, surface, success, error, warning
	palettes := map[string][]string{
		"catppuccin":  {"#cba6f7", "#585b70", "#a6adc8", "#6c7086", "#cdd6f4", "#1e1e2e", "#313244", "#a6e3a1", "#f38ba8", "#f9e2af"},
		"tokyo-night": {"#7aa2f7", "#414868", "#a9b1d6", "#565f89", "#c0caf5", "#1a1b26", "#24283b", "#9ece6a", "#f7768e", "#e0af68"},
		"nord":        {"#88c0d0", "#4c566a", "#a5b1c2", "#616e88", "#eceff4", "#2e3440", "#3b4252", "#a3be8c", "#bf616a", "#ebcb8b"},
		"rose-pine":   {"#c4a7e7", "#524f67", "#908caa", "#6e6a86", "#e0def4", "#191724", "#26233a", "#9ccfd8", "#eb6f92", "#f6c177"},
	}
	p, ok := palettes[name]
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q", name)
	}
	return Theme{FramePreset: name, Accent: p[0], Border: p[1], Muted: p[2], Dim: p[3], Text: p[4], Background: p[5], UserBackground: p[6], AssistantBackground: p[5], ToolBackground: p[6], Success: p[7], Error: p[8], Warning: p[9], Thinking: p[2]}, nil
}

// SaveTheme changes only frontend preferences, leaving native pi settings intact.
func SaveTheme(agentDir, name string) error {
	if _, err := LoadTheme(agentDir, name); err != nil {
		return err
	}
	path := filepath.Join(agentDir, "goui.json")
	prefs := map[string]json.RawMessage{}
	if err := readJSON(path, &prefs); err != nil {
		return err
	}
	if prefs == nil {
		prefs = map[string]json.RawMessage{}
	}
	prefs["theme"], _ = json.Marshal(name)
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(agentDir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(agentDir, ".goui-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
