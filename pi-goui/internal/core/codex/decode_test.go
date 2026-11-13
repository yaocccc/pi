package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWeeklyWindowSelectionAndDisplayOptOut(t *testing.T) {
	now := time.Unix(1700000000, 0)
	weekly := func(used any) map[string]any {
		return map[string]any{"used_percent": used, "reset_at": 1800000000, "limit_window_seconds": 604800}
	}
	for _, slot := range []string{"primary_window", "secondary_window"} {
		for _, used := range []any{19.8, "19.8", -5, 105} {
			other := "primary_window"
			if slot == other {
				other = "secondary_window"
			}
			root := map[string]any{"usageDisplay": map[string]any{"show": true}, "rate_limit": map[string]any{slot: weekly(used), other: map[string]any{"limit_window_seconds": 18000, "used_percent": 99, "reset_at": 1800000000}}}
			data, _ := json.Marshal(root)
			s, err := decode(data, now)
			if err != nil || s.RemainingPercent == nil || s.ResetAt.Unix() != 1800000000 {
				t.Fatalf("weekly %s: %+v %v", slot, s, err)
			}
			want := 80.2
			if used == -5 {
				want = 100
			}
			if used == 105 {
				want = 0
			}
			if *s.RemainingPercent != want {
				t.Fatalf("remaining=%v want %v", *s.RemainingPercent, want)
			}
		}
	}
	for _, raw := range []string{
		`{"rate_limit":{"primary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":18000}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":604801}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":null,"reset_at":1800000000,"limit_window_seconds":604800}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":"NaN","reset_at":1800000000,"limit_window_seconds":604800}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":true,"reset_at":1800000000,"limit_window_seconds":604800}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":null,"limit_window_seconds":604800}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1e300,"limit_window_seconds":604800}}}`,
		`{"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":604800,"usageDisplay":{"show":false}}}}`,
		`{"rate_limit":null,"credits":null}`,
		`{"usageDisplay":{"show":null},"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":604800}}}`,
		`{"usageDisplay":{"show":"true"},"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":604800}}}`,
		`{"usageDisplay":null,"rate_limit":{"secondary_window":{"used_percent":10,"reset_at":1800000000,"limit_window_seconds":604800}}}`,
	} {
		s, err := decode([]byte(raw), now)
		if err != nil || s.RemainingPercent != nil || !s.ResetAt.IsZero() {
			t.Fatalf("invalid/nonweekly quota was displayed: %+v %v", s, err)
		}
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(goodPayload), &payload)
	payload["usageDisplay"] = map[string]any{"show": false}
	data, _ := json.Marshal(payload)
	s, err := decode(data, now)
	if err != nil || s.RemainingPercent != nil || s.Credits != nil {
		t.Fatal("global response opt-out ignored")
	}
}
func TestCreditShapesAndUnknowns(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		balance   *float64
		unlimited bool
	}{
		{`{"has_credits":true,"balance":82334}`, ptr(82334), false},
		{`{"has_credits":true,"balance":"82334.5"}`, ptr(82334.5), false},
		{`{"has_credits":false,"balance":"0"}`, ptr(0), false},
		{`{"has_credits":false}`, nil, false},
		{`{"has_credits":false,"balance":88}`, nil, false},
		{`{"has_credits":true}`, nil, false},
		{`{"balance":null}`, nil, false},
		{`{"balance":true}`, nil, false},
		{`{"balance":""}`, nil, false},
		{`{"balance":"Infinity"}`, nil, false},
		{`{"balance":-1}`, nil, false},
		{`{"unlimited":true,"balance":999}`, nil, true},
		{`{"unlimited":true}`, nil, true},
		{`{"balance":5,"usageDisplay":{"show":false}}`, nil, false},
	} {
		s, err := decode([]byte(`{"credits":`+tc.raw+`}`), time.Unix(1700000000, 0))
		if err != nil {
			t.Fatal(err)
		}
		if (s.Credits == nil) != (tc.balance == nil) || s.Credits != nil && *s.Credits != *tc.balance || s.UnlimitedCredits != tc.unlimited {
			t.Fatalf("credit %s => %+v", tc.raw, s)
		}
	}
}
func ptr(v float64) *float64 { return &v }
