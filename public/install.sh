#!/bin/sh
set -eu

BASE_URL="${CETUS_BASE_URL:-https://cetus.cenvero.org}"
MANIFEST_URL="${BASE_URL}/manifest.json"
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

version="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\(v[^"]*\)".*/\1/p' "$manifest" | head -n 1)"
if [ -z "$version" ]; then
  echo "no stable Cetus release is published yet" >&2
  exit 1
fi

url="$(awk -v platform="\"$platform\"" '
  $0 ~ platform { in_platform=1 }
  in_platform && /"url"/ {
    gsub(/[",]/, "", $2)
    print $2
    exit
  }
' "$manifest")"

sha="$(awk -v platform="\"$platform\"" '
  $0 ~ platform { in_platform=1 }
  in_platform && /"sha256"/ {
    gsub(/[",]/, "", $2)
    print $2
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

echo "installed cetus $version to $INSTALL_DIR/cetus"
