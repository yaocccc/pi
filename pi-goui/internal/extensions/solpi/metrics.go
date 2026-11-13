package solpi

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
)

const MetricsEntryType = "sol-pi-online-context-metrics-v1"

// CompactionMetrics describe estimated content, not billed provider usage.
type CompactionMetrics = extensionapi.CompactionMetrics

func MetricsFromEntry(entryType, customType string, raw json.RawMessage) (string, *CompactionMetrics) {
	if entryType != "custom" || customType != MetricsEntryType {
		return "", nil
	}
	var data struct {
		Version      int    `json:"version"`
		CompactionID string `json:"compactionId"`
		Estimated    bool   `json:"estimated"`
		BeforeTokens *int   `json:"beforeTokens"`
		AfterTokens  *int   `json:"afterTokens"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Version != 1 || data.CompactionID == "" || !data.Estimated ||
		data.BeforeTokens == nil || data.AfterTokens == nil || *data.BeforeTokens < 0 || *data.AfterTokens < 0 {
		return "", nil
	}
	return data.CompactionID, &CompactionMetrics{BeforeTokens: *data.BeforeTokens, AfterTokens: *data.AfterTokens}
}
