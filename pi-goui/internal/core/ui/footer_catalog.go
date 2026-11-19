package ui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"

	"pi-goui/internal/core/session"
)

// A cold tab has no RPC connection. Project only display metadata from Pi's
// optional local model snapshot and custom models, never auth or executable settings.
// Catalog rows are hints: selecting one must be checked against the live list.
type catalogModel struct {
	session.ModelInfo
	ThinkingLevelMap map[string]json.RawMessage `json:"thinkingLevelMap"`
}

type footerCatalog struct {
	models []option
	width  int
	levels map[[2]string]map[string]json.RawMessage
}

func readFooterCatalog(agentDir string, custom map[string]previewProvider) *footerCatalog {
	entries := make(map[[2]string]catalogModel)
	add := func(provider, id string, m catalogModel) {
		if provider == "" || id == "" {
			return
		}
		key := [2]string{provider, id}
		merged := entries[key]
		merged.Provider, merged.ID = provider, id
		if m.ThinkingLevelMap != nil {
			if merged.ThinkingLevelMap == nil {
				merged.ThinkingLevelMap = make(map[string]json.RawMessage)
			}
			for level, v := range m.ThinkingLevelMap {
				merged.ThinkingLevelMap[level] = v
			}
		}
		entries[key] = merged
	}
	// This is an optional cache, not a requirement for starting or selecting a model.
	const limit = 4 << 20
	path := filepath.Join(agentDir, "models-store.json")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Size() <= limit {
		if f, err := os.Open(path); err == nil {
			raw, err := io.ReadAll(io.LimitReader(f, limit+1))
			_ = f.Close()
			var snapshot map[string]struct{ Models []catalogModel }
			if err == nil && len(raw) <= limit && json.Unmarshal(raw, &snapshot) == nil {
				for provider, data := range snapshot {
					for _, m := range data.Models {
						add(provider, m.ID, m)
					}
				}
			}
		}
	}
	for provider, data := range custom {
		for _, m := range data.Models {
			add(provider, m.ID, m)
		}
		for id, m := range data.ModelOverrides {
			add(provider, id, m)
		}
	}
	c := &footerCatalog{levels: make(map[[2]string]map[string]json.RawMessage)}
	var models []session.ModelInfo
	for key, m := range entries {
		models = append(models, m.ModelInfo)
		c.levels[key] = m.ThinkingLevelMap
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	c.models, c.width = footerModelOptions(models)
	return c
}

func (c *footerCatalog) options(kind string, state *session.State) ([]option, int) {
	if kind == "model" {
		if c != nil && len(c.models) > 0 {
			return c.models, c.width
		}
		if state.Model.Provider != "" && state.Model.ID != "" {
			return footerModelOptions([]session.ModelInfo{state.Model})
		}
		return nil, 0
	}
	// Missing mappings can inherit Pi defaults. Don't guess them: preview only
	// explicitly named levels and the current setting; label click opens the full live list.
	var levels []string
	if c != nil {
		for level, v := range c.levels[[2]string{state.Model.Provider, state.Model.ID}] {
			if level != "" && level != state.Thinking && !bytes.Equal(v, []byte("null")) {
				levels = append(levels, level)
			}
		}
	}
	sort.Strings(levels)
	if state.Thinking != "" {
		levels = append([]string{state.Thinking}, levels...)
	}
	var options []option
	for _, level := range levels {
		options = append(options, option{Label: level, Value: level})
	}
	return options, 0
}
