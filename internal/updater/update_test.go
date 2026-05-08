package updater

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    int
		ok      bool
	}{
		{current: "v1.0.0", latest: "v1.0.1", want: -1, ok: true},
		{current: "1.2.0", latest: "v1.1.9", want: 1, ok: true},
		{current: "v1.2.3", latest: "1.2.3", want: 0, ok: true},
		{current: "v1.2.3-beta.1", latest: "v1.2.3-beta.2", want: -1, ok: true},
		{current: "v1.2.3-beta.2", latest: "v1.2.3-rc.1", want: -1, ok: true},
		{current: "v1.2.3-rc.1", latest: "v1.2.3", want: -1, ok: true},
		{current: "v1.2.3", latest: "v1.2.3-rc.1", want: 1, ok: true},
		{current: "dev", latest: "v1.0.0", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_"+tt.latest, func(t *testing.T) {
			got, ok := compareVersions(tt.current, tt.latest)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("compareVersions() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	got, ok := parseVersion("v1.2.3-beta.1")
	if !ok {
		t.Fatal("parseVersion returned !ok")
	}
	if got != [3]int{1, 2, 3} {
		t.Fatalf("parseVersion = %#v, want [1 2 3]", got)
	}
}

func TestChannelForVersion(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{version: "v1.2.3", want: ChannelStable},
		{version: "v1.2.3-beta.1", want: ChannelBeta},
		{version: "v1.2.3-rc.1", want: ChannelRC},
		{version: "dev", want: ChannelStable},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := ChannelForVersion(tt.version); got != tt.want {
				t.Fatalf("ChannelForVersion(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestResolveChannel(t *testing.T) {
	got, err := ResolveChannel("v1.2.3-beta.1", ChannelAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got != ChannelBeta {
		t.Fatalf("ResolveChannel() = %q, want %q", got, ChannelBeta)
	}

	got, err = ResolveChannel("v1.2.3-beta.1", ChannelStable)
	if err != nil {
		t.Fatal(err)
	}
	if got != ChannelStable {
		t.Fatalf("ResolveChannel() explicit = %q, want %q", got, ChannelStable)
	}

	if _, err := ResolveChannel("v1.2.3", "nightly"); err == nil {
		t.Fatal("ResolveChannel() accepted invalid channel")
	}
}

func TestSelectReleaseUsesRequestedChannel(t *testing.T) {
	platform := platformKey()
	manifest := fmt.Sprintf(`{
  "generated_at": "2026-05-08T00:00:00Z",
  "channels": {
    "stable": {
      "version": "v1.0.0",
      "release_date": "2026-05-08T00:00:00Z",
      "min_supported": "v1.0.0",
      "release_notes_url": "https://github.com/cenvero/cetus/releases/tag/v1.0.0",
      "history": ["v1.0.0"]
    },
    "beta": {
      "version": "v1.1.0-beta.2",
      "release_date": "2026-05-08T00:00:00Z",
      "min_supported": "v1.1.0-beta.1",
      "release_notes_url": "https://github.com/cenvero/cetus/releases/tag/v1.1.0-beta.2",
      "history": ["v1.1.0-beta.2"]
    }
  },
  "binaries": {
    "v1.0.0": {
      %[1]q: {
        "url": "https://example.com/stable.tar.gz",
        "sha256": "stable-sha",
        "signature_url": "https://example.com/stable.tar.gz.minisig",
        "size": 1
      }
    },
    "v1.1.0-beta.2": {
      %[1]q: {
        "url": "https://example.com/beta.tar.gz",
        "sha256": "beta-sha",
        "signature_url": "https://example.com/beta.tar.gz.minisig",
        "size": 1
      }
    }
  }
}`, platform)

	parsed, err := decodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	channel, binary, err := selectRelease(parsed, ChannelBeta, platform)
	if err != nil {
		t.Fatal(err)
	}
	if channel.Version != "v1.1.0-beta.2" {
		t.Fatalf("selectRelease beta version = %q, want beta latest", channel.Version)
	}
	if binary.SHA256 != "beta-sha" {
		t.Fatalf("selectRelease beta sha = %q, want beta-sha", binary.SHA256)
	}

	channel, binary, err = selectRelease(parsed, ChannelStable, platform)
	if err != nil {
		t.Fatal(err)
	}
	if channel.Version != "v1.0.0" {
		t.Fatalf("selectRelease stable version = %q, want stable latest", channel.Version)
	}
	if binary.SHA256 != "stable-sha" {
		t.Fatalf("selectRelease stable sha = %q, want stable-sha", binary.SHA256)
	}
}

func decodeManifest(data string) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal([]byte(data), &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
