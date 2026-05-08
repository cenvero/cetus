package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const DefaultManifestURL = "https://cetus.cenvero.org/manifest.json"

const (
	ChannelAuto   = "auto"
	ChannelStable = "stable"
	ChannelBeta   = "beta"
	ChannelRC     = "rc"
)

type Manifest struct {
	GeneratedAt string                      `json:"generated_at"`
	Channels    map[string]Channel          `json:"channels"`
	Binaries    map[string]PlatformBinaries `json:"binaries"`
}

type Channel struct {
	Version         string   `json:"version"`
	ReleaseDate     string   `json:"release_date"`
	MinSupported    string   `json:"min_supported"`
	ReleaseNotesURL string   `json:"release_notes_url"`
	History         []string `json:"history"`
}

type PlatformBinaries map[string]Binary

type Binary struct {
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	SignatureURL string `json:"signature_url"`
	Size         int64  `json:"size"`
}

type CheckResult struct {
	CurrentVersion    string
	LatestVersion     string
	Channel           string
	Platform          string
	ReleaseNotesURL   string
	Binary            Binary
	UpdateAvailable   bool
	CurrentComparable bool
	HomebrewManaged   bool
	HomebrewPath      string
}

type ApplyResult struct {
	Check         *CheckResult
	Applied       bool
	InstalledPath string
}

func Check(ctx context.Context, currentVersion, manifestURL, channel string) (*CheckResult, error) {
	if manifestURL == "" {
		manifestURL = DefaultManifestURL
	}
	resolvedChannel, err := ResolveChannel(currentVersion, channel)
	if err != nil {
		return nil, err
	}

	homebrew, homebrewPath := IsHomebrewManaged()
	result := &CheckResult{
		CurrentVersion:  currentVersion,
		Channel:         resolvedChannel,
		Platform:        platformKey(),
		HomebrewManaged: homebrew,
		HomebrewPath:    homebrewPath,
	}

	manifest, err := fetchManifest(ctx, manifestURL)
	if err != nil {
		return nil, err
	}

	channelInfo, binary, err := selectRelease(manifest, resolvedChannel, result.Platform)
	if err != nil {
		return nil, err
	}
	result.LatestVersion = channelInfo.Version
	result.ReleaseNotesURL = channelInfo.ReleaseNotesURL
	result.Binary = binary

	cmp, comparable := compareVersions(currentVersion, channelInfo.Version)
	result.CurrentComparable = comparable
	result.UpdateAvailable = !comparable || cmp < 0

	return result, nil
}

func Apply(ctx context.Context, currentVersion, manifestURL, channel string, force bool) (*ApplyResult, error) {
	check, err := Check(ctx, currentVersion, manifestURL, channel)
	if err != nil {
		return nil, err
	}
	if check.HomebrewManaged {
		return &ApplyResult{Check: check}, nil
	}
	if !check.UpdateAvailable && !force {
		return &ApplyResult{Check: check}, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate current executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "cetus-update-*")
	if err != nil {
		return nil, fmt.Errorf("create update temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "cetus-archive")
	if err := download(ctx, check.Binary.URL, archivePath); err != nil {
		return nil, err
	}
	if err := verifySHA256(archivePath, check.Binary.SHA256); err != nil {
		return nil, err
	}

	newBinary := filepath.Join(tmpDir, executableName())
	if err := extractBinary(archivePath, newBinary); err != nil {
		return nil, err
	}
	if err := os.Chmod(newBinary, 0o700); err != nil { // #nosec G302 -- downloaded update binary must be executable before replacement.
		return nil, fmt.Errorf("mark downloaded binary executable: %w", err)
	}

	backupPath := executable + ".old"
	_ = os.Remove(backupPath)
	if err := os.Rename(executable, backupPath); err != nil {
		return nil, fmt.Errorf("move current binary aside: %w", err)
	}
	if err := os.Rename(newBinary, executable); err != nil {
		_ = os.Rename(backupPath, executable)
		return nil, fmt.Errorf("install updated binary: %w", err)
	}
	_ = os.Remove(backupPath)

	return &ApplyResult{Check: check, Applied: true, InstalledPath: executable}, nil
}

func ResolveChannel(currentVersion, requested string) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" || requested == ChannelAuto {
		return ChannelForVersion(currentVersion), nil
	}
	switch requested {
	case ChannelStable, ChannelBeta, ChannelRC:
		return requested, nil
	default:
		return "", fmt.Errorf("unsupported update channel %q; use stable, beta, rc, or auto", requested)
	}
}

func ChannelForVersion(version string) string {
	parsed, ok := parseSemVersion(version)
	if !ok {
		return ChannelStable
	}
	switch parsed.preName {
	case ChannelBeta:
		return ChannelBeta
	case ChannelRC:
		return ChannelRC
	default:
		return ChannelStable
	}
}

func selectRelease(manifest *Manifest, channel, platform string) (Channel, Binary, error) {
	channelInfo, ok := manifest.Channels[channel]
	if !ok || channelInfo.Version == "" {
		return Channel{}, Binary{}, fmt.Errorf("no %s Cetus release is published yet", channel)
	}

	versionBinaries, ok := manifest.Binaries[channelInfo.Version]
	if !ok {
		return Channel{}, Binary{}, fmt.Errorf("manifest has no binaries for %s", channelInfo.Version)
	}
	binary, ok := versionBinaries[platform]
	if !ok {
		return Channel{}, Binary{}, fmt.Errorf("manifest has no binary for %s on %s", channelInfo.Version, platform)
	}
	return channelInfo, binary, nil
}

