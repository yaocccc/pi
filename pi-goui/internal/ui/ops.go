package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type externalMsg struct {
	id   int
	text string
	err  error
}

func externalEditor(t *tab) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return func() tea.Msg { return noticeMsg{t.id, "Set VISUAL or EDITOR to use the external editor"} }
	}
	f, err := os.CreateTemp("", "pi-goui-editor-*.md")
	if err != nil {
		return func() tea.Msg { return externalMsg{id: t.id, err: err} }
	}
	path := f.Name()
	_, err = f.WriteString(t.editor.Value())
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return func() tea.Msg { return externalMsg{id: t.id, err: err} }
	}
	cmd := exec.Command("sh", "-c", editor+" \"$1\"", "pi-goui-editor", path)
	id := t.id
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return externalMsg{id: id, err: err}
		}
		data, err := os.ReadFile(path)
		return externalMsg{id, string(data), err}
	})
}
func saveThinking(t *tab, root string) tea.Cmd {
	id, level := t.id, t.state.Thinking
	return func() tea.Msg {
		if level == "" {
			return noticeMsg{id, "No thinking level to save"}
		}
		path := filepath.Join(root, "settings.json")
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return noticeMsg{id, err.Error()}
		}
		settings := map[string]any{}
		if len(data) > 0 {
			if err = json.Unmarshal(data, &settings); err != nil {
				return noticeMsg{id, err.Error()}
			}
		}
		settings["defaultThinkingLevel"] = level
		data, err = json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return noticeMsg{id, err.Error()}
		}
		data = append(data, '\n')
		if err = os.MkdirAll(root, 0700); err != nil {
			return noticeMsg{id, err.Error()}
		}
		f, err := os.CreateTemp(root, ".settings-*.tmp")
		if err != nil {
			return noticeMsg{id, err.Error()}
		}
		defer os.Remove(f.Name())
		if stat, e := os.Stat(path); e == nil {
			_ = f.Chmod(stat.Mode().Perm())
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err != nil {
			return noticeMsg{id, err.Error()}
		}
		return noticeMsg{id, "Saved default thinking level: " + level}
	}
}

type clipboardImageMsg struct {
	id         int
	image      map[string]any
	err        error
	anchor     *imagePasteAnchor
	generation uint64
	pending    bool
}

const imageLimit = 20 << 20

func imageData(data []byte, mimeType string) (map[string]any, error) {
	if len(data) == 0 {
		return nil, errors.New("empty image")
	}
	if len(data) > imageLimit {
		return nil, errors.New("image exceeds 20 MiB")
	}
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, errors.New("supported images: PNG, JPEG, GIF, WebP")
	}
	return map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(data), "mimeType": mimeType}, nil
}
func loadImage(path string) (map[string]any, error) {
	f, err := os.Open(strings.Trim(path, "\""))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, imageLimit+1))
	if err != nil {
		return nil, err
	}
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	return imageData(b, typ)
}
func readClipboardImage(id int) tea.Cmd {
	return func() tea.Msg {
		var name string
		var args []string
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			name = "wl-paste"
			args = []string{"--type", "image/png"}
		} else if os.Getenv("DISPLAY") != "" {
			name = "xclip"
			args = []string{"-selection", "clipboard", "-t", "image/png", "-o"}
		}
		if name == "" {
			return clipboardImageMsg{id: id, err: errors.New("No image clipboard helper; trying terminal text clipboard. Use /attach for image files.")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return clipboardImageMsg{id: id, err: err}
		}
		if err = cmd.Start(); err != nil {
			return clipboardImageMsg{id: id, err: fmt.Errorf("%s unavailable; use /attach or paste text", name)}
		}
		data, err := io.ReadAll(io.LimitReader(pipe, imageLimit+1))
		if len(data) > imageLimit {
			cancel()
		}
		we := cmd.Wait()
		if err == nil {
			err = we
		}
		if err != nil {
			return clipboardImageMsg{id: id, err: errors.New("No clipboard image; trying terminal text clipboard")}
		}
		image, err := imageData(data, "image/png")
		return clipboardImageMsg{id: id, image: image, err: err}
	}
}
