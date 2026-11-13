// Package extensionapi defines host-neutral UI contracts, not extension behavior.
package extensionapi

import (
	"encoding/json"
	"pi-goui/internal/config"
)

type DetailHooks struct {
	Line       func(text, color string, width int) []string
	Clean      func(string) string
	CodeRows   func(source, path, language string, start, width int, theme config.Theme) []string
	DiffRows   func(diff string, width int, theme config.Theme) []string
	DiffCounts func(diff string) (added, removed int)
}

// DetailHost delegates generic unabridged formatting to the host renderer.
type DetailHost struct {
	Theme   config.Theme
	Width   int
	Hooks   DetailHooks
	Rows    *[]string
	Line    func(string, string)
	Heading func(string)
	Output  func(string)
	Value   func(any, string)
	Field   func(map[string]any, string, string)
	Scalar  func(any) string
}

type Block struct {
	ID, Role string
	// InspectorParent/Task identify an isolated Worker task independently of
	// transcript positions or the mutable order of snapshot arrays.
	InspectorParent, InspectorTask string
	MessageIndex                   int
	Start, End                     int
	Foldable                       bool
	// Expanded is the effective state after global and per-ID overrides.
	// toolGroup ranges cover only their header; children have separate ranges.
	Expanded bool
}

type CardOptions struct {
	ExpandTools bool
	Expanded    map[string]bool
}

// CardHost provides only generic terminal primitives; adapters own layouts.
type CardHost struct {
	Map      func(json.RawMessage) map[string]any
	String   func(map[string]any, ...string) string
	Paint    func(string, string) string
	Clean    func(string) string
	OneLine  func(string) string
	Fit      func(string, int) string
	Wrap     func(string, int) []string
	Badge    func(string, string, int, int, bool) string
	Tokens   func(int, config.Theme) string
	Expanded func(string, CardOptions) bool
	Compact  func([]string, string, int) ([]string, int)
	Frame    func([]string, string, int, string, bool, int, ...config.Theme) []string
}
