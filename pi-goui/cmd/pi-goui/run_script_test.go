package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunScriptPreservesArguments(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix launcher")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	script, err := os.ReadFile("../../run.sh")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, dir := range []string{"bin", "tools"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"run.sh":      string(script),
		"tools/go":    "#!/bin/sh\nexit 0\n",
		"bin/pi-goui": "#!/bin/sh\nprintf '%s\\n' \"$@\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"--cwd", "a path with spaces"}, {"--share=false"}} {
		cmd := exec.Command(bash, append([]string{filepath.Join(root, "run.sh")}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "tools")+":"+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("launcher: %v: %s", err, out)
		}
		want := strings.Join(args, "\n") + "\n"
		if string(out) != want {
			t.Fatalf("launcher arguments: %q, want %q", out, want)
		}
	}
}
