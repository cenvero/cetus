package assets

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestExtractBrotliTarChromeBundle(t *testing.T) {
	data := brotliTar(t, []tarEntry{
		{name: "chrome/", mode: 0o755, typ: tar.TypeDir},
		{name: "chrome/" + chromeExeName(), mode: 0o755, body: "browser"},
		{name: "chrome/icudtl.dat", mode: 0o644, body: "icu"},
	})
	dest := t.TempDir()

	if err := extractBrotliTar(data, dest); err != nil {
		t.Fatalf("extractBrotliTar returned error: %v", err)
	}

	chromePath, ok := chromeBundleExecutable(dest)
	if !ok {
		t.Fatal("chromeBundleExecutable returned !ok")
	}
	if filepath.Base(chromePath) != chromeExeName() {
		t.Fatalf("chrome executable = %q, want %q", chromePath, chromeExeName())
	}
	if _, err := os.Stat(filepath.Join(dest, "chrome", "icudtl.dat")); err != nil {
		t.Fatalf("icudtl.dat was not extracted: %v", err)
	}
}

func TestExtractBrotliTarRejectsUnsafePath(t *testing.T) {
	data := brotliTar(t, []tarEntry{
		{name: "../escape", mode: 0o644, body: "bad"},
	})

	err := extractBrotliTar(data, t.TempDir())
	if err == nil {
		t.Fatal("extractBrotliTar returned nil error")
	}
	if !strings.Contains(err.Error(), "unsafe chrome bundle path") {
		t.Fatalf("error = %q, want unsafe path validation", err)
	}
}

func TestChromeBundleExecutableRequiresICUData(t *testing.T) {
	dest := t.TempDir()
	chromePath := filepath.Join(dest, "chrome", chromeExeName())
	if err := os.MkdirAll(filepath.Dir(chromePath), 0o755); err != nil {
		t.Fatalf("create chrome dir: %v", err)
	}
	if err := os.WriteFile(chromePath, []byte("browser"), 0o755); err != nil {
		t.Fatalf("write chrome executable: %v", err)
	}

	if _, ok := chromeBundleExecutable(dest); ok {
		t.Fatal("chromeBundleExecutable returned ok without icudtl.dat")
	}

	if err := os.WriteFile(filepath.Join(dest, "chrome", "icudtl.dat"), []byte("icu"), 0o644); err != nil {
		t.Fatalf("write icudtl.dat: %v", err)
	}
	if _, ok := chromeBundleExecutable(dest); !ok {
		t.Fatal("chromeBundleExecutable returned !ok with executable and icudtl.dat")
	}
}

func TestAssetCacheDirUsesCenveroHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := assetCacheDir("v1.2.3")
	if err != nil {
		t.Fatalf("assetCacheDir returned error: %v", err)
	}
	want := filepath.Join(home, ".cenvero-cetus", "v1.2.3")
	if got != want {
		t.Fatalf("assetCacheDir() = %q, want %q", got, want)
	}
}

func TestAssetCacheDirRejectsUnsafeVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := assetCacheDir("../v1.2.3"); err == nil {
		t.Fatal("assetCacheDir returned nil error")
	}
}

func TestCleanupOldAssetCachesKeepsCurrentAndDev(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".cenvero-cetus")
	for _, name := range []string{"0.3.0", "0.4.0", "dev"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatalf("create cache dir %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	if err := cleanupOldAssetCaches("0.4.0"); err != nil {
		t.Fatalf("cleanupOldAssetCaches returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "0.4.0")); err != nil {
		t.Fatalf("current cache was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dev")); err != nil {
		t.Fatalf("dev cache was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatalf("cache file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "0.3.0")); !os.IsNotExist(err) {
		t.Fatalf("old cache still exists, stat error: %v", err)
	}
}

func TestCleanupOldAssetCachesSkipsDevBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldCache := filepath.Join(home, ".cenvero-cetus", "0.3.0")
	if err := os.MkdirAll(oldCache, 0o700); err != nil {
		t.Fatalf("create old cache dir: %v", err)
	}

	if err := cleanupOldAssetCaches("dev"); err != nil {
		t.Fatalf("cleanupOldAssetCaches returned error: %v", err)
	}

	if _, err := os.Stat(oldCache); err != nil {
		t.Fatalf("dev cleanup removed release cache: %v", err)
	}
}

func TestHeaderModeUsesTarPermissions(t *testing.T) {
	got := headerMode(&tar.Header{Mode: 0o100755}, 0o600)
	if got != 0o755 {
		t.Fatalf("headerMode() = %v, want 0755", got)
	}
}

func TestHeaderModeFallsBackForEmptyOrInvalidMode(t *testing.T) {
	tests := []struct {
		name string
		mode int64
	}{
		{name: "empty", mode: 0},
		{name: "negative", mode: -1},
		{name: "overflow", mode: 1 << 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := headerMode(&tar.Header{Mode: tt.mode}, 0o600)
			if got != 0o600 {
				t.Fatalf("headerMode() = %v, want fallback 0600", got)
			}
		})
	}
}

type tarEntry struct {
	name string
	mode int64
	typ  byte
	body string
}

func brotliTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()

	var tarData bytes.Buffer
	tw := tar.NewWriter(&tarData)
	for _, entry := range entries {
		typ := entry.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Typeflag: typ,
			Size:     int64(len(entry.body)),
		}
		if typ == tar.TypeDir {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	var compressed bytes.Buffer
	bw := brotli.NewWriter(&compressed)
	if _, err := bw.Write(tarData.Bytes()); err != nil {
		t.Fatalf("write brotli data: %v", err)
	}
	if err := bw.Close(); err != nil {
		t.Fatalf("close brotli writer: %v", err)
	}
	return compressed.Bytes()
}
