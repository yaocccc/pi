package worker

// RPC data only. The Worker capability validates and persists settings.
const SettingsDialogTitle = "pi-goui:worker-settings:v1"

type PresetSettings struct {
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
}
type RoutingSettings struct {
	Version              int            `json:"version"`
	Fast                 PresetSettings `json:"fast"`
	Normal               PresetSettings `json:"normal"`
	Deep                 PresetSettings `json:"deep"`
	MaxConcurrentWorkers int            `json:"maxConcurrentWorkers"`
	DefaultTimeoutMs     int            `json:"defaultTimeoutMs"`
	MaxOutputBytes       int            `json:"maxOutputBytes"`
}
type SettingsError struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
	Message  string `json:"message"`
}
type ModelOption struct {
	ID       string   `json:"id"`
	Thinking []string `json:"thinking"`
}
type SettingsRequest struct {
	Config   RoutingSettings  `json:"config"`
	Original *RoutingSettings `json:"original,omitempty"`
	Models   []ModelOption    `json:"models"`
	Error    *SettingsError   `json:"error,omitempty"`
}
