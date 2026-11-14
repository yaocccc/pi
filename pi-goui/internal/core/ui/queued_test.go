package ui

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	rpc "pi-goui/internal/runtime"
)

// A native RPC fixture, not a second implementation of queue merging. The
// runtime owns clear_queue + prompt sequencing and authoritative queue text.
func TestQueuedRPCProcess(t *testing.T) {
	if os.Getenv("PIGOUI_UI_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req map[string]any
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		f, err := os.OpenFile(os.Getenv("PIGOUI_UI_WIRE"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(2)
		}
		_ = json.NewEncoder(f).Encode(req)
		_ = f.Close()
		kind := req["type"]
		if kind == "prompt" && os.Getenv("GOUI_QUEUE_FAILURE") == "disconnect" {
			os.Exit(0) // Written, but no acknowledgement: never retry in the UI.
		}
		data := json.RawMessage(`{}`)
		if kind == "clear_queue" {
			data = json.RawMessage(os.Getenv("GOUI_QUEUE_TEXT"))
		}
		response := map[string]any{"type": "response", "id": req["id"], "command": kind, "success": true, "data": data}
		if kind == os.Getenv("GOUI_QUEUE_FAILURE") {
			response["success"], response["error"] = false, "fake rejection"
		}
		if encoder.Encode(response) != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func queuedClient(t *testing.T, queue, failure string) (*rpc.Client, string) {
	t.Helper()
	t.Setenv("GOUI_QUEUE_TEXT", queue)
	t.Setenv("GOUI_QUEUE_FAILURE", failure)
	cfg, wire := fakeConfig(t)
	wrapper, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Bin, []byte(strings.Replace(string(wrapper), "TestUIRPCProcess", "TestQueuedRPCProcess", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, wire
}

func TestQueuedSubmissionUsesRuntime(t *testing.T) {
	for _, tc := range []struct {
		name, queue, want, behavior string
		queued, follow, busy        bool
	}{
		{"steering", `{"steering":["first","second"]}`, "first\n\nsecond\n\nnew", "steer", true, false, true},
		{"follow-up queue", `{"followUp":["first"]}`, "first\n\nnew", "followUp", true, false, true},
		{"both", `{"steering":["first"],"followUp":["second"]}`, "first\n\nsecond\n\nnew", "steer", true, false, true},
		{"consumed", `{}`, "new", "steer", true, false, true},
		{"explicit follow-up", `{"steering":["first"]}`, "first\n\nnew", "steer", true, true, true},
		{"no queued text", `{}`, "new", "steer", false, false, true},
		{"idle", `{}`, "new", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			client, wire := queuedClient(t, tc.queue, "")
			tab.client, tab.state.Busy = client, tc.busy
			if tc.queued {
				tab.state.Steering = []string{"stale UI text must not be sent"}
			}
			tab.setValue(" new [image1] \n")
			tab.images = []map[string]any{{"type": "image", "data": "YQ==", "mimeType": "image/png"}}
			reply := m.submit(tab, tc.follow)().(replyMsg)
			if reply.err != nil {
				t.Fatal(reply.err)
			}
			m.Update(reply)
			n := 1
			if tc.busy {
				n++
			}
			records := wireRecords(t, wire, n)
			if len(records) != n || n == 2 && records[0]["type"] != "clear_queue" {
				t.Fatalf("unexpected RPCs: %v", records)
			}
			prompt := records[n-1]
			if prompt["type"] != "prompt" || prompt["message"] != tc.want+" [image1]" {
				t.Fatalf("wrong prompt: %v", prompt)
			}
			if behavior, _ := prompt["streamingBehavior"].(string); behavior != tc.behavior {
				t.Fatalf("behavior = %q, want %q", behavior, tc.behavior)
			}
			images, ok := prompt["images"].([]any)
			if !ok || len(images) != 1 || images[0].(map[string]any)["data"] != "YQ==" {
				t.Fatalf("images lost: %v", prompt)
			}
			if tab.editor.Value() != "" || len(tab.images) != 0 || len(tab.history) != 1 || tab.history[0] != strings.TrimSuffix(tc.want, "new")+" new [image1] \n" {
				t.Fatal("accepted submission did not consume local draft exactly once")
			}
		})
	}
}

func TestQueuedSubmissionFailureRestoresDraftWithoutRetry(t *testing.T) {
	for _, failure := range []string{"clear_queue", "prompt", "disconnect"} {
		for _, edited := range []bool{false, true} {
			t.Run(failure+map[bool]string{false: "/unchanged", true: "/new-draft"}[edited], func(t *testing.T) {
				m := testModel(t, "")
				tab := m.current()
				client, wire := queuedClient(t, `{"steering":["first"]}`, failure)
				tab.client, tab.state.Busy = client, true
				tab.state.Steering = []string{"stale"}
				tab.setValue(" new [image1] \n")
				tab.images = []map[string]any{{"data": "YQ==", "mimeType": "image/png"}}
				cmd := m.submit(tab, false)
				if edited {
					tab.setValue("newer local draft")
					tab.images = []map[string]any{{"data": "newer image"}}
				}
				reply := cmd().(replyMsg)
				if reply.err == nil {
					t.Fatal("missing rejection/disconnection")
				}
				m.Update(reply)
				want, image := " new [image1] \n", "YQ=="
				if failure == "prompt" {
					want = "first\n\n" + want
				}
				if edited {
					want, image = "newer local draft", "newer image"
				}
				if tab.editor.Value() != want || len(tab.images) != 1 || tab.images[0]["data"] != image {
					t.Fatalf("recovery overwrote/lost draft: %q %v", tab.editor.Value(), tab.images)
				}
				if tab.sending || tab.awaitingStart || len(tab.history) != 0 || tab.toast.text == "" {
					t.Fatal("failure missing toast or left send state/history changed")
				}
				n := 2
				if failure == "clear_queue" {
					n = 1
				}
				if records := wireRecords(t, wire, n); len(records) != n {
					t.Fatalf("failure retried prompt: %v", records)
				}
			})
		}
	}
}
