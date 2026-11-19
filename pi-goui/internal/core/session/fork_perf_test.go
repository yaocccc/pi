package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Include checkpoint comparison, name allocation, source identity validation,
// and writing native JSONL as used by the UI. Exclude rendering and RPC.
func BenchmarkLongForkPersistence(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, "sessions")
	if err := os.Mkdir(dir, 0700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "source.jsonl")
	f, e := os.Create(path)
	if e != nil {
		b.Fatal(e)
	}
	fmt.Fprintf(f, "{\"type\":\"session\",\"id\":\"s\",\"version\":3,\"cwd\":%q}\n", root)
	parent := "null"
	body := strings.Repeat("x", 8192)
	for i := range 768 {
		for _, role := range []string{"user", "assistant"} {
			id := fmt.Sprintf("%s%d", role, i)
			fmt.Fprintf(f, "{\"type\":\"message\",\"id\":%q,\"parentId\":%s,\"message\":{\"role\":%q,\"content\":%q,\"timestamp\":%d,\"stopReason\":\"stop\"}}\n", id, parent, role, body, i)
			parent = fmt.Sprintf("%q", id)
		}
	}
	if e = f.Close(); e != nil {
		b.Fatal(e)
	}
	snap, e := ReadFile(path)
	if e != nil {
		b.Fatal(e)
	}
	var s State
	if e = s.LoadEntries(snap); e != nil {
		b.Fatal(e)
	}
	c := s.ForkCheckpoint()
	selected := "user760"
	b.ReportAllocs()
	b.SetBytes(int64(len(snap)))
	b.ResetTimer()
	for range b.N {
		p, e := PrepareFork(selected, snap)
		if e != nil || !c.Matches(p.Review()) {
			b.Fatal(e)
		}
		branch, e := p.PersistNumbered(root, path, "s", root, "source")
		if e != nil {
			b.Fatal(e)
		}
		if e = os.Remove(branch.Path); e != nil {
			b.Fatal(e)
		}
	}
}
