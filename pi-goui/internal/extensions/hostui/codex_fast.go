package hostui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"errors"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/rpc"
	"pi-goui/internal/extensions/codexfast"
	"pi-goui/internal/extensions/internal/protocol"
	"pi-goui/internal/extensions/toolrender/cards"
	"strings"
	"time"
)

// Legacy dialog compatibility: these are labels, never persistence rules.
var CodexFastLevels = []string{"normal", "fast", "ultrafast"}

func (h DialogHost) OpenCodexFastSettings(d *Dialog, payload string) bool {
	var request struct {
		Level  string   `json:"level"`
		Levels []string `json:"levels"`
	}
	if json.Unmarshal([]byte(payload), &request) != nil {
		return false
	}
	if len(request.Levels) == 0 {
		request.Levels = CodexFastLevels
	}
	state := codexfast.State{Level: request.Level, Levels: request.Levels}
	if !state.Available() {
		return false
	}
	d.Kind = "select"
	d.Title = "Codex 档位 · 当前 " + request.Level
	d.Message = "回车选择并保存 · Esc 退出"
	d.Editor.SetValue("")
	d.Editor.Area.Placeholder = "搜索档位"
	d.Options = nil
	for _, level := range request.Levels {
		label := level
		if level == request.Level {
			label += " · 当前"
		}
		d.Options = append(d.Options, Option{Label: label, Value: level})
	}
	h.Resize()
	return true
}

type CodexState struct {
	State   codexfast.State
	Checked time.Time
	Pending bool
	Version uint64
}
type CodexResult struct {
	ID      int
	Version uint64
	State   codexfast.State
	Setting bool
	Err     error
}

func (c *CodexState) Mode(dead bool) string {
	if !dead && c.State.Available() {
		return c.State.Level
	}
	return ""
}
func (c *CodexState) Request(id int, client extension.Client, dead bool, level string, now time.Time) tea.Cmd {
	if client == nil || dead || c.Pending {
		return nil
	}
	if level == "" && !c.Checked.IsZero() && now.Sub(c.Checked) < 5*time.Second {
		return nil
	}
	c.Checked = now
	c.Pending = true
	version := c.Version
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var state codexfast.State
		var err error
		if level == "" {
			state, err = codexfast.Get(ctx, client)
		} else {
			state, err = codexfast.Set(ctx, client, level)
		}
		return CodexResult{ID: id, Version: version, State: state, Setting: level != "", Err: err}
	}
}
func (c *CodexState) Apply(v CodexResult, dead bool, notify func(string)) {
	c.Pending = false
	if dead {
		return
	}
	if errors.Is(v.Err, rpc.ErrExtensionUnavailable) {
		c.State = codexfast.State{}
	} else if v.Err != nil {
		if v.Setting {
			notify("Save Codex mode failed: " + v.Err.Error())
		}
	} else if v.Version == c.Version {
		c.State = v.State
	}
}
func (c *CodexState) Event(message string, notify func(string)) bool {
	event, handled := protocol.DecodeEvent(message)
	if !handled {
		return false
	}
	if event.Name == "codex.settings-saved" {
		var data struct {
			Level string `json:"level"`
		}
		if json.Unmarshal(event.Data, &data) == nil {
			for _, level := range c.State.Levels {
				if level == data.Level {
					c.State.Level = level
					c.Version++
					break
				}
			}
		}
	}
	if text := protocol.Notice(event); text != "" {
		notify(text)
	}
	return true
}

// CodexView contains only layout inputs and display state from the host.
type CodexView struct {
	State                                                               codexfast.State
	Blocked, PointerKnown, Hover                                        bool
	SidebarWidth, MainWidth, Width, Height, ModeRow, PointerX, PointerY int
	Theme                                                               config.Theme
}

func (v CodexView) Geometry() (Rect, Rect, bool) {
	if v.Blocked || !v.State.Available() || v.SidebarWidth < 12 {
		return Rect{}, Rect{}, false
	}
	y := v.ModeRow
	if y < 0 || y >= v.Height-1 {
		return Rect{}, Rect{}, false
	}
	value, offset := render.SidebarModeValue(cards.CodexModeLabel(v.State.Level), v.SidebarWidth-4)
	target := Rect{X: v.MainWidth + 2 + offset, Y: y, Width: ansi.StringWidth(value)}
	width := min(24, v.SidebarWidth-4)
	height := len(v.State.Levels) + 4
	if target.Width == 0 || width < 19 || y < height {
		return Rect{}, Rect{}, false
	}
	return target, Rect{X: target.X + target.Width - width, Y: y - height, Width: width, Height: height}, true
}
func (v CodexView) Visible() bool {
	target, box, ok := v.Geometry()
	if !ok || !v.PointerKnown {
		return false
	}
	return target.Contains(v.PointerX, v.PointerY) || v.Hover && box.Contains(v.PointerX, v.PointerY)
}
func (v CodexView) Click(x, y int) (bool, string) {
	if !v.Visible() {
		return false, ""
	}
	target, box, _ := v.Geometry()
	if target.Contains(x, y) {
		return true, ""
	}
	if !box.Contains(x, y) {
		return false, ""
	}
	row := y - box.Y - 1
	if row < 0 || row >= len(v.State.Levels) {
		return true, ""
	}
	return true, v.State.Levels[row]
}
func (v CodexView) Overlay(view string, backdrop func(string, int, int) string) string {
	if !v.Visible() {
		return view
	}
	_, box, _ := v.Geometry()
	rows := make([]string, 0, len(v.State.Levels))
	for _, level := range v.State.Levels {
		prefix := "  "
		if level == v.State.Level {
			prefix = "› "
		}
		rows = append(rows, prefix+cards.CodexModeLabel(level))
	}
	framed := render.SidebarDetailFrame(rows, []string{"Click to set"}, box.Width, v.Theme)
	hover := v.PointerY - box.Y - 1
	if box.Contains(v.PointerX, v.PointerY) && hover >= 0 && hover < len(rows) {
		line := framed[hover+1]
		framed[hover+1] = ansi.Cut(line, 0, 1) + render.SidebarHoverText(ansi.Cut(line, 1, box.Width-1)) + ansi.Cut(line, box.Width-1, box.Width)
	}
	lines := strings.Split(view, "\n")
	for i, line := range framed {
		y := box.Y + i
		if y >= 0 && y < len(lines) {
			lines[y] = backdrop(lines[y], 0, box.X) + line + backdrop(lines[y], box.X+box.Width, v.Width)
		}
	}
	return strings.Join(lines, "\n")
}
