//go:build unix

package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pi-goui/internal/runtime"
)

const desktopRegistry = "goui-instances"

func DesktopSharingSupported() bool { return true }

func privateInfo(info os.FileInfo, directory bool) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid() && info.Mode()&os.ModeSymlink == 0 &&
		info.IsDir() == directory && info.Mode().Perm()&0077 == 0
}

func openDesktopRegistry(agentDir string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(agentDir) {
		return nil, errors.New("desktop discovery requires an absolute agent directory")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, errors.New("desktop discovery requires an absolute home directory")
	}
	parent := filepath.Join(home, ".pi")
	path := filepath.Join(parent, desktopRegistry)
	if create {
		if err := os.MkdirAll(parent, 0700); err != nil {
			return nil, err
		}
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	before, err := os.Lstat(path)
	if err != nil || !privateInfo(before, true) {
		return nil, errors.New("desktop registry must be a private, owned directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, errors.New("desktop registry identity changed")
	}
	return root, nil
}

func validDesktopID(id string) bool {
	if len(id) < 20 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func checkedSocket(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "desktop.sock" {
		return false
	}
	dir := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), "pi-goui-desktop-") {
		return false
	}
	// Resolve all ancestors as well: neither a descriptor nor a socket path
	// may smuggle symlinks past the private-directory checks.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || real != dir {
		return false
	}
	d, err := os.Lstat(dir)
	if err != nil || !privateInfo(d, true) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && privateInfo(info, false) && info.Mode()&os.ModeSocket != 0
}

func desktopClient(socket string) *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "desktop.local:80" || !checkedSocket(socket) {
				return nil, errors.New("invalid private desktop socket")
			}
			return (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(ctx, "unix", socket)
		},
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       time.Second,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func openDesktop(agentDir, token string) (*DesktopEndpoint, error) {
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid private desktop token")
	}
	if agentDir == "" {
		return nil, errors.New("agent directory is empty")
	}
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return nil, err
	}
	agentDir, err := runtime.CanonicalAgentDir(agentDir)
	if err != nil {
		return nil, err
	}
	root, err := openDesktopRegistry(agentDir, true)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			root.Close()
		}
	}()
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	// /tmp (rather than a potentially long agent or TMPDIR path) keeps Unix
	// paths below sockaddr_un limits. MkdirTemp creates an owned 0700 directory.
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(base, "pi-goui-desktop-")
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, "desktop.sock")
	defer func() {
		if !success {
			_ = os.Remove(socket)
			_ = os.Remove(dir)
		}
	}()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !success {
			listener.Close()
		}
	}()
	if err = os.Chmod(socket, 0600); err != nil {
		return nil, err
	}
	data, err := json.Marshal(Record{Version: 1, ID: id, Socket: socket, AgentDir: filepath.Clean(agentDir), Token: token})
	if err != nil {
		return nil, err
	}
	name := id + ".json"
	tmp := id + ".tmp"
	file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(data)
	identity, statErr := file.Stat()
	closeErr := file.Close()
	if err = errors.Join(writeErr, statErr, closeErr); err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
		return nil, err
	}
	client := desktopClient(socket)
	success = true
	return &DesktopEndpoint{ID: id, Listener: listener, HTTPClient: client, cleanup: func() {
		if current, err := root.Lstat(name); err == nil && os.SameFile(identity, current) && current.Size() == identity.Size() && current.ModTime().Equal(identity.ModTime()) {
			_ = root.Remove(name)
		}
		_ = root.Close()
		// net.UnixListener unlinks its own socket on Close. Remove only this
		// empty private directory, never recursively delete arbitrary contents.
		_ = os.Remove(dir)
	}}, nil
}

func recordFromRoot(root *os.Root, agentDir, id string) (Record, bool) {
	var record Record
	if !validDesktopID(id) {
		return record, false
	}
	name := id + ".json"
	before, err := root.Lstat(name)
	if err != nil || !privateInfo(before, false) || !before.Mode().IsRegular() || before.Size() > 8192 {
		return record, false
	}
	// Root prevents path escapes, and the explicit lstat/identity comparison
	// rejects a replaced symlink or file rather than trusting its contents.
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return record, false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !privateInfo(opened, false) {
		return record, false
	}
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(data) > 8192 || json.Unmarshal(data, &record) != nil {
		return Record{}, false
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || !privateInfo(after, false) {
		return Record{}, false
	}
	if !filepath.IsAbs(record.AgentDir) {
		return Record{}, false
	}
	canonical, err := runtime.CanonicalAgentDir(record.AgentDir)
	if err != nil {
		return Record{}, false
	}
	if record.Version != 1 || record.ID != id || canonical != agentDir || record.Token == "" || len(record.Token) > 4096 || strings.ContainsAny(record.Token, "\r\n") || !checkedSocket(record.Socket) {
		return Record{}, false
	}
	record.AgentDir = canonical
	return record, true
}

// ReadDesktopRecord validates a private registration in the caller's agent scope.
func ReadDesktopRecord(agentDir, id string) (Record, bool) {
	agentDir, err := runtime.CanonicalAgentDir(agentDir)
	if err != nil {
		return Record{}, false
	}
	root, err := openDesktopRegistry(agentDir, false)
	if err != nil {
		return Record{}, false
	}
	defer root.Close()
	return recordFromRoot(root, agentDir, id)
}

// ReadDesktopRecords lists valid private records without probing or deleting them.
func ReadDesktopRecords(agentDir string) []Record {
	agentDir, err := runtime.CanonicalAgentDir(agentDir)
	if err != nil {
		return nil
	}
	root, err := openDesktopRegistry(agentDir, false)
	if err != nil {
		return nil
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil
	}
	defer dir.Close()
	entries, _ := dir.ReadDir(1024) // Bound local discovery work and memory.
	var records []Record
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			if record, ok := recordFromRoot(root, agentDir, strings.TrimSuffix(entry.Name(), ".json")); ok {
				records = append(records, record)
			}
		}
	}
	return records
}
