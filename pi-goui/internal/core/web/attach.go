package web

import (
	"context"
	"errors"
	"net/http"
	"os"
)

// AttachOptions connects a real desktop terminal to an already selected project.
// The caller owns the host; other viewers share its output and input without
// disconnecting this local display.
// Input and Output must be terminals. They are restored before Attach returns.
type AttachOptions struct {
	URL, Token, ProjectID string
	Input, Output         *os.File
	HTTPClient            *http.Client // Optional private Unix-socket transport.
}

// AttachResult distinguishes a normal TUI exit from losing control. Attach does
// not reconnect automatically: doing so would fight the browser for ownership.
type AttachResult struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

var errTerminalRequired = errors.New("shared desktop mode requires terminal stdin and stdout")

// Attach streams the existing PTY, not a second Bubble Tea or Pi process.
func Attach(ctx context.Context, opts AttachOptions) (AttachResult, error) {
	return attach(ctx, opts)
}

// WaitForRetake waits for Enter in the restored, cooked terminal. EOF returns
// false; context cancellation interrupts even a terminal with no pending input.
func WaitForRetake(ctx context.Context, input *os.File) (bool, error) {
	return waitForRetake(ctx, input)
}

// CheckAttachTerminal rejects unsupported platforms or redirected stdio before
// the host opens a listener or starts any child process.
func CheckAttachTerminal(input, output *os.File) error {
	return checkAttachTerminal(input, output)
}
