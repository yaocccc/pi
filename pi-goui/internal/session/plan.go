package session

import "encoding/json"

// PlanStep is a read-only projection of SoL-Pi's current working plan.
type PlanStep struct {
	ID     string `json:"id"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

const onlinePlanEntry = "sol-pi-online-context-state-v1"

func parsePlan(raw json.RawMessage) ([]PlanStep, bool) {
	var v struct {
		Plan json.RawMessage `json:"plan"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Plan) == 0 || string(v.Plan) == "null" {
		return nil, false
	}
	var steps []PlanStep
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

func planFromEntry(e entry) ([]PlanStep, bool) {
	if e.Type != "custom" || e.CustomType != onlinePlanEntry {
		return nil, false
	}
	var v struct{ Version int }
	if json.Unmarshal(e.Data, &v) != nil || v.Version != 1 {
		return nil, false
	}
	return parsePlan(e.Data)
}

func (s *State) setPlan(steps []PlanStep) {
	if len(s.Plan) == len(steps) {
		equal := true
		for i := range steps {
			if steps[i] != s.Plan[i] {
				equal = false
				break
			}
		}
		if equal {
			return
		}
	}
	s.Plan = steps
	s.PlanRevision++
}
