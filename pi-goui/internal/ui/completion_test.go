package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestFuzzyCommandsExecuteOnFirstEnter(t *testing.T) {
	m := testModel(t, "")
	c, wire := fakeClient(t)
	tab := m.current()
	tab.client = c
	tab.commands = []command{{Name: "codex-fast"}, {Name: "fast"}, {Name: "other-fast"}}
	tab.setValue("/ fast")
	choices := m.completion(tab)
	if len(choices) < 3 || choices[0].Name != "fast" || choices[1].Name != "codex-fast" {
		t.Fatalf("ranking: %v", choices)
	}
	tab.commands = []command{{Name: "codex-fast"}}
	tab.setValue("/ fast extra argument")
	msg := commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEnter})).(replyMsg)
	if msg.op != "prompt" || tab.editor.Value() != "" {
		t.Fatal("Enter only filled rather than executing")
	}
	record := wireRecords(t, wire, 1)[0]
	if record["message"] != "/codex-fast extra argument" {
		t.Fatalf("lost canonical command/arguments: %v", record)
	}
	tab.setValue("/ cpt")
	msg = commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEnter})).(replyMsg)
	if msg.op != "compact" {
		t.Fatalf("cpt didn't execute compact: %s", msg.op)
	}
	tab.setValue("/ FAST")
	m.completionHidden = false
	m.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if tab.editor.Value() != "/codex-fast " {
		t.Fatal(tab.editor.Value())
	}
	tab.setValue("/zzzz-no-match")
	m.completionHidden = false
	if cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("unknown command was sent")
	}
	for _, pair := range [][2]string{{"工具搜索", "工索"}, {"compact", "cpt"}, {"codex-fast", "FAST"}} {
		if _, ok := fuzzyScore(pair[0], pair[1]); !ok {
			t.Fatal(pair)
		}
	}
}
func TestFileSuggestionsInsertAtCursorAndContinueDirectory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	root := m.opts.Cwd
	if err := os.MkdirAll(filepath.Join(root, "dir space"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha.go", "dir space/中文.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture never read"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tab.setValue("review @al then explain")
	tab.editor.CompleteRange(10, 10, "", 0)
	// CompleteRange sets cursor to its start; the @al token occupies runes 7..10.
	cmd := m.ensureFileCompletion()
	if cmd == nil {
		t.Fatal("no async index request")
	}
	m.Update(cmd())
	choices := m.completion(tab)
	if len(choices) == 0 || choices[0].Name != "alpha.go" {
		t.Fatal(choices)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if tab.editor.Value() != "review @alpha.go then explain" {
		t.Fatal(tab.editor.Value())
	}
	if got := tab.editor.CursorOffset(); got != 16 {
		t.Fatalf("cursor moved past suffix: %d", got)
	}
	tab.setValue("@dir")
	m.completionHidden = false
	choices = m.completion(tab)
	if len(choices) == 0 || choices[0].Description != "directory" {
		t.Fatal(choices)
	}
	cmd = m.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if tab.editor.Value() != "@\"dir space/\"" {
		t.Fatal(tab.editor.Value())
	}
	if cmd != nil {
		m.Update(cmd())
	}
	choices = m.completion(tab)
	if len(choices) != 2 || choices[0].Name != "dir space/中文.go" || choices[1].Description != "parent directory" {
		t.Fatal(choices)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if tab.editor.Value() != "@\"dir space/中文.go\" " {
		t.Fatal(tab.editor.Value())
	}
	if tab.sending {
		t.Fatal("selecting file submitted prompt")
	}
}
func TestFileSuggestionsNewestFirst(t *testing.T) {
	m := testModel(t, "")
	root := m.opts.Cwd
	older := time.Unix(1700000000, 0)
	newer := older.Add(time.Hour)
	for name, modified := range map[string]time.Time{"alpha.go": older, "zebra.go": newer} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	for _, recursive := range []bool{false, true} {
		entries, err := scanFileChoices(context.Background(), root, recursive)
		if err != nil {
			t.Fatal(err)
		}
		query := "./a"
		if recursive {
			query = "a"
		}
		mention := fileMention{query: query}
		key, _, prefix, _, _ := fileRequest(root, mention)
		m.files.cache = map[string]fileCache{key: {entries: entries, at: time.Now()}}
		choices := m.fileChoices(mention)
		if len(choices) != 2 || choices[0].Name != prefix+"zebra.go" || choices[1].Name != prefix+"alpha.go" {
			t.Fatalf("recursive=%v: %v", recursive, choices)
		}
	}
}

func TestFileSuggestionsShallowPathsFirst(t *testing.T) {
	m := testModel(t, "")
	older := time.Unix(1700000000, 0)
	newer := older.Add(time.Hour)
	entries := []fileEntry{
		{name: "src/nested/main.go", modTime: newer},
		{name: "src/main.go", modTime: older},
		{name: "main.go", modTime: older},
		{name: "lib/main.go", modTime: newer},
	}
	for _, query := range []string{"", "main.go"} {
		mention := fileMention{query: query}
		key, _, _, _, _ := fileRequest(m.tabDirectory(m.current()), mention)
		m.files.cache = map[string]fileCache{key: {entries: entries, at: time.Now()}}
		choices := m.fileChoices(mention)
		want := []string{"main.go", "lib/main.go", "src/main.go", "src/nested/main.go"}
		if len(choices) < len(want) {
			t.Fatalf("query %q: %v", query, choices)
		}
		for i, name := range want {
			if choices[i].Name != name {
				t.Fatalf("query %q, position %d: got %s, want %s", query, i, choices[i].Name, name)
			}
		}
	}
}

func TestFileParentNavigation(t *testing.T) {
	for _, key := range []rune{tea.KeyTab, tea.KeyEnter} {
		m := testModel(t, "")
		tab := m.current()
		// No cached listing: even an empty or still-loading directory can exit.
		tab.setValue("review @\"dir space/child/\" suffix")
		cursor := len([]rune("review @\"dir space/child/"))
		tab.editor.CompleteRange(cursor, cursor, "", 0)
		choices := m.completion(tab)
		if len(choices) != 1 || choices[0].Name != "dir space/" || m.completionLabel(tab, choices[0]) != "../" {
			t.Fatalf("parent choices: %v", choices)
		}
		m.key(tea.KeyPressMsg{Code: key})
		if tab.editor.Value() != "review @\"dir space/\" suffix" || m.completionHidden || tab.sending {
			t.Fatalf("parent navigation: %q", tab.editor.Value())
		}
		mention, ok := mentionAt(tab.editor.Value(), tab.editor.CursorOffset())
		if !ok || mention.query != "dir space/" {
			t.Fatalf("cursor not inside directory reference: %+v", mention)
		}
		choices = m.completion(tab)
		m.complete(tab, choices)
		if tab.editor.Value() != "review @./ suffix" {
			t.Fatal(tab.editor.Value())
		}
	}
	m := testModel(t, "")
	for _, query := range []string{"/", "src/file"} {
		if choices := m.fileChoices(fileMention{query: query}); len(choices) != 0 {
			t.Fatalf("unexpected navigation for %q: %v", query, choices)
		}
	}
	for query, want := range map[string]string{"": "../", "src/..": "./", "../": "../../"} {
		choices := m.fileChoices(fileMention{query: query})
		if len(choices) != 1 || choices[0].Name != want {
			t.Fatalf("parent for %q: %v, want %s", query, choices, want)
		}
	}
}

func TestFileIndexIgnoredDirectoriesAndStaleResults(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".git", "node_modules", "src"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"src/visible.go", "node_modules/hidden.js", ".git/secret"} {
		if err := os.WriteFile(filepath.Join(root, path), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(root, filepath.Join(root, "loop"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	items, err := scanFileChoices(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 6 {
		t.Fatal("followed symlink loop")
	}
	for _, item := range items {
		if strings.Contains(item.name, "hidden") || strings.HasPrefix(item.name, ".git") {
			t.Fatal(item)
		}
	}
	m := testModel(t, "")
	m.files.pending = "new"
	m.receiveFiles(filesMsg{key: "old", entries: []fileEntry{{name: "wrong"}}})
	if m.files.pending != "new" || len(m.files.cache) != 0 {
		t.Fatal("stale query applied")
	}
	m.current().setValue("@a")
	m.files.pending = ""
	cmd := m.ensureFileCompletion()
	m.fileReader.close()
	if msg := cmd().(filesMsg); len(msg.entries) != 0 {
		t.Fatal("closed index reader started work")
	}
}
func TestGitFileIndexRespectsIgnore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("temporary git init: %v %s", err, out)
	}
	for name, text := range map[string]string{".gitignore": "ignored.txt\n", "ignored.txt": "private fixture", "visible.txt": "fixture"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	items, err := scanFileChoices(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.name == "ignored.txt" {
			t.Fatal("ignored file indexed")
		}
		if item.name == "visible.txt" && item.modTime.IsZero() {
			t.Fatal("git index entry missing modification time")
		}
		found = found || item.name == "visible.txt"
	}
	if !found {
		t.Fatal("missing visible file")
	}
}

func TestFilePasteAndEscapeDismissDoNotSubmitSelections(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	client, wire := fakeClient(t)
	tab.client = client
	if err := os.WriteFile(filepath.Join(m.opts.Cwd, "visible.txt"), []byte("not read by completion"), 0600); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(tea.PasteMsg{Content: "@vis"})
	var execute func(tea.Cmd)
	execute = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, child := range msg {
				execute(child)
			}
		case filesMsg:
			m.Update(msg)
		}
	}
	execute(cmd)
	if choices := m.completion(tab); len(choices) != 1 || choices[0].Name != "visible.txt" {
		t.Fatal(choices)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if tab.sending || tab.editor.Value() != "@visible.txt " {
		t.Fatal("file selection sent prompt")
	}
	tab.setValue("@missing-ref")
	m.completionHidden = false
	if cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("missing reference unexpectedly submitted")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.completionHidden {
		t.Fatal("empty file choices could not be dismissed")
	}
	reply := commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEnter})).(replyMsg)
	if reply.op != "prompt" {
		t.Fatal(reply.op)
	}
	records := wireRecords(t, wire, 1)
	if records[0]["message"] != "@missing-ref" {
		t.Fatal(records)
	}
}
