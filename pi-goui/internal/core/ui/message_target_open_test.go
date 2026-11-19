package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestResolveMessageTarget(t *testing.T) {
	cwd := t.TempDir()
	file := filepath.Join(cwd, "notes with spaces.md")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ raw, want string }{
		{"https://example.com/a%20b?q=x%26y#anchor", "https://example.com/a%20b?q=x%26y#anchor"},
		{"HTTP://localhost:8080/", "http://localhost:8080/"},
		{"notes with spaces.md", file},
		{"notes with spaces.md:10", file},
		{"notes with spaces.md:10:2", file},
		{file, file},
		{"https:///missing-host", ""},
		{"https://example.com/path with spaces", ""},
		{"https://example.com/\nother", ""},
		{"ftp://example.com/a", ""},
		{"file://" + file, ""},
		{"javascript:alert(1)", ""},
		{"missing.go:1", ""},
		{cwd, ""},
	} {
		got, ok := resolveMessageTarget(cwd, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("resolveMessageTarget(%q) = %q, %v; want %q", tc.raw, got, ok, tc.want)
		}
	}
	if runtime.GOOS != "windows" {
		literal := file + ":10"
		if err := os.WriteFile(literal, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if got, ok := resolveMessageTarget(cwd, literal); !ok || got != literal {
			t.Fatalf("literal path ending in :10 resolved to %q, %v", got, ok)
		}
	}
}

func TestMessageBrowserCommandArguments(t *testing.T) {
	const target = "https://example.com/?a=1&b=$(literal)"
	for platform, browser := range map[string]string{"linux": "xdg-open", "darwin": "open"} {
		cmd, err := messageBrowserCommand(platform, target)
		if err != nil || !reflect.DeepEqual(cmd.Args, []string{browser, target}) {
			t.Fatalf("unexpected %s opener: %v, %v", platform, cmd, err)
		}
	}
}

func TestMessageEditorCommandArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses Unix executable permissions")
	}
	binDir := t.TempDir()
	editor := filepath.Join(binDir, "nvim")
	if err := os.WriteFile(editor, nil, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	// The untrusted file remains an editor argument even if it is a script.
	target := filepath.Join(t.TempDir(), "-$(literal);script.sh")
	cmd, err := messageEditorCommand(target)
	if err != nil || cmd.Path != editor || !reflect.DeepEqual(cmd.Args[1:], []string{target}) {
		t.Fatalf("unexpected fallback editor: %v, %v", cmd, err)
	}
	t.Setenv("VISUAL", "nvim -R")
	t.Setenv("EDITOR", "other-editor")
	cmd, err = messageEditorCommand(target)
	if err != nil || cmd.Path != editor || !reflect.DeepEqual(cmd.Args[1:], []string{"-R", target}) {
		t.Fatalf("unexpected configured editor: %v, %v", cmd, err)
	}
}
