package ui

import (
	"context"
	"encoding/json"
	"strings"
)

// Take the authoritative pending text, not a possibly stale UI snapshot: text
// already consumed by the agent must never be submitted a second time.
func mergeQueuedPrompt(ctx context.Context, call func(context.Context, map[string]any) (json.RawMessage, error), payload map[string]any, draft string) (string, error) {
	data, err := call(ctx, map[string]any{"type": "clear_queue"})
	if err != nil {
		return draft, err
	}
	var queue struct{ Steering, FollowUp []string }
	if err := json.Unmarshal(data, &queue); err != nil {
		return draft, err
	}
	pending := append(queue.Steering, queue.FollowUp...)
	if len(pending) == 0 {
		return draft, nil
	}
	// Appending does not change when the existing message should be delivered.
	if len(queue.Steering) > 0 {
		payload["streamingBehavior"] = "steer"
	} else {
		payload["streamingBehavior"] = "followUp"
	}
	prefix := strings.Join(pending, "\n\n") + "\n\n"
	payload["message"] = prefix + payload["message"].(string)
	// Restore the entire removed queue if the subsequent prompt fails.
	return prefix + draft, nil
}
