package render

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	frames "pi-goui/internal/core/render/frame"
	"pi-goui/internal/core/session"
)

// Native dist/extensions/codemode/execute.js publishes details.calls snapshots.
// Each record contains id/name/args/status plus optional durationMs/error/cost;
// args is a preformatted string, not the original argument object.
type codemodeCall struct {
	Name, Args, Status, Error string
	DurationMS                *float64 `json:"durationMs"`
	Cost                      *float64
}

var codemodeHeader = regexp.MustCompile(`^Script (completed|failed)\nWall time ([0-9]+(?:\.[0-9]+)?) seconds\nOutput:\n`)

func codemodeArgs(raw string) string {
	if raw == "" {
		return ""
	}
	value, ok := visualJSON([]byte(raw))
	if !ok {
		if strings.HasPrefix(strings.TrimSpace(raw), "{") || strings.HasPrefix(strings.TrimSpace(raw), "[") {
			return "parameters (incomplete preview)"
		}
		return oneLine(raw)
	}
	switch args := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			switch v := args[k].(type) {
			case map[string]any:
				parts = append(parts, oneLine(k)+fmt.Sprintf(": %d fields", len(v)))
			case []any:
				parts = append(parts, oneLine(k)+fmt.Sprintf(": %d items", len(v)))
			default:
				parts = append(parts, oneLine(k)+": "+oneLine(visualScalar(v)))
			}
		}
		return strings.Join(parts, " · ")
	case []any:
		return fmt.Sprintf("%d argument items", len(args))
	default:
		return oneLine(visualScalar(value))
	}
}
func finiteNonnegative(v *float64) bool {
	return v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}
func codemodeCost(cost float64) string {
	if cost >= .01 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.2g", cost)
}

func codemodeRows(item session.Item, theme config.Theme, width int) []string {
	if width <= 0 {
		return nil
	}
	args := visualMap(item.Arguments)
	var rows []string
	code, valid := args["code"].(string)
	if !valid {
		code, valid = args["script"].(string)
	}
	if valid {
		// Unlike file previews, script rows deliberately have no line-number gutter.
		highlighted := highlightedLines(code, "", "javascript", theme)
		expected := strings.Count(cleanCode(code), "\n") + 1
		if len(highlighted) > expected {
			highlighted = highlighted[:expected]
		}
		for _, line := range highlighted {
			rows = append(rows, visualWrap(line, width)...)
		}
	} else {
		rows = append(rows, visualWrap(paint("Script unavailable · incomplete arguments", theme.Muted), width)...)
	}
	appendSection := func(section []string) {
		if len(section) == 0 {
			return
		}
		if len(rows) > 0 && rows[len(rows)-1] != "" {
			rows = append(rows, "")
		}
		rows = append(rows, section...)
	}
	var details struct {
		Calls          []json.RawMessage
		FullOutputPath string
	}
	if len(item.Details) > 0 && string(item.Details) != "null" {
		if json.Unmarshal(item.Details, &details) != nil {
			appendSection(visualWrap(paint("Call trace unavailable · invalid metadata", theme.Muted), width))
		}
	}
	var calls []string
	priced, total := 0, 0.0
	for _, raw := range details.Calls {
		var call codemodeCall
		if json.Unmarshal(raw, &call) != nil || call.Name == "" {
			calls = append(calls, visualWrap(paint("? Call trace entry unavailable", theme.Muted), width)...)
			continue
		}
		symbol, color := "○", theme.Muted
		switch call.Status {
		case "ok":
			symbol, color = "✓", theme.Success
		case "running":
			symbol, color = "⋯", theme.Warning
		case "error":
			symbol, color = "✗", theme.Error
		case "cancelled":
			symbol = "⊘"
		}
		name := oneLine(call.Name)
		line := paint(symbol, color) + " " + paint(name, theme.Accent)
		if preview := codemodeArgs(call.Args); preview != "" {
			line += "  " + paint(preview, visualDim(theme))
		}
		suffix := ""
		if finiteNonnegative(call.DurationMS) {
			if *call.DurationMS < 1000 {
				suffix = fmt.Sprintf("%.0fms", *call.DurationMS)
			} else {
				suffix = fmt.Sprintf("%.1fs", *call.DurationMS/1000)
			}
		}
		if finiteNonnegative(call.Cost) && *call.Cost > 0 {
			if suffix != "" {
				suffix += " "
			}
			suffix += codemodeCost(*call.Cost)
			total += *call.Cost
			priced++
		}
		if suffix != "" {
			suffix = paint(" "+suffix, visualDim(theme))
			if width > ansi.StringWidth(suffix)+3 {
				line = fit(line, width-ansi.StringWidth(suffix)) + suffix
			} else {
				line = fit(line+suffix, width)
			}
		} else {
			line = fit(line, width)
		}
		calls = append(calls, line)
		if call.Error != "" {
			for _, line := range visualWrap(paint(Clean(call.Error), theme.Error), max(1, width-2)) {
				if width > 2 {
					line = "  " + line
				}
				calls = append(calls, line)
			}
		}
	}
	if priced > 1 {
		calls = append(calls, visualWrap(paint("Model calls: "+codemodeCost(total), theme.Muted), width)...)
	}
	appendSection(calls)
	// The native renderer also retains the script's explicit output after its
	// trace. Only its generated header is removed, never substantive output.
	output := strings.ReplaceAll(item.Text, "\r\n", "\n")
	match := codemodeHeader.FindStringSubmatchIndex(output)
	failed := item.Error
	if match != nil {
		failed = failed || output[match[2]:match[3]] == "failed"
		output = output[match[1]:]
		output = strings.TrimPrefix(output, "\n")
	}
	if strings.TrimSpace(output) != "" {
		v := &toolVisual{theme: theme, width: width}
		if value, ok := visualJSON([]byte(output)); ok {
			v.structured(value, 0, "")
		} else {
			v.literal(output)
		}
		appendSection(v.rows)
	}
	if failed {
		appendSection(visualWrap(paint("Script failed", theme.Error), width))
	} else if item.Running && len(details.Calls) == 0 {
		appendSection(visualWrap(paint("Running…", theme.Warning), width))
	}
	if details.FullOutputPath != "" {
		appendSection(visualWrap(paint("Full output: "+oneLine(details.FullOutputPath), theme.Muted), width))
	}
	return rows
}

// A dedicated, trusted styled title avoids relaxing frame's untrusted-title
// sanitization for other tools. Only renderer-generated name/token SGR enters it.
func codemodeFrame(body []string, item session.Item, theme config.Theme, width int) []string {
	border := theme.Border
	if item.Error {
		border = theme.Error
	}
	rows := frame(body, "", width, border, false, width)
	title := paint("codemode", theme.Muted)
	if badge := tokenBadge(item.EstimatedTokens, theme); badge != "" {
		title += " " + badge
	}
	title = fit(title, max(0, width-6))
	rows[0] = paint("┌─ ", border) + title + paint(" "+strings.Repeat("─", max(0, width-5-ansi.StringWidth(title)))+"┐", border)
	rows[len(rows)-1] = paint("└"+strings.Repeat("─", max(0, width-2))+"┘", border)
	kind := "codemode"
	if item.Error {
		kind = "error"
	}
	rows = frames.Gradient(kind, rows, width, theme)
	// Keep the semantic token badge colors inside the gradient border.
	rows[0] = ansi.Cut(rows[0], 0, 3) + title + ansi.Cut(rows[0], 3+ansi.StringWidth(title), width)
	return rows
}
