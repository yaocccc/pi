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
// normal Pi prompt. Shared clients execute both operations as one executor
// transaction. Unknown outcomes return no draft to accidentally resend.
func (c *Client) SubmitPrompt(ctx context.Context, payload map[string]any, draft string, mergeQueued bool) (string, error) {
	return c.submitPrompt(ctx, payload, draft, mergeQueued, nil)
}
func (c *Client) submitPrompt(ctx context.Context, payload map[string]any, draft string, mergeQueued bool, sent func()) (string, error) {
	if c.Shared() {
		raw, err := c.Call(ctx, map[string]any{"type": "goui_submit_prompt", "payload": payload, "draft": draft, "mergeQueued": mergeQueued})
		if err != nil {
			return "", fmt.Errorf("prompt outcome unknown; not retried: %w", err)
		}
		var reply struct {
			RestoreDraft string
			Error        string
		}
		if err = json.Unmarshal(raw, &reply); err != nil {
			return "", fmt.Errorf("prompt outcome unknown; invalid executor reply: %w", err)
		}
		if reply.Error != "" {
			return reply.RestoreDraft, errors.New(reply.Error)
		}
		return reply.RestoreDraft, nil
	}
	command := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		command[k] = v
	}
	command["type"] = "prompt"
	message, ok := command["message"].(string)
	if !ok {
		return draft, errors.New("prompt requires a string message")
	}
	restore := draft
	if mergeQueued {
		raw, err := c.Call(ctx, map[string]any{"type": "clear_queue"})
		if err != nil {
			var rejected *ResponseError
			if errors.As(err, &rejected) {
				return draft, err
			}
			return "", fmt.Errorf("queue outcome unknown; not retried: %w", err)
		}
		var queue struct{ Steering, FollowUp []string }
		if err = json.Unmarshal(raw, &queue); err != nil {
			return "", fmt.Errorf("removed queue unavailable; not retried: %w", err)
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
	_, err := c.call(ctx, command, sent)
	if err != nil {
		var rejected *ResponseError
		if errors.As(err, &rejected) {
			return restore, err
		}
		return "", fmt.Errorf("prompt outcome unknown; not retried: %w", err)
	}
	return restore, nil
}
