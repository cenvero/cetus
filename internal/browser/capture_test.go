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
	defer func() { _ = cache.close() }()
	pngData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	if err := cache.write(3, pngData); err != nil {
		t.Fatalf("write returned error: %v", err)
	}

	resumeCache, err := newFrameCache(CaptureOptions{FramesDir: dir, Resume: true}, comp)
	if err != nil {
		t.Fatalf("resume newFrameCache returned error: %v", err)
	}
	defer func() { _ = resumeCache.close() }()
	got, ok, err := resumeCache.read(3)
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if !ok || string(got) != string(pngData) {
		t.Fatalf("cached frame = %q, %v; want valid PNG data, true", string(got), ok)
	}

	mismatched := *comp
	mismatched.Width = 1280
	if mismatchedCache, err := newFrameCache(CaptureOptions{FramesDir: dir, Resume: true}, &mismatched); err == nil {
		_ = mismatchedCache.close()
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

	cache, err := newFrameCache(CaptureOptions{FramesDir: dir}, comp)
	if err != nil {
		t.Fatalf("newFrameCache returned error: %v", err)
	}
	defer func() { _ = cache.close() }()
	if _, err := os.Stat(oldFrame); !os.IsNotExist(err) {
		t.Fatalf("old frame still exists, stat error: %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("non-frame file was removed: %v", err)
	}
}

func TestFrameCacheExistsValidatesPNGHeader(t *testing.T) {
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
	defer func() { _ = cache.close() }()

	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	if err := cache.write(3, validPNG); err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	ok, err := cache.exists(3)
	if err != nil {
		t.Fatalf("exists returned error: %v", err)
	}
	if !ok {
		t.Fatal("exists returned false for valid PNG")
	}

	if err := os.WriteFile(filepath.Join(dir, frameFileName(4)), []byte("not png"), 0o600); err != nil {
		t.Fatalf("write invalid frame: %v", err)
	}
	ok, err = cache.exists(4)
	if err != nil {
		t.Fatalf("exists returned error for invalid PNG: %v", err)
	}
	if ok {
		t.Fatal("exists returned true for invalid PNG")
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

func TestEffectiveCaptureWorkers(t *testing.T) {
	tests := []struct {
		name   string
		input  int
		frames int
		want   int
	}{
		{name: "defaults to one", input: 0, frames: 30, want: 1},
		{name: "keeps requested worker count", input: 4, frames: 30, want: 4},
		{name: "caps to frame count", input: 8, frames: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCaptureWorkers(tt.input, tt.frames); got != tt.want {
				t.Fatalf("effectiveCaptureWorkers() = %d, want %d", got, tt.want)
			}
		})
	}
}