func IsHomebrewManaged() (bool, string) {
	executable, err := os.Executable()
	if err != nil {
		return false, ""
	}
	paths := []string{executable}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		paths = append(paths, resolved)
	}

	for _, path := range paths {
		clean := filepath.ToSlash(path)
		if strings.Contains(clean, "/Cellar/cenvero-cetus/") || strings.Contains(clean, "/Cellar/cetus/") {
			return true, path
		}
		if strings.Contains(clean, "/Homebrew/Cellar/") && strings.HasSuffix(clean, "/bin/cetus") {
			return true, path
		}
	}

	return false, ""
}

func fetchManifest(ctx context.Context, manifestURL string) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download manifest: unexpected status %s", resp.Status)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

func download(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download release archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download release archive: unexpected status %s", resp.Status)
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- destPath is created inside a private updater temp directory.
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write archive file: %w", err)
	}
	return nil
}

func verifySHA256(path, expected string) error {
	if expected == "" {
		return fmt.Errorf("manifest binary is missing sha256")
	}

	file, err := os.Open(path) // #nosec G304 -- path is the downloaded archive path inside the updater temp directory.
	if err != nil {
		return fmt.Errorf("open archive for checksum: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}

	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func extractBinary(archivePath, destPath string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZipBinary(archivePath, destPath)
	}
	if err := extractTarGzBinary(archivePath, destPath); err == nil {
		return nil
	}
	return extractZipBinary(archivePath, destPath)
}

func extractTarGzBinary(archivePath, destPath string) error {
	file, err := os.Open(archivePath) // #nosec G304 -- archivePath is the downloaded archive path inside the updater temp directory.
	if err != nil {
		return fmt.Errorf("open tar archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != executableName() {
			continue
		}
		return writeExtractedFile(reader, destPath)
	}

	return fmt.Errorf("%s not found in tar archive", executableName())
}

func extractZipBinary(archivePath, destPath string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || filepath.Base(file.Name) != executableName() {
			continue
		}
		in, err := file.Open()
		if err != nil {
			return fmt.Errorf("open binary in zip archive: %w", err)
		}
		err = writeExtractedFile(in, destPath)
		closeErr := in.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("close binary in zip archive: %w", closeErr)
		}
		return nil
	}

	return fmt.Errorf("%s not found in zip archive", executableName())
}

func writeExtractedFile(reader io.Reader, destPath string) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- destPath is the extracted binary path inside the updater temp directory.
	if err != nil {
		return fmt.Errorf("create extracted binary: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, reader); err != nil {
		return fmt.Errorf("write extracted binary: %w", err)
	}
	return nil
}

func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "cetus.exe"
	}
	return "cetus"
}

func compareVersions(current, latest string) (int, bool) {
	currentVersion, ok := parseSemVersion(current)
	if !ok {
		return 0, false
	}
	latestVersion, ok := parseSemVersion(latest)
	if !ok {
		return 0, false
	}

	return currentVersion.compare(latestVersion), true
}

func parseVersion(version string) ([3]int, bool) {
	parsed, ok := parseSemVersion(version)
	if !ok {
		return [3]int{}, false
	}
	return parsed.core, true
}

type semVersion struct {
	core    [3]int
	pre     string
	preName string
	preNum  int
}

func parseSemVersion(version string) (semVersion, bool) {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	version = strings.Split(version, "+")[0]
	versionParts := strings.SplitN(version, "-", 2)
	core := versionParts[0]
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semVersion{}, false
	}

	var parsed [3]int
	for i, part := range parts {
		if part == "" {
			return semVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semVersion{}, false
		}
		parsed[i] = n
	}

	result := semVersion{core: parsed}
	if len(versionParts) == 1 {
		return result, true
	}

	result.pre = versionParts[1]
	preParts := strings.Split(result.pre, ".")
	result.preName = strings.ToLower(preParts[0])
	if result.preName != ChannelBeta && result.preName != ChannelRC {
		return semVersion{}, false
	}
	if len(preParts) > 1 {
		n, err := strconv.Atoi(preParts[1])
		if err != nil || n < 0 {
			return semVersion{}, false
		}
		result.preNum = n
	}
	return result, true
}

func (v semVersion) compare(other semVersion) int {
	for i := 0; i < 3; i++ {
		if v.core[i] < other.core[i] {
			return -1
		}
		if v.core[i] > other.core[i] {
			return 1
		}
	}
	if v.pre == "" && other.pre == "" {
		return 0
	}
	if v.pre == "" {
		return 1
	}
	if other.pre == "" {
		return -1
	}
	if rankPrerelease(v.preName) < rankPrerelease(other.preName) {
		return -1
	}
	if rankPrerelease(v.preName) > rankPrerelease(other.preName) {
		return 1
	}
	if v.preNum < other.preNum {
		return -1
	}
	if v.preNum > other.preNum {
		return 1
	}
	return 0
}

func rankPrerelease(name string) int {
	switch name {
	case ChannelBeta:
		return 1
	case ChannelRC:
		return 2
	default:
		return 0
	}
}
