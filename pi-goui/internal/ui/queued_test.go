package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMergeQueuedPrompt(t *testing.T) {
	for _, tc := range []struct{ name, queue, want, behavior string }{
		{"steering", `{"steering":["first","second"]}`, "first\n\nsecond\n\nnew", "steer"},
		{"follow-up", `{"followUp":["first"]}`, "first\n\nnew", "followUp"},
		{"consumed", `{}`, "new", "steer"},
		{"both", `{"steering":["first"],"followUp":["second"]}`, "first\n\nsecond\n\nnew", "steer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"message": "new", "streamingBehavior": "steer", "images": "preserved"}
			draft, err := mergeQueuedPrompt(context.Background(), func(_ context.Context, command map[string]any) (json.RawMessage, error) {
				if command["type"] != "clear_queue" {
					t.Fatal(command)
				}
				return json.RawMessage(tc.queue), nil
			}, payload, "new")
			if err != nil || draft != tc.want || payload["message"] != tc.want || payload["streamingBehavior"] != tc.behavior || payload["images"] != "preserved" {
				t.Fatalf("draft=%q payload=%v err=%v", draft, payload, err)
			}
		})
	}
}

func TestMergeQueuedPromptFailure(t *testing.T) {
	payload := map[string]any{"message": "new"}
	draft, err := mergeQueuedPrompt(context.Background(), func(context.Context, map[string]any) (json.RawMessage, error) {
		return nil, errors.New("offline")
	}, payload, "original")
	if err == nil || draft != "original" || payload["message"] != "new" {
		t.Fatalf("%q %v %v", draft, payload, err)
	}
}
