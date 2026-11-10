package config

type Config struct {
	AgentDir  string
	Keys      Bindings
	Theme     Theme
	ThemeName string
	Settings  map[string]any
}
type Bindings struct{ bindings map[string][]string }
type Theme struct {
	FramePreset                                         string // Empty uses the pi border palette; comparable for transcript caching.
	Accent, Border, Muted, Dim, Text, Background        string
	UserBackground, AssistantBackground, ToolBackground string
	Success, Error, Warning, Thinking                   string
}
