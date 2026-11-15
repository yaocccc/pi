package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ResponseError is an explicit negative Pi acknowledgement, as opposed to an
// unknown outcome (timeout, EOF or interrupted write). No request is retried.
type ResponseError struct{ Command, Message string }

func (e *ResponseError) Error() string { return fmt.Sprintf("pi %s: %s", e.Command, e.Message) }

// SubmitPrompt merges and consumes the authoritative queue before sending a
// normal Pi prompt on the exclusively owned backend. Unknown outcomes return
// no draft to accidentally resend. The returned native response retains prompt
// disposition (including extension-handled/queued input) for lifecycle decisions.
func (c *Client) SubmitPrompt(ctx context.Context, payload map[string]any, draft string, mergeQueued bool) (string, json.RawMessage, error) {
	command := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		command[k] = v
	}
	command["type"] = "prompt"
	message, ok := command["message"].(string)
	if !ok {
		return draft, nil, errors.New("prompt requires a string message")
	}
	restore := draft
	if mergeQueued {
		raw, err := c.Call(ctx, map[string]any{"type": "clear_queue"})
		if err != nil {
			var rejected *ResponseError
			if errors.As(err, &rejected) {
				return draft, nil, err
			}
			return "", nil, fmt.Errorf("queue outcome unknown; not retried: %w", err)
		}
		var queue struct{ Steering, FollowUp []string }
		if err = json.Unmarshal(raw, &queue); err != nil {
			return "", nil, fmt.Errorf("removed queue unavailable; not retried: %w", err)
		}
		pending := append(queue.Steering, queue.FollowUp...)
		if len(pending) > 0 {
			prefix := strings.Join(pending, "\n\n") + "\n\n"
			command["message"] = prefix + message
			restore = prefix + draft
			if len(queue.Steering) > 0 {
				command["streamingBehavior"] = "steer"
			} else {
				command["streamingBehavior"] = "followUp"
			}
		}
	}
	data, err := c.Call(ctx, command)
	if err != nil {
		var rejected *ResponseError
		if errors.As(err, &rejected) {
			return restore, nil, err
		}
		return "", nil, fmt.Errorf("prompt outcome unknown; not retried: %w", err)
	}
	return restore, data, nil
}
