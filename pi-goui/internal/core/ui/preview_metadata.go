package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"pi-goui/internal/core/session"
)

// These are display hints only. Starting a writer still loads authoritative RPC
// state. Never resolve API keys, execute config commands or start Pi here.
type previewModelConfig struct {
	settings struct {
		DefaultProvider      string
		DefaultModel         string
		DefaultThinkingLevel string
		ModelThinkingLevels  map[string]string
	}
	providers map[string]previewProvider
	catalog   *footerCatalog
	valid     bool
}

type previewProvider struct {
	BaseURL        string `json:"baseUrl"`
	Models         []catalogModel
	ModelOverrides map[string]catalogModel
}

func (m *Model) previewConfigReader(t *tab) func() previewModelConfig {
	agentDir, cwd := m.cfg.AgentDir, m.tabDirectory(t)
	needCatalog := t.footerCatalog == nil
	return func() previewModelConfig {
		var cfg previewModelConfig
		for _, path := range []string{filepath.Join(agentDir, "settings.json"), filepath.Join(cwd, ".pi", "settings.json")} {
			if raw, err := os.ReadFile(path); err == nil {
				// Do not partly apply malformed settings over a valid default.
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) == nil {
					for key, value := range fields {
						switch key {
						case "defaultProvider":
							_ = json.Unmarshal(value, &cfg.settings.DefaultProvider)
						case "defaultModel":
							_ = json.Unmarshal(value, &cfg.settings.DefaultModel)
						case "defaultThinkingLevel":
							_ = json.Unmarshal(value, &cfg.settings.DefaultThinkingLevel)
						case "modelThinkingLevels":
							_ = json.Unmarshal(value, &cfg.settings.ModelThinkingLevels)
						}
					}
				}
			}
		}
		var registry struct{ Providers map[string]previewProvider }
		raw, err := os.ReadFile(filepath.Join(agentDir, "models.json"))
		cfg.valid = errors.Is(err, os.ErrNotExist) || (err == nil && strings.TrimSpace(string(raw)) != "null" && json.Unmarshal(raw, &registry) == nil)
		if cfg.valid {
			cfg.providers = registry.Providers
		}
		if needCatalog {
			cfg.catalog = readFooterCatalog(agentDir, cfg.providers)
		}
		return cfg
	}
}

func (c previewModelConfig) apply(s *session.State, previous session.ModelInfo) {
	model := s.Model
	if model.ID == "" && model.Provider == "" {
		model.ID, model.Provider = c.settings.DefaultModel, c.settings.DefaultProvider
	}
	if s.Thinking == "" {
		s.Thinking = c.settings.ModelThinkingLevels[model.Provider+"/"+model.ID]
		if s.Thinking == "" {
			s.Thinking = c.settings.DefaultThinkingLevel
		}
	}
	if c.valid && model.ID != "" && model.Provider != "" {
		// A previous RPC result is useful only for this exact model, never for
		// a different tab/model or a speculative history cache entry.
		if previous.ID == model.ID && previous.Provider == model.Provider {
			model.Name, model.BaseURL, model.ContextWindow = previous.Name, previous.BaseURL, previous.ContextWindow
		}
		provider := c.providers[model.Provider]
		if provider.BaseURL != "" {
			model.BaseURL = provider.BaseURL
		} else if model.Provider == "openai-codex" && model.BaseURL == "" {
			// Native built-in Codex uses this origin. The quota client still
			// independently checks OAuth and rejects custom API-key providers.
			model.BaseURL = "https://chatgpt.com/backend-api"
		}
		merge := func(info session.ModelInfo) {
			if info.Name != "" {
				model.Name = info.Name
			}
			if info.BaseURL != "" {
				model.BaseURL = info.BaseURL
			}
			if info.ContextWindow > 0 {
				model.ContextWindow = info.ContextWindow
			}
		}
		for _, info := range provider.Models {
			if info.ID == model.ID {
				merge(info.ModelInfo)
				break
			}
		}
		merge(provider.ModelOverrides[model.ID].ModelInfo)
	}
	s.Model = model
	s.Stats.ContextWindow = model.ContextWindow
	s.Stats.ContextPercent = nil
	if s.Stats.ContextTokens != nil && model.ContextWindow > 0 {
		percent := float64(*s.Stats.ContextTokens) * 100 / float64(model.ContextWindow)
		s.Stats.ContextPercent = &percent
	}
}

func finishResumePreview(v resumePreviewMsg) resumePreviewMsg {
	if v.loadConfig != nil {
		v.config = v.loadConfig()
		v.loadConfig = nil
	}
	return v
}
