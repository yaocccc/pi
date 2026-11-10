package ui

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

const fileCompletionLimit = 10000

type fileMention struct {
	start, end int
	query      string
}

func mentionAt(text string, cursor int) (fileMention, bool) {
	r := []rune(text)
	cursor = max(0, min(cursor, len(r)))
	for i := 0; i < len(r); i++ {
		if r[i] != '@' || (i > 0 && !unicode.IsSpace(r[i-1])) {
			continue
		}
		start, from := i, i+1
		end := from
		quote := rune(0)
		if from < len(r) && (r[from] == '"' || r[from] == '\'') {
			quote = r[from]
			from++
			end = from
			for end < len(r) {
				if r[end] == '\\' && end+1 < len(r) {
					end += 2
					continue
				}
				if r[end] == quote {
					break
				}
				end++
			}
		} else {
			for end < len(r) && !unicode.IsSpace(r[end]) {
				end++
			}
		}
		contentEnd := end
		if quote != 0 && end < len(r) && r[end] == quote {
			end++
		}
		if cursor >= from && cursor <= end {
			q := string(r[from:min(cursor, contentEnd)])
			if quote != 0 {
				q = strings.ReplaceAll(strings.ReplaceAll(q, "\\\"", "\""), "\\\\", "\\")
			}
			return fileMention{start, end, q}, true
		}
		i = max(i, end-1)
	}
	return fileMention{}, false
}

type fileEntry struct {
	name    string
	dir     bool
	modTime time.Time
}
type fileCache struct {
	entries []fileEntry
	at      time.Time
}
type fileCompletionState struct {
	cache   map[string]fileCache
	pending string
}
type filesMsg struct {
	key     string
	entries []fileEntry
	err     error
}
type fileReader struct {
	mu     sync.Mutex
	closed atomic.Bool
}

