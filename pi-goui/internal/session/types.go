// Package session projects pi's full session branch, not its compacted model context.
package session

import "encoding/json"

type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseUrl"`
	ContextWindow int    `json:"contextWindow"`
}
type Stats struct {
	Input, Output, CacheRead, CacheWrite, Total int
	Cost                                        float64
	HasCost                                     bool
	ContextTokens                               *int
	ContextPercent                              *float64
	ContextWindow                               int
}

// Image retains saved attachment bytes for explicit preview, never display text.
type Image struct {
	Data, MimeType string
}

// Item is a display block; IDs remain stable while tools/messages stream.
type Item struct {
	ID, Role, Text, Thinking string
	ToolID, ToolName         string
	Arguments, Details       json.RawMessage
	Images                   []Image
	Running, Error           bool
	HasResult                bool
	// EstimatedTokens counts historical call/result payload, not billed usage.
	EstimatedTokens int
	// MessageIndex is the 1-based full-branch record ordinal, shared by its blocks.
	// Zero means execution output arrived before any attributable message record.
	MessageIndex  int
	resultChars   int
	final         bool
	errorKnown    bool
	contentKnown  bool
	bashExecution bool
	// Arguments are immutable within a projected record/tool version.
	argumentChars      int
	argumentCharsKnown bool
}

// State is owned exclusively by the UI event loop.
type State struct {
	ID, File, Name, Cwd, Thinking string
	Model                         ModelInfo
	Stats                         Stats
	Items                         []Item
	// MessageCount includes full-branch message records and summary boundaries,
	// independent of content blocks, wrapping, folding or thinking visibility.
	MessageCount       int
	Busy, Compacting   bool
	Steering, FollowUp []string
	records            []record
	live               []record
	tools              map[string]Item
	toolIDs            []string
	aliases            map[string]string
	leaf               string
	pathIDs            map[string]bool
	serial             uint64
	active             string
	runActive          bool
	loadedState        bool
}
type Summary struct {
	ID, Path, Name, FirstMessage, Cwd string
	Modified                          int64
}
