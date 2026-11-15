package runtime

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	ownedHome := ""
	if os.Getenv("GOUI_RUNTIME_TEST_HOME") == "" {
		home, err := os.MkdirTemp("", "goui-runtime-test-home-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ownedHome = home
		_ = os.Setenv("HOME", home)
		_ = os.Setenv("GOUI_RUNTIME_TEST_HOME", home)
	}
	code := m.Run()
	if ownedHome != "" {
		_ = os.RemoveAll(ownedHome)
	}
	os.Exit(code)
}
