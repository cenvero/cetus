package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseComposition(t *testing.T) {
	path := writeComposition(t, `<!doctype html>
<html>
<body>
  <div id="root" data-composition-id="intro" data-width="1920" data-height="1080" data-duration="5" data-fps="24">
    <h1 id="title" class="clip hero" data-start="0.5" data-duration="4" data-track-index="2" data-volume="0.4">Hello</h1>
  </div>
</body>
</html>`)

	comp, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if comp.ID != "intro" {
		t.Fatalf("ID = %q, want intro", comp.ID)
	}
	if comp.Width != 1920 || comp.Height != 1080 || comp.FPS != 24 || comp.TotalFrames != 120 {
		t.Fatalf("unexpected composition: %+v", comp)
	}
	if len(comp.Clips) != 1 {
		t.Fatalf("len(Clips) = %d, want 1", len(comp.Clips))
	}
	clip := comp.Clips[0]
	if clip.ID != "title" || clip.Start != 0.5 || clip.Duration != 4 || clip.TrackIndex != 2 || clip.Volume != 0.4 || clip.Element != "h1" {
		t.Fatalf("unexpected clip: %+v", clip)
	}
}

func TestParseRejectsClipPastCompositionDuration(t *testing.T) {
	path := writeComposition(t, `<!doctype html>
<div data-composition-id="intro" data-width="1920" data-height="1080" data-duration="2">
  <h1 class="clip" data-start="1" data-duration="2" data-track-index="0">Hello</h1>
</div>`)

	_, err := Parse(path)
	if err == nil {
		t.Fatal("Parse returned nil error")
	}
	if !strings.Contains(err.Error(), "exceeds composition duration") {
		t.Fatalf("error = %q, want duration validation", err)
	}
}

func TestApplyOverrides(t *testing.T) {
	comp := &Composition{Width: 1920, Height: 1080, FPS: 30, Duration: 1}
	if err := comp.ApplyOverrides(60, 1280, 720); err != nil {
		t.Fatalf("ApplyOverrides returned error: %v", err)
	}
	if comp.FPS != 60 || comp.Width != 1280 || comp.Height != 720 || comp.TotalFrames != 60 {
		t.Fatalf("unexpected composition after overrides: %+v", comp)
	}
}

func writeComposition(t *testing.T, html string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cetus.html")
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
