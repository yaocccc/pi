//go:build !unix

package desktop

import "testing"

func TestDesktopUnsupported(t *testing.T) {
	if DesktopSharingSupported() {
		t.Fatal("desktop sharing supported without Unix sockets")
	}
	if endpoint, err := OpenDesktop("agent", "token"); err == nil || endpoint != nil {
		t.Fatal("unsupported registration succeeded")
	}
	if _, ok := ReadDesktopRecord("agent", "id"); ok || len(ReadDesktopRecords("agent")) != 0 {
		t.Fatal("unsupported discovery returned records")
	}
}
