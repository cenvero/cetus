#!/bin/sh
set -eu

BASE_URL="${CETUS_BASE_URL:-https://cetus.cenvero.org}"
MANIFEST_URL="${BASE_URL}/manifest.json"
CHANNEL="${CETUS_CHANNEL:-stable}"
INSTALL_DIR="${CETUS_INSTALL_DIR:-$HOME/.local/bin}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

need curl
need tar

case "$CHANNEL" in
  stable|beta|rc) ;;
  *) echo "unsupported channel: $CHANNEL" >&2; exit 1 ;;
esac

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux) goos="linux" ;;
  darwin) goos="darwin" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

platform="${goos}-${goarch}"
manifest="$TMP_DIR/manifest.json"
curl -fsSL "$MANIFEST_URL" -o "$manifest"

version="$(awk -v channel="\"$CHANNEL\"" '
  $0 ~ channel "[[:space:]]*:" { in_channel=1 }
  in_channel && /"version"[[:space:]]*:/ {
    line=$0
    sub(/^.*"version"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"
if [ -z "$version" ]; then
  echo "no $CHANNEL Cetus release is published yet" >&2
  exit 1
fi

url="$(awk -v version="\"$version\"" -v platform="\"$platform\"" '
  /"binaries"[[:space:]]*:/ { in_binaries=1 }
  in_binaries && $0 ~ version "[[:space:]]*:" { in_version=1 }
  in_version && $0 ~ platform "[[:space:]]*:" { in_platform=1 }
  in_platform && /"url"/ {
    line=$0
    sub(/^.*"url"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"

sha="$(awk -v version="\"$version\"" -v platform="\"$platform\"" '
  /"binaries"[[:space:]]*:/ { in_binaries=1 }
  in_binaries && $0 ~ version "[[:space:]]*:" { in_version=1 }
  in_version && $0 ~ platform "[[:space:]]*:" { in_platform=1 }
  in_platform && /"sha256"/ {
    line=$0
    sub(/^.*"sha256"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"

if [ -z "$url" ] || [ -z "$sha" ]; then
  echo "manifest does not contain a binary for $platform" >&2
  exit 1
fi

archive="$TMP_DIR/cetus.tar.gz"
curl -fsSL "$url" -o "$archive"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
fi

if [ "$actual" != "$sha" ]; then
  echo "checksum mismatch for $url" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
tar -xzf "$archive" -C "$TMP_DIR"
install -m 0755 "$TMP_DIR/cetus" "$INSTALL_DIR/cetus"

echo "installed cetus $version ($CHANNEL) to $INSTALL_DIR/cetus"
