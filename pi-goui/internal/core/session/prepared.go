package session

import (
	"encoding/json"
	"maps"
	"slices"
)

// SnapshotPreparation owns detached containers and immutable record versions.
// It never contains an extension capability and can be used outside the UI loop.
// Capturing it must happen on the State's owning goroutine.
type SnapshotPreparation struct{ base State }

// PreparedSnapshot is a single snapshot result, not a cache. Its internals are
// private so callers cannot mutate records while another goroutine applies it.
type PreparedSnapshot struct {
	state                       State
	path                        []entry
	stateData, statsData        json.RawMessage
	hasEntries, reset, branched bool
}

func detachedState(s State) State {
	s.records = slices.Clone(s.records)
	s.live = slices.Clone(s.live)
	s.tools = maps.Clone(s.tools)
	s.toolIDs = slices.Clone(s.toolIDs)
	s.aliases = maps.Clone(s.aliases)
	s.pathIDs = maps.Clone(s.pathIDs)
	s.Items = slices.Clone(s.Items)
	s.Steering = slices.Clone(s.Steering)
	s.FollowUp = slices.Clone(s.FollowUp)
	s.extensionState = extensionState{}
	return s
}

func (s *State) SnapshotPreparation() SnapshotPreparation {
	return SnapshotPreparation{base: detachedState(*s)}
}

// Prepare parses, reconciles aliases/live records, hashes message content and
// constructs record-local presentation off the event loop. The caller must
// validate the captured owner/backend/revision before applying the result.
// A stats error can accompany a valid result; an invalid state/history cannot.
func (p SnapshotPreparation) Prepare(state, entries, stats json.RawMessage) (*PreparedSnapshot, error) {
	next := detachedState(p.base)
	out := &PreparedSnapshot{hasEntries: len(entries) > 0, stateData: slices.Clone(state), statsData: slices.Clone(stats)}
	if len(state) > 0 {
		if err := next.LoadState(state); err != nil {
			return nil, err
		}
		out.reset = p.base.loadedState && p.base.ID != "" && next.ID != "" && next.ID != p.base.ID
	}
	previousLeaf := next.leaf
	if out.hasEntries {
		if err := next.loadEntries(entries, &out.path); err != nil {
			return nil, err
		}
		out.branched = previousLeaf != "" && next.leaf != previousLeaf && !next.pathIDs[previousLeaf]
	}
	var err error
	if len(stats) > 0 {
		err = next.LoadStats(stats)
	}
	out.state = next
	return out, err
}

// ApplySnapshot performs only capability-owned metadata replay and container
// adoption. Record bodies and their projections have already been prepared.
// Extension capabilities stay on the event loop, never in the async result.
func (s *State) ApplySnapshot(p *PreparedSnapshot) {
	if p == nil {
		return
	}
	// Adopt only history fields from the captured state. Small metadata is
	// applied to the current owner, preserving fields omitted by endpoints
	// (for example a queue cleared while the history command was running).
	next := detachedState(*s)
	if len(p.stateData) > 0 {
		_ = next.LoadState(p.stateData) // Validated during preparation.
	}
	if len(p.statsData) > 0 {
		_ = next.LoadStats(p.statsData)
	}
	if p.hasEntries {
		history := detachedState(p.state)
		next.records, next.live = history.records, history.live
		next.tools, next.toolIDs = history.tools, history.toolIDs
		next.aliases, next.pathIDs = history.aliases, history.pathIDs
		next.leaf, next.serial, next.active = history.leaf, history.serial, history.active
		for _, e := range p.path {
			if e.Type == "session_info" {
				next.Name = p.state.Name
				break
			}
		}
	}
	next.extensionState = s.extensionState
	if p.reset {
		next.extensionState.Reset()
	}
	if p.hasEntries {
		projection := next.newExtensionSnapshot(p.path)
		recordIndex := 0
		for i := len(p.path) - 1; i >= 0; i-- {
			e := p.path[i]
			projection.observeEntry(e)
			switch e.Type {
			case "message":
				projection.observeMessage(next.records[recordIndex].msg)
				recordIndex++
			case "compaction":
				r := &next.records[recordIndex]
				r.compaction = projection.compaction(e.ID)
				r.projection = nil // Tiny metadata-only summary, not a message body.
				recordIndex++
			case "branch_summary":
				recordIndex++
			}
		}
		next.commitExtensionSnapshot(projection, p.branched)
		next.project()
	}
	*s = next
}
