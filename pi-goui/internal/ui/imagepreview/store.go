// Package imagepreview owns private image files and external viewer lifetimes.
package imagepreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"pi-goui/internal/session"
)

// MaxBytes bounds decoded image data for both preview and image input.
const MaxBytes = 20 << 20

// Store owns a private image cache. Its zero value is ready for use; callers must Close it.
// opener is a test seam; assign it before using the store, never concurrently.
// Files are removed on Close, not when a launcher exits: launchers may delegate
// to an already-running viewer. A viewer needing the original file after the TUI
// exits will no longer find it; already-loaded image windows are not terminated.
type Store struct {
	mu     sync.Mutex
	dir    string
	closed bool
	opener func(string) error
}

func decodePreviewImage(img session.Image) ([]byte, string, error) {
	if img.Data == "" {
		return nil, "", errors.New("empty image")
	}
	if len(img.Data) > base64.StdEncoding.EncodedLen(MaxBytes) {
		return nil, "", errors.New("image exceeds 20 MiB")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(img.Data)
	if err != nil {
		return nil, "", errors.New("invalid image encoding")
	}
	if len(data) == 0 {
		return nil, "", errors.New("empty image")
	}
	if len(data) > MaxBytes {
		return nil, "", errors.New("image exceeds 20 MiB")
	}
	// Never choose a filename or application from the untrusted declared MIME.
	var extension string
	switch http.DetectContentType(data) {
	case "image/png":
		extension = ".png"
	case "image/jpeg":
		extension = ".jpg"
	case "image/gif":
		extension = ".gif"
	case "image/webp":
		extension = ".webp"
	default:
		return nil, "", errors.New("preview supports PNG, JPEG, GIF and WebP images only")
	}
	if extension == ".webp" {
		// WebP has no standard-library decoder. Check its RIFF container and first
		// chunk bounds in addition to MIME magic, without decoding untrusted pixels.
		if len(data) < 20 || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
			return nil, "", errors.New("invalid WebP image container")
		}
		size := uint64(binary.LittleEndian.Uint32(data[16:20]))
		if size == 0 || 20+size+(size&1) > uint64(len(data)) {
			return nil, "", errors.New("invalid WebP image chunk")
		}
	} else {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			return nil, "", errors.New("invalid image header")
		}
	}
	return data, extension, nil
}

func (s *Store) prepareLocked(data []byte, extension string) (string, error) {
	if s.closed {
		return "", errors.New("image preview is closed")
	}
	if s.dir == "" {
		dir, err := os.MkdirTemp("", "pi-goui-images-")
		if err != nil {
			return "", errors.New("cannot create private image preview directory")
		}
		s.dir = dir
	}
	sum := sha256.Sum256(data)
	path := filepath.Join(s.dir, fmt.Sprintf("%x%s", sum, extension))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		// Reuse only an intact private file, never follow a replaced symlink.
		st, statErr := os.Lstat(path)
		if statErr != nil || !st.Mode().IsRegular() || st.Size() != int64(len(data)) {
			return "", errors.New("image preview cache file changed")
		}
		saved, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(saved, data) {
			return "", errors.New("image preview cache file changed")
		}
		return path, nil
	}
	if err != nil {
		return "", errors.New("cannot create private image preview file")
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", errors.New("cannot write image preview")
	}
	return path, nil
}

func (s *Store) prepare(img session.Image) (string, error) {
	data, ext, err := decodePreviewImage(img)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prepareLocked(data, ext)
}

// Open validates and privately caches an image, then launches its viewer.
func (s *Store) Open(img session.Image) error {
	data, ext, err := decodePreviewImage(img)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.prepareLocked(data, ext)
	if err != nil {
		return err
	}
	opener := s.opener
	if opener == nil {
		opener = startImageViewer
	}
	// Hold the lifecycle lock through bounded launch acknowledgement, so Close
	// cannot remove the file between preparing it and starting the viewer.
	return opener(path)
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
		s.dir = ""
	}
}

func imageViewerCommand(platform, path string) (*exec.Cmd, error) {
	switch platform {
	case "linux":
		return exec.Command("xdg-open", path), nil
	case "darwin":
		return exec.Command("open", path), nil
	default:
		return nil, fmt.Errorf("system image preview is not supported on %s", platform)
	}
}

func launchImageViewer(cmd *exec.Cmd, immediate time.Duration) error {
	// No shell, inherited terminal handles, input, or unsanitized child output.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start system image viewer: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }() // Reap even when the viewer outlives this command.
	timer := time.NewTimer(immediate)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("system image viewer failed: %w", err)
		}
		return nil
	case <-timer.C:
		// Some launchers wait for their viewer window; do not kill that window or
		// hold the TUI command until it closes. Later errors cannot be acknowledged.
		return nil
	}
}

func startImageViewer(path string) error {
	cmd, err := imageViewerCommand(runtime.GOOS, path)
	if err != nil {
		return err
	}
	return launchImageViewer(cmd, 200*time.Millisecond)
}
