package session

import (
	"encoding/json"
	"testing"
)

func TestCompactionMetricsLiveAndHistory(t *testing.T) {
	compact := `{"type":"compaction","id":"c","parentId":null,"summary":"# Summary\nKeep working"}`
	metrics := `{"type":"custom","id":"m","parentId":"c","customType":"sol-pi-online-context-metrics-v1","data":{"version":1,"compactionId":"c","estimated":true,"beforeTokens":80000,"afterTokens":24000}}`
	var live State
	if err := live.LoadEntries(json.RawMessage(`{"entries":[` + compact + `],"leafId":"c"}`)); err != nil {
		t.Fatal(err)
	}
	if len(live.Items) != 1 || live.Items[0].Compaction != nil {
		t.Fatal("old checkpoints must not invent metrics")
	}
	if !live.Apply(json.RawMessage(`{"type":"entry_appended","entry":` + metrics + `}`)) {
		t.Fatal("metrics append must refresh history")
	}
	var restored State
	for _, s := range []*State{&live, &restored} {
		if err := s.LoadEntries(json.RawMessage(`{"entries":[` + compact + `,` + metrics + `],"leafId":"m"}`)); err != nil {
			t.Fatal(err)
		}
		if len(s.Items) != 1 || s.MessageCount != 1 {
			t.Fatalf("display-only metadata added transcript records: %#v", s.Items)
		}
		m := s.Items[0].Compaction
		if m == nil || m.BeforeTokens != 80000 || m.AfterTokens != 24000 {
			t.Fatalf("missing checkpoint metrics: %#v", m)
		}
		// Metadata on another branch must not leak into an older checkpoint.
		if err := s.LoadEntries(json.RawMessage(`{"entries":[` + compact + `,` + metrics + `],"leafId":"c"}`)); err != nil {
			t.Fatal(err)
		}
		if s.Items[0].Compaction != nil {
			t.Fatal("off-branch metrics leaked")
		}
	}
}

func TestCompactionMetricsValidation(t *testing.T) {
	for _, tc := range []struct {
		data string
		ok   bool
	}{
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":0,"afterTokens":0}`, true},
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":10,"afterTokens":20}`, true},
		{`{"version":2,"compactionId":"c","estimated":true,"beforeTokens":10,"afterTokens":2}`, false},
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":10}`, false},
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":-10,"afterTokens":2}`, false},
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":10,"afterTokens":null}`, false},
		{`{"version":1,"compactionId":"c","estimated":true,"beforeTokens":10,"afterTokens":2.5}`, false},
		{`{"version":1,"compactionId":"","estimated":true,"beforeTokens":10,"afterTokens":2}`, false},
		{`{"version":1,"compactionId":"c","beforeTokens":10,"afterTokens":2}`, false},
		{`{`, false},
	} {
		e := entry{Type: "custom", CustomType: onlineMetricsEntry, Data: json.RawMessage(tc.data)}
		id, got := compactionMetricsFromEntry(e)
		if (got != nil) != tc.ok || (tc.ok && id != "c") {
			t.Errorf("%s: id=%q metrics=%#v", tc.data, id, got)
		}
	}
}
