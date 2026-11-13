package worker

import (
	"context"
	"encoding/json"
)

type fakeRPC func(context.Context, string, any) (json.RawMessage, error)

func (f fakeRPC) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return f(ctx, method, params)
}

const transcriptTestID = "12345678-1234-4234-8234-123456789abc"
