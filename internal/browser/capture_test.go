package browser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cenvero/cetus/internal/compose"
)

func TestFrameCacheWritesReadsAndValidatesManifest(t *testing.T) {
	dir := t.TempDir()
	comp := &compose.Composition{
		ID:          "intro",
		Width:       640,
		Height:      360,
		FPS:         30,
		Duration:    1,
		TotalFrames: 30,
	}

	cache, err := newFrameCache(CaptureOptions{FramesDir: dir}, comp)
	if err != nil {
		t.Fatalf("newFrameCache returned error: %v", err)
	}
	if err := cache.write(3, []byte("png")); err != nil {
		t.Fatalf("write returned error: %v", err)
	}

	resumeCache, err := newFrameCache(CaptureOptions{FramesDir: dir, Resume: true}, comp)
	if err != nil {
		t.Fatalf("resume newFrameCache returned error: %v", err)
	}
	got, ok, err := resumeCache.read(3)
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if !ok || string(got) != "png" {
		t.Fatalf("cached frame = %q, %v; want png, true", string(got), ok)
	}

	mismatched := *comp
	mismatched.Width = 1280
	if _, err := newFrameCache(CaptureOptions{FramesDir: dir, Resume: true}, &mismatched); err == nil {
		t.Fatal("newFrameCache accepted mismatched manifest")
	}
}

func TestFrameCacheClearsOldFramesWhenNotResuming(t *testing.T) {
	dir := t.TempDir()
	comp := &compose.Composition{
		ID:          "intro",
		Width:       640,
		Height:      360,
		FPS:         30,
		Duration:    1,
		TotalFrames: 30,
	}
	oldFrame := filepath.Join(dir, "frame-000003.png")
	if err := os.WriteFile(oldFrame, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old frame: %v", err)
	}
	keepFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(keepFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write keep file: %v", err)
	}

	if _, err := newFrameCache(CaptureOptions{FramesDir: dir}, comp); err != nil {
		t.Fatalf("newFrameCache returned error: %v", err)
	}
	if _, err := os.Stat(oldFrame); !os.IsNotExist(err) {
		t.Fatalf("old frame still exists, stat error: %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("non-frame file was removed: %v", err)
	}
}

func TestFrameCacheRejectsResumeWithoutManifestWhenFramesExist(t *testing.T) {
	dir := t.TempDir()
	comp := &compose.Composition{
		ID:          "intro",
		Width:       640,
		Height:      360,
		FPS:         30,
		Duration:    1,
		TotalFrames: 30,
	}
	if err := os.WriteFile(filepath.Join(dir, "frame-000003.png"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old frame: %v", err)
	}

	if _, err := newFrameCache(CaptureOptions{FramesDir: dir, Resume: true}, comp); err == nil {
		t.Fatal("newFrameCache accepted frame files without a manifest")
	}
}
