package codex

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func number(raw json.RawMessage) (float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var text string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return 0, false
		}
		text = strings.TrimSpace(text)
	} else {
		text = string(raw)
	}
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func object(raw json.RawMessage) map[string]json.RawMessage {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}
func trueValue(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func decode(data []byte, now time.Time) (Snapshot, error) {
	snapshot := Snapshot{FetchedAt: now}
	root := object(data)
	if root == nil {
		return Snapshot{}, errPayload
	}
	// A missing display flag matches the existing pi extension's default. An
	// explicit false opt-out never becomes a numeric quota or credit estimate.
	if !enabled(root["usageDisplay"]) {
		return snapshot, nil
	}
	limits := object(root["rate_limit"])
	if enabled(limits["usageDisplay"]) {
		for _, key := range []string{"secondary_window", "primary_window"} {
			window := object(limits[key])
			if window == nil || !enabled(window["usageDisplay"]) {
				continue
			}
			seconds, ok := number(window["limit_window_seconds"])
			if !ok || seconds != 604800 {
				continue
			}
			used, ok := number(window["used_percent"])
			if !ok {
				continue
			}
			reset, ok := number(window["reset_at"])
			if !ok || reset <= 0 || reset > 253402300799 {
				continue
			}
			remaining := 100 - math.Min(100, math.Max(0, used))
			snapshot.RemainingPercent = &remaining
			snapshot.ResetAt = time.Unix(int64(reset), 0)
			break
		}
	}
	if credits := object(root["credits"]); credits != nil && enabled(credits["usageDisplay"]) {
		if trueValue(credits["unlimited"]) {
			snapshot.UnlimitedCredits = true
		} else if balance, ok := number(credits["balance"]); ok && balance >= 0 {
			// has_credits=false may accompany a zero balance; contradictory positive
			// balances are unknown rather than fabricated as either positive or zero.
			if raw, exists := credits["has_credits"]; !exists || trueValue(raw) || balance == 0 {
				snapshot.Credits = &balance
			}
		}
	}
	return snapshot, nil
}
