package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckValidComposition(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("png"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	path := writeHTML(t, dir, `<!doctype html>
<div data-composition-id="intro" data-width="640" data-height="360" data-duration="2" data-fps="30">
  <img id="logo" class="clip" data-start="0" data-duration="2" data-track-index="0" src="logo.png">
</div>
<script>
  window.__timelines = window.__timelines || [];
  const tl = gsap.timeline({ paused: true });
  tl.from("#logo", { opacity: 0, duration: 0.5 });
  window.__timelines.push(tl);
</script>`)

	result, err := Check(path)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.ErrorCount() != 0 || result.WarningCount() != 0 {
		t.Fatalf("unexpected findings: %+v", result.Findings)
	}
	if result.Composition == nil || result.Composition.TotalFrames != 60 {
		t.Fatalf("unexpected composition: %+v", result.Composition)
	}
}

func TestCheckReportsMissingAssetAndGSAPRegistration(t *testing.T) {
	path := writeHTML(t, t.TempDir(), `<!doctype html>
<div data-composition-id="intro" data-width="640" data-height="360" data-duration="2">
  <img id="logo" class="clip" data-start="0" data-duration="2" data-track-index="0" src="missing.png">
</div>
<script>
  const tl = gsap.timeline();
  tl.from("#logo", { opacity: 0, duration: 0.5 });
</script>`)

	result, err := Check(path)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.ErrorCount() != 3 {
		t.Fatalf("ErrorCount() = %d, want 3; findings: %+v", result.ErrorCount(), result.Findings)
	}
	assertFinding(t, result, "missing local asset")
	assertFinding(t, result, "without paused: true")
	assertFinding(t, result, "not registered")
}

func TestCheckWarnsAboutRemoteURLsNoClipsAndInlineBounds(t *testing.T) {
	path := writeHTML(t, t.TempDir(), `<!doctype html>
<div data-composition-id="intro" data-width="640" data-height="360" data-duration="2">
  <div style="position:absolute; left: 800px; top: 10px;">Outside</div>
  <link rel="stylesheet" href="https://example.com/style.css">
</div>`)

	result, err := Check(path)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.ErrorCount() != 0 {
		t.Fatalf("unexpected errors: %+v", result.Findings)
	}
	if result.WarningCount() != 3 {
		t.Fatalf("WarningCount() = %d, want 3; findings: %+v", result.WarningCount(), result.Findings)
	}
	assertFinding(t, result, "remote URL")
	assertFinding(t, result, "no .clip")
	assertFinding(t, result, "outside the 640px frame")
}

func writeHTML(t *testing.T, dir, html string) string {
	t.Helper()
	path := filepath.Join(dir, "cetus.html")
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		t.Fatalf("write html: %v", err)
	}
	return path
}

func assertFinding(t *testing.T, result *Result, want string) {
	t.Helper()
	for _, finding := range result.Findings {
		if strings.Contains(finding.Message, want) {
			return
		}
	}
	t.Fatalf("finding containing %q not found in %+v", want, result.Findings)
}
