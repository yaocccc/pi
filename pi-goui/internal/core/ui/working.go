package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"pi-goui/internal/core/config"
	rpc "pi-goui/internal/runtime"
)

// A tab formats the shared executor's view or uses the same accumulator locally.
type workingState rpc.WorkingState
type workingUsageObserver = rpc.WorkingUsageObserver

func (w *workingState) view() rpc.WorkingSnapshot { return (*rpc.WorkingState)(w).View() }
func (w *workingState) applyShared(raw json.RawMessage) bool {
	return (*rpc.WorkingState)(w).ApplyShared(raw)
}

func (w *workingState) apply(raw json.RawMessage, now time.Time) {
	(*rpc.WorkingState)(w).Apply(raw, now)
}
func (w *workingState) reset() { (*rpc.WorkingState)(w).Dismiss() }
func workingNonnegative(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}
func workingTokens(value float64) string {
	value = workingNonnegative(value)
	if value >= 1e6 {
		return fmt.Sprintf("%.1fM", value/1e6)
	}
	if value >= 1000 {
		return fmt.Sprintf("%.0fk", value/1000)
	}
	return fmt.Sprintf("%.0f", value)
}
func (w *workingState) line(now time.Time, theme config.Theme) string {
	return w.formatLine(now, theme, true)
}

func (w *workingState) formatLine(now time.Time, theme config.Theme, detailed bool) string {
	v := w.view()
	if !v.Present {
		return ""
	}
	end := now
	if !v.Active {
		end = v.Ended
	}
	elapsed := end.Sub(v.Started)
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := int(elapsed / time.Second)
	duration := fmt.Sprintf("%ds", seconds)
	if seconds >= 60 {
		duration = fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
	}
	input, output := v.Input, v.Output
	var parts []string
	if detailed {
		parts = append(parts, fmt.Sprintf("%d turns", max(1, v.Turn)))
	}
	var usage []string
	if input > 0 {
		usage = append(usage, "↑"+workingTokens(input))
	}
	if output > 0 {
		usage = append(usage, "↓"+workingTokens(output))
	}
	if len(usage) > 0 {
		parts = append(parts, strings.Join(usage, " "))
	}
	if detailed && elapsed > 0 && output > 0 {
		parts = append(parts, fmt.Sprintf("%.1f TPS", output/elapsed.Seconds()))
	}
	parts = append(parts, duration)
	paint := func(s, c string) string {
		if c == "" {
			return s
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(s)
	}
	text := strings.Join(parts, " · ")
	if v.Active {
		return paint(text, theme.Muted)
	}
	return paint("✓", theme.Success) + " " + paint(text, theme.Muted)
}
