package assets

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/andybalholm/brotli"
)

const (
	chromeUnixName    = "chrome-headless-shell"
	chromeWindowsName = "chrome-headless-shell.exe"
	ffmpegUnixName    = "ffmpeg"
	ffmpegWindowsName = "ffmpeg.exe"
)

func EnsureAssets(version string) (chromePath, ffmpegPath string, err error) {
	if version == "" {
		version = "dev"
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", "", fmt.Errorf("locate user cache directory: %w", err)
	}

	cacheDir := filepath.Join(cacheRoot, "cetus", version)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create asset cache directory: %w", err)
	}

	chromePath = filepath.Join(cacheDir, chromeExeName())
	ffmpegPath = filepath.Join(cacheDir, ffmpegExeName())

	if executableExists(chromePath) && executableExists(ffmpegPath) {
		return chromePath, ffmpegPath, nil
	}

	if err := decompressAsset(headlessShellData, chromePath); err != nil {
		return "", "", fmt.Errorf("extract chrome headless shell: %w", err)
	}
	if err := decompressAsset(ffmpegData, ffmpegPath); err != nil {
		return "", "", fmt.Errorf("extract ffmpeg: %w", err)
	}
	if err := os.Chmod(chromePath, 0o755); err != nil {
		return "", "", fmt.Errorf("mark chrome executable: %w", err)
	}
	if err := os.Chmod(ffmpegPath, 0o755); err != nil {
		return "", "", fmt.Errorf("mark ffmpeg executable: %w", err)
	}

	return chromePath, ffmpegPath, nil
}

func chromeExeName() string {
	if runtime.GOOS == "windows" {
		return chromeWindowsName
	}
	return chromeUnixName
}

func ffmpegExeName() string {
	if runtime.GOOS == "windows" {
		return ffmpegWindowsName
	}
	return ffmpegUnixName
}

func executableExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func decompressAsset(data []byte, destPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("embedded asset is empty; run scripts/prep-assets.sh before building a release")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("stub")) {
		return fmt.Errorf("embedded asset is a placeholder; run scripts/prep-assets.sh before building a render-capable binary")
	}

	tmpPath := destPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create temporary asset file: %w", err)
	}

	_, copyErr := io.Copy(out, brotli.NewReader(bytes.NewReader(data)))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("decompress brotli asset: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temporary asset file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move asset into cache: %w", err)
	}

	return nil
}
