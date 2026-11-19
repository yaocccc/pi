package config

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
)

const DefaultThemeName = "gradient-classic"

// ThemeNames lists concrete palettes, grouped by border style.
func ThemeNames() []string {
	return []string{DefaultThemeName,
		"gradient-amber", "gradient-azure", "gradient-clay", "gradient-copper", "gradient-frost", "gradient-indigo", "gradient-lavender", "gradient-lilac", "gradient-mint", "gradient-moss", "gradient-raspberry", "gradient-sand", "gradient-sky", "gradient-teal", "gradient-violet", "solid-amber", "solid-jade", "solid-plum", "solid-slate",
	}
}

// CanonicalThemeName accepts previous preference and command spellings.
func CanonicalThemeName(name string) string {
	switch name {
	case "pi":
		return "gradient-classic"
	case "ayu-mirage":
		return "gradient-amber"
	case "github":
		return "gradient-azure"
	case "one-dark":
		return "gradient-sky"
	case "tokyo-night":
		return "gradient-indigo"
	case "nord":
		return "gradient-frost"
	case "rose-pine":
		return "gradient-lilac"
	case "catppuccin":
		return "gradient-lavender"
	case "dracula":
		return "gradient-violet"
	case "claude":
		return "gradient-clay"
	case "gruvbox":
		return "gradient-copper"
	case "codex":
		return "gradient-mint"
	case "everforest":
		return "gradient-moss"
	case "monokai":
		return "gradient-raspberry"
	case "kanagawa":
		return "gradient-sand"
	case "solarized-dark":
		return "gradient-teal"
	case "flat-amber":
		return "solid-amber"
	case "flat-jade":
		return "solid-jade"
	case "flat-plum":
		return "solid-plum"
	case "flat-slate":
		return "solid-slate"
	}
	return name
}

// Keep the startup draw stable across previews and configuration reloads.
var startupTheme = sync.OnceValue(func() string {
	names := ThemeNames()
	return names[rand.IntN(len(names))]
})

func LoadTheme(agentDir, name string) (Theme, error) {
	name = CanonicalThemeName(name)
	if name == "random" {
		name = startupTheme()
	}
	t, _ := parseTheme(themeJSON)
	if name == DefaultThemeName {
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
		"gradient-lavender":  {"#cba6f7", "#585b70", "#a6adc8", "#6c7086", "#cdd6f4", "#1e1e2e", "#313244", "#a6e3a1", "#f38ba8", "#f9e2af"},
		"gradient-indigo":    {"#7aa2f7", "#414868", "#a9b1d6", "#565f89", "#c0caf5", "#1a1b26", "#24283b", "#9ece6a", "#f7768e", "#e0af68"},
		"gradient-frost":     {"#88c0d0", "#4c566a", "#a5b1c2", "#616e88", "#eceff4", "#2e3440", "#3b4252", "#a3be8c", "#bf616a", "#ebcb8b"},
		"gradient-lilac":     {"#c4a7e7", "#524f67", "#908caa", "#6e6a86", "#e0def4", "#191724", "#26233a", "#9ccfd8", "#eb6f92", "#f6c177"},
		"gradient-clay":      {"#e59978", "#645348", "#c5b6a8", "#97877c", "#f3e9df", "#211e1b", "#312a25", "#a8c693", "#ef958c", "#e6bf81"},
		"gradient-mint":      {"#88e1cd", "#41535a", "#a8babc", "#819498", "#e9f1f0", "#13191c", "#20292d", "#77dba4", "#ff949f", "#ebc37c"},
		"gradient-azure":     {"#58a6ff", "#484f58", "#b1bac4", "#8b949e", "#e6edf3", "#0d1117", "#161b22", "#3fb950", "#f85149", "#d29922"},
		"gradient-violet":    {"#bd93f9", "#555768", "#b6b7c8", "#9296ab", "#f8f8f2", "#282a36", "#343746", "#50fa7b", "#ff5555", "#f1fa8c"},
		"gradient-copper":    {"#fe8019", "#665c54", "#bdae93", "#a89984", "#ebdbb2", "#282828", "#3c3836", "#b8bb26", "#fb4934", "#fabd2f"},
		"gradient-raspberry": {"#f92672", "#59594e", "#bcbcae", "#969686", "#f8f8f2", "#272822", "#383830", "#a6e22e", "#ff6188", "#e6db74"},
		"gradient-sky":       {"#61afef", "#4b5263", "#abb2bf", "#8892a3", "#d7dae0", "#282c34", "#323842", "#98c379", "#e06c75", "#e5c07b"},
		"gradient-moss":      {"#a7c080", "#56635c", "#b8c5b2", "#9da9a0", "#d3c6aa", "#2d353b", "#343f44", "#a7c080", "#e67e80", "#dbbc7f"},
		"gradient-sand":      {"#c0a36e", "#54546d", "#b4b1a1", "#96969c", "#dcd7ba", "#1f1f28", "#2a2a37", "#98bb6c", "#e46876", "#e6c384"},
		"gradient-teal":      {"#2aa198", "#586e75", "#93a1a1", "#839496", "#e0e7df", "#002b36", "#073642", "#92ae20", "#ff7771", "#d4a72c"},
		"gradient-amber":     {"#ffcc66", "#535e73", "#b8c3d4", "#919dae", "#cccac2", "#1f2430", "#292f3c", "#bae67e", "#ff7373", "#ffd580"},
		"solid-slate":        {"#9bbfea", "#475264", "#b1bfce", "#8b9bad", "#e2e9f1", "#1b212a", "#282f39", "#93c6a3", "#ef9c9b", "#e1bf82"},
		"solid-amber":        {"#edbc6c", "#635342", "#c4b8a6", "#a49786", "#eee2cc", "#24201b", "#342e26", "#adca88", "#ec9284", "#f3ca7d"},
		"solid-jade":         {"#83c7a7", "#425b51", "#afc4b8", "#90a69a", "#defeeb", "#18231f", "#25322c", "#9adba0", "#ea9792", "#e1c477"},
		"solid-plum":         {"#d5a4d9", "#645069", "#c8b4ca", "#aa93ad", "#f0e2ee", "#281f2a", "#382c3a", "#b4ce9e", "#f1a0ac", "#ecc382"},
	}
	p, ok := palettes[name]
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q", name)
	}
	return Theme{FramePreset: name, Accent: p[0], Border: p[1], Muted: p[2], Dim: p[3], Text: p[4], Background: p[5], UserBackground: p[6], AssistantBackground: p[5], ToolBackground: p[6], Success: p[7], Error: p[8], Warning: p[9], Thinking: p[2]}, nil
}

// SaveTheme changes only frontend preferences, leaving native pi settings intact.
func SaveTheme(agentDir, name string) error {
	name = CanonicalThemeName(name)
	if _, err := LoadTheme(agentDir, name); err != nil {
		return err
	}
	return savePreference(agentDir, "theme", name)
}
