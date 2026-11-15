package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPromptResultProcess(t *testing.T) {
	mode := os.Getenv("GOUI_PROMPT_RESULT")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		data := map[string]any{}
		response := map[string]any{"type": "response", "id": request["id"], "command": request["type"], "success": true, "data": data}
		if request["type"] == "clear_queue" {
			data["steering"] = []string{"queued"}
			data["followUp"] = []string{"later"}
		}
		if request["type"] == "prompt" {
			if mode == "unknown" {
				os.Exit(3)
			}
			if mode == "rejected" {
				response["success"] = false
				response["error"] = "preflight rejected"
			}
			data["disposition"] = "handled"
			data["message"] = request["message"]
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
	}
	os.Exit(0)
}

func TestSubmitPromptRetainsDispositionAndUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"handled", "rejected", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOUI_PROMPT_RESULT", mode)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			c, err := Start(Config{Bin: exe, NoSession: true, Args: []string{"-test.run=^TestPromptResultProcess$", "--"}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			restore, data, err := c.SubmitPrompt(ctx, map[string]any{"message": "hello"}, "draft", true)
			if mode == "unknown" {
				if err == nil || restore != "" || data != nil {
					t.Fatalf("unknown outcome retained resendable input: %q %s %v", restore, data, err)
				}
				return
			}
			if restore != "queued\n\nlater\n\ndraft" {
				t.Fatalf("restore=%q", restore)
			}
			if mode == "rejected" {
				var rejected *ResponseError
				if !errors.As(err, &rejected) {
					t.Fatalf("lost explicit rejection: %v", err)
				}
				return
			}
			var result struct{ Disposition, Message string }
			if err != nil || json.Unmarshal(data, &result) != nil || result.Disposition != "handled" || result.Message != "queued\n\nlater\n\nhello" {
				t.Fatalf("lost native disposition/prompt: %s %v", data, err)
			}
		})
	}
}
