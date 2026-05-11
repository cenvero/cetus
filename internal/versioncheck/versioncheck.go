package versioncheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "cetus", "update_check.json")
}

func readCache() (cacheEntry, bool) {
	path := cacheFile()
	if path == "" {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(path)
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

func isNewer(latest, current string) bool {
	return latest != "" && latest != current
}
