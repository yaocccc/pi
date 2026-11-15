package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func metadataPreviewFile(t *testing.T, records ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metadata.jsonl")
	if err := os.WriteFile(path, []byte(previewHeader+strings.Join(records, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func applyMetadataPreview(t *testing.T, path string, base State) (State, *PreparedSnapshot) {
	t.Helper()
	_, prepared, err := base.SnapshotPreparation().PrepareFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base.ApplySnapshot(prepared)
	return base, prepared
}

func TestPreviewMetadataColdAndPreloaded(t *testing.T) {
	path := metadataPreviewFile(t,
		`{"type":"model_change","id":"m0","parentId":null,"provider":"old-provider","modelId":"old-model"}`,
		`{"type":"thinking_level_change","id":"thinking0","parentId":"m0","thinkingLevel":"high"}`,
		`{"type":"message","id":"u","parentId":"thinking0","message":{"role":"user","content":"question"}}`,
		`{"type":"message","id":"a0","parentId":"u","message":{"role":"assistant","provider":"old-provider","model":"old-model","content":[{"type":"toolCall","id":"call","name":"read","arguments":{"path":"x"}}],"usage":{"input":100,"output":10,"cacheRead":20,"cacheWrite":5,"cost":{"total":0.5}}}}`,
		`{"type":"model_change","id":"wrong-model","parentId":"a0","provider":"wrong","modelId":"wrong"}`,
		`{"type":"message","id":"wrong-answer","parentId":"wrong-model","message":{"role":"assistant","provider":"wrong","model":"wrong","content":"other branch","usage":{"input":1000,"output":100,"cacheRead":200,"cacheWrite":50,"cost":{"total":4}}}}`,
		`{"type":"model_change","id":"m1","parentId":"a0","provider":"openai-codex","modelId":"saved-model"}`,
		`{"type":"thinking_level_change","id":"thinking1","parentId":"m1","thinkingLevel":"off"}`,
		`{"type":"message","id":"result","parentId":"thinking1","message":{"role":"toolResult","toolCallId":"call","toolName":"read","content":"result","usage":{"input":3,"output":2,"cacheRead":1,"cacheWrite":0,"cost":{"total":0.125}}}}`,
		`{"type":"usage","id":"usage","parentId":"result","usage":{"input":7,"output":4,"cacheRead":2,"cacheWrite":1,"cost":{"total":0.125}}}`,
		`{"type":"compaction","id":"compact","parentId":"usage","summary":"compacted","usage":{"input":11,"output":6,"cacheRead":4,"cacheWrite":2,"cost":{"total":0.25}}}`,
		`{"type":"branch_summary","id":"branch","parentId":"compact","summary":"branched","usage":{"input":13,"output":8,"cacheRead":6,"cacheWrite":3,"cost":{"total":0.5}}}`,
		`{"type":"message","id":"answer","parentId":"branch","message":{"role":"assistant","provider":"openai-codex","model":"saved-model","content":"saved answer","stopReason":"stop","usage":{"input":200,"output":20,"cacheRead":40,"cacheWrite":10,"totalTokens":271,"cost":{"total":1}}}}`,
	)
	staleTokens, stalePercent := 99999, 99.0
	base := State{
		Model:    ModelInfo{ID: "unrelated", Provider: "unrelated", Name: "old name", BaseURL: "https://old.invalid", ContextWindow: 999999},
		Thinking: "xhigh", Stats: Stats{Input: 999999, ContextTokens: &staleTokens, ContextPercent: &stalePercent, ContextWindow: 999999},
		Busy: true, Steering: []string{"local queue"},
	}
	cold, _ := applyMetadataPreview(t, path, base)
	if cold.Model != (ModelInfo{Provider: "openai-codex", ID: "saved-model"}) || cold.Thinking != "off" {
		t.Fatalf("wrong branch/model/thinking or stale model limits: %+v %q", cold.Model, cold.Thinking)
	}
	wantTokens := 271
	wantStats := Stats{
		UserMessages: 1, AssistantMessages: 3, ToolResults: 1, ToolCalls: 1, TotalMessages: 5,
		Input: 1334, Output: 150, CacheRead: 273, CacheWrite: 71, Total: 1828,
		Cost: 6.5, HasCost: true, ContextTokens: &wantTokens,
	}
	if !reflect.DeepEqual(cold.Stats, wantStats) || cold.MessageCount != 6 {
		t.Fatalf("wrong native totals or branch record count: stats=%+v count=%d", cold.Stats, cold.MessageCount)
	}
	if !cold.Busy || !reflect.DeepEqual(cold.Steering, base.Steering) {
		t.Fatal("disk metadata changed backend/queue authority")
	}

	_, preloaded := applyMetadataPreview(t, path, State{})
	warm := base
	warm.ApplySnapshot(preloaded)
	if !reflect.DeepEqual(cold.Model, warm.Model) || cold.Thinking != warm.Thinking || !reflect.DeepEqual(cold.Stats, warm.Stats) {
		t.Fatal("cold and preloaded metadata differ")
	}
	*warm.Stats.ContextTokens = 1
	var another State
	another.ApplySnapshot(preloaded)
	if another.Stats.ContextTokens == nil || *another.Stats.ContextTokens != 271 {
		t.Fatal("reusable preview leaked mutable token pointers between owners")
	}
}

func TestPreviewModelFallbackAndExplicitChange(t *testing.T) {
	answer := `{"type":"message","id":"a","parentId":null,"message":{"role":"assistant","provider":"saved-provider","model":"saved-model","content":"answer"}}`
	for _, tc := range []struct {
		name     string
		extra    []string
		model    ModelInfo
		thinking string
	}{
		{name: "assistant fallback", model: ModelInfo{ID: "saved-model", Provider: "saved-provider"}},
		{name: "new model after answer", extra: []string{`{"type":"model_change","id":"m","parentId":"a","provider":"new-provider","modelId":"new-model"}`}, model: ModelInfo{ID: "new-model", Provider: "new-provider"}},
		{name: "only active branch", extra: []string{
			`{"type":"model_change","id":"wrong","parentId":"a","provider":"wrong","modelId":"wrong"}`,
			`{"type":"thinking_level_change","id":"wrong-thinking","parentId":"wrong","thinkingLevel":"xhigh"}`,
			`{"type":"session_info","id":"leaf","parentId":"a","name":"active"}`,
		}, model: ModelInfo{ID: "saved-model", Provider: "saved-provider"}},
		{name: "last thinking wins", extra: []string{
			`{"type":"thinking_level_change","id":"high","parentId":"a","thinkingLevel":"high"}`,
			`{"type":"thinking_level_change","id":"off","parentId":"high","thinkingLevel":"off"}`,
		}, model: ModelInfo{ID: "saved-model", Provider: "saved-provider"}, thinking: "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := metadataPreviewFile(t, append([]string{answer}, tc.extra...)...)
			got, _ := applyMetadataPreview(t, path, State{Model: ModelInfo{Provider: "stale", ID: "stale", ContextWindow: 999}, Thinking: "stale"})
			if got.Model != tc.model || got.Thinking != tc.thinking {
				t.Fatalf("got model=%+v thinking=%q", got.Model, got.Thinking)
			}
		})
	}
}

func TestPreviewContextRequiresCurrentReliableUsage(t *testing.T) {
	answer := `{"type":"message","id":"a","parentId":null,"message":{"role":"assistant","content":"answer","usage":{"input":100,"output":10,"cacheRead":5,"cacheWrite":2,"cost":{"total":0}}}}`
	compact := `{"type":"compaction","id":"c","parentId":"a","summary":"small context"}`
	for _, tc := range []struct {
		name   string
		extra  []string
		tokens int // Negative means unknown, not zero.
	}{
		{name: "latest usage", tokens: 117},
		{name: "metadata leaves context intact", extra: []string{`{"type":"session_info","id":"s","parentId":"a","name":"new name"}`}, tokens: 117},
		{name: "trailing user", extra: []string{`{"type":"message","id":"u","parentId":"a","message":{"role":"user","content":"next question"}}`}, tokens: -1},
		{name: "trailing tool", extra: []string{`{"type":"message","id":"r","parentId":"a","message":{"role":"toolResult","toolCallId":"t","content":"new output"}}`}, tokens: -1},
		{name: "compacted", extra: []string{compact}, tokens: -1},
		{name: "context edited", extra: []string{`{"type":"context_edit","id":"edit","parentId":"a","edits":[]}`}, tokens: -1},
		{name: "after compaction", extra: []string{compact, `{"type":"message","id":"after","parentId":"c","message":{"role":"assistant","content":"after","usage":{"input":12,"output":3,"cacheRead":2,"cacheWrite":1}}}`}, tokens: 18},
		{name: "error after compaction", extra: []string{compact, `{"type":"message","id":"after","parentId":"c","message":{"role":"assistant","stopReason":"error","content":"failed","usage":{"input":12000,"output":30}}}`}, tokens: -1},
		{name: "aborted", extra: []string{`{"type":"message","id":"after","parentId":"a","message":{"role":"assistant","stopReason":"aborted","content":"stopped","usage":{"input":12000,"output":30}}}`}, tokens: -1},
		{name: "zero usage", extra: []string{`{"type":"message","id":"after","parentId":"a","message":{"role":"assistant","content":"no billable response","usage":{"input":0,"output":0}}}`}, tokens: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := metadataPreviewFile(t, append([]string{answer}, tc.extra...)...)
			state, _ := applyMetadataPreview(t, path, State{})
			got := state.Stats.ContextTokens
			if tc.tokens < 0 && got != nil || tc.tokens >= 0 && (got == nil || *got != tc.tokens) {
				t.Fatalf("context=%v, want %d", got, tc.tokens)
			}
			if state.Stats.ContextWindow != 0 || state.Stats.ContextPercent != nil {
				t.Fatal("preview fabricated model limits or context percentage")
			}
		})
	}
}

func TestPreviewMetadataAbsentOrMalformedDoesNotLeakOrHideHistory(t *testing.T) {
	for _, record := range []string{
		`{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"readable"}}`,
		`{"type":"message","id":"a","parentId":null,"message":{"role":"assistant","content":"readable","usage":{"input":"unknown"}}}`,
	} {
		path := metadataPreviewFile(t, record)
		tokens, percent := 50, 25.0
		state, _ := applyMetadataPreview(t, path, State{Model: ModelInfo{ID: "old", Provider: "old", ContextWindow: 200}, Thinking: "high", Stats: Stats{ContextTokens: &tokens, ContextPercent: &percent, ContextWindow: 200}})
		if state.Model != (ModelInfo{}) || state.Thinking != "" || state.Stats.ContextTokens != nil || state.Stats.ContextPercent != nil || state.Stats.ContextWindow != 0 || !strings.Contains(texts(&state), "readable") {
			t.Fatalf("unknown metadata leaked prior state or hid history: %+v", state)
		}
	}
}

func TestPreviewMetadataDoesNotOverrideRPC(t *testing.T) {
	path := metadataPreviewFile(t,
		`{"type":"model_change","id":"m","parentId":null,"provider":"historical","modelId":"historical"}`,
		`{"type":"thinking_level_change","id":"t","parentId":"m","thinkingLevel":"off"}`,
		`{"type":"message","id":"a","parentId":"t","message":{"role":"assistant","content":"answer","usage":{"input":10,"output":2}}}`,
	)
	state, _ := applyMetadataPreview(t, path, State{})
	_, entries, err := ReadPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	rawState := []byte(`{"sessionId":"preview","model":{"id":"live","provider":"live","contextWindow":1000},"thinkingLevel":"high"}`)
	rawStats := []byte(`{"tokens":{"input":50,"output":5},"cost":1.5,"totalMessages":7,"assistantMessages":4,"contextUsage":{"tokens":90,"contextWindow":1000,"percent":9}}`)
	prepared, err := state.SnapshotPreparation().Prepare(rawState, entries, rawStats)
	if err != nil {
		t.Fatal(err)
	}
	state.ApplySnapshot(prepared)
	if state.Model.ID != "live" || state.Thinking != "high" || state.Stats.Input != 50 || state.Stats.ContextTokens == nil || *state.Stats.ContextTokens != 90 || state.Stats.TotalMessages != 7 || state.Stats.AssistantMessages != 4 {
		t.Fatalf("historical metadata overrode RPC: %+v", state)
	}
	model, thinking, stats := state.Model, state.Thinking, state.Stats
	prepared, err = state.SnapshotPreparation().Prepare(nil, entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	state.ApplySnapshot(prepared)
	if state.Model != model || state.Thinking != thinking || !reflect.DeepEqual(state.Stats, stats) {
		t.Fatal("history-only RPC refresh replaced authoritative metadata")
	}
}
