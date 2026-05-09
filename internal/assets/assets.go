package assets

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andybalholm/brotli"
)

const (
	chromeUnixName    = "chrome-headless-shell"
	chromeWindowsName = "chrome-headless-shell.exe"
	ffmpegUnixName    = "ffmpeg"
	ffmpegWindowsName = "ffmpeg.exe"
)

type ProgressEvent struct {
	Message string
}

type ProgressFunc func(ProgressEvent)

func EnsureAssets(version string) (chromePath, ffmpegPath string, err error) {
	return EnsureAssetsWithProgress(version, nil)
}

func EnsureAssetsWithProgress(version string, progress ProgressFunc) (chromePath, ffmpegPath string, err error) {
	if version == "" {
		version = "dev"
	}

	reportProgress(progress, "Checking renderer assets")

	cacheDir, err := assetCacheDir(version)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create asset cache directory: %w", err)
	}

	chromeDir := filepath.Join(cacheDir, "chrome")
	ffmpegPath = filepath.Join(cacheDir, ffmpegExeName())
	chromePath, chromeReady := chromeBundleExecutable(chromeDir)

	if chromeReady && executableExists(ffmpegPath) {
		_ = cleanupOldAssetCaches(version)
		reportProgress(progress, "Renderer assets ready")
		return chromePath, ffmpegPath, nil
	}

	if !chromeReady {
		reportProgress(progress, "Extracting embedded browser bundle")
		if err := os.RemoveAll(chromeDir); err != nil {
			return "", "", fmt.Errorf("clear chrome bundle cache: %w", err)
		}
		if err := os.MkdirAll(chromeDir, 0o700); err != nil {
			return "", "", fmt.Errorf("create chrome bundle cache: %w", err)
		}
		if err := extractBrotliTar(headlessShellData, chromeDir); err != nil {
			return "", "", fmt.Errorf("extract chrome headless shell bundle: %w", err)
		}
		chromePath, chromeReady = chromeBundleExecutable(chromeDir)
		if !chromeReady {
			return "", "", fmt.Errorf("chrome headless shell bundle is missing %s or icudtl.dat", chromeExeName())
		}
	}
	if !executableExists(ffmpegPath) {
		reportProgress(progress, "Extracting embedded ffmpeg")
		if err := decompressAsset(ffmpegData, ffmpegPath); err != nil {
			return "", "", fmt.Errorf("extract ffmpeg: %w", err)
		}
	}
	if err := os.Chmod(chromePath, 0o700); err != nil { // #nosec G302 -- cached browser binary must be executable by the current user.
		return "", "", fmt.Errorf("mark chrome executable: %w", err)
	}
	if err := os.Chmod(ffmpegPath, 0o700); err != nil { // #nosec G302 -- cached ffmpeg binary must be executable by the current user.
		return "", "", fmt.Errorf("mark ffmpeg executable: %w", err)
	}

	_ = cleanupOldAssetCaches(version)
	reportProgress(progress, "Renderer assets ready")
	return chromePath, ffmpegPath, nil
}

func reportProgress(progress ProgressFunc, message string) {
	if progress != nil {
		progress(ProgressEvent{Message: message})
	}
}

func chromeBundleExecutable(root string) (string, bool) {
	chromePath, chromeOK := findInTree(root, chromeExeName())
	if !chromeOK {
		return "", false
	}
	if _, icuOK := findInTree(root, "icudtl.dat"); !icuOK {
		return "", false
	}
	return chromePath, true
}