func (r *fileReader) close() { r.closed.Store(true); r.mu.Lock(); r.mu.Unlock() }
func fileRequest(cwd string, mention fileMention) (key, dir, prefix, needle string, recursive bool) {
	query := filepath.ToSlash(mention.query)
	recursive = !strings.Contains(query, "/")
	if recursive {
		dir = cwd
		needle = query
	} else {
		at := strings.LastIndex(query, "/")
		prefix = query[:at+1]
		needle = query[at+1:]
		dir = filepath.FromSlash(prefix)
		if strings.HasPrefix(dir, "~/") {
			if home, e := os.UserHomeDir(); e == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
	}
	dir = filepath.Clean(dir)
	key = dir + "\x00"
	if recursive {
		key += "recursive"
	} else {
		key += "direct"
	}
	return
}
func (m *Model) ensureFileCompletion() tea.Cmd {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || m.completionHidden {
		return nil
	}
	mention, ok := mentionAt(t.editor.Value(), t.editor.CursorOffset())
	if !ok {
		return nil
	}
	key, dir, _, _, recursive := fileRequest(m.tabDirectory(t), mention)
	if m.files.cache == nil {
		m.files.cache = map[string]fileCache{}
	}
	if c, ok := m.files.cache[key]; ok && time.Since(c.at) < 5*time.Second {
		return nil
	}
	if m.files.pending != "" {
		return nil
	}
	m.files.pending = key
	reader := &m.fileReader
	return func() tea.Msg {
		reader.mu.Lock()
		defer reader.mu.Unlock()
		if reader.closed.Load() {
			return filesMsg{key: key}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		entries, err := scanFileChoices(ctx, dir, recursive)
		return filesMsg{key, entries, err}
	}
}
func (m *Model) receiveFiles(msg filesMsg) {
	if msg.key != m.files.pending {
		return
	}
	m.files.pending = ""
	if m.files.cache == nil {
		m.files.cache = map[string]fileCache{}
	}
	if len(m.files.cache) >= 8 {
		m.files.cache = map[string]fileCache{}
	}
	m.files.cache[msg.key] = fileCache{entries: msg.entries, at: time.Now()}
	if t := m.current(); t != nil {
		m.completionIndex = min(m.completionIndex, max(0, len(m.completion(t))-1))
		m.resizeEditor(t)
	}
}
func (m *Model) fileChoices(mention fileMention) []command {
	t := m.current()
	key, dir, prefix, needle, _ := fileRequest(m.tabDirectory(t), mention)
	cache := m.files.cache[key]
	type scored struct {
		entry fileEntry
		score int
	}
	var matches []scored
	for _, e := range cache.entries {
		score, yes := fuzzyScore(e.name, needle)
		if !yes {
			continue
		}
		matches = append(matches, scored{e, score})
	}
	sort.Slice(matches, func(i, j int) bool {
		depthI := strings.Count(matches[i].entry.name, "/")
		depthJ := strings.Count(matches[j].entry.name, "/")
		if depthI != depthJ {
			return depthI < depthJ
		}
		if !matches[i].entry.modTime.Equal(matches[j].entry.modTime) {
			return matches[i].entry.modTime.After(matches[j].entry.modTime)
		}
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		if matches[i].entry.dir != matches[j].entry.dir {
			return matches[i].entry.dir
		}
		return matches[i].entry.name < matches[j].entry.name
	})
	out := make([]command, 0, min(64, len(matches)))
	for _, s := range matches[:min(64, len(matches))] {
		description := "file"
		name := prefix + s.entry.name
		if s.entry.dir {
			description = "directory"
			name += "/"
		}
		out = append(out, command{Name: name, Description: description})
	}
	// Keep ordinary matches first so entering a directory never defaults to
	// immediately leaving it. Navigation remains available before scanning and
	// in empty directories, but does not pollute filename searches.
	if parent := filepath.Dir(dir); parent != dir && (needle == "" || needle == "." || needle == "..") {
		name := parent
		if !filepath.IsAbs(filepath.FromSlash(prefix)) && !strings.HasPrefix(prefix, "~/") {
			if relative, err := filepath.Rel(m.tabDirectory(t), parent); err == nil {
				name = relative
			}
		}
		name = strings.TrimSuffix(filepath.ToSlash(name), "/") + "/"
		out = append(out, command{Name: name, Description: "parent directory"})
	}
	return out
}
func (m *Model) insertFile(t *tab, mention fileMention, c command) {
	path := c.Name
	isDir := c.Description == "directory" || c.Description == "parent directory"
	reference := "@" + path
	quoted := strings.ContainsAny(path, " \t\"'\\")
	if quoted {
		reference = "@\"" + strings.ReplaceAll(strings.ReplaceAll(path, "\\", "\\\\"), "\"", "\\\"") + "\""
	}
	cursor := len([]rune(reference))
	if isDir && quoted {
		cursor--
	}
	if !isDir {
		r := []rune(t.editor.Value())
		if mention.end >= len(r) || !unicode.IsSpace(r[mention.end]) {
			reference += " "
			cursor++
		}
	}
	t.dismissCompletedWorking()
	t.editor.CompleteRange(mention.start, mention.end, reference, cursor)
	t.draftRevision++
	t.historyIndex = -1
	m.completionIndex = 0
	m.completionHidden = !isDir
	m.resizeEditor(t)
}
func allowedFileName(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".git" || part == "node_modules" {
			return false
		}
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func scanFileChoices(ctx context.Context, root string, recursive bool) ([]fileEntry, error) {
	if recursive {
		data, _, err := gitQuery(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if err == nil {
			seen := map[string]bool{}
			var out []fileEntry
			add := func(name string, dir bool) {
				if len(out) >= fileCompletionLimit || name == "." || name == "" || seen[name] || !allowedFileName(name) {
					return
				}
				seen[name] = true
				entry := fileEntry{name: name, dir: dir}
				if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); err == nil {
					entry.modTime = info.ModTime()
				}
				out = append(out, entry)
			}
			records := bytes.Split(data, []byte{0})
			for _, record := range records[:max(0, len(records)-1)] {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				if len(out) >= fileCompletionLimit {
					break
				}
				name := filepath.ToSlash(string(record))
				add(name, false)
				for parent := filepath.ToSlash(filepath.Dir(name)); parent != "." && parent != "/"; parent = filepath.ToSlash(filepath.Dir(parent)) {
					add(parent, true)
				}
			}
			return out, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	// Bounded directory batches avoid a full ReadDir allocation and never follow
	// directory symlinks. No file contents are opened, even for image references.
	type directory struct {
		relative string
		depth    int
	}
	queue := []directory{{"", 0}}
	var out []fileEntry
	for len(queue) > 0 && len(out) < fileCompletionLimit {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		current := queue[0]
		queue = queue[1:]
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(current.relative)))
		if err != nil {
			if current.relative == "" {
				return out, err
			}
			continue
		}
		for len(out) < fileCompletionLimit {
			if err = ctx.Err(); err != nil {
				_ = f.Close()
				return out, err
			}
			batch, readErr := f.ReadDir(256)
			for _, entry := range batch {
				if len(out) >= fileCompletionLimit {
					break
				}
				name := filepath.ToSlash(filepath.Join(current.relative, entry.Name()))
				if !allowedFileName(name) {
					continue
				}
				dir := entry.IsDir()
				item := fileEntry{name: name, dir: dir}
				if info, err := entry.Info(); err == nil {
					item.modTime = info.ModTime()
				}
				out = append(out, item)
				if recursive && dir && current.depth < 10 {
					queue = append(queue, directory{name, current.depth + 1})
				}
			}
			if readErr != nil {
				if readErr != io.EOF && current.relative == "" {
					_ = f.Close()
					return out, readErr
				}
				break
			}
		}
		_ = f.Close()
	}
	return out, nil
}
