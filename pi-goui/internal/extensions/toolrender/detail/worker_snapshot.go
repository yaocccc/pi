package detail

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"pi-goui/internal/core/session"
)

// WorkerSnapshot fixes the task order and identity shared by cards and inspectors.
// Authoritative snapshots never consult legacy result text.
type WorkerSnapshot struct {
	Args, Details, Root map[string]any
	Authoritative       bool
	Tasks               []WorkerSlot
}

type WorkerSlot struct {
	Position     int
	Input, State map[string]any
	Result       any
	hasResult    bool
	Key          string // empty for missing or ambiguous identities
}

func ReadWorkerSnapshot(item session.Item) WorkerSnapshot {
	s := WorkerSnapshot{Args: decodeMap(item.Arguments), Details: decodeMap(item.Details)}
	s.Authoritative = stringField(s.Details, "kind") == "worker-ui"
	var inputs, states, results []any
	if s.Authoritative {
		inputs, _ = s.Details["tasks"].([]any)
	} else {
		s.Root = decodeMap([]byte(item.Text))
		inputs, _ = s.Args["tasks"].([]any)
		if len(inputs) == 0 {
			if task, ok := s.Args["task"].(map[string]any); ok {
				inputs = []any{task}
			}
		}
		states, _ = s.Root["tasks"].([]any)
		result, _ := s.Root["result"].(map[string]any)
		results, _ = result["results"].([]any)
		if len(results) == 0 {
			results, _ = s.Root["results"].([]any)
		}
	}
	for i := 0; i < max(len(inputs), len(states), len(results)); i++ {
		slot := WorkerSlot{Position: i}
		if i < len(inputs) {
			slot.Input, _ = inputs[i].(map[string]any)
		}
		if s.Authoritative && slot.Input == nil {
			continue
		}
		if i < len(states) {
			slot.State, _ = states[i].(map[string]any)
		}
		if i < len(results) {
			slot.Result = results[i]
			slot.hasResult = true
		}
		s.Tasks = append(s.Tasks, slot)
	}
	return s
}

// ResolveKeys is needed only for inspectable rows, not collapsed headers.
func (s *WorkerSnapshot) ResolveKeys() {
	counts := map[string]int{}
	for i := range s.Tasks {
		slot := &s.Tasks[i]
		field := func(k string) string {
			if _, ok := slot.State[k]; ok {
				return stringField(slot.State, k)
			}
			return stringField(slot.Input, k)
		}
		key := ""
		if id := field("taskId"); id != "" {
			key = "id:" + id
		} else if s.Authoritative {
			if index, ok := slot.Input["index"].(json.Number); ok {
				if n, err := index.Int64(); err == nil && n >= 0 {
					key = "index:" + stringField(s.Details, "batchId") + ":" + index.String()
				}
			}
		} else if objective := field("objective"); objective != "" {
			identity, _ := json.Marshal([]string{field("mode"), objective, field("title")})
			key = fmt.Sprintf("definition:%x", sha256.Sum256(identity))
		}
		slot.Key = key
		counts[key]++
	}
	for i := range s.Tasks {
		if counts[s.Tasks[i].Key] != 1 {
			s.Tasks[i].Key = ""
		}
	}
}
