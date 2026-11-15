package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/codex"
	"pi-goui/internal/core/session"
)

func writePreviewConfig(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

const metadataHistory = `{"type":"session","version":3,"id":"preview-metadata"}
{"type":"model_change","id":"model","parentId":null,"provider":"openai-codex","modelId":"codex-preview"}
{"type":"thinking_level_change","id":"thinking","parentId":"model","thinkingLevel":"high"}
{"type":"message","id":"answer","parentId":"thinking","message":{"role":"assistant","provider":"openai-codex","model":"codex-preview","content":[{"type":"text","text":"METADATA-PREVIEW"}],"stopReason":"stop","usage":{"input":100,"output":20,"cacheRead":80,"cacheWrite":0,"totalTokens":200,"cost":{"total":0.25}}}}
`

func TestResumePreviewRestoresMetadataAndStartsQuotaWithoutPi(t *testing.T) {
	for _, mode := range []string{"cold", "cached", "pending preload"} {
		t.Run(mode, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 150, Height: 42})
			writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "settings.json"), `{"defaultProvider":"other","defaultModel":"wrong","defaultThinkingLevel":"low"}`)
			models := filepath.Join(m.cfg.AgentDir, "models.json")
			writePreviewConfig(t, models, `{"providers":{"openai-codex":{"models":[{"id":"codex-preview","name":"Codex preview","contextWindow":10000}]}}}`)
			file := filepath.Join(t.TempDir(), "session.jsonl")
			writePreviewConfig(t, file, metadataHistory)
			var preload tea.Cmd
			if mode != "cold" {
				preload = m.requestResumePreload(file)
				if mode == "cached" {
					m.Update(preload())
				}
			}
			owner := m.addTab(file)
			owner.state.Model = session.ModelInfo{Provider: "other", ID: "wrong", ContextWindow: 999, BaseURL: "https://wrong.invalid"}
			owner.setValue("retained draft")
			preview := m.previewSession(owner)
			if mode == "pending preload" {
				if preview != nil {
					t.Fatal("pending preload was not shared")
				}
				for _, message := range resumeCommandMessages(m.receiveResumePreload(preload().(resumePreloadMsg))) {
					m.Update(message)
				}
			} else {
				m.Update(preview())
			}
			model, stats := owner.state.Model, owner.state.Stats
			if model.ID != "codex-preview" || model.Provider != "openai-codex" || model.Name != "Codex preview" || model.ContextWindow != 10000 || model.BaseURL != "https://chatgpt.com/backend-api" || owner.state.Thinking != "high" {
				t.Fatalf("preview metadata missing or inherited: model=%+v thinking=%q", model, owner.state.Thinking)
			}
			if stats.Input != 100 || stats.Output != 20 || stats.CacheRead != 80 || !stats.HasCost || stats.Cost != .25 || stats.ContextTokens == nil || *stats.ContextTokens != 200 || stats.ContextPercent == nil || *stats.ContextPercent != 2 {
				t.Fatalf("preview usage/context missing: %+v", stats)
			}
			if !m.quotaPending || m.quotaKey == "" || owner.client != nil || len(m.pool.clients) != 0 || owner.resume.authoritative || owner.editor.Value() != "retained draft" || owner.working.Present {
				t.Fatal("metadata did not trigger quota eligibility, or changed execution/draft authority")
			}
			// Eligibility uses the existing quota client; no auth exists in this
			// isolated agent. Supply a result to exercise the actual UI receiver.
			remaining, credits := 75.0, 12.5
			m.Update(quotaMsg{key: m.quotaKey, generation: m.quotaGeneration, snapshot: codex.Snapshot{LoggedIn: true, RemainingPercent: &remaining, Credits: &credits, ResetAt: time.Now().Add(time.Hour)}})
			if quota := m.sidebarContext().Quota; quota.Credits == nil || *quota.Credits != credits {
				t.Fatal("preview quota credits were not applied")
			}
			view := ansi.Strip(m.View().Content)
			for _, label := range []string{"codex-preview", "high", "200/10K", "75.0%", "Credits"} {
				if !strings.Contains(view, label) {
					t.Fatalf("metadata %q absent before backend startup: %s", label, view)
				}
			}
			// Prepared history may be cached; local limits are refreshed rather
			// than captured from the tab that happened to preload the history.
			writePreviewConfig(t, models, `{"providers":{"openai-codex":{"modelOverrides":{"codex-preview":{"contextWindow":20000}}}}}`)
			m.Update(m.previewSession(owner)())
			if owner.state.Model.ContextWindow != 20000 || owner.state.Stats.ContextPercent == nil || *owner.state.Stats.ContextPercent != 1 {
				t.Fatal("cached history retained obsolete local model limits")
			}
			if raw, err := os.ReadFile(file); err != nil || string(raw) != metadataHistory {
				t.Fatal("preview changed native history")
			}
		})
	}
}

