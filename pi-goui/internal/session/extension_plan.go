package session

import (
	"encoding/json"
	"pi-goui/internal/extensions/solpi"
)

type PlanStep = solpi.Step

const onlinePlanEntry = solpi.EntryType
const updatePlanTool = solpi.UpdatePlanTool

func parsePlan(raw json.RawMessage) ([]PlanStep, bool) { return solpi.ParsePlan(raw) }
func planFromEntry(e entry) ([]PlanStep, bool) {
	return solpi.PlanFromEntry(e.Type, e.CustomType, e.Data)
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
