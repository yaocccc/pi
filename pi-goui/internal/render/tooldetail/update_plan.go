package tooldetail

import (
	"fmt"
	"reflect"
	"strings"

	"pi-goui/internal/session"
)

// updatePlan uses the recorded plan, never promotes an argument preview to a result.
func (v *renderer) updatePlan(item session.Item, args, details map[string]any) {
	plan, recorded := details["plan"]
	if !recorded {
		plan = args["steps"]
	}
	if recorded {
		v.heading("Plan")
	} else {
		v.heading("Plan preview · not recorded")
	}
	if steps, ok := plan.([]any); ok {
		for i, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				v.value(value, "")
				continue
			}
			rest := copyMap(step)
			status, _ := step["status"].(string)
			marker, color := "?", v.theme.Muted
			switch status {
			case "completed":
				marker, color = "✓", v.theme.Success
			case "in_progress":
				marker, color = "●", v.theme.Warning
			case "pending":
				marker = "○"
			}
			v.line(fmt.Sprintf("%s %d · %s", marker, i+1, status), color)
			if _, ok := step["status"].(string); ok {
				delete(rest, "status")
			}
			if goal, ok := step["goal"].(string); ok {
				v.line(goal, v.theme.Text)
				delete(rest, "goal")
			}
			if id, ok := step["id"].(string); ok {
				v.line("id · "+id, v.theme.Muted)
				delete(rest, "id")
			}
			if len(rest) > 0 {
				v.value(rest, "")
			}
		}
		if len(steps) == 0 {
			v.line("(empty plan)", v.theme.Muted)
		}
	} else if recorded || plan != nil {
		v.value(plan, "")
	}
	if recorded {
		delete(details, "plan")
	}
	params := copyMap(args)
	if reflect.DeepEqual(args["steps"], plan) {
		delete(params, "steps")
	}
	if progress, ok := params["progress"]; ok {
		v.heading("Progress evidence · submitted")
		v.value(progress, "")
		delete(params, "progress")
	}
	if len(params) > 0 {
		v.heading("Parameters")
		v.value(params, "")
	}
	activeSnapshot := details["task_status"] == "active"
	v.field(details, "task_status", "Task status")
	v.field(details, "boundary", "Completion boundary")
	v.field(details, "completed_step_ids", "Completed step IDs")
	v.field(details, "progress_recorded", "Progress recorded")
	// Only remove the exact machine snapshot already represented above. Advice
	// after it, unfamiliar envelopes and extra snapshot fields stay visible.
	text := item.Text
	const prefix = `<sol-pi-plan task_status="active">`
	const suffix = "</sol-pi-plan>"
	if recorded && activeSnapshot && strings.HasPrefix(text, prefix) {
		if end := strings.Index(text, suffix); end >= len(prefix) {
			snapshot := decodeMap([]byte(text[len(prefix):end]))
			if len(snapshot) == 1 && reflect.DeepEqual(snapshot["steps"], plan) {
				text = strings.TrimPrefix(text[end+len(suffix):], "\n")
			}
		}
	}
	v.output(text)
}
