package session

import "encoding/json"

const onlineMetricsEntry = "sol-pi-online-context-metrics-v1"

// CompactionMetrics are content estimates, not provider usage or billed tokens.
// They describe the same committed history immediately before and after its cut.
type CompactionMetrics struct {
	BeforeTokens int
	AfterTokens  int
}

func compactionMetricsFromEntry(e entry) (string, *CompactionMetrics) {
	if e.Type != "custom" || e.CustomType != onlineMetricsEntry {
		return "", nil
	}
	var data struct {
		Version      int    `json:"version"`
		CompactionID string `json:"compactionId"`
		Estimated    bool   `json:"estimated"`
		BeforeTokens *int   `json:"beforeTokens"`
		AfterTokens  *int   `json:"afterTokens"`
	}
	if json.Unmarshal(e.Data, &data) != nil || data.Version != 1 || data.CompactionID == "" || !data.Estimated ||
		data.BeforeTokens == nil || data.AfterTokens == nil || *data.BeforeTokens < 0 || *data.AfterTokens < 0 {
		return "", nil
	}
	return data.CompactionID, &CompactionMetrics{BeforeTokens: *data.BeforeTokens, AfterTokens: *data.AfterTokens}
}
