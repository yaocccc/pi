package ui

import (
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
)

// Single-use result, owned exclusively by the preparation command until the
// validated forkCreated response transfers it to a newly created tab.
type forkLayout struct {
	state   session.State
	header  session.Header
	stamp   resumePreviewStamp
	catalog *footerCatalog
}

// Capture a fresh projection and the fork's local configuration inputs on the
// event loop. The command needs no source tab/client or mutable Model fields.
func (m *Model) forkLayoutReader(cwd string) func(session.Fork) (*forkLayout, error) {
	projection := m.extensions.NewProjection()
	loadConfig := m.previewConfigReader(&tab{state: session.State{Cwd: cwd}})
	return func(f session.Fork) (*forkLayout, error) {
		// The stamp must describe the file actually read, not a later stat
		// paired with an older in-memory get_entries envelope. PrepareFile also
		// derives historical model/thinking/usage in this same parse.
		stamp, err := previewStamp(f.Path)
		if err != nil {
			return nil, err
		}
		layout := &forkLayout{stamp: stamp}
		state := &layout.state
		state.SetExtensionProjection(projection)
		identity, _ := json.Marshal(map[string]string{"sessionId": f.ID, "sessionFile": f.Path, "sessionName": f.Name, "cwd": f.Cwd})
		if err := state.LoadState(identity); err != nil {
			return nil, err
		}
		header, prepared, err := state.SnapshotPreparation().PrepareFile(f.Path)
		if err != nil {
			return nil, err
		}
		if header.ID != f.ID || !stamp.current(f.Path) {
			return nil, fmt.Errorf("fork history changed while preparing preview")
		}
		layout.header = header
		// This state and extension projection have never belonged to a tab;
		// replay them here, not in the UI's first frame.
		state.ApplySnapshot(prepared)
		cfg := loadConfig()
		cfg.apply(state, session.ModelInfo{})
		layout.catalog = cfg.catalog
		return layout, nil
	}
}

// Only call after validating the source tab, sequence and backend. addTab
// activates once; reactivation after scheduling would invalidate the job.
func (m *Model) openForkLayout(source *tab, f session.Fork, layout *forkLayout) (*tab, tea.Cmd, error) {
	if layout == nil || layout.state.ID != f.ID || layout.state.File != f.Path || !layout.stamp.current(f.Path) {
		return nil, nil, fmt.Errorf("fork history changed before opening; reopen it from the session list")
	}
	t := m.addTab(f.Path)
	t.state, t.origin, t.footerCatalog = layout.state, layout.header, layout.catalog
	t.resume.preview, t.cycle.preview = true, layout.stamp
	t.revision++
	t.streamDirty = true // addTab marked dirty; do not synchronously render even on a mouse/key event.
	now := time.Now()
	render := m.scheduleDocument(t)
	// Seed only this private job, never publish the source document. Cache
	// sections are immutable and must match the fork's own items/options.
	if job := t.documentJob; job != nil && source != nil {
		job.cache = source.renderCache
	}
	return t, tea.Batch(render, m.pollQuota(now), m.pollSession(t, now, true), m.focus(t)), nil
}
