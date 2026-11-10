// Package updates checks the selected Pi installation's own update APIs.
// Checks are read-only; this package has no installation or update command.
package updates

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed bridge.mjs
var bridge string

const checkTimeout = 20 * time.Second
const maxOutput = 1 << 20
const packageName = "@earendil-works/pi-coding-agent"

var errCheck = errors.New("update metadata unavailable; try again later or update manually")
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.+-]+)?$`)

type Config struct {
	PiBin, AgentDir string
	Offline         bool
}

type Package struct {
	Source      string `json:"source"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
	Scope       string `json:"scope"`
}

type Report struct {
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion"`
	Packages       []Package `json:"packages"`
	Offline        bool      `json:"offline"`
	Unsupported    string    `json:"unsupported"`
}

func (r Report) HasUpdates() bool {
	return !r.Offline && r.Unsupported == "" && (r.LatestVersion != "" || len(r.Packages) > 0)
}

type installation struct{ Root, Version string }

func offline(c Config) bool {
	// Native Pi treats every nonempty value (including "0") as offline.
	return c.Offline || os.Getenv("PI_OFFLINE") != ""
}

func normalize(c Config) (Config, error) {
	if c.PiBin == "" {
		c.PiBin = "pi"
	}
	if c.AgentDir == "" {
		c.AgentDir = os.Getenv("PI_CODING_AGENT_DIR")
		if c.AgentDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return c, errCheck
			}
			c.AgentDir = filepath.Join(home, ".pi", "agent")
		}
	}
	var err error
	c.AgentDir, err = filepath.Abs(c.AgentDir)
	if err != nil {
		return c, errCheck
	}
	return c, nil
}

// discover follows only the selected executable. It does not run npm root or
// guess another Pi installation for wrappers, binaries, or unsupported layouts.
func discover(c Config) (installation, bool) {
	bin, err := exec.LookPath(c.PiBin)
	if err != nil {
		return installation{}, false
	}
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		return installation{}, false
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return installation{}, false
	}
	// The declared CLI may live in dist/cli.js or dist/bundle/cli.js.
	// Find its nearest package manifest, then verify the name and exact bin
	// declaration below; never fall back to an unrelated Pi on PATH.
	root := filepath.Dir(bin)
	var f *os.File
	for {
		f, err = os.Open(filepath.Join(root, "package.json"))
		if err == nil {
			break
		}
		parent := filepath.Dir(root)
		if !os.IsNotExist(err) || parent == root {
			return installation{}, false
		}
		root = parent
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxOutput+1))
	if err != nil || len(data) > maxOutput {
		return installation{}, false
	}
	var meta struct {
		Name, Version string
		Bin           json.RawMessage
	}
	if json.Unmarshal(data, &meta) != nil || meta.Name != packageName || !versionPattern.MatchString(meta.Version) {
		return installation{}, false
	}
	var declared string
	if json.Unmarshal(meta.Bin, &declared) != nil {
		var bins map[string]string
		if json.Unmarshal(meta.Bin, &bins) != nil {
			return installation{}, false
		}
		declared = bins["pi"]
	}
	if declared == "" {
		return installation{}, false
	}
	expected, err := filepath.EvalSymlinks(filepath.Join(root, declared))
	if err != nil || expected != bin {
		return installation{}, false
	}
	return installation{Root: root, Version: meta.Version}, true
}

type request struct {
	Root     string `json:"root"`
	Version  string `json:"version"`
	AgentDir string `json:"agentDir"`
}

type boundedOutput struct {
	bytes.Buffer
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := maxOutput - b.Len()
	if n > room {
		b.Buffer.Write(p[:room])
		b.exceeded = true
		b.cancel()
	} else {
		b.Buffer.Write(p)
	}
	return n, nil
}

func environment(c Config) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		// Do not let inherited repository overrides redirect metadata queries.
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		switch key {
		case "PI_CODING_AGENT_DIR", "GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "NODE_OPTIONS", "NODE_PATH":
			continue
		}
		env = append(env, value)
	}
	return append(env, "PI_CODING_AGENT_DIR="+c.AgentDir, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}

func runBridge(ctx context.Context, c Config, inst installation, out any) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	input, _ := json.Marshal(request{inst.Root, inst.Version, c.AgentDir})
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "--eval", bridge)
	cmd.Dir = c.AgentDir
	cmd.Env = environment(c)
	cmd.Stdin = bytes.NewReader(input)
	output := &boundedOutput{cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	configureProcess(cmd)
	defer finishProcess(cmd)
	if err := cmd.Run(); err != nil || output.exceeded {
		if ctx.Err() != nil && !output.exceeded {
			return ctx.Err()
		}
		return errCheck
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(output.Bytes(), &failure) != nil || failure.Error != "" {
		return errCheck
	}
	if json.Unmarshal(output.Bytes(), out) != nil {
		return errCheck
	}
	return nil
}

func Check(ctx context.Context, c Config) (Report, error) {
	if offline(c) {
		return Report{Offline: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	c, err := normalize(c)
	if err != nil {
		return Report{}, err
	}
	inst, ok := discover(c)
	if !ok {
		return Report{Unsupported: "The selected Pi executable is not a supported Node installation; check and update it manually."}, nil
	}
	if _, err := exec.LookPath("node"); err != nil {
		return Report{CurrentVersion: inst.Version, Unsupported: "Node is unavailable; check and update Pi manually."}, nil
	}
	var report Report
	if err := runBridge(ctx, c, inst, &report); err != nil {
		return Report{}, err
	}
	report.CurrentVersion = inst.Version
	if report.LatestVersion != "" && !versionPattern.MatchString(report.LatestVersion) {
		return Report{}, errCheck
	}
	if report.Unsupported != "" {
		// Module compatibility errors must not leak paths or package URLs.
		message := "This Pi installation has no compatible update-check API; update it manually."
		if report.Unsupported == "Custom npm command wrappers require manual update checks." {
			message = report.Unsupported
		}
		return Report{CurrentVersion: inst.Version, Unsupported: message}, nil
	}
	return report, nil
}