func findInTree(root, name string) (string, bool) {
	var found string
	if root == "" || name == "" {
		return "", false
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == name {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found, err == nil && found != ""
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

func extractBrotliTar(data []byte, destDir string) error {
	if len(data) == 0 {
		return fmt.Errorf("embedded asset is empty; run scripts/prep-assets.sh before building a release")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("stub")) {
		return fmt.Errorf("embedded asset is a placeholder; run scripts/prep-assets.sh before building a render-capable binary")
	}

	reader := tar.NewReader(brotli.NewReader(bytes.NewReader(data)))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		targetPath, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}
		if targetPath == "" {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, headerMode(header, 0o700)); err != nil {
				return fmt.Errorf("create chrome bundle directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return fmt.Errorf("create chrome bundle parent directory: %w", err)
			}
			if err := writeTarFile(targetPath, reader, headerMode(header, 0o600)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return fmt.Errorf("create chrome bundle symlink parent directory: %w", err)
			}
			if err := createSafeSymlink(destDir, header.Linkname, targetPath); err != nil {
				return err
			}
		}
	}
}

func assetCacheDir(version string) (string, error) {
	root, err := assetCacheRoot()
	if err != nil {
		return "", err
	}
	cleanVersion, err := safeAssetCacheVersion(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, cleanVersion), nil
}

func assetCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("locate user home directory: empty path")
	}
	return filepath.Join(home, ".cenvero-cetus"), nil
}

func safeAssetCacheVersion(version string) (string, error) {
	cleanVersion := filepath.Clean(version)
	if cleanVersion == "." || filepath.IsAbs(cleanVersion) || cleanVersion == ".." || strings.HasPrefix(cleanVersion, ".."+string(os.PathSeparator)) || strings.Contains(cleanVersion, string(os.PathSeparator)) {
		return "", fmt.Errorf("version %q is not a safe asset cache path segment", version)
	}
	return cleanVersion, nil
}

func cleanupOldAssetCaches(currentVersion string) error {
	cleanCurrent, err := safeAssetCacheVersion(currentVersion)
	if err != nil {
		return err
	}
	if cleanCurrent == "dev" {
		return nil
	}

	root, err := assetCacheRoot()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read asset cache directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == cleanCurrent || name == "dev" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("remove old asset cache %q: %w", name, err)
		}
	}
	return nil
}

func safeExtractPath(destDir, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." {
		return "", nil
	}
	if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe chrome bundle path %q", name)
	}
	targetPath := filepath.Join(destDir, cleanName)
	rel, err := filepath.Rel(destDir, targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve chrome bundle path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe chrome bundle path %q", name)
	}
	return targetPath, nil
}

func headerMode(header *tar.Header, fallback fs.FileMode) fs.FileMode {
	if header.Mode < 0 || header.Mode > 1<<32-1 {
		return fallback
	}
	mode := fs.FileMode(header.Mode).Perm()
	if mode == 0 {
		return fallback
	}
	return mode
}

func writeTarFile(path string, reader io.Reader, mode fs.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // #nosec G304 -- path is validated under the user-owned asset directory.
	if err != nil {
		return fmt.Errorf("create chrome bundle file: %w", err)
	}
	_, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("write chrome bundle file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close chrome bundle file: %w", closeErr)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set chrome bundle file mode: %w", err)
	}
	return nil
}

func createSafeSymlink(destDir, linkName, targetPath string) error {
	if strings.TrimSpace(linkName) == "" {
		return fmt.Errorf("unsafe chrome bundle symlink %q", linkName)
	}
	cleanLink := filepath.Clean(filepath.FromSlash(linkName))
	if filepath.IsAbs(cleanLink) {
		return fmt.Errorf("unsafe chrome bundle symlink %q", linkName)
	}
	resolvedTarget := filepath.Clean(filepath.Join(filepath.Dir(targetPath), cleanLink))
	rel, err := filepath.Rel(destDir, resolvedTarget)
	if err != nil {
		return fmt.Errorf("resolve chrome bundle symlink: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("unsafe chrome bundle symlink %q", linkName)
	}
	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("replace chrome bundle symlink: %w", err)
	}
	if err := os.Symlink(cleanLink, targetPath); err != nil {
		return fmt.Errorf("create chrome bundle symlink: %w", err)
	}
	return nil
}

func decompressAsset(data []byte, destPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("embedded asset is empty; run scripts/prep-assets.sh before building a release")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("stub")) {
		return fmt.Errorf("embedded asset is a placeholder; run scripts/prep-assets.sh before building a render-capable binary")
	}

	tmpPath := destPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- destPath is built from the user-owned asset directory and fixed asset names.
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
