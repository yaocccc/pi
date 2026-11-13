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
	forkNameMu.Lock()
	defer forkNameMu.Unlock()
	list, err := List(root, "")
	if err != nil {
		return Fork{}, err
	}
	// Explicitly opened sessions may live outside the configured session root.
	// Avoid reading their directory twice when List already covered it.
	rel, relErr := filepath.Rel(filepath.Join(root, "sessions"), filepath.Dir(sourcePath))
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		siblings, err := listSessions(filepath.Dir(sourcePath), "")
		if err != nil {
			return Fork{}, err
		}
		list = append(list, siblings...)
	}
	name, err = nextForkName(name, list)
	if err != nil {
		return Fork{}, err
	}
	return CreateFork(sourcePath, sourceID, cwd, name, selected, snapshot)
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
