package updates

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ReleaseNotes contains only the selected Pi installation's current release.
// The private receipt is stored separately from Pi settings and session history.
type ReleaseNotes struct {
	Version, Markdown string
	seenPath          string
}

var releaseHeading = regexp.MustCompile(`(?m)^## \[?v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)\]?(?:[ \t].*)?$`)

func LoadReleaseNotes(c Config) ReleaseNotes {
	c, err := normalize(c)
	if err != nil {
		return ReleaseNotes{}
	}
	inst, ok := discover(c)
	if !ok {
		return ReleaseNotes{}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ReleaseNotes{}
	}
	// Keep the key stable when an upgrade changes the executable's symlink target.
	bin, _ := exec.LookPath(c.PiBin)
	bin, _ = filepath.Abs(bin)
	key := sha256.Sum256([]byte(c.AgentDir + "\x00" + bin))
	path := filepath.Join(cache, "pi-goui", fmt.Sprintf("pi-version-%x", key[:12]))
	return loadReleaseNotes(inst, path)
}

func readLimited(path string, limit int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return ""
	}
	return string(data)
}

func loadReleaseNotes(inst installation, seenPath string) ReleaseNotes {
	if strings.TrimSpace(readLimited(seenPath, 256)) == inst.Version {
		return ReleaseNotes{}
	}
	changelog := readLimited(filepath.Join(inst.Root, "CHANGELOG.md"), 2<<20)
	matches := releaseHeading.FindAllStringSubmatchIndex(changelog, -1)
	for i, match := range matches {
		if changelog[match[2]:match[3]] != inst.Version {
			continue
		}
		end := len(changelog)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := strings.TrimSpace(changelog[match[1]:end])
		if body != "" {
			return ReleaseNotes{Version: inst.Version, Markdown: body, seenPath: seenPath}
		}
	}
	return ReleaseNotes{}
}

// MarkSeen is called only after the UI presents the release notes, not when the
// metadata is read. Interrupted/failed startup therefore leaves them available.
func (n ReleaseNotes) MarkSeen() error {
	if n.seenPath == "" || n.Version == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(n.seenPath), 0700); err != nil {
		return err
	}
	return os.WriteFile(n.seenPath, []byte(n.Version+"\n"), 0600)
}
