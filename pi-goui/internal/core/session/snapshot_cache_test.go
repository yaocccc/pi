package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"pi-goui/internal/extensions/sessionview"
)

func TestSnapshotCacheReusesParsingButRefreshesStateAndStats(t *testing.T) {
	var s State
	projection := &ownerProjection{State: &sessionview.State{}}
	s.SetExtensionProjection(projection)
	identity := json.RawMessage(`{"sessionId":"one","sessionFile":"one.jsonl","thinkingLevel":"low"}`)
	p, err := s.SnapshotPreparation().Prepare(identity, json.RawMessage(preparedHistory), json.RawMessage(`{"tokens":{"input":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.entriesCache != nil {
		t.Fatal("unapplied preparation published a cache")
	}
	s.ApplySnapshot(p)
	cache, oldProjection := s.entriesCache, s.records[1].projection
	projection.forbid = true
	p, err = s.SnapshotPreparation().Prepare(json.RawMessage(`{"sessionId":"one","sessionFile":"one.jsonl","thinkingLevel":"high","isStreaming":true}`), json.RawMessage(preparedHistory), json.RawMessage(`{"tokens":{"input":9},"cost":0.5}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.state.entriesCache != cache || p.state.records[1].projection != oldProjection {
		t.Fatal("unchanged history was reparsed or reprojected")
	}
	if s.Thinking != "low" || s.Stats.Input != 3 {
		t.Fatal("Prepare mutated its owner")
	}
	projection.forbid = false
	s.ApplySnapshot(p)
	if s.Thinking != "high" || s.Stats.Input != 9 || !s.Busy || s.Stats.Cost != .5 || projection.calls != 2 {
		t.Fatalf("cache hid fresh metadata/extension replay: %+v, calls=%d", s, projection.calls)
	}
	for _, e := range cache.path {
		if len(e.Message) != 0 {
			t.Fatal("cache retained duplicate message envelope bytes")
		}
	}
}

func TestSnapshotCacheChangesInvalidateAndErrorsAreAtomic(t *testing.T) {
	var s State
	if err := s.LoadState(json.RawMessage(`{"sessionId":"one","sessionFile":"one.jsonl"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	original := s.entriesCache
	for name, raw := range map[string]string{
		"invalid-json":    `{"entries":[`,
		"duplicate-id":    strings.Replace(preparedHistory, `"id":"a"`, `"id":"u"`, 1),
		"missing-parent":  strings.Replace(preparedHistory, `"parentId":"u"`, `"parentId":"missing"`, 1),
		"cycle":           strings.Replace(preparedHistory, `"parentId":null`, `"parentId":"a"`, 1),
		"invalid-message": strings.Replace(preparedHistory, `"content":"question"`, `"content":"question","isError":"invalid"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			p, err := s.SnapshotPreparation().Prepare(nil, json.RawMessage(raw), nil)
			if err == nil || p != nil {
				t.Fatal("bad changed history reused successful cache")
			}
			if s.entriesCache != original || len(s.Items) != 2 || s.Items[1].Text != "answer" {
				t.Fatal("failed history changed owner/cache")
			}
		})
	}
	changed := json.RawMessage(strings.Replace(preparedHistory, "answer", "changed", 1))
	if err := s.LoadEntries(changed); err != nil {
		t.Fatal(err)
	}
	if s.entriesCache == original || s.Items[1].Text != "changed" {
		t.Fatal("same entry id with changed content reused cache")
	}
	newCache := s.entriesCache
	if err := s.LoadEntries(json.RawMessage(strings.Replace(string(changed), `"leafId":"a"`, `"leafId":"u"`, 1))); err != nil {
		t.Fatal(err)
	}
	if s.entriesCache == newCache || len(s.Items) != 1 {
		t.Fatal("leaf navigation reused old branch")
	}
	if err := s.LoadEntries(json.RawMessage(`{"entries":[],"leafId":null}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.records) != 0 || len(s.Items) != 0 {
		t.Fatal("empty snapshot did not replace cache/history")
	}
}

func TestSnapshotCacheScopedToSessionIdentity(t *testing.T) {
	var s State
	for _, identity := range []string{
		`{"sessionId":"one","sessionFile":"one.jsonl"}`,
		`{"sessionId":"two","sessionFile":"one.jsonl"}`,
		`{"sessionId":"two","sessionFile":"two.jsonl"}`,
	} {
		previous := s.entriesCache
		p, err := s.SnapshotPreparation().Prepare(json.RawMessage(identity), json.RawMessage(preparedHistory), nil)
		if err != nil {
			t.Fatal(err)
		}
		s.ApplySnapshot(p)
		if s.entriesCache == previous || s.entriesCache.id != s.ID || s.entriesCache.file != s.File {
			t.Fatal("cache crossed file/session identity")
		}
	}
}

func TestSnapshotCacheStillReconcilesLiveToolsAndAliases(t *testing.T) {
	raw := json.RawMessage(`{"entries":[{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"question"}},{"type":"message","id":"r","parentId":"u","message":{"role":"toolResult","toolCallId":"tool","toolName":"read","content":"result"}}]}`)
	var cached State
	if err := cached.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	cache := cached.entriesCache
	cached.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"live after snapshot"}],"timestamp":42}}`))
	cached.tools["tool"] = Item{ToolID: "tool", Role: "tool", Details: json.RawMessage(`{"latest":true}`)}
	uncached := detachedState(cached)
	uncached.entriesCache = nil
	want, err := uncached.SnapshotPreparation().Prepare(nil, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cached.SnapshotPreparation().Prepare(nil, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.state.entriesCache != cache {
		t.Fatal("did not exercise cache hit")
	}
	if !reflect.DeepEqual(got.state.Items, want.state.Items) || !reflect.DeepEqual(got.state.aliases, want.state.aliases) || !reflect.DeepEqual(got.state.live, want.state.live) {
		t.Fatal("cache skipped live/tool/alias reconciliation")
	}
	if len(got.state.live) != 1 || len(got.state.records[1].msg.Details) == 0 || len(cache.messages["r"].message.Details) != 0 {
		t.Fatal("fallback details were lost or contaminated immutable parsed cache")
	}
}

func TestSnapshotCacheConcurrentPreparationAndOwnerUpdates(t *testing.T) {
	var s State
	if err := s.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	preparation := s.SnapshotPreparation()
	cache := s.entriesCache
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				p, err := preparation.Prepare(nil, json.RawMessage(preparedHistory), nil)
				if err != nil || p.state.entriesCache != cache || len(p.state.Items) != 2 {
					t.Errorf("unstable cached preparation: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		s.Apply(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"later"}}`))
		changed := json.RawMessage(strings.Replace(preparedHistory, "answer", fmt.Sprintf("answer-%d", i), 1))
		if err := s.LoadEntries(changed); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func BenchmarkSnapshotPreparationCache(b *testing.B) {
	entries := make([]map[string]any, 300)
	for i := range entries {
		var parent any
		if i > 0 {
			parent = fmt.Sprint(i - 1)
		}
		entries[i] = map[string]any{"type": "message", "id": fmt.Sprint(i), "parentId": parent, "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": strings.Repeat("history content ", 2048)}}, "stopReason": "stop"}}
	}
	raw, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		b.Fatal(err)
	}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%t", cached), func(b *testing.B) {
			var state State
			if err := state.LoadEntries(raw); err != nil {
				b.Fatal(err)
			}
			if !cached {
				state.entriesCache = nil
			}
			preparation := state.SnapshotPreparation()
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := preparation.Prepare(nil, raw, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
