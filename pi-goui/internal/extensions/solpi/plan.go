// Package solpi projects optional SoL-Pi data for display. It never compacts
// context, mutates plans, or reads the extension's private configuration.
package solpi

import "encoding/json"

// Step is a read-only projection of SoL-Pi's current working plan.
type Step struct {
	ID     string `json:"id"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

const EntryType = "sol-pi-online-context-state-v1"
const UpdatePlanTool = "update_plan"

func ParsePlan(raw json.RawMessage) ([]Step, bool) {
	var v struct {
		Plan json.RawMessage `json:"plan"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Plan) == 0 || string(v.Plan) == "null" {
		return nil, false
	}
	var steps []Step
	if json.Unmarshal(v.Plan, &steps) != nil {
		return nil, false
	}
	ids := map[string]bool{}
	for _, step := range steps {
		if step.ID == "" || step.Goal == "" || ids[step.ID] {
			return nil, false
		}
		switch step.Status {
		case "pending", "in_progress", "completed":
		default:
			return nil, false
		}
		ids[step.ID] = true
	}
	return steps, true
}

func PlanFromEntry(entryType, customType string, data json.RawMessage) ([]Step, bool) {
	if entryType != "custom" || customType != EntryType {
		return nil, false
	}
	var v struct{ Version int }
	if json.Unmarshal(data, &v) != nil || v.Version != 1 {
		return nil, false
	}
	return ParsePlan(data)
}
