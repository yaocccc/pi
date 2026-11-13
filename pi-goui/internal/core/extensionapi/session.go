package extensionapi

import "encoding/json"

// PlanStep is neutral, read-only checklist presentation data.
type PlanStep struct {
	ID     string `json:"id"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

type SessionMessage struct {
	Role, ToolName, ToolCallID string
	Details                    json.RawMessage
	IsError                    *bool
}

type SessionEntry struct {
	Type, ID, CustomType string
	Data                 json.RawMessage
}

type SessionView struct {
	Plan         []PlanStep
	PlanRevision uint64
}

// CompactionMetrics describe content estimates, not billed usage.
type CompactionMetrics struct{ BeforeTokens, AfterTokens int }

// SessionSnapshot is provisional until native history validation succeeds.
type SessionSnapshot interface {
	ObserveEntry(SessionEntry)
	ObserveMessage(SessionMessage)
	Compaction(string) *CompactionMetrics
}

// SessionProjection is owned by one session and called only by its event loop.
// It receives native metadata, never the host's State or mutable transcript.
// Views are read-only; the host copies any data it retains for display.
type SessionProjection interface {
	Reset()
	View() SessionView
	ObserveEvent(json.RawMessage)
	ObserveMessage(SessionMessage, bool)
	ObserveFallback([]SessionMessage, bool)
	NewSnapshot([]SessionEntry) SessionSnapshot
	Commit(SessionSnapshot, bool)
	HideMessage(role, customType string) bool
}
