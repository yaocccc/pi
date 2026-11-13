// Package worker adapts the optional Worker extension's RPC records for display.
// It never knows where the backend stores recordings.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"pi-goui/internal/rpc"
)

// Transcript is an opaque recording identifier, never a filesystem path.
type Transcript struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Incomplete bool   `json:"incomplete,omitempty"`
	Error      string `json:"error,omitempty"`
}

var transcriptID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (d Transcript) Valid() bool { return d.Version == 1 && transcriptID.MatchString(d.ID) }

type RPCClient interface {
	CallExtension(context.Context, string, any) (json.RawMessage, error)
}

// Cursor is opaque backend state. The host owns cancellation and polling;
// neither a directory, an offset nor an unvalidated local recording is accepted.
type Cursor struct {
	token   string
	partial bool
}
type Batch struct {
	Reset   bool              `json:"reset"`
	Records []json.RawMessage `json:"records"`
	More    bool              `json:"more"`
}

func (c Cursor) Partial() bool { return c.partial }
func (c Cursor) Read(ctx context.Context, client RPCClient, d Transcript) (Cursor, Batch, error) {
	var batch Batch
	if !d.Valid() {
		return c, batch, errors.New("invalid Worker transcript descriptor")
	}
	if client == nil {
		return c, batch, rpc.ErrExtensionUnavailable
	}
	request := struct {
		Descriptor Transcript `json:"descriptor"`
		Cursor     string     `json:"cursor,omitempty"`
	}{d, c.token}
	raw, err := client.CallExtension(ctx, "worker.transcript", request)
	if err != nil {
		return c, batch, err
	}
	var response struct {
		Batch
		Cursor  string `json:"cursor"`
		Partial bool   `json:"partial"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || !transcriptID.MatchString(response.Cursor) {
		return c, batch, errors.New("invalid Worker transcript response")
	}
	for _, record := range response.Records {
		var header struct{ Type string }
		if json.Unmarshal(record, &header) != nil || header.Type == "" {
			return c, batch, errors.New("invalid Worker transcript record")
		}
	}
	return Cursor{token: response.Cursor, partial: response.Partial}, response.Batch, nil
}
