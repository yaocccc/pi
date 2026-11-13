package solpi

import (
	"encoding/json"
	"testing"
)

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
		id, got := MetricsFromEntry("custom", MetricsEntryType, json.RawMessage(tc.data))
		if (got != nil) != tc.ok || (tc.ok && id != "c") {
			t.Errorf("%s: id=%q metrics=%#v", tc.data, id, got)
		}
	}
}
