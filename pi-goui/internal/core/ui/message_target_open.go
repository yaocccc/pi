package ui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

var messageFileLocation = regexp.MustCompile(`:[0-9]+(?::[0-9]+)?$`)

// resolveMessageTarget accepts browser URLs and actual files only. Returning an
// absolute path keeps file names from being interpreted as editor options.
func resolveMessageTarget(cwd, raw string) (string, bool) {
	if raw == "" || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", false
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.ContainsFunc(raw, unicode.IsSpace) {
			return "", false
		}
		return u.String(), true
	}
	path := raw
	if !filepath.IsAbs(path) {
		if cwd == "" {
			return "", false
		}
		path = filepath.Join(cwd, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, true
	}
	// Prefer the literal file when its name actually ends in :line[:column].
	if base := messageFileLocation.ReplaceAllString(path, ""); base != path {
		if info, err := os.Stat(base); err == nil && info.Mode().IsRegular() {
			return base, true
		}
	}
	return "", false
}

func messageBrowserCommand(platform, target string) (*exec.Cmd, error) {
	switch platform {
	case "linux":
		return exec.Command("xdg-open", target), nil
	case "darwin":
		return exec.Command("open", target), nil
	default:
		return nil, fmt.Errorf("opening links is not supported on %s", platform)
	}
}

func messageEditorCommand(target string) (*exec.Cmd, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		editor := strings.TrimSpace(os.Getenv(key))
		if editor == "" {
			continue
		}
		// Allow a path with spaces if it names an executable. Otherwise split
		// simple arguments without evaluating shell syntax or substitutions.
		if bin, err := exec.LookPath(editor); err == nil {
			return exec.Command(bin, target), nil
		}
		args := strings.Fields(editor)
		return exec.Command(args[0], append(args[1:], target)...), nil
	}
	// Do not use the system file opener here: it could execute the file.
	for _, editor := range []string{"nvim", "vim", "vi"} {
		if bin, err := exec.LookPath(editor); err == nil {
			return exec.Command(bin, target), nil
		}
	}
	return nil, fmt.Errorf("set VISUAL or EDITOR to open files")
}

// openMessageTarget revalidates the click target before launching anything.
func openMessageTarget(id int, cwd, target string) tea.Cmd {
	notice := func(err error) tea.Cmd {
		return func() tea.Msg { return noticeMsg{id: id, text: "Open message target: " + err.Error()} }
	}
	resolved, ok := resolveMessageTarget(cwd, target)
	if !ok {
		return notice(fmt.Errorf("not a valid HTTP(S) link or regular file"))
	}
	if !filepath.IsAbs(resolved) {
		cmd, err := messageBrowserCommand(runtime.GOOS, resolved)
		if err != nil {
			return notice(err)
		}
		return func() tea.Msg {
			if err := cmd.Run(); err != nil {
				return notice(err)()
			}
			return nil
		}
	}
	cmd, err := messageEditorCommand(resolved)
	if err != nil {
		return notice(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return notice(err)()
		}
		return nil
	})
}
