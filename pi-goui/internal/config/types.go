package config

type Config struct {
	AgentDir string
	Keys     Bindings
	Theme    Theme
	Settings map[string]any
}
type Bindings struct{ bindings map[string][]string }
type Theme struct {
	Accent, Border, Muted, Dim, Text, Background        string
	UserBackground, AssistantBackground, ToolBackground string
	Success, Error, Warning, Thinking                   string
}
