package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
)

var codexFastLevels = []string{"normal", "fast", "ultrafast"}

// The extension transports only the current value; labels and selection live here.
// Confirmation returns the level to the extension, which validates and saves it.
func (m *Model) openCodexFastSettings(d *dialog, payload string) bool {
	var request struct {
		Level string `json:"level"`
	}
	if json.Unmarshal([]byte(payload), &request) != nil || (request.Level != "normal" && request.Level != "fast" && request.Level != "ultrafast") {
		return false
	}
	d.kind = "select"
	d.title = "Codex 档位 · 当前 " + request.Level
	d.message = "回车选择并保存 · Esc 退出"
	d.editor.SetValue("")
	d.editor.Area.Placeholder = "搜索档位"
	d.options = nil
	for _, level := range codexFastLevels {
		label := level
		if level == request.Level {
			label += " · 当前"
		}
		d.options = append(d.options, option{Label: label, Value: level})
	}
	m.resizeDialog(d)
	return true
}

func (m *Model) codexFastMode() string {
	if m.codexFastLabel == "fast" || m.codexFastLabel == "ultrafast" {
		return m.codexFastLabel
	}
	return "normal"
}

// Display the configured level, not the model-specific service_tier fallback.
func (m *Model) refreshCodexFast(now time.Time) {
	if !m.codexFastChecked.IsZero() && now.Sub(m.codexFastChecked) < time.Second {
		return
	}
	m.codexFastChecked = now
	if m.cfg.AgentDir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(m.cfg.AgentDir, "codex-fast.json"))
	if err != nil {
		if os.IsNotExist(err) {
			m.codexFastLabel = "normal"
		}
		return
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil {
		return
	}
	level := "normal"
	if value, exists := config["level"]; exists {
		_ = json.Unmarshal(value, &level)
	} else {
		var fast, ultrafast bool
		_ = json.Unmarshal(config["fast"], &fast)
		_ = json.Unmarshal(config["ultrafast"], &ultrafast)
		if ultrafast {
			level = "ultrafast"
		} else if fast {
			level = "fast"
		}
	}
	m.codexFastLabel = level
	m.codexFastLabel = m.codexFastMode()
}

func saveCodexFast(dir, level string) error {
	if dir == "" {
		return fmt.Errorf("agent directory unavailable")
	}
	if level != "normal" && level != "fast" && level != "ultrafast" {
		return fmt.Errorf("invalid Codex mode")
	}
	f, err := os.CreateTemp(dir, ".codex-fast-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	data, _ := json.Marshal(struct {
		Level string `json:"level"`
	}{level})
	_, err = f.Write(append(data, '\n'))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "codex-fast.json"))
}

// The popup touches the value's top edge so entering it has no dead gap.
// Right alignment keeps the popup stationary when the selected label changes.
func (m *Model) codexFastGeometry() (hitRect, footerPopupRect, bool) {
	t := m.current()
	if t == nil || m.inspectorOpen() || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" || m.sidebarWidth() < 12 {
		return hitRect{}, footerPopupRect{}, false
	}
	y := m.sidebarLayout().ModeRow
	if y < 0 || y >= m.height-1 {
		return hitRect{}, footerPopupRect{}, false
	}
	value, offset := render.SidebarModeValue(m.codexFastMode(), m.sidebarWidth()-4)
	target := hitRect{x: m.mainWidth() + 2 + offset, y: y, width: ansi.StringWidth(value)}
	width := min(24, m.sidebarWidth()-4)
	if target.width == 0 || width < 19 || y < 7 {
		return hitRect{}, footerPopupRect{}, false
	}
	box := footerPopupRect{x: target.x + target.width - width, y: y - 7, width: width, height: 7}
	return target, box, true
}

func (m *Model) codexFastPopupVisible() bool {
	if m.inspectorFullscreen() {
		return false
	}
	target, box, ok := m.codexFastGeometry()
	if !ok || !m.sidebarHistory.pointerKnown {
		return false
	}
	x, y := m.sidebarHistory.pointerX, m.sidebarHistory.pointerY
	return target.contains(x, y) || m.codexFastHover && box.contains(x, y)
}

func (m *Model) clickCodexFast(x, y int) bool {
	if !m.codexFastPopupVisible() {
		return false
	}
	target, box, _ := m.codexFastGeometry()
	if target.contains(x, y) {
		return true
	}
	if !box.contains(x, y) {
		return false
	}
	row := y - box.y - 1
	if row < 0 || row >= len(codexFastLevels) {
		return true
	}
	level := codexFastLevels[row]
	if err := saveCodexFast(m.cfg.AgentDir, level); err != nil {
		m.current().notify("Save Codex mode failed: " + err.Error())
		return true
	}
	m.codexFastLabel = level
	m.codexFastChecked = time.Now()
	m.codexFastHover = false
	return true
}

func (m *Model) overlayCodexFast(view string) string {
	if !m.codexFastPopupVisible() {
		return view
	}
	_, box, _ := m.codexFastGeometry()
	rows := make([]string, 0, len(codexFastLevels))
	for _, level := range codexFastLevels {
		prefix := "  "
		if level == m.codexFastMode() {
			prefix = "› "
		}
		rows = append(rows, prefix+render.CodexModeLabel(level))
	}
	framed := render.SidebarDetailFrame(rows, []string{"Click to set"}, box.width, m.cfg.Theme)
	hover := m.sidebarHistory.pointerY - box.y - 1
	if box.contains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY) && hover >= 0 && hover < len(rows) {
		line := framed[hover+1]
		framed[hover+1] = ansi.Cut(line, 0, 1) + render.SidebarHoverText(ansi.Cut(line, 1, box.width-1)) + ansi.Cut(line, box.width-1, box.width)
	}
	lines := strings.Split(view, "\n")
	for i, line := range framed {
		y := box.y + i
		if y >= 0 && y < len(lines) {
			lines[y] = popupBackdropSlice(lines[y], 0, box.x) + line + popupBackdropSlice(lines[y], box.x+box.width, m.width)
		}
	}
	return strings.Join(lines, "\n")
}
