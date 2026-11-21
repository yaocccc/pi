// Package session projects pi's full session branch, not its compacted model context.
package session

import (
	"encoding/json"
	"slices"
)

type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseUrl"`
	ContextWindow int    `json:"contextWindow"`
}
type Stats struct {
	UserMessages, AssistantMessages             int
	ToolResults, ToolCalls, TotalMessages       int
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
	// EstimatedTokens counts historical call/result payload, not billed usage.
	EstimatedTokens int
	Compaction      *CompactionMetrics
	// MessageIndex is the 1-based full-branch record ordinal, shared by its blocks.
	// Zero means execution output arrived before any attributable message record.
	MessageIndex               int
	resultChars, argumentChars int
	// Native JSON versions are frozen when the producing record/event changes,
	// independently of the mutable RawMessage views exposed to extensions.
	layoutArgs, layoutDetails                                          *string
	Running, Error, HasResult                                          bool
	final, errorKnown, contentKnown, bashExecution, argumentCharsKnown bool
}

func freezeJSON(raw json.RawMessage) *string {
	if len(raw) == 0 {
		return nil
	}
	s := string(raw)
	return &s
}

// LayoutSnapshot shares native payload versions. Caller-built Items
// have no such ownership boundary and must still be copied.
func (i Item) LayoutSnapshot() Item {
	if i.layoutArgs != nil {
		i.Arguments = nil
	} else {
		i.Arguments = slices.Clone(i.Arguments)
	}
	if i.layoutDetails != nil {
		i.Details = nil
	} else {
		i.Details = slices.Clone(i.Details)
	}
	i.Images = slices.Clone(i.Images)
	if i.Compaction != nil {
		metrics := *i.Compaction
		i.Compaction = &metrics
	}
	return i
}

// LayoutJSONKeys also supports live, non-snapshot Items: never trust a
// caller-owned RawMessage as a cache key after this render.
func (i Item) LayoutJSONKeys(prevArgs, prevDetails string) (string, string) {
	key := func(raw json.RawMessage, frozen *string, previous string) string {
		if raw == nil && frozen != nil {
			return *frozen
		}
		if string(raw) == previous {
			return previous
		}
		return string(raw)
	}
	return key(i.Arguments, i.layoutArgs, prevArgs), key(i.Details, i.layoutDetails, prevDetails)
}

// MaterializeLayoutJSON thaws the frozen fields in a layout snapshot. A
// provider may mutate or retain those bytes without changing the native
// record or another job. Ordinary Items keep their existing RawMessages.
func (i Item) MaterializeLayoutJSON() Item {
	if i.Arguments == nil && i.layoutArgs != nil {
		i.Arguments = json.RawMessage(*i.layoutArgs)
	}
	if i.Details == nil && i.layoutDetails != nil {
		i.Details = json.RawMessage(*i.layoutDetails)
	}
	return i
}

// State is owned exclusively by the UI event loop.
type State struct {
	ID, File, Name, Cwd, Thinking string
	Model                         ModelInfo
	Stats                         Stats
	Items                         []Item
	extensionState
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
	metadataRevision   uint64
	metadataChanges    map[string]uint64
	entriesCache       *snapshotCache
	deferProjection    bool
	projectionPending  bool
}
type Summary struct {
	ID, Path, Name, FirstMessage, Cwd string
	Modified                          int64
	ParentSession                     string
	Fork                              *ForkProvenance
}
