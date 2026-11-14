//go:build unix

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDesktopRegistrySecurityAndCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	endpoint, err := OpenDesktop(agent, "private-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	registry := filepath.Join(os.Getenv("HOME"), ".pi", desktopRegistry)
	if _, err := os.Stat(filepath.Join(agent, desktopRegistry)); !os.IsNotExist(err) {
		t.Fatal("registry still created under agent directory", err)
	}
	file := filepath.Join(registry, endpoint.ID+".json")
	record, ok := ReadDesktopRecord(agent, endpoint.ID)
	if !ok || endpoint.HTTPClient.Timeout != 0 {
		t.Fatal("invalid endpoint client/record")
	}
	for _, path := range []string{registry, filepath.Dir(record.Socket), file, record.Socket} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("not private: %s %v", path, err)
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(){
		"world-readable":           func() { _ = os.Chmod(file, 0644) },
		"oversized":                func() { _ = os.WriteFile(file, []byte(strings.Repeat("x", 8193)), 0600) },
		"bad-json":                 func() { _ = os.WriteFile(file, []byte("{"), 0600) },
		"wrong-id":                 func() { bad := record; bad.ID = "wrong"; b, _ := json.Marshal(bad); _ = os.WriteFile(file, b, 0600) },
		"file-symlink":             func() { _ = os.Remove(file); _ = os.Symlink(filepath.Join(agent, "elsewhere"), file) },
		"socket-world-readable":    func() { _ = os.Chmod(record.Socket, 0666) },
		"directory-world-readable": func() { _ = os.Chmod(filepath.Dir(record.Socket), 0755) },
	} {
		t.Run(name, func(t *testing.T) {
			change()
			if _, ok := ReadDesktopRecord(agent, endpoint.ID); ok {
				t.Fatal("untrusted record accepted")
			}
			_ = os.Remove(file)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(record.Socket, 0600)
			_ = os.Chmod(filepath.Dir(record.Socket), 0700)
		})
	}
	for _, id := range []string{"../escape", endpoint.ID + "/..", "", "../../" + endpoint.ID} {
		if _, ok := ReadDesktopRecord(agent, id); ok {
			t.Fatal("path traversal accepted")
		}
	}
	if err := os.Chmod(registry, 0755); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadDesktopRecord(agent, endpoint.ID); ok {
		t.Fatal("untrusted registry accepted")
	}
	_ = os.Chmod(registry, 0700)
	// Cleanup must not delete a descriptor replaced by somebody else. This
	// fixture replaced it above; remove that replacement explicitly afterward.
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(file); err != nil {
		t.Fatal("Close deleted replacement descriptor")
	}
	if _, err := os.Lstat(record.Socket); !os.IsNotExist(err) {
		t.Fatal("socket not removed")
	}
	if _, err := os.Lstat(filepath.Dir(record.Socket)); !os.IsNotExist(err) {
		t.Fatal("private socket directory not removed")
	}
	_ = os.Remove(file)
	fresh, err := OpenDesktop(agent, "another-private-secret")
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close()
	_ = fresh.Close()
	if _, err := os.Lstat(filepath.Join(registry, fresh.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("owned registration not removed")
	}
}

func TestDesktopRejectsRegistryAndSocketSymlinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	endpoint, err := OpenDesktop(agent, "local-service-token")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	record, ok := ReadDesktopRecord(agent, endpoint.ID)
	if !ok {
		t.Fatal("registration missing")
	}
	registry := filepath.Join(os.Getenv("HOME"), ".pi", desktopRegistry)
	moved := filepath.Join(agent, "real-registry")
	if err := os.Rename(registry, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadDesktopRecord(agent, endpoint.ID); ok {
		t.Fatal("symlink registry accepted")
	}
	if _, err := OpenDesktop(agent, "another-local-token"); err == nil {
		t.Fatal("registered through symlink directory")
	}
	_ = os.Remove(registry)
	if err := os.Rename(moved, registry); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Dir(filepath.Dir(record.Socket)), "pi-goui-desktop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(dir, "desktop.sock")); _ = os.Remove(dir) })
	if err := os.Symlink(record.Socket, filepath.Join(dir, "desktop.sock")); err != nil {
		t.Fatal(err)
	}
	if checkedSocket(filepath.Join(dir, "desktop.sock")) {
		t.Fatal("symlink socket accepted")
	}
	alias := filepath.Join(agent, "pi-goui-desktop-alias")
	if err := os.Symlink(filepath.Dir(record.Socket), alias); err != nil {
		t.Fatal(err)
	}
	if checkedSocket(filepath.Join(alias, "desktop.sock")) {
		t.Fatal("symlink socket parent accepted")
	}
}

type desktopForeignOwnerInfo struct{ os.FileInfo }

func (i desktopForeignOwnerInfo) Sys() any {
	st := *i.FileInfo.Sys().(*syscall.Stat_t)
	st.Uid++
	return &st
}
func TestDesktopRejectsForeignOwner(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if privateInfo(desktopForeignOwnerInfo{info}, true) {
		t.Fatal("foreign-owner private directory accepted")
	}
}
