package versioncheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cenvero/cetus/internal/updater"
)

const cacheTTL = 10 * time.Minute

type cacheEntry struct {
	LatestVersion string    `json:"latest_version"`
	Channel       string    `json:"channel"`
	CheckedAt     time.Time `json:"checked_at"`
}

// BackgroundCheck starts an async version check and returns a channel that
// receives the latest version string if one newer than currentVersion is
// available. The channel receives "" if no update is found or on any error.
// The check uses a 10-minute on-disk cache so it rarely hits the network.
func BackgroundCheck(currentVersion string) <-chan string {
	ch := make(chan string, 1)
	go func() {
		latest, newer := check(currentVersion)
		if newer {
			ch <- latest
		} else {
			ch <- ""
		}
	}()
	return ch
}

func check(currentVersion string) (latestVersion string, newer bool) {
	if entry, ok := readCache(); ok {
		return entry.LatestVersion, isNewer(entry.LatestVersion, currentVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	channel, _ := updater.ResolveChannel(currentVersion, updater.ChannelAuto)
	result, err := updater.Check(ctx, currentVersion, updater.DefaultManifestURL, channel)
	if err != nil {
		return "", false
	}

	writeCache(cacheEntry{
		LatestVersion: result.LatestVersion,
		Channel:       result.Channel,
		CheckedAt:     time.Now(),
	})

	return result.LatestVersion, result.UpdateAvailable && result.CurrentComparable
}

func cacheFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cenvero-cetus", "update_check.json")
}

func readCache() (cacheEntry, bool) {
	path := cacheFile()
	if path == "" {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from os.UserHomeDir(), a trusted user-controlled location
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheEntry{}, false
	}
	if time.Since(entry.CheckedAt) > cacheTTL {
		return cacheEntry{}, false
	}
	return entry, true
}

func writeCache(entry cacheEntry) {
	path := cacheFile()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// isNewer returns true only if latest is strictly greater than current.
// Both versions may have an optional "v" prefix (e.g. "v0.9.0" or "0.9.0").
// Pre-release suffixes (e.g. "-beta") are stripped before comparison so a
// stable version is never incorrectly promoted over a pre-release binary.
func isNewer(latest, current string) bool {
	if latest == "" {
		return false
	}
	return semverGT(latest, current)
}

// semverGT returns true if a > b, comparing major.minor.patch numerically.
func semverGT(a, b string) bool {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := range ap {
		if ap[i] > bp[i] {
			return true
		}
		if ap[i] < bp[i] {
			return false
		}
	}
	return false
}

// parseSemver strips the leading "v" and any pre-release suffix, then returns
// [major, minor, patch] as integers. Malformed segments default to 0.
func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	// strip pre-release suffix (e.g. "-beta", "-rc.1")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}
