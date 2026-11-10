package ui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

const cwdHelp = "Choose a directory, then press Enter to apply."

type cwdCompletion struct {
	query   string
	pending bool
}
type cwdChoicesMsg struct {
	id      int
	dialog  *dialog
	query   string
	options []option
	err     error
}

func directoryPrefix(path string) string {
	return strings.TrimRight(path, string(filepath.Separator)) + string(filepath.Separator)
}

func (m *Model) ensureCwdCompletion() tea.Cmd {
	t := m.current()
	if t == nil || len(t.dialogs) == 0 || t.dialogs[0].kind != "cwd" {
		return nil
	}
	d := t.dialogs[0]
	query := d.editor.Value()
	if d.cwd != nil && (d.cwd.pending || d.cwd.query == query) {
		return nil
	}
	d.cwd = &cwdCompletion{query: query, pending: true}
	d.options = nil
	d.selected = 0
	d.message = cwdHelp
	id, base := t.id, m.tabDirectory(t)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		options, err := scanCwdChoices(ctx, base, query)
		return cwdChoicesMsg{id, d, query, options, err}
	}
}

func (m *Model) receiveCwdChoices(msg cwdChoicesMsg) {
	t := m.byID(msg.id)
	if t == nil || len(t.dialogs) == 0 || t.dialogs[0] != msg.dialog {
		return
	}
	d := msg.dialog
	if d.cwd == nil || d.cwd.query != msg.query {
		return
	}
	d.cwd.pending = false
	if d.editor.Value() != msg.query {
		return // Update schedules a new lookup for the current input.
	}
	d.options = msg.options
	if msg.err != nil {
		d.message = msg.err.Error()
	}
	m.resizeDialog(d)
}

func (m *Model) completeCwd(d *dialog) tea.Cmd {
	options := m.filtered(d)
	if len(options) == 0 {
		return nil
	}
	d.selected = min(max(0, d.selected), len(options)-1)
	d.editor.SetValue(options[d.selected].Value)
	d.selected = 0
	return m.ensureCwdCompletion()
}

// Only inspect one directory, with bounded entries/time and no file contents.
func scanCwdChoices(ctx context.Context, base, query string) ([]option, error) {
	_, dir, _, needle, _ := fileRequest(base, fileMention{query: strings.TrimSpace(query)})
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var options []option
	seen := 0
	for seen < fileCompletionLimit {
		if err := ctx.Err(); err != nil {
			return options, err
		}
		entries, readErr := f.ReadDir(min(256, fileCompletionLimit-seen))
		seen += len(entries)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return options, err
			}
			name := entry.Name()
			if !strings.HasPrefix(name, needle) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				continue
			}
			path := filepath.Join(dir, name)
			isDir := entry.IsDir()
			if entry.Type()&os.ModeSymlink != 0 {
				info, err := os.Stat(path)
				isDir = err == nil && info.IsDir()
			}
			if isDir {
				options = append(options, option{Label: name + "/", Value: directoryPrefix(path)})
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return options, readErr
			}
			break
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Label < options[j].Label })
	if parent := filepath.Dir(dir); parent != dir && strings.HasPrefix("..", needle) {
		options = append(options, option{Label: "../", Detail: "parent directory", Value: directoryPrefix(parent)})
	}
	return options, nil
}
