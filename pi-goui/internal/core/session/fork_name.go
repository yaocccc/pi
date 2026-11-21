package session

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Serialize allocation and persistence so simultaneous forks in this process
// cannot choose the same name. CreateFork remains available for explicit names.
var forkNameMu sync.Mutex

func CreateNumberedFork(root, sourcePath, sourceID, cwd, name, selected string, snapshot json.RawMessage) (Fork, error) {
	p, err := PrepareFork(selected, snapshot)
	if err != nil {
		return Fork{}, err
	}
	return p.createNumbered(root, sourcePath, sourceID, cwd, name, true)
}

// PersistNumbered writes a validated prefix without building a second RPC-sized
// buffer. The UI prepares its projection from the resulting file identity.
func (p *ForkPlan) PersistNumbered(root, sourcePath, sourceID, cwd, name string) (Fork, error) {
	return p.createNumbered(root, sourcePath, sourceID, cwd, name, false)
}

func (p *ForkPlan) createNumbered(root, sourcePath, sourceID, cwd, name string, includeEntries bool) (Fork, error) {
	if cwd == "" {
		return Fork{}, fmt.Errorf("fork source project directory is unavailable")
	}
	forkNameMu.Lock()
	defer forkNameMu.Unlock()
	// Number within the source project, not across unrelated projects. List
	// checks cwd before reading history.
	list, err := List(root, cwd)
	if err != nil {
		return Fork{}, err
	}
	// Explicitly opened sessions may live outside the configured session root.
	// Avoid reading their directory twice when List already covered it.
	rel, relErr := filepath.Rel(filepath.Join(root, "sessions"), filepath.Dir(sourcePath))
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		siblings, err := listSessions(filepath.Dir(sourcePath), cwd, nil, nil)
		if err != nil {
			return Fork{}, err
		}
		list = append(list, siblings...)
	}
	name, err = nextForkName(name, list)
	if err != nil {
		return Fork{}, err
	}
	return p.create(sourcePath, sourceID, cwd, name, includeEntries)
}

func forkNameParts(name string) (int, string) {
	if !strings.HasPrefix(name, "[") {
		return 0, name
	}
	end := strings.IndexByte(name, ']')
	if end < 2 {
		return 0, name
	}
	n, err := strconv.Atoi(name[1:end])
	if err != nil || n < 1 {
		return 0, name
	}
	return n, name[end+1:]
}

func nextForkName(name string, list []Summary) (string, error) {
	n, base := forkNameParts(name)
	for _, saved := range list {
		index, candidate := forkNameParts(saved.Name)
		if candidate == base && index > n {
			n = index
		}
	}
	if n == int(^uint(0)>>1) {
		return "", fmt.Errorf("fork name counter exhausted")
	}
	return fmt.Sprintf("[%d]%s", n+1, base), nil
}
