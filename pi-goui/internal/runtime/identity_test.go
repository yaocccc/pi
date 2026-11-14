package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalAgentDirIsPureIdentity(t *testing.T) {
	if _, err := CanonicalAgentDir(""); err == nil {
		t.Fatal("empty agent accepted")
	}
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	if _, err := CanonicalAgentDir(missing); err == nil {
		t.Fatal("missing agent accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("normalization created a directory")
	}
	expected, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := CanonicalAgentDir(filepath.Join(root, "."))
	if err != nil || actual != expected {
		t.Fatal(actual, err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	actual, err = CanonicalAgentDir(alias)
	if err != nil || actual != expected {
		t.Fatal(actual, err)
	}
}
