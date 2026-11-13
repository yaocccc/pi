package imagepreview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-goui/internal/core/session"
)

func previewFixture(t *testing.T) (session.Image, []byte) {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	return session.Image{Data: base64.StdEncoding.EncodeToString(data), MimeType: "image/png"}, data
}

func TestImagePreviewPrivateFilesAndCache(t *testing.T) {
	img, data := previewFixture(t)
	// The declared MIME and content cannot choose executable extensions or paths.
	img.MimeType = "text/html; name=../../unsafe.html"
	var store Store
	t.Cleanup(store.Close)
	path, err := store.prepare(img)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".png" || filepath.Dir(path) != store.dir {
		t.Fatal(path)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("preview changed original bytes", err)
	}
	if runtime.GOOS != "windows" {
		dir, _ := os.Stat(store.dir)
		file, _ := os.Stat(path)
		if dir.Mode().Perm() != 0700 || file.Mode().Perm() != 0600 {
			t.Fatal("preview is not private")
		}
	}
	again, err := store.prepare(img)
	if err != nil || again != path {
		t.Fatal("duplicate image not cached", err)
	}
	entries, _ := os.ReadDir(store.dir)
	if len(entries) != 1 {
		t.Fatal("duplicate preview files")
	}
	dir := store.dir
	store.Close()
	store.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("private directory survived Close")
	}
	if _, err := store.prepare(img); err == nil || store.dir != "" {
		t.Fatal("late prepare recreated files")
	}
}

func TestImagePreviewRejectsUnsafeInvalidAndOversized(t *testing.T) {
	for _, data := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("<html><script>unsafe</script></html>")), base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n")), strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxBytes)+4)} {
		var store Store
		_, err := store.prepare(session.Image{Data: data, MimeType: "image/png"})
		if err == nil {
			t.Fatal("accepted unsafe or invalid content")
		}
		if store.dir != "" {
			t.Fatal("invalid content allocated preview directory")
		}
		if strings.Contains(err.Error(), "<script>") {
			t.Fatal("image data leaked in error")
		}
	}
}

func TestImagePreviewWebPMagicAndContainer(t *testing.T) {
	img := session.Image{Data: "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA", MimeType: "application/octet-stream"}
	data, extension, err := decodePreviewImage(img)
	if err != nil || extension != ".webp" {
		t.Fatalf("valid WebP container rejected: %v", err)
	}
	data[4]++
	img.Data = base64.StdEncoding.EncodeToString(data)
	if _, _, err = decodePreviewImage(img); err == nil {
		t.Fatal("invalid RIFF length accepted")
	}
}

func TestImagePreviewRejectsChangedCacheFile(t *testing.T) {
	img, _ := previewFixture(t)
	var store Store
	defer store.Close()
	path, err := store.prepare(img)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.prepare(img); err == nil {
		t.Fatal("changed preview reused")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "untouched")
	if err = os.WriteFile(external, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(external, path); err != nil {
		t.Skip("symlink unavailable")
	}
	if _, err = store.prepare(img); err == nil {
		t.Fatal("preview followed symlink")
	}
	got, _ := os.ReadFile(external)
	if string(got) != "safe" {
		t.Fatal("external file changed")
	}
}

func TestImagePreviewCommandAndStartupFailure(t *testing.T) {
	img, _ := previewFixture(t)
	calls := 0
	store := Store{opener: func(path string) error {
		calls++
		if _, err := os.Stat(path); err != nil {
			t.Fatal("viewer started without image")
		}
		return errors.New("fixture launch failure")
	}}
	defer store.Close()
	if err := store.Open(img); err == nil || calls != 1 {
		t.Fatalf("incorrect launch result: %v (calls=%d)", err, calls)
	}
	store.Close()
	if err := store.Open(img); err == nil || calls != 1 {
		t.Fatal("launch after Close")
	}
	for _, tc := range []struct{ platform, name string }{{"linux", "xdg-open"}, {"darwin", "open"}} {
		path := "/tmp/private image;$(unsafe).png"
		cmd, err := imageViewerCommand(tc.platform, path)
		if err != nil || len(cmd.Args) != 2 || cmd.Args[0] != tc.name || cmd.Args[1] != path {
			t.Fatalf("unsafe command: %+v, %v", cmd, err)
		}
	}
	if _, err := imageViewerCommand("unsupported", "/tmp/image.png"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	if err := launchImageViewer(exec.Command(filepath.Join(t.TempDir(), "missing-viewer")), time.Second); err == nil {
		t.Fatal("missing viewer not reported")
	}
}

func TestImagePreviewCloseWaitsForLaunchAndPreventsLateWrites(t *testing.T) {
	img, _ := previewFixture(t)
	entered, release, done, closed := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan struct{})
	store := Store{opener: func(path string) error {
		close(entered)
		<-release
		_, err := os.Stat(path)
		return err
	}}
	go func() { done <- store.Open(img) }()
	<-entered
	go func() { store.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close removed file during launch")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-closed
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.prepare(img); err == nil {
				t.Error("prepare after close succeeded")
			}
		}()
	}
	wg.Wait()
	if store.dir != "" {
		t.Fatal("late private directory leaked")
	}
}

func TestImagePreviewHelperProcess(t *testing.T) {
	switch os.Getenv("GOUI_IMAGE_PREVIEW_HELPER") {
	case "fail":
		os.Exit(7)
	case "slow":
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}
}

func TestImagePreviewLauncherAcknowledgementIsBounded(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(mode string) *exec.Cmd {
		cmd := exec.Command(executable, "-test.run=^TestImagePreviewHelperProcess$")
		cmd.Env = append(os.Environ(), "GOUI_IMAGE_PREVIEW_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
		return cmd
	}
	if err = launchImageViewer(child("fail"), 3*time.Second); err == nil {
		t.Fatal("immediate failure not reported")
	}
	started := time.Now()
	if err = launchImageViewer(child("slow"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 400*time.Millisecond {
		t.Fatal("launcher waited for viewer lifetime")
	}
	// Only a dedicated test helper ran. Allow its Wait goroutine to reap it.
	time.Sleep(650 * time.Millisecond)
}