func TestBlankPreviewUsesLocalDefaultsAndRejectsLateMetadata(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "settings.json"), `{"defaultProvider":"test","defaultModel":"default-model","defaultThinkingLevel":"low"}`)
	writePreviewConfig(t, filepath.Join(m.opts.Cwd, ".pi", "settings.json"), `{"defaultThinkingLevel":"high"}`)
	writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "models.json"), `{"providers":{"test":{"baseUrl":"https://test.invalid","models":[{"id":"default-model","contextWindow":32000}]}}}`)
	owner.setValue("unsent draft")
	m.Update(m.previewSession(owner)())
	if owner.state.Model.ID != "default-model" || owner.state.Model.ContextWindow != 32000 || owner.state.Thinking != "high" || owner.state.Stats.ContextTokens != nil || owner.state.Stats.ContextPercent != nil || owner.client != nil || owner.editor.Value() != "unsent draft" {
		t.Fatal("blank defaults were not applied as local hints with unknown context")
	}
	late := m.previewSession(owner)()
	owner.cycle.generation++
	owner.resume.authoritative = true
	owner.state.Model = session.ModelInfo{ID: "rpc-model", Provider: "rpc-provider"}
	owner.state.Thinking = "medium"
	m.Update(late)
	if owner.state.Model.ID != "rpc-model" || owner.state.Thinking != "medium" {
		t.Fatal("late local metadata replaced authoritative RPC metadata")
	}
}

func TestPreviewModelConfigDoesNotInventLimitsOrTrustBrokenRegistry(t *testing.T) {
	m := testModel(t, "")
	for _, registry := range []string{`{"providers":{"openai-codex":{"baseUrl":"https://proxy.invalid"}}}`, `{broken`} {
		writePreviewConfig(t, filepath.Join(m.cfg.AgentDir, "models.json"), registry)
		cfg := m.previewConfigReader(m.current())()
		state := session.State{Model: session.ModelInfo{Provider: "openai-codex", ID: "uncatalogued"}, Thinking: "off"}
		cfg.apply(&state, session.ModelInfo{Provider: "other", ID: "old", ContextWindow: 99999})
		if state.Model.ContextWindow != 0 || state.Stats.ContextPercent != nil || state.Thinking != "off" {
			t.Fatal("unknown model inherited old limits or changed explicit off")
		}
		if cfg.valid && state.Model.BaseURL != "https://proxy.invalid" || !cfg.valid && state.Model.BaseURL != "" {
			t.Fatal("proxy or malformed registry was silently treated as official Codex")
		}
	}
}

func TestResolvedUnknownPreviewClearsPreviousQuota(t *testing.T) {
	m := testModel(t, "")
	m.quotaKey = "old identity"
	remaining := 75.0
	m.quotaSnapshot = codex.Snapshot{LoggedIn: true, RemainingPercent: &remaining}
	m.Update(m.previewSession(m.current())())
	if m.quotaKey != "" || m.sidebarContext().Quota.RemainingPercent != nil {
		t.Fatal("resolved unknown model kept another session's quota")
	}
}
