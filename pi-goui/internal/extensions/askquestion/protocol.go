// Package askquestion describes the optional ask-question extension's RPC data.
// It performs no question validation or persistence; the backend owns both.
package askquestion

const DialogTitle = "pi-goui:ask-question:v1"

type Question struct {
	Question    string   `json:"question"`
	Label       string   `json:"label"`
	Options     []string `json:"options"`
	MultiSelect bool     `json:"multiSelect"`
}
type Answer struct {
	Answers       []string `json:"answers"`
	CustomAnswers []string `json:"customAnswers,omitempty"`
	WasCustom     bool     `json:"wasCustom"`
	Completed     bool     `json:"completed"`
}
