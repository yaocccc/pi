package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const extensionCommand = "pi-ui-rpc"
const extensionReplyPrefix = "pi-ui:rpc:v1:"

// ErrExtensionUnavailable means the optional bridge or requested capability is absent.
var ErrExtensionUnavailable = errors.New("extension unavailable")

type ExtensionError struct{ Code, Message string }

func (e *ExtensionError) Error() string { return e.Message }
func (e *ExtensionError) Is(target error) bool {
	return target == ErrExtensionUnavailable && e.Code == "unavailable"
}

// CallExtension uses only standard Pi RPC. Discovery is checked before sending
// a slash command, so a user without the optional bridge never prompts the LLM.
// The bridge is generic; this transport knows no extension-specific operations.
func (c *Client) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := c.Call(ctx, map[string]any{"type": "get_commands"})
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Commands []struct{ Name, Source string }
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	available := false
	for _, command := range catalog.Commands {
		if command.Name == extensionCommand && command.Source == "extension" {
			available = true
			break
		}
	}
	if !available {
		return nil, ErrExtensionUnavailable
	}
	id := fmt.Sprintf("goui-ext-%d", c.next.Add(1))
	body, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{id, method, params})
	if err != nil {
		return nil, err
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	if c.extensionPending == nil {
		c.extensionPending = make(map[string]chan reply)
	}
	c.extensionPending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.extensionPending, id); c.mu.Unlock() }()
	_, err = c.Call(ctx, map[string]any{"type": "prompt", "message": "/" + extensionCommand + " " + string(body)})
	if err != nil {
		select {
		case result := <-ch:
			return result.data, result.err
		default:
			return nil, err
		}
	}
	select {
	case result := <-ch:
		return result.data, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stop:
		return nil, c.failure()
	case <-c.readDone:
		select {
		case result := <-ch:
			return result.data, result.err
		default:
			return nil, c.failure()
		}
	}
}

// Consume only our correlated protocol envelopes. Other extension notifications
// continue through the ordinary event stream unchanged.
func (c *Client) extensionReply(raw json.RawMessage) bool {
	var event struct{ Type, Method, Message string }
	if json.Unmarshal(raw, &event) != nil || event.Type != "extension_ui_request" || event.Method != "notify" || !strings.HasPrefix(event.Message, extensionReplyPrefix) {
		return false
	}
	var response struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *ExtensionError `json:"error"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(event.Message, extensionReplyPrefix)), &response) != nil || !strings.HasPrefix(response.ID, "goui-ext-") {
		return false
	}
	c.mu.Lock()
	ch := c.extensionPending[response.ID]
	c.mu.Unlock()
	if ch != nil {
		result := reply{data: response.Result}
		if response.Error != nil {
			result.err = response.Error
		}
		select {
		case ch <- result:
		default:
		}
	}
	// Late responses for cancelled calls are ours too, not user notifications.
	return true
}
