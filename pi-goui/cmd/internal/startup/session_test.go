package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionFile(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if got, err := SessionFile(agent, cwd, "", true); got != "" || err != nil {
		t.Fatalf("missing history should open a new session: %q, %v", got, err)
	}

	dir := filepath.Join(agent, "sessions")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	add := func(name, project string, modified time.Time) string {
		t.Helper()
		path := filepath.Join(dir, name+".jsonl")
		header := fmt.Sprintf("{\"type\":\"session\",\"id\":%q,\"cwd\":%q}\n", name, project)
		if err := os.WriteFile(path, []byte(header), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
		return path
	}

	now := time.Now()
	older := add("older", cwd, now.Add(-2*time.Hour))
	latest := add("latest", cwd, now.Add(-time.Hour))
	add("other-project", t.TempDir(), now)
	for _, tc := range []struct {
		name     string
		explicit string
		resume   bool
		want     string
	}{
		{name: "latest in selected project", resume: true, want: latest},
		{name: "explicit overrides preference", explicit: older, resume: true, want: older},
		{name: "disabled ignores history"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SessionFile(agent, cwd, tc.explicit, tc.resume)
			if got != tc.want || err != nil {
				t.Fatalf("SessionFile = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
