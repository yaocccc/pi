package protocol

// DialogKind identifies optional enhanced views, not backend operations.
// Unrecognized requests must retain the standard Pi dialog fallback.
type DialogKind string

const (
	Questionnaire     DialogKind = "questionnaire"
	WorkerSettings    DialogKind = "worker-settings"
	CodexFastSettings DialogKind = "codex-fast"
)

func Dialog(title string) DialogKind {
	switch title {
	case "pi-goui:ask-question:v1":
		return Questionnaire
	case "pi-goui:worker-settings:v1":
		return WorkerSettings
	case "pi-goui:codex-fast:v1":
		return CodexFastSettings
	default:
		return ""
	}
}
